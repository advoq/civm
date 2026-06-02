package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/advoq/civm/internal/hook"
	"github.com/advoq/civm/internal/idle"
)

var testWatchdogNow = time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

func baseWatchdogOptions(t *testing.T) WatchdogOptions {
	t.Helper()
	opts := DefaultWatchdogOptions()
	opts.Execute = true
	opts.Repos = []string{"advoq/civm"}
	opts.InferRepos = false
	opts.NetworkFn = func(context.Context, time.Duration) error { return nil }
	opts.ActivityFn = func(context.Context) ([]idle.Activity, error) { return nil, nil }
	opts.SystemRunnersFn = func(context.Context) ([]Status, error) {
		return []Status{{
			UnitName:    "actions.runner.advoq-civm.civm-self.service",
			Repo:        "advoq/civm",
			Name:        "civm-self",
			ActiveState: "active",
			SubState:    "running",
		}}, nil
	}
	opts.GitHubRunnersFn = func(context.Context, string) ([]WatchdogGitHubRunner, error) {
		return []WatchdogGitHubRunner{{
			Repo:   "advoq/civm",
			Name:   "civm-self",
			Status: "online",
			Labels: []string{"self-hosted", "civm"},
		}}, nil
	}
	opts.HookInstallFn = func(context.Context, hook.InstallOptions) hook.InstallResult {
		return hook.InstallResult{Executed: true, HooksDir: hook.DefaultHooksDir}
	}
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return []byte("active\n"), nil }
	opts.SleepFn = func(time.Duration) {}
	opts.NowFn = func() time.Time { return testWatchdogNow }
	opts.MarkerPath = t.TempDir() + "/reruns.json"
	opts.ReadFileFn = os.ReadFile
	opts.WriteFileFn = os.WriteFile
	opts.MkdirAllFn = os.MkdirAll
	return opts
}

func TestWatchdogNetworkDownDoesNotMutate(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.NetworkFn = func(context.Context, time.Duration) error { return errors.New("dial timeout") }
	hookCalls := 0
	restartCalls := 0
	rerunCalls := 0
	opts.HookInstallFn = func(context.Context, hook.InstallOptions) hook.InstallResult {
		hookCalls++
		return hook.InstallResult{}
	}
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && strings.Join(args, " ") == "systemctl restart actions.runner.advoq-civm.civm-self.service" {
			restartCalls++
		}
		return []byte("active\n"), nil
	}
	opts.RerunNetworkFailures = true
	opts.RerunFn = func(context.Context, string, int64) error {
		rerunCalls++
		return nil
	}

	report := Watchdog(context.Background(), opts)
	if report.Exit != 1 {
		t.Fatalf("Exit = %d, want 1", report.Exit)
	}
	if !hasWatchdogEvent(report, "network-down") {
		t.Fatalf("events = %+v, want network-down", report.Events)
	}
	if hookCalls != 0 || restartCalls != 0 || rerunCalls != 0 {
		t.Fatalf("mutated despite network down: hooks=%d restarts=%d reruns=%d", hookCalls, restartCalls, rerunCalls)
	}
}

func TestWatchdogBusyHostDoesNotMutate(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RerunNetworkFailures = true
	opts.SystemRunnersFn = func(context.Context) ([]Status, error) {
		return []Status{{
			UnitName:    "actions.runner.advoq-civm.civm-self.service",
			Repo:        "advoq/civm",
			Name:        "civm-self",
			ActiveState: "failed",
			SubState:    "failed",
		}}, nil
	}
	opts.ActivityFn = func(context.Context) ([]idle.Activity, error) {
		return []idle.Activity{{PID: 99, Command: "Runner.Worker run"}}, nil
	}
	hookCalls := 0
	restartCalls := 0
	rerunCalls := 0
	opts.HookInstallFn = func(context.Context, hook.InstallOptions) hook.InstallResult {
		hookCalls++
		return hook.InstallResult{}
	}
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && strings.Contains(strings.Join(args, " "), "systemctl restart") {
			restartCalls++
		}
		return []byte("active\n"), nil
	}
	opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
		return []WatchdogRun{{ID: 1, HeadSHA: "abc", Conclusion: "failure", PullRequests: []WatchdogPullRequestRef{{Number: 7}}}}, nil
	}
	opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
		return WatchdogPullRequest{Number: 7, State: "open", MergeableState: "clean"}, nil
	}
	logCalls := 0
	opts.RunLogFn = func(context.Context, string, int64) (string, error) {
		logCalls++
		return "Run actions/checkout@v5\nfatal: early EOF\ninvalid index-pack output", nil
	}
	opts.RerunFn = func(context.Context, string, int64) error {
		rerunCalls++
		return nil
	}

	report := Watchdog(context.Background(), opts)
	// host-busy is the expected steady state on a shared runner box: the
	// watchdog must defer maintenance (no mutations) AND report success
	// (exit 0). Marking the systemd unit failed on every busy tick would
	// keep it perpetually red and mask genuine faults. (Kahneman #13: the
	// prior assertion wanted exit 1, locking in the opposite of the purpose.)
	if report.Exit != 0 {
		t.Fatalf("Exit = %d, want 0 (host-busy deferral is success) events=%+v", report.Exit, report.Events)
	}
	if !hasWatchdogEventWithReason(report, "rerun-skipped", "host-busy") {
		t.Fatalf("events = %+v, want rerun-skipped host-busy", report.Events)
	}
	if hookCalls != 0 || restartCalls != 0 || rerunCalls != 0 {
		t.Fatalf("mutated despite busy host: hooks=%d restarts=%d reruns=%d", hookCalls, restartCalls, rerunCalls)
	}
}

