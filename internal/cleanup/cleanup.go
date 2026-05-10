// Package cleanup implements disk hygiene for the civm runner host.
// All actions are dry-run by default; --execute flag flips to mutating.
package cleanup

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emersonbusson/civm/internal/civm"
)

type deleteCandidate struct {
	path string
	size int64
}

// Activity is evidence that a CI job or build is currently active on the host.
type Activity struct {
	PID     int
	Command string
}

var dangerousAbsoluteRoots = map[string]struct{}{
	"/":     {},
	"/home": {},
	"/root": {},
}

var allowedTopLevelCleanupRoots = map[string]struct{}{
	civm.DefaultTmpDir: {},
}

// Action is one cleanup step result.
type Action struct {
	Name       string
	Path       string
	BytesFound int64
	BytesFreed int64
	Executed   bool
	Err        error
}

// Options control which steps run.
type Options struct {
	Execute        bool
	WorkDir        string
	TmpDir         string
	TmpThreshold   time.Duration
	WorkThreshold  time.Duration
	DockerPrune    bool
	AptClean       bool
	SkipIdleGuard  bool
	IdleProbeDelay time.Duration
	Now            time.Time

	WalkFn     func(root string, fn fs.WalkDirFunc) error
	StatFn     func(path string) (fs.FileInfo, error)
	RunFn      func(ctx context.Context, name string, args ...string) ([]byte, error)
	ActivityFn func(ctx context.Context) ([]Activity, error)
}

// DefaultOptions returns sane defaults: dry-run, 7d /tmp, 14d _work.
func DefaultOptions() Options {
	return Options{
		Execute:        false,
		WorkDir:        civm.DefaultWorkDir,
		TmpDir:         civm.DefaultTmpDir,
		TmpThreshold:   7 * 24 * time.Hour,
		WorkThreshold:  14 * 24 * time.Hour,
		DockerPrune:    true,
		AptClean:       true,
		IdleProbeDelay: 2 * time.Second,
		Now:            time.Now(),
		WalkFn:         filepath.WalkDir,
		StatFn:         defaultStat,
		RunFn:          defaultRun,
		ActivityFn:     defaultActivities,
	}
}

// Run executes every enabled step and returns one Action per step.
// Errors are captured per-Action; the function itself returns nil.
func Run(ctx context.Context, opts Options) []Action {
	applyDefaults(&opts)
	if opts.Execute && !opts.SkipIdleGuard {
		if err := ensureIdle(ctx, opts); err != nil {
			return []Action{{
				Name: "host_idle",
				Path: "(runner/build activity)",
				Err:  err,
			}}
		}
	}
	var out []Action
	out = append(out, scanAndMaybeDelete(ctx, opts, "tmp_old", opts.TmpDir, opts.TmpThreshold))
	out = append(out, scanAndMaybeDelete(ctx, opts, "work_old", opts.WorkDir, opts.WorkThreshold))
	if opts.DockerPrune {
		out = append(out, dockerPrune(ctx, opts))
	}
	if opts.AptClean {
		out = append(out, aptClean(ctx, opts))
	}
	return out
}

