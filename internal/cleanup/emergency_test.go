package cleanup

import (
	"context"
	"strings"
	"testing"
)

func busyActivity(context.Context) ([]Activity, error) {
	return []Activity{{PID: 4242, Command: "/home/emdev/actions-runner/bin/Runner.Worker run"}}, nil
}

// TestRunBusyEmergencyBypassRunsSafeReclaim guards the 2026-06-10 failure mode:
// the disk-watchdog fired at 83% used while a job filled the disk, every action
// was deferred-by-host-busy (freed=0) and the guest ran to 0% free, wedging
// sshd. Under EmergencyBypassIdle the SAFE reclaim (old /tmp, cache trim) must
// run even while busy; the privileged work-dir sweep must stay deferred.
func TestRunBusyEmergencyBypassRunsSafeReclaim(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.WorkDir = "work"
	opts.TmpDir = "tmp"
	opts.DockerPrune = true
	opts.AptClean = true
	opts.ActivityFn = busyActivity
	opts.EmergencyBypassIdle = true
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, nil
	}
	// Hermetic cache trim: one fake cache root, no real filesystem touched.
	opts.GlobFn = func(pattern string) ([]string, error) { return nil, nil }
	rec := &safeDeleteRecorder{}
	opts.SafeDeleteFn = rec.fn

	actions := Run(context.Background(), opts)

	names := make([]string, 0, len(actions))
	for _, a := range actions {
		names = append(names, a.Name)
		if a.Err != nil {
			t.Fatalf("emergency bypass surfaced an error action: %+v", a)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "tmp_old") {
		t.Fatalf("emergency bypass must reclaim old /tmp while busy; actions=%s", joined)
	}
	if !strings.Contains(joined, "emergency-bypass-idle") {
		t.Fatalf("emergency bypass must be visible as an action; actions=%s", joined)
	}
	if strings.Contains(joined, "work_old") {
		t.Fatalf("privileged work sweep must stay deferred while busy; actions=%s", joined)
	}
	if strings.Contains(joined, deferredByHostBusy) {
		t.Fatalf("emergency bypass must replace the busy deferral; actions=%s", joined)
	}
}

// TestRunBusyWithoutEmergencyStillDefers is the refusal pair: below the
// emergency level the busy-host deferral is unchanged.
func TestRunBusyWithoutEmergencyStillDefers(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.WorkDir = "work"
	opts.TmpDir = "tmp"
	opts.ActivityFn = busyActivity
	opts.EmergencyBypassIdle = false
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }

	actions := Run(context.Background(), opts)

	var deferred bool
	for _, a := range actions {
		if a.Name == deferredByHostBusy {
			deferred = true
		}
		if a.Name == "tmp_old" || a.Name == "cache_trim" {
			t.Fatalf("non-emergency busy run must defer reclaim: %+v", actions)
		}
	}
	if !deferred {
		t.Fatalf("busy host without emergency must emit %s: %+v", deferredByHostBusy, actions)
	}
}