func TestWatchdogIdleUnknownDefersWithoutFailing(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RerunNetworkFailures = true
	// A failed idle probe means we cannot prove the host is idle, so the
	// watchdog must refrain from acting. Like host-busy, this is a safe
	// deferral, not a watchdog failure: exit 0 with a warning event, never a
	// red systemd unit. (SPEC RF-6: non-zero is reserved for real faults.)
	hookCalls := 0
	restartCalls := 0
	rerunCalls := 0
	opts.ActivityFn = func(context.Context) ([]idle.Activity, error) {
		return nil, errors.New("ps probe failed")
	}
	opts.HookInstallFn = func(context.Context, hook.InstallOptions) hook.InstallResult {
		hookCalls++
		return hook.InstallResult{}
	}
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "sudo" && strings.Contains(strings.Join(args, " "), "systemctl restart") {
			restartCalls++
		}
		return []byte("active\n"), nil
	}
	opts.RerunFn = func(context.Context, string, int64) error {
		rerunCalls++
		return nil
	}

	report := Watchdog(context.Background(), opts)
	if report.Exit != 0 {
		t.Fatalf("Exit = %d, want 0 (host-idle-unknown deferral is success) events=%+v", report.Exit, report.Events)
	}
	if !hasWatchdogEventWithReason(report, "runner-restart-skipped", "host-idle-unknown") {
		t.Fatalf("events = %+v, want runner-restart-skipped host-idle-unknown", report.Events)
	}
	if hookCalls != 0 || restartCalls != 0 || rerunCalls != 0 {
		t.Fatalf("mutated despite unknown host state: hooks=%d restarts=%d reruns=%d", hookCalls, restartCalls, rerunCalls)
	}
}

// --- ITEM-10 broken-runner auto-restart (DT-8) ---

type detectIO struct {
	files      map[string][]byte
	restarted  []string
	restartErr error
}

func newDetectIO() *detectIO { return &detectIO{files: map[string][]byte{}} }

func (d *detectIO) opts(now time.Time) WatchdogOptions {
	return WatchdogOptions{
		Execute:            true,
		HooksLogPath:       "/var/log/civm/hooks.jsonl",
		MarkerPath:         "/var/lib/civm/marker.json",
		AutoRestartPerHour: 3,
		NowFn:              func() time.Time { return now },
		ReadFileFn: func(p string) ([]byte, error) {
			if b, ok := d.files[p]; ok {
				return b, nil
			}
			return nil, os.ErrNotExist
		},
		WriteFileFn: func(p string, b []byte, _ os.FileMode) error { d.files[p] = b; return nil },
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		RestartFn: func(_ context.Context, unit string) error {
			d.restarted = append(d.restarted, unit)
			return d.restartErr
		},
	}
}

func sentinelLine(now time.Time, workRoot string) string {
	return `{"time":"` + now.Format(time.RFC3339) + `","event":"job-completed","decision":"error","work_root":"` +
		workRoot + `","actions":[{"name":"work_root","error":"wrapper rm failed: boom"}]}`
}

func brokenRunnerSystemd() []Status {
	return []Status{
		{UnitName: "actions.runner.advoq-org.civm-advoq-org.service", Name: "civm-advoq-org", WorkingDirectory: "/home/emdev/actions-runner-advoq-org"},
		{UnitName: "actions.runner.vitae.civm-vitae.service", Name: "civm-vitae", WorkingDirectory: "/home/emdev/actions-runner-vitae"},
	}
}

func TestDetectBrokenRunnerRestartsCorrectUnit(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now.Add(-5*time.Minute), "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	var report WatchdogReport
	detectBrokenRunner(context.Background(), io.opts(now), brokenRunnerSystemd(), &report)
	if len(io.restarted) != 1 || io.restarted[0] != "actions.runner.advoq-org.civm-advoq-org.service" {
		t.Fatalf("restarted = %v, want only the advoq-org unit (deterministic WorkRoot map)", io.restarted)
	}
	if !hasWatchdogEvent(report, "runner-auto-restarted") {
		t.Fatalf("events = %+v, want runner-auto-restarted", report.Events)
	}
}