func applyDefaults(opts *Options) {
	if opts.WalkFn == nil {
		opts.WalkFn = filepath.WalkDir
	}
	if opts.StatFn == nil {
		opts.StatFn = defaultStat
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.ActivityFn == nil {
		opts.ActivityFn = defaultActivities
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
}

func scanAndMaybeDelete(ctx context.Context, opts Options, name, root string, threshold time.Duration) Action {
	a := Action{Name: name, Path: root}
	cleanRoot, err := validateCleanupRoot(root)
	if err != nil {
		a.Err = err
		return a
	}
	root = cleanRoot
	a.Path = cleanRoot
	cutoff := opts.Now.Add(-threshold)
	var toDelete []deleteCandidate
	err = opts.WalkFn(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		// Anti-jobs-em-curso: skip arquivos com mtime <2h.
		if opts.Now.Sub(info.ModTime()) < 2*time.Hour {
			return nil
		}
		size := dirSize(opts, path, info)
		a.BytesFound += size
		toDelete = append(toDelete, deleteCandidate{path: path, size: size})
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		a.Err = err
		return a
	}
	if !opts.Execute {
		return a
	}
	if err := ensureIdle(ctx, opts); err != nil {
		a.Err = err
		return a
	}
	for _, candidate := range toDelete {
		if _, err := opts.RunFn(ctx, "rm", "-rf", candidate.path); err != nil {
			a.Err = err
			break
		}
		a.BytesFreed += candidate.size
	}
	a.Executed = true
	return a
}

func validateCleanupRoot(root string) (string, error) {
	clean, err := civm.CleanDir(root, "cleanup root")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(clean) {
		return clean, nil
	}
	if _, ok := allowedTopLevelCleanupRoots[clean]; ok {
		return clean, nil
	}
	if _, ok := dangerousAbsoluteRoots[clean]; ok {
		return "", fmt.Errorf("cleanup root perigoso: %s", clean)
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return "", fmt.Errorf("cleanup root nao pode ser home inteiro: %s", clean)
	}
	if strings.HasPrefix(clean, "/home/") && strings.Count(strings.Trim(clean, "/"), "/") == 1 {
		return "", fmt.Errorf("cleanup root nao pode ser home inteiro: %s", clean)
	}
	return clean, nil
}

func dirSize(opts Options, root string, info fs.FileInfo) int64 {
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = opts.WalkFn(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}

func dockerPrune(ctx context.Context, opts Options) Action {
	a := Action{Name: "docker_prune", Path: "(docker daemon)"}
	if !opts.Execute {
		// Best-effort estimate without execute: parse `docker system df`.
		out, err := opts.RunFn(ctx, "docker", "system", "df", "--format", "{{.Reclaimable}}")
		if err == nil {
			a.BytesFound = parseReclaimable(string(out))
		}
		return a
	}
	if err := ensureIdle(ctx, opts); err != nil {
		a.Err = err
		return a
	}
	out, err := opts.RunFn(ctx, "docker", "system", "prune", "-af", "--volumes")
	if err != nil {
		a.Err = err
		return a
	}
	a.BytesFreed = parseTotalReclaimed(string(out))
	a.Executed = true
	return a
}

func aptClean(ctx context.Context, opts Options) Action {
	a := Action{Name: "apt_cache", Path: "/var/cache/apt"}
	if !opts.Execute {
		return a
	}
	if err := ensureIdle(ctx, opts); err != nil {
		a.Err = err
		return a
	}
	if _, err := opts.RunFn(ctx, "apt-get", "clean"); err != nil {
		a.Err = err
		return a
	}
	if _, err := opts.RunFn(ctx, "apt-get", "autoremove", "-y"); err != nil {
		a.Err = err
		return a
	}
	a.Executed = true
	return a
}

func ensureIdle(ctx context.Context, opts Options) error {
	if opts.SkipIdleGuard || !opts.Execute {
		return nil
	}
	if err := assertIdle(ctx, opts.ActivityFn); err != nil {
		return err
	}
	if opts.IdleProbeDelay <= 0 {
		return nil
	}
	timer := time.NewTimer(opts.IdleProbeDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return assertIdle(ctx, opts.ActivityFn)
}

func assertIdle(ctx context.Context, activityFn func(context.Context) ([]Activity, error)) error {
	activities, err := activityFn(ctx)
	if err != nil {
		return fmt.Errorf("cleanup abortado: nao foi possivel provar host ocioso: %w", err)
	}
	if len(activities) == 0 {
		return nil
	}
	return fmt.Errorf("cleanup abortado: host nao esta ocioso (%s)", formatActivities(activities))
}

func formatActivities(activities []Activity) string {
	limit := len(activities)
	if limit > 3 {
		limit = 3
	}
	parts := make([]string, 0, limit+1)
	for _, a := range activities[:limit] {
		cmd := a.Command
		if len(cmd) > 90 {
			cmd = cmd[:89] + "..."
		}
		parts = append(parts, fmt.Sprintf("pid=%d %s", a.PID, cmd))
	}
	if len(activities) > limit {
		parts = append(parts, fmt.Sprintf("+%d outro(s)", len(activities)-limit))
	}
	return strings.Join(parts, "; ")
}

func defaultActivities(ctx context.Context) ([]Activity, error) {
	out, err := exec.CommandContext(ctx, "ps", "-eo", "pid=,ppid=,comm=,args=").Output()
	if err != nil {
		return nil, err
	}
	return parseActiveProcesses(string(out), os.Getpid()), nil
}

func parseActiveProcesses(psOutput string, currentPID int) []Activity {
	var activities []Activity
	for _, line := range strings.Split(psOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == currentPID {
			continue
		}
		comm := fields[2]
		args := strings.Join(fields[3:], " ")
		if isActiveBuildProcess(comm, args) {
			activities = append(activities, Activity{PID: pid, Command: args})
		}
	}
	return activities
}

func isActiveBuildProcess(comm, args string) bool {
	if strings.Contains(args, "civmctl cleanup") || strings.Contains(args, "civmctl disk-watchdog") {
		return false
	}
	switch {
	case strings.Contains(comm, "Runner.Worker"), strings.Contains(args, "Runner.Worker"):
		return true
	case strings.Contains(args, "Runner.PluginHost"):
		return true
	case strings.Contains(args, "/_work/"):
		return true
	case strings.Contains(args, "docker build"), strings.Contains(args, "docker compose"):
		return true
	case strings.Contains(args, "docker-compose"), strings.Contains(args, "buildx build"):
		return true
	case strings.Contains(args, "buildctl "):
		return true
	}
	return false
}

// parseReclaimable parses output of `docker system df --format {{.Reclaimable}}`.
// Each line looks like "1.234GB (100%)". We sum bytes.
func parseReclaimable(s string) int64 {
	var total int64
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		total += parseHumanBytes(fields[0])
	}
	return total
}

// parseTotalReclaimed parses "Total reclaimed space: 1.234GB" line.
func parseTotalReclaimed(s string) int64 {
	for _, line := range strings.Split(s, "\n") {
		if !strings.Contains(line, "reclaimed space:") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		return parseHumanBytes(strings.TrimSpace(line[idx+1:]))
	}
	return 0
}

// parseHumanBytes accepts "1.5GB", "200MB", "10kB", "100B".
func parseHumanBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		mult = 1 << 30
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult = 1 << 20
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "kB"), strings.HasSuffix(s, "KB"):
		mult = 1 << 10
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return 0
	}
	return int64(f * float64(mult))
}

