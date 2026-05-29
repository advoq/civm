// Package capacity exposes a stable status contract for integrations such as Busson.
package capacity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/advoq/civm/internal/civm"
	"github.com/advoq/civm/internal/dockerlock"
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
	// DockerHeavyLockActive mirrors dockerlock.IsActive: a docker-heavy job is
	// holding the box-wide serialization lock right now (ITEM-7 / DT-v2-13).
	DockerHeavyLockActive bool `json:"docker_heavy_lock_active"`
	// DockerHeavyLockHolder is a non-PII label of the current holder (scope and
	// repo/run when available), empty when no fresh lock is held.
	DockerHeavyLockHolder string `json:"docker_heavy_lock_holder,omitempty"`
	// RunnerPortBlocks is the best-effort slot->base map read from the port
	// allocation state file; nil when unreadable or empty.
	RunnerPortBlocks map[string]int `json:"runner_port_blocks,omitempty"`
}

type Options struct {
	Path       string
	MaxDiskPct int
	StatfsFn   func(path string) (totalBytes, freeBytes uint64, err error)
	RunFn      func(ctx context.Context, name string, args ...string) ([]byte, error)
	// LockActiveFn reports the docker-heavy lock state (active, holder, err). It
	// is injected so unit tests never touch /run/civm; the default wraps
	// dockerlock.IsActive + dockerlock.Holder.
	LockActiveFn func() (active bool, holder string, err error)
	// PortBlocksFn returns the slot->base allocation map best-effort; the default
	// reads civm.DefaultPortBlockStatePath and returns nil on any error.
	PortBlocksFn func() map[string]int
}

func DefaultOptions() Options {
	return Options{
		Path:         "/",
		MaxDiskPct:   civm.DefaultCapacityMaxDiskPct,
		StatfsFn:     defaultStatfs,
		RunFn:        defaultRun,
		LockActiveFn: defaultLockActive,
		PortBlocksFn: defaultPortBlocks,
	}
}

// defaultLockActive wraps dockerlock so capacity does not duplicate the
// staleness/PID-reuse logic. Import direction is capacity -> dockerlock only;
// dockerlock never imports capacity (DT-v2-13).
func defaultLockActive() (bool, string, error) {
	opts := dockerlock.DefaultOptions()
	active, err := dockerlock.IsActive(opts)
	if err != nil || !active {
		return false, "", err
	}
	return true, dockerlock.Holder(opts), nil
}

// defaultPortBlocks reads the sticky slot->base map persisted by portblock.
// Any error (missing file, malformed JSON) yields nil — this is observability,
// never a hard dependency.
func defaultPortBlocks() map[string]int {
	data, err := os.ReadFile(civm.DefaultPortBlockStatePath)
	if err != nil {
		return nil
	}
	var allocs []struct {
		Slot string `json:"slot"`
		Base int    `json:"base"`
	}
	if err := json.Unmarshal(data, &allocs); err != nil {
		return nil
	}
	if len(allocs) == 0 {
		return nil
	}
	m := make(map[string]int, len(allocs))
	for _, a := range allocs {
		m[a.Slot] = a.Base
	}
	return m
}

func Check(ctx context.Context, opts Options) Report {
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.MaxDiskPct == 0 {
		opts.MaxDiskPct = civm.DefaultCapacityMaxDiskPct
	}
	if opts.StatfsFn == nil {
		opts.StatfsFn = defaultStatfs
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.LockActiveFn == nil {
		opts.LockActiveFn = defaultLockActive
	}
	if opts.PortBlocksFn == nil {
		opts.PortBlocksFn = defaultPortBlocks
	}
	r := Report{DiskPath: opts.Path, AcceptingJobs: true}
	// Lock/port telemetry is best-effort and never blocks job acceptance: a
	// read error leaves DockerHeavyLockActive=false (DT-v2-13).
	active, holder, _ := opts.LockActiveFn()
	r.DockerHeavyLockActive = active
	r.DockerHeavyLockHolder = holder
	r.RunnerPortBlocks = opts.PortBlocksFn()
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
	if r.DockerHeavyLockActive {
		holder := r.DockerHeavyLockHolder
		if holder == "" {
			holder = "(unknown)"
		}
		fmt.Fprintf(w, "Docker-heavy lock: held by %s\n", holder)
	}
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