func TestDetectBrokenRunnerNoSentinelNoRestart(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	// job-completed with a CLEAN work_root action (no error) — not a sentinel.
	io.files["/var/log/civm/hooks.jsonl"] = []byte(`{"time":"` + now.Format(time.RFC3339) + `","decision":"ok","work_root":"/home/emdev/actions-runner-advoq-org/_work","actions":[{"name":"work_root","executed":true}]}` + "\n")
	var report WatchdogReport
	detectBrokenRunner(context.Background(), io.opts(now), brokenRunnerSystemd(), &report)
	if len(io.restarted) != 0 {
		t.Fatalf("restarted = %v, want none (no broken sentinel)", io.restarted)
	}
}

func TestDetectBrokenRunnerStaleSentinelIgnored(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now.Add(-3*time.Hour), "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	var report WatchdogReport
	detectBrokenRunner(context.Background(), io.opts(now), brokenRunnerSystemd(), &report)
	if len(io.restarted) != 0 {
		t.Fatalf("restarted = %v, want none (sentinel older than 1h)", io.restarted)
	}
}

func TestDetectBrokenRunnerUnknownWorkRootDoesNotTouchWrongUnit(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	// work_root maps to NO known unit → must restart nothing (never guess).
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now, "/home/emdev/actions-runner-ghost/_work") + "\n")
	var report WatchdogReport
	detectBrokenRunner(context.Background(), io.opts(now), brokenRunnerSystemd(), &report)
	if len(io.restarted) != 0 {
		t.Fatalf("restarted = %v, want none (no unit owns the work_root)", io.restarted)
	}
	if !hasWatchdogEventWithReason(report, "runner-auto-restart-skipped", "no-unit-for-work-root") {
		t.Fatalf("events = %+v, want no-unit-for-work-root skip", report.Events)
	}
}

func TestDetectBrokenRunnerRateCapSkips(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now, "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	// Pre-seed the marker at the cap for this unit in the current hour.
	io.files["/var/lib/civm/marker.json"] = []byte(`{"reruns":{},"auto_restarts":{"actions.runner.advoq-org.civm-advoq-org.service":{"count":3,"window_start":"` + now.Format(time.RFC3339) + `"}}}`)
	var report WatchdogReport
	detectBrokenRunner(context.Background(), io.opts(now), brokenRunnerSystemd(), &report)
	if len(io.restarted) != 0 {
		t.Fatalf("restarted = %v, want none (rate cap reached)", io.restarted)
	}
	if !hasWatchdogEventWithReason(report, "runner-auto-restart-skipped", "rate-cap-reached") {
		t.Fatalf("events = %+v, want rate-cap-reached skip", report.Events)
	}
}

func TestDetectBrokenRunnerRestartErrorExits2(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	io.restartErr = errors.New("systemctl restart failed")
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now, "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	var report WatchdogReport
	detectBrokenRunner(context.Background(), io.opts(now), brokenRunnerSystemd(), &report)
	if report.Exit != 2 {
		t.Fatalf("Exit = %d, want 2 (real restart failure)", report.Exit)
	}
}

func TestDetectBrokenRunnerDryRunCandidateOnly(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	opts := io.opts(now)
	opts.Execute = false
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now, "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	var report WatchdogReport
	detectBrokenRunner(context.Background(), opts, brokenRunnerSystemd(), &report)
	if len(io.restarted) != 0 {
		t.Fatalf("restarted = %v, want none in dry-run", io.restarted)
	}
	if !hasWatchdogEvent(report, "runner-auto-restarted") {
		t.Fatalf("dry-run should surface the candidate event: %+v", report.Events)
	}
}

func TestUnitForWorkRootRejectsPrefixCollision(t *testing.T) {
	t.Parallel()
	systemd := []Status{
		{UnitName: "actions.runner.bare.civm-bare.service", WorkingDirectory: "/home/emdev/actions-runner"},
		{UnitName: "actions.runner.advoq.civm-advoq.service", WorkingDirectory: "/home/emdev/actions-runner-advoq"},
	}
	if got := unitForWorkRoot("/home/emdev/actions-runner-advoq/_work", systemd); got != "actions.runner.advoq.civm-advoq.service" {
		t.Fatalf("got %q, want the -advoq unit (no prefix collision with bare actions-runner)", got)
	}
	if got := unitForWorkRoot("/home/emdev/actions-runner/_work", systemd); got != "actions.runner.bare.civm-bare.service" {
		t.Fatalf("got %q, want the bare unit", got)
	}
	if got := unitForWorkRoot("/home/emdev/actions-runner-ghost/_work/", systemd); got != "" {
		t.Fatalf("got %q, want '' for an unowned work_root", got)
	}
}

