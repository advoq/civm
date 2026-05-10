// Package cleanup implements disk hygiene for the ci-vm runner host.
// All actions are dry-run by default; --execute flag flips to mutating.
package cleanup

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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
	Execute       bool
	WorkDir       string
	TmpDir        string
	TmpThreshold  time.Duration
	WorkThreshold time.Duration
	DockerPrune   bool
	AptClean      bool
	Now           time.Time

	WalkFn func(root string, fn fs.WalkDirFunc) error
	StatFn func(path string) (fs.FileInfo, error)
	RunFn  func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultOptions returns sane defaults: dry-run, 7d /tmp, 14d _work.
func DefaultOptions() Options {
	return Options{
		Execute:       false,
		WorkDir:       "/home/runner/_work",
		TmpDir:        "/tmp",
		TmpThreshold:  7 * 24 * time.Hour,
		WorkThreshold: 14 * 24 * time.Hour,
		DockerPrune:   true,
		AptClean:      true,
		Now:           time.Now(),
		WalkFn:        filepath.WalkDir,
		StatFn:        defaultStat,
		RunFn:         defaultRun,
	}
}

// Run executes every enabled step and returns one Action per step.
// Errors are captured per-Action; the function itself returns nil.
func Run(ctx context.Context, opts Options) []Action {
	if opts.WalkFn == nil {
		opts.WalkFn = filepath.WalkDir
	}
	if opts.StatFn == nil {
		opts.StatFn = defaultStat
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
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

func scanAndMaybeDelete(ctx context.Context, opts Options, name, root string, threshold time.Duration) Action {
	a := Action{Name: name, Path: root}
	cutoff := opts.Now.Add(-threshold)
	var toDelete []string
	err := opts.WalkFn(root, func(path string, d fs.DirEntry, err error) error {
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
		toDelete = append(toDelete, path)
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
	for _, p := range toDelete {
		if _, err := opts.RunFn(ctx, "rm", "-rf", p); err != nil {
			a.Err = err
			break
		}
		a.BytesFreed += a.BytesFound // approximated; precise per-path freed not tracked
	}
	a.Executed = true
	return a
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
