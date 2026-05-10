// Package diskwatchdog implements a disk-pressure watchdog: when the
// filesystem usage exceeds a threshold, it triggers aggressive cleanup
// (delegating to internal/cleanup). Stdlib-only.
package diskwatchdog

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"github.com/emersonbusson/ci-vm/internal/cleanup"
)

// Decision is the watchdog outcome.
type Decision int

const (
	DecisionOK            Decision = iota // disk OK, nothing done
	DecisionCleanupTriggered              // disk above threshold, cleanup ran
	DecisionError                         // erro lendo disk OR rodando cleanup
)

func (d Decision) String() string {
	switch d {
	case DecisionOK:
		return "ok"
	case DecisionCleanupTriggered:
		return "cleanup-triggered"
	case DecisionError:
		return "error"
	}
	return "?"
}

// Result captures watchdog execution outcome.
type Result struct {
	Decision     Decision
	Path         string
	UsedPct      int
	UsedGB       int64
	TotalGB      int64
	ThresholdPct int
	CleanupActions []cleanup.Action // populated when DecisionCleanupTriggered
	Err          error
}

// Options control the watchdog.
type Options struct {
	Path         string // diretorio a monitorar (ex: "/")
	ThresholdPct int    // disparar cleanup se usedPct > ThresholdPct (default 80)
	Execute      bool   // false = dry-run (apenas verifica + reporta)
	WorkDir      string // passado para cleanup.Options.WorkDir
	TmpDir       string // passado para cleanup.Options.TmpDir
	StatfsFn     func(path string) (totalBytes, freeBytes uint64, err error)
	RunFn        func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultOptions returns sane production defaults.
func DefaultOptions() Options {
	return Options{
		Path:         "/",
		ThresholdPct: 80,
		Execute:      false,
		WorkDir:      "/home/runner/_work",
		TmpDir:       "/tmp",
		StatfsFn:     defaultStatfs,
		RunFn:        defaultRun,
	}
}

// Check reads disk usage and triggers cleanup if above threshold.
func Check(ctx context.Context, opts Options) Result {
	if opts.StatfsFn == nil {
		opts.StatfsFn = defaultStatfs
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.ThresholdPct == 0 {
		opts.ThresholdPct = 80
	}
	r := Result{Path: opts.Path, ThresholdPct: opts.ThresholdPct}
	total, free, err := opts.StatfsFn(opts.Path)
	if err != nil {
		r.Decision = DecisionError
		r.Err = err
		return r
	}
	if total == 0 {
		r.Decision = DecisionError
		r.Err = fmt.Errorf("statfs total = 0")
		return r
	}
	used := total - free
	r.UsedGB = int64(used / (1 << 30))
	r.TotalGB = int64(total / (1 << 30))
	r.UsedPct = int(used * 100 / total)
	if r.UsedPct <= opts.ThresholdPct {
		r.Decision = DecisionOK
		return r
	}
	// Above threshold: trigger cleanup
	cleanOpts := cleanup.DefaultOptions()
	cleanOpts.Execute = opts.Execute
	cleanOpts.WorkDir = opts.WorkDir
	cleanOpts.TmpDir = opts.TmpDir
	// Aggressive: shorter thresholds when disk pressure
	cleanOpts.TmpThreshold = 24 * time.Hour
	cleanOpts.WorkThreshold = 7 * 24 * time.Hour
	cleanOpts.RunFn = opts.RunFn
	r.CleanupActions = cleanup.Run(ctx, cleanOpts)
	r.Decision = DecisionCleanupTriggered
	for _, a := range r.CleanupActions {
		if a.Err != nil {
			r.Decision = DecisionError
			r.Err = a.Err
			break
		}
	}
	return r
}

// ExitCode returns exit code matching Decision.
func (r Result) ExitCode() int {
	switch r.Decision {
	case DecisionOK:
		return 0
	case DecisionCleanupTriggered:
		return 0 // cleanup successful is OK
	case DecisionError:
		return 2
	}
	return 1
}

// Render writes a human-readable report.
func (r Result) Render(w io.Writer) {
	fmt.Fprintf(w, "Disk watchdog: %s\n", r.Path)
	fmt.Fprintf(w, "Used: %d GB / %d GB (%d%%) | Threshold: %d%%\n",
		r.UsedGB, r.TotalGB, r.UsedPct, r.ThresholdPct)
	fmt.Fprintf(w, "Decision: %s\n", r.Decision)
	if r.Err != nil {
		fmt.Fprintf(w, "Error: %v\n", r.Err)
	}
	if len(r.CleanupActions) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Aggressive cleanup actions (TmpThreshold=24h, WorkThreshold=7d):")
		for _, a := range r.CleanupActions {
			status := "ok"
			if a.Err != nil {
				status = "erro: " + a.Err.Error()
			} else if a.Executed {
				status = "aplicado"
			} else {
				status = "(dry-run)"
			}
			fmt.Fprintf(w, "  %-14s found=%s freed=%s %s\n",
				a.Name, cleanup.FormatBytes(a.BytesFound), cleanup.FormatBytes(a.BytesFreed), status)
		}
	}
}

// ---- defaults ----

func defaultStatfs(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	total := uint64(st.Blocks) * uint64(st.Bsize)
	free := uint64(st.Bavail) * uint64(st.Bsize)
	return total, free, nil
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