func TestDetectBrokenRunnerMarkerWriteFailureFailsClosed(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now, "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	opts := io.opts(now)
	// Persistent marker-write failure (correlated with the disk fault that wedges
	// the runner). The cap must hold: NO restart without persisting the slot.
	opts.WriteFileFn = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	for i := 0; i < 6; i++ {
		var report WatchdogReport
		detectBrokenRunner(context.Background(), opts, brokenRunnerSystemd(), &report)
		if report.Exit != 2 {
			t.Fatalf("tick %d: Exit = %d, want 2 (marker-write-failed fails closed)", i, report.Exit)
		}
	}
	if len(io.restarted) != 0 {
		t.Fatalf("restarted %d time(s) with unwritable cap state; want 0 (fail-closed)", len(io.restarted))
	}
}

func TestDetectBrokenRunnerDedupesSameSentinelAcrossTicks(t *testing.T) {
	t.Parallel()
	now := testWatchdogNow
	io := newDetectIO()
	io.files["/var/log/civm/hooks.jsonl"] = []byte(sentinelLine(now.Add(-2*time.Minute), "/home/emdev/actions-runner-advoq-org/_work") + "\n")
	opts := io.opts(now)
	// The same sentinel line persists in the log across ticks; it must restart
	// the runner exactly ONCE (dedup), not once per tick up to the cap.
	for i := 0; i < 4; i++ {
		var report WatchdogReport
		detectBrokenRunner(context.Background(), opts, brokenRunnerSystemd(), &report)
	}
	if len(io.restarted) != 1 {
		t.Fatalf("restarted %d times for one persistent sentinel; want exactly 1 (dedup)", len(io.restarted))
	}
}

func TestWatchdogRepairsHooksWhenIdle(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	hookCalls := 0
	opts.HookInstallFn = func(_ context.Context, got hook.InstallOptions) hook.InstallResult {
		hookCalls++
		if !got.Execute {
			t.Fatalf("hook install Execute = false, want true")
		}
		if got.RestartRunners {
			t.Fatalf("watchdog hook repair must not restart all runners directly")
		}
		return hook.InstallResult{Executed: true, HooksDir: got.HooksDir, RunnerEnvFiles: []string{"/home/runner/actions-runner/.env"}}
	}

	report := Watchdog(context.Background(), opts)
	if report.Exit != 0 {
		t.Fatalf("Exit = %d, want 0 events=%+v", report.Exit, report.Events)
	}
	if hookCalls != 1 {
		t.Fatalf("hookCalls = %d, want 1", hookCalls)
	}
	if !hasWatchdogEvent(report, "hooks-repaired") {
		t.Fatalf("events = %+v, want hooks-repaired", report.Events)
	}
}

func TestWatchdogRestartsFailedSystemdRunner(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RestartDelay = 0
	opts.SystemRunnersFn = func(context.Context) ([]Status, error) {
		return []Status{{
			UnitName:    "actions.runner.advoq-civm.civm-self.service",
			Repo:        "advoq/civm",
			Name:        "civm-self",
			ActiveState: "failed",
			SubState:    "failed",
		}}, nil
	}
	var calls []string
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if strings.HasPrefix(call, "systemctl is-active") {
			return []byte("active\n"), nil
		}
		return nil, nil
	}

	report := Watchdog(context.Background(), opts)
	if report.Exit != 0 {
		t.Fatalf("Exit = %d, want 0 events=%+v", report.Exit, report.Events)
	}
	if !hasWatchdogEvent(report, "runner-restarted") {
		t.Fatalf("events = %+v, want runner-restarted", report.Events)
	}
	assertWatchdogCall(t, calls, "sudo systemctl restart actions.runner.advoq-civm.civm-self.service")
}

func TestClassifyFailureLogNetworkCheckout(t *testing.T) {
	t.Parallel()
	log := "Run actions/checkout@v5\nRPC failed; curl 56 GnuTLS recv error (-54)\nfatal: early EOF\ninvalid index-pack output"
	got := ClassifyFailureLog(log)
	if got.Kind != FailureNetworkCheckout {
		t.Fatalf("Kind = %s, want %s (%+v)", got.Kind, FailureNetworkCheckout, got)
	}
}

func TestClassifyFailureLogLintBeforeNetworkIsNotNetwork(t *testing.T) {
	t.Parallel()
	log := "Run golangci-lint run ./...\ninternal/foo.go:12:1: lint failed\nfatal: early EOF"
	got := ClassifyFailureLog(log)
	if got.Kind == FailureNetworkCheckout {
		t.Fatalf("Kind = %s, want non-network (%+v)", got.Kind, got)
	}
}