// FormatBytes returns a human-friendly size.
func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(b)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

// RenderTable writes actions as a fixed-width table (PT-BR).
func RenderTable(actions []Action, opts Options, w io.Writer) {
	mode := "DRY-RUN"
	if opts.Execute {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "Modo: %s\n", mode)
	fmt.Fprintf(w, "%-14s %-30s %-12s %-12s %s\n", "ACAO", "PATH", "ENCONTRADO", "LIBERADO", "STATUS")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	var totalFound, totalFreed int64
	for _, a := range actions {
		status := "ok"
		if a.Err != nil {
			status = "erro: " + a.Err.Error()
		} else if !a.Executed && opts.Execute {
			status = "skip"
		} else if !opts.Execute {
			status = "(dry-run)"
		}
		fmt.Fprintf(w, "%-14s %-30s %-12s %-12s %s\n", a.Name, truncatePath(a.Path, 30), FormatBytes(a.BytesFound), FormatBytes(a.BytesFreed), status)
		totalFound += a.BytesFound
		totalFreed += a.BytesFreed
	}
	fmt.Fprintln(w, strings.Repeat("-", 80))
	fmt.Fprintf(w, "TOTAL          %-30s %-12s %s\n", "", FormatBytes(totalFound), FormatBytes(totalFreed))
	if !opts.Execute {
		fmt.Fprintln(w, "Para aplicar: rode novamente com --execute")
	}
}

func truncatePath(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}

// ---- defaults ----

func defaultStat(path string) (fs.FileInfo, error) {
	return defaultStatImpl(path)
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
