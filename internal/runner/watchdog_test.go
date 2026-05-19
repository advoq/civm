package runner

import (
	"context"
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
	if report.Exit != 1 {
		t.Fatalf("Exit = %d, want 1", report.Exit)
	}
	if !hasWatchdogEventWithReason(report, "rerun-skipped", "host-busy") {
		t.Fatalf("events = %+v, want rerun-skipped host-busy", report.Events)
	}
	if hookCalls != 0 || restartCalls != 0 || rerunCalls != 0 {
		t.Fatalf("mutated despite busy host: hooks=%d restarts=%d reruns=%d", hookCalls, restartCalls, rerunCalls)
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