func TestWatchdogMarkerPreventsDuplicateRerun(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RerunNetworkFailures = true
	opts.ReadFileFn = func(string) ([]byte, error) {
		return []byte(`{"reruns":{"77/abc":{"repo":"advoq/civm","run_id":77,"head_sha":"abc","rerun_at":"2026-05-19T12:00:00Z"}}}`), nil
	}
	rerunCalls := 0
	opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
		return []WatchdogRun{{
			ID:           77,
			HeadSHA:      "abc",
			Conclusion:   "failure",
			CreatedAt:    testWatchdogNow.Add(-time.Hour),
			PullRequests: []WatchdogPullRequestRef{{Number: 7}},
		}}, nil
	}
	opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
		return WatchdogPullRequest{Number: 7, State: "open", MergeableState: "clean"}, nil
	}
	logCalls := 0
	opts.RunLogFn = func(context.Context, string, int64) (string, error) {
		logCalls++
		return "Run actions/checkout@v5\nfatal: early EOF\ninvalid index-pack output", nil
	}
	opts.RerunFn = func(context.Context, string, int64) error {
		rerunCalls++
		return nil
	}

	report := Watchdog(context.Background(), opts)
	if rerunCalls != 0 {
		t.Fatalf("rerunCalls = %d, want 0", rerunCalls)
	}
	if logCalls != 0 {
		t.Fatalf("logCalls = %d, want 0", logCalls)
	}
	if !hasWatchdogEventWithReason(report, "rerun-skipped", "already-rerun") {
		t.Fatalf("events = %+v, want rerun-skipped already-rerun", report.Events)
	}
}

func TestWatchdogTriggersNetworkRerunAndWritesMarker(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RerunNetworkFailures = true
	written := ""
	reruns := []int64{}
	opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
		return []WatchdogRun{{
			ID:           99,
			HeadSHA:      "abc123",
			Conclusion:   "timed_out",
			CreatedAt:    testWatchdogNow.Add(-time.Hour),
			PullRequests: []WatchdogPullRequestRef{{Number: 7}},
		}}, nil
	}
	opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
		return WatchdogPullRequest{Number: 7, State: "open", MergeableState: "clean"}, nil
	}
	opts.RunLogFn = func(context.Context, string, int64) (string, error) {
		return "Run actions/checkout@v5\nRPC failed; curl 92 HTTP/2 stream was not closed cleanly: CANCEL\n", nil
	}
	opts.RerunFn = func(_ context.Context, repo string, runID int64) error {
		if repo != "advoq/civm" {
			t.Fatalf("repo = %q", repo)
		}
		reruns = append(reruns, runID)
		return nil
	}
	opts.WriteFileFn = func(_ string, data []byte, _ os.FileMode) error {
		written = string(data)
		return nil
	}

	report := Watchdog(context.Background(), opts)
	if report.Exit != 0 {
		t.Fatalf("Exit = %d, want 0 events=%+v", report.Exit, report.Events)
	}
	if len(reruns) != 1 || reruns[0] != 99 {
		t.Fatalf("reruns = %v, want [99]", reruns)
	}
	if !hasWatchdogEvent(report, "rerun-triggered") {
		t.Fatalf("events = %+v, want rerun-triggered", report.Events)
	}
	if report.Metrics.RunsConsidered != 1 || report.Metrics.RerunsTriggered != 1 || report.Metrics.RerunsSkipped != 0 {
		t.Fatalf("metrics = %+v, want considered=1 triggered=1 skipped=0", report.Metrics)
	}
	if !strings.Contains(written, `"99/abc123"`) {
		t.Fatalf("marker not written for run/head: %s", written)
	}
}

func TestWatchdogSkipsOldRunBeforePRAndLog(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RerunNetworkFailures = true
	prCalls := 0
	logCalls := 0
	opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
		return []WatchdogRun{{
			ID:           101,
			HeadSHA:      "old",
			Conclusion:   "failure",
			CreatedAt:    testWatchdogNow.Add(-7 * time.Hour),
			PullRequests: []WatchdogPullRequestRef{{Number: 7}},
		}}, nil
	}
	opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
		prCalls++
		return WatchdogPullRequest{Number: 7, State: "open", MergeableState: "clean"}, nil
	}
	opts.RunLogFn = func(context.Context, string, int64) (string, error) {
		logCalls++
		return "fatal: early EOF", nil
	}

	report := Watchdog(context.Background(), opts)
	if prCalls != 0 || logCalls != 0 {
		t.Fatalf("old run reached PR/log: pr=%d log=%d", prCalls, logCalls)
	}
	if !hasWatchdogEventWithReason(report, "rerun-skipped", "run-too-old") {
		t.Fatalf("events = %+v, want rerun-skipped run-too-old", report.Events)
	}
	if report.Metrics.RunsConsidered != 1 || report.Metrics.RerunsSkipped != 1 {
		t.Fatalf("metrics = %+v, want considered=1 skipped=1", report.Metrics)
	}
}

