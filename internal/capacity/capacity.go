// Package capacity exposes a stable status contract for integrations such as Busson.
package capacity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type Report struct {
	DiskPath       string `json:"disk_path"`
	DiskUsedPct    int    `json:"disk_used_pct"`
	DiskFreeGB     int64  `json:"disk_free_gb"`
	DiskTotalGB    int64  `json:"disk_total_gb"`
	RunnerServices int    `json:"runner_services"`
	RunnerWorkers  int    `json:"runner_workers"`
	AcceptingJobs  bool   `json:"accepting_jobs"`
	Reason         string `json:"reason,omitempty"`
}

type Options struct {
	Path       string
	MaxDiskPct int
	StatfsFn   func(path string) (totalBytes, freeBytes uint64, err error)
	RunFn      func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func DefaultOptions() Options {
	return Options{Path: "/", MaxDiskPct: 85, StatfsFn: defaultStatfs, RunFn: defaultRun}
}

func Check(ctx context.Context, opts Options) Report {
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.MaxDiskPct == 0 {
		opts.MaxDiskPct = 85
	}
	if opts.StatfsFn == nil {
		opts.StatfsFn = defaultStatfs
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	r := Report{DiskPath: opts.Path, AcceptingJobs: true}
	total, free, err := opts.StatfsFn(opts.Path)
	if err != nil || total == 0 {
		r.AcceptingJobs = false
		r.Reason = fmt.Sprintf("disk stat failed: %v", err)
		return r
	}
	used := total - free
	r.DiskUsedPct = int(used * 100 / total)
	r.DiskFreeGB = int64(free / (1 << 30))
	r.DiskTotalGB = int64(total / (1 << 30))
	r.RunnerServices = countRunnerServices(ctx, opts.RunFn)
	r.RunnerWorkers = countRunnerWorkers(ctx, opts.RunFn)
	if r.DiskUsedPct >= opts.MaxDiskPct {
		r.AcceptingJobs = false
		r.Reason = fmt.Sprintf("disk usage %d%% >= %d%%", r.DiskUsedPct, opts.MaxDiskPct)
	}
	return r
}

func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func RenderText(w io.Writer, r Report) {
	state := "accepting"
	if !r.AcceptingJobs {
		state = "blocked"
	}
	fmt.Fprintf(w, "Capacity: %s, disk=%d%% free=%dGB/%dGB runners=%d workers=%d\n", state, r.DiskUsedPct, r.DiskFreeGB, r.DiskTotalGB, r.RunnerServices, r.RunnerWorkers)
	if r.Reason != "" {
		fmt.Fprintf(w, "Reason: %s\n", r.Reason)
	}
}

func countRunnerServices(ctx context.Context, runFn func(context.Context, string, ...string) ([]byte, error)) int {
	out, err := runFn(ctx, "systemctl", "list-units", "--type=service", "--no-pager", "actions.runner.*")
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "actions.runner") {
			count++
		}
	}
	return count
}

func countRunnerWorkers(ctx context.Context, runFn func(context.Context, string, ...string) ([]byte, error)) int {
	out, err := runFn(ctx, "pgrep", "-fc", "Runner.Worker")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func defaultStatfs(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return uint64(st.Blocks) * uint64(st.Bsize), uint64(st.Bavail) * uint64(st.Bsize), nil
}
