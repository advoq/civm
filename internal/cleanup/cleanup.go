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
	"sort"
	"strings"
	"time"

	"github.com/advoq/civm/internal/civm"
	"github.com/advoq/civm/internal/idle"
)

type deleteCandidate struct {
	path string
	size int64
}

// Activity is evidence that a CI job or build is currently active on the host.
type Activity = idle.Activity

var dangerousAbsoluteRoots = map[string]struct{}{
	"/":     {},
	"/home": {},
	"/root": {},
}

var allowedTopLevelCleanupRoots = map[string]struct{}{
	civm.DefaultTmpDir: {},
}

var protectedWorkCacheDirs = map[string]struct{}{
	"_actions": {},
	"_tool":    {},
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
	GlobFn     func(pattern string) ([]string, error)
	RunFn      func(ctx context.Context, name string, args ...string) ([]byte, error)
	ActivityFn func(ctx context.Context) ([]Activity, error)
}

// DefaultOptions returns sane defaults: dry-run, 1d /tmp, 3d _work.
// Hook job-completed already wipes _work per job; these are a safety net for
// orphaned dirs from crashes or runner restarts, kept short to free SSD space.
func DefaultOptions() Options {
	return Options{
		Execute:        false,
		WorkDir:        civm.DefaultWorkDir,
		TmpDir:         civm.DefaultTmpDir,
		TmpThreshold:   24 * time.Hour,
		WorkThreshold:  3 * 24 * time.Hour,
		DockerPrune:    true,
		AptClean:       true,
		IdleProbeDelay: 2 * time.Second,
		Now:            time.Now(),
		WalkFn:         filepath.WalkDir,
		StatFn:         defaultStat,
		GlobFn:         filepath.Glob,
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
	out = append(out, scanWorkAndMaybeDelete(ctx, opts))
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
	if opts.GlobFn == nil {
		opts.GlobFn = filepath.Glob
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

func scanWorkAndMaybeDelete(ctx context.Context, opts Options) Action {
	roots := workCleanupRoots(opts)
	if len(roots) == 1 {
		return scanAndMaybeDelete(ctx, opts, "work_old", roots[0], opts.WorkThreshold)
	}
	a := Action{Name: "work_old", Path: strings.Join(roots, ", ")}
	for _, root := range roots {
		part := scanAndMaybeDelete(ctx, opts, "work_old", root, opts.WorkThreshold)
		a.BytesFound += part.BytesFound
		a.BytesFreed += part.BytesFreed
		a.Executed = a.Executed || part.Executed
		if part.Err != nil && a.Err == nil {
			a.Err = part.Err
		}
	}
	return a
}

func workCleanupRoots(opts Options) []string {
	workDir := filepath.Clean(opts.WorkDir)
	if workDir != filepath.Clean(civm.DefaultWorkDir) {
		return []string{opts.WorkDir}
	}
	matches, err := opts.GlobFn("/home/*/actions-runner-*/_work")
	if err != nil || len(matches) == 0 {
		return []string{opts.WorkDir}
	}
	sort.Strings(matches)
	return matches
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
		if name == "work_old" && isProtectedWorkCacheDir(root, path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
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

func isProtectedWorkCacheDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == "" {
		return false
	}
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	_, ok := protectedWorkCacheDirs[first]
	return ok
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
	idleOpts := idle.DefaultOptions()
	idleOpts.ActivityFn = opts.ActivityFn
	idleOpts.ProbeDelay = opts.IdleProbeDelay
	return idle.Ensure(ctx, idleOpts, "cleanup")
}

func formatActivities(activities []Activity) string {
	return idle.FormatActivities(activities)
}

func defaultActivities(ctx context.Context) ([]Activity, error) {
	return idle.DefaultActivities(ctx)
}

func parseActiveProcesses(psOutput string, currentPID int) []Activity {
	return idle.ParseActiveProcesses(psOutput, currentPID)
}

func isActiveBuildProcess(comm, args string) bool {
	return idle.IsActiveBuildProcess(comm, args)
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