func TestWatchdogSkipsMissingCreatedAtBeforePRAndLog(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.RerunNetworkFailures = true
	prCalls := 0
	logCalls := 0
	opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
		return []WatchdogRun{{
			ID:           102,
			HeadSHA:      "missing",
			Conclusion:   "failure",
			PullRequests: []WatchdogPullRequestRef{{Number: 7}},
		}}, nil
	}
	opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
		prCalls++
		return WatchdogPullRequest{Number: 7, State: "open", MergeableState: "clean"}, nil
	}
	opts.RunLogFn = func(context.Context, string, int64) (string, error) {
		logCalls++
		return "fatal: early EOF", nil
	}

	report := Watchdog(context.Background(), opts)
	if prCalls != 0 || logCalls != 0 {
		t.Fatalf("missing created_at reached PR/log: pr=%d log=%d", prCalls, logCalls)
	}
	if !hasWatchdogEventWithReason(report, "rerun-skipped", "run-created-at-missing") {
		t.Fatalf("events = %+v, want rerun-skipped run-created-at-missing", report.Events)
	}
	if report.Metrics.RunsConsidered != 1 || report.Metrics.RerunsSkipped != 1 {
		t.Fatalf("metrics = %+v, want considered=1 skipped=1", report.Metrics)
	}
}

func TestWatchdogRerunMetricsCountDryRunTriggerDecision(t *testing.T) {
	t.Parallel()
	opts := baseWatchdogOptions(t)
	opts.Execute = false
	opts.RerunNetworkFailures = true
	opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
		return []WatchdogRun{
			{ID: 1, HeadSHA: "missing", Conclusion: "failure", PullRequests: []WatchdogPullRequestRef{{Number: 7}}},
			{ID: 2, HeadSHA: "code", Conclusion: "failure", CreatedAt: testWatchdogNow.Add(-time.Hour), PullRequests: []WatchdogPullRequestRef{{Number: 7}}},
			{ID: 3, HeadSHA: "network", Conclusion: "failure", CreatedAt: testWatchdogNow.Add(-time.Hour), PullRequests: []WatchdogPullRequestRef{{Number: 7}}},
		}, nil
	}
	opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
		return WatchdogPullRequest{Number: 7, State: "open", MergeableState: "clean"}, nil
	}
	opts.RunLogFn = func(_ context.Context, _ string, runID int64) (string, error) {
		if runID == 2 {
			return "Run go test ./...\ntests failed\nfatal: early EOF", nil
		}
		return "Run actions/checkout@v5\ncurl 56\nfatal: early EOF", nil
	}
	rerunCalls := 0
	opts.RerunFn = func(context.Context, string, int64) error {
		rerunCalls++
		return nil
	}

	report := Watchdog(context.Background(), opts)
	if rerunCalls != 0 {
		t.Fatalf("dry-run rerunCalls = %d, want 0", rerunCalls)
	}
	if report.Metrics.RunsConsidered != 3 || report.Metrics.RerunsTriggered != 1 || report.Metrics.RerunsSkipped != 2 {
		t.Fatalf("metrics = %+v, want considered=3 triggered=1 skipped=2", report.Metrics)
	}
	for _, event := range report.Events {
		if event.Event == "rerun-triggered" && event.Executed {
			t.Fatalf("dry-run rerun-triggered event executed=true: %+v", event)
		}
	}
}

func TestWatchdogSkipsClosedOrConflictingPR(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		pr   WatchdogPullRequest
		want string
	}{
		{name: "closed", pr: WatchdogPullRequest{Number: 7, State: "closed", MergeableState: "clean"}, want: "pr-not-open"},
		{name: "conflicting", pr: WatchdogPullRequest{Number: 7, State: "open", MergeableState: "dirty"}, want: "pr-conflicting"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := baseWatchdogOptions(t)
			opts.RerunNetworkFailures = true
			rerunCalls := 0
			opts.ListRunsFn = func(context.Context, string, int) ([]WatchdogRun, error) {
				return []WatchdogRun{{
					ID:           88,
					HeadSHA:      "abc",
					Conclusion:   "failure",
					CreatedAt:    testWatchdogNow.Add(-time.Hour),
					PullRequests: []WatchdogPullRequestRef{{Number: 7}},
				}}, nil
			}
			opts.PullRequestFn = func(context.Context, string, int) (WatchdogPullRequest, error) {
				return tt.pr, nil
			}
			opts.RunLogFn = func(context.Context, string, int64) (string, error) {
				return "Run actions/checkout@v5\nConnection timed out\n", nil
			}
			opts.RerunFn = func(context.Context, string, int64) error {
				rerunCalls++
				return nil
			}

			report := Watchdog(context.Background(), opts)
			if rerunCalls != 0 {
				t.Fatalf("rerunCalls = %d, want 0", rerunCalls)
			}
			if !hasWatchdogEventWithReason(report, "rerun-skipped", tt.want) {
				t.Fatalf("events = %+v, want rerun-skipped %s", report.Events, tt.want)
			}
		})
	}
}

func TestWatchdogInfersHyphenatedRepoFromRunnerConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := DefaultWatchdogOptions()
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if call == "systemctl show actions.runner.acme-org-deep-repo-name.civm-acme.service --property=WorkingDirectory --value" {
			return []byte(dir + "\n"), nil
		}
		return nil, errors.New("unexpected call: " + call)
	}
	opts.ReadFileFn = func(path string) ([]byte, error) {
		if !strings.HasSuffix(path, "/.runner") {
			return nil, errors.New("unexpected read: " + path)
		}
		return []byte(`{"gitHubUrl":"https://github.com/acme-org/deep-repo-name"}`), nil
	}
	systemd := []Status{{
		UnitName: "actions.runner.acme-org-deep-repo-name.civm-acme.service",
		Repo:     "acme/org-deep-repo-name",
		Name:     "civm-acme",
	}}

	repos := inferWatchdogRepos(enrichWatchdogSystemdRepos(context.Background(), opts, systemd))
	if strings.Join(repos, ",") != "acme-org/deep-repo-name" {
		t.Fatalf("repos = %v, want acme-org/deep-repo-name", repos)
	}
}

func TestWatchdogInferReposFallsBackToUnitParser(t *testing.T) {
	t.Parallel()
	opts := DefaultWatchdogOptions()
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("systemctl show unavailable")
	}
	systemd := []Status{{
		UnitName: "actions.runner.owner-repo-with-hyphen.civm.service",
		Repo:     "owner/repo-with-hyphen",
		Name:     "civm",
	}}

	repos := inferWatchdogRepos(enrichWatchdogSystemdRepos(context.Background(), opts, systemd))
	if strings.Join(repos, ",") != "owner/repo-with-hyphen" {
		t.Fatalf("repos = %v, want fallback owner/repo-with-hyphen", repos)
	}
}

func TestApplyWatchdogDefaultsInstallsCommandBackedFunctions(t *testing.T) {
	t.Parallel()
	opts := WatchdogOptions{
		RunFn: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "git" && strings.Join(args, " ") == "ls-remote https://github.com/actions/checkout.git HEAD" {
				return []byte("abc\tHEAD\n"), nil
			}
			if name == "systemctl" && strings.Join(args, " ") == "list-units --type=service --no-pager --no-legend --all actions.runner.*" {
				return []byte(fakeSystemctlOutput), nil
			}
			if name == "gh" && strings.Join(args, " ") == "api /repos/advoq/civm/actions/runners" {
				return []byte(`{"runners":[{"id":1,"name":"civm-self","status":"online","busy":false,"labels":[{"name":"self-hosted"},{"name":"civm"}]}]}`), nil
			}
			if name == "gh" && strings.Join(args, " ") == "api /repos/advoq/civm/actions/runs?per_page=20&status=completed" {
				return []byte(`{"workflow_runs":[{"id":8,"head_sha":"abc","status":"completed","conclusion":"failure","created_at":"2026-05-19T12:00:00Z","html_url":"https://github.com/advoq/civm/actions/runs/8","pull_requests":[{"number":7}]}]}`), nil
			}
			if name == "gh" && strings.Join(args, " ") == "api /repos/advoq/civm/pulls/7" {
				return []byte(`{"number":7,"state":"open","mergeable_state":"clean"}`), nil
			}
			if name == "gh" && strings.Join(args, " ") == "run view 8 --repo advoq/civm --log-failed" {
				return []byte("Run actions/checkout@v5\nfatal: early EOF\n"), nil
			}
			if name == "gh" && strings.Join(args, " ") == "run rerun 8 --repo advoq/civm --failed" {
				return []byte(""), nil
			}
			return nil, errors.New("unexpected call: " + name + " " + strings.Join(args, " "))
		},
	}
	applyWatchdogDefaults(&opts)

	if err := opts.NetworkFn(context.Background(), time.Second); err != nil {
		t.Fatalf("NetworkFn error: %v", err)
	}
	statuses, err := opts.SystemRunnersFn(context.Background())
	if err != nil {
		t.Fatalf("SystemRunnersFn error: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("statuses=%d, want 3", len(statuses))
	}
	runners, err := opts.GitHubRunnersFn(context.Background(), "advoq/civm")
	if err != nil {
		t.Fatalf("GitHubRunnersFn error: %v", err)
	}
	if len(runners) != 1 || runners[0].Name != "civm-self" || !hasWatchdogLabel(runners[0].Labels, "civm") {
		t.Fatalf("runners=%+v, want civm runner with label", runners)
	}
	runs, err := opts.ListRunsFn(context.Background(), "advoq/civm", opts.RunLimit)
	if err != nil {
		t.Fatalf("ListRunsFn error: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != 8 || len(runs[0].PullRequests) != 1 {
		t.Fatalf("runs=%+v, want parsed run with PR", runs)
	}
	pr, err := opts.PullRequestFn(context.Background(), "advoq/civm", 7)
	if err != nil {
		t.Fatalf("PullRequestFn error: %v", err)
	}
	if pr.State != "open" || pr.MergeableState != "clean" {
		t.Fatalf("pr=%+v, want open clean", pr)
	}
	log, err := opts.RunLogFn(context.Background(), "advoq/civm", 8)
	if err != nil {
		t.Fatalf("RunLogFn error: %v", err)
	}
	if !strings.Contains(log, "early EOF") {
		t.Fatalf("log=%q, want checkout failure", log)
	}
	if err := opts.RerunFn(context.Background(), "advoq/civm", 8); err != nil {
		t.Fatalf("RerunFn error: %v", err)
	}
}

func TestWatchdogCLIParsersRejectInvalidJSON(t *testing.T) {
	t.Parallel()
	runFn := func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{`), nil
	}

	if _, err := listWatchdogGitHubRunners(context.Background(), "advoq/civm", runFn); err == nil {
		t.Fatal("listWatchdogGitHubRunners error=nil, want parse error")
	}
	if _, err := listWatchdogRuns(context.Background(), "advoq/civm", 5, runFn); err == nil {
		t.Fatal("listWatchdogRuns error=nil, want parse error")
	}
	if _, err := getWatchdogPullRequest(context.Background(), "advoq/civm", 7, runFn); err == nil {
		t.Fatal("getWatchdogPullRequest error=nil, want parse error")
	}
}

func TestValidateWatchdogOptionsRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(*WatchdogOptions)
	}{
		{name: "repo", edit: func(o *WatchdogOptions) { o.Repos = []string{"bad"} }},
		{name: "network-timeout", edit: func(o *WatchdogOptions) { o.NetworkTimeout = 0 }},
		{name: "restart-delay", edit: func(o *WatchdogOptions) { o.RestartDelay = -time.Second }},
		{name: "max-run-age", edit: func(o *WatchdogOptions) { o.MaxRunAge = 0 }},
		{name: "run-limit-low", edit: func(o *WatchdogOptions) { o.RunLimit = 0 }},
		{name: "run-limit-high", edit: func(o *WatchdogOptions) { o.RunLimit = 101 }},
		{name: "marker-path", edit: func(o *WatchdogOptions) { o.MarkerPath = "relative.json" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := WatchdogOptions{
				Repos:          []string{"advoq/civm"},
				NetworkTimeout: time.Second,
				RestartDelay:   0,
				MaxRunAge:      time.Hour,
				RunLimit:       10,
				MarkerPath:     "/tmp/runner-watchdog.json",
			}
			tt.edit(&opts)
			if err := validateWatchdogOptions(opts); err == nil {
				t.Fatal("validateWatchdogOptions error=nil, want error")
			}
		})
	}
}

func TestWatchdogReportRenderers(t *testing.T) {
	t.Parallel()
	report := WatchdogReport{
		Executed:     true,
		Repos:        []string{"advoq/civm"},
		RunnerOnline: true,
		Metrics:      WatchdogMetrics{RunsConsidered: 2, RerunsTriggered: 1, RerunsSkipped: 1},
		Events: []WatchdogEvent{
			{Event: "runner-online", Severity: "info", Repo: "advoq/civm", Runner: "civm-self", Online: true},
			{Event: "rerun-triggered", Severity: "info", Repo: "advoq/civm", RunID: 8, Reason: "network-checkout", Detail: "signature=early eof"},
		},
		Exit: 0,
	}

	var jsonBuf bytes.Buffer
	if err := report.RenderJSON(&jsonBuf); err != nil {
		t.Fatalf("RenderJSON error: %v", err)
	}
	var parsed WatchdogReport
	if err := json.Unmarshal(jsonBuf.Bytes(), &parsed); err != nil {
		t.Fatalf("RenderJSON produced invalid JSON: %v", err)
	}
	if parsed.Metrics.RerunsTriggered != 1 {
		t.Fatalf("parsed metrics=%+v, want trigger count", parsed.Metrics)
	}

	var textBuf bytes.Buffer
	report.Render(&textBuf)
	out := textBuf.String()
	for _, want := range []string{"EXECUTE", "runner_online=true", "Repos: advoq/civm", "rerun-triggered", "network-checkout: signature=early eof"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render output missing %q:\n%s", want, out)
		}
	}
}

func hasWatchdogEvent(report WatchdogReport, event string) bool {
	for _, item := range report.Events {
		if item.Event == event {
			return true
		}
	}
	return false
}

func hasWatchdogEventWithReason(report WatchdogReport, event, reason string) bool {
	for _, item := range report.Events {
		if item.Event == event && item.Reason == reason {
			return true
		}
	}
	return false
}

func assertWatchdogCall(t *testing.T, calls []string, want string) {
	t.Helper()
	for _, call := range calls {
		if call == want {
			return
		}
	}
	t.Fatalf("missing call %q in %v", want, calls)
}
