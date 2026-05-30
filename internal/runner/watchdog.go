package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/advoq/civm/internal/civm"
	"github.com/advoq/civm/internal/hook"
	"github.com/advoq/civm/internal/idle"
)

const (
	DefaultWatchdogMarkerPath = "/var/lib/civm/runner-watchdog-reruns.json"
	DefaultWatchdogMaxRunAge  = 6 * time.Hour
	defaultWatchdogRunLimit   = 20
)

type WatchdogOptions struct {
	Execute              bool
	Repos                []string
	InferRepos           bool
	RerunNetworkFailures bool
	NetworkTimeout       time.Duration
	RestartDelay         time.Duration
	IdleProbeDelay       time.Duration
	MaxRunAge            time.Duration
	RunLimit             int
	MarkerPath           string
	HooksDir             string
	RunnerGlob           string
	CivmctlPath          string
	RunFn                func(ctx context.Context, name string, args ...string) ([]byte, error)
	NetworkFn            func(ctx context.Context, timeout time.Duration) error
	ActivityFn           func(ctx context.Context) ([]idle.Activity, error)
	SystemRunnersFn      func(ctx context.Context) ([]Status, error)
	GitHubRunnersFn      func(ctx context.Context, repo string) ([]WatchdogGitHubRunner, error)
	HookInstallFn        func(ctx context.Context, opts hook.InstallOptions) hook.InstallResult
	ListRunsFn           func(ctx context.Context, repo string, limit int) ([]WatchdogRun, error)
	PullRequestFn        func(ctx context.Context, repo string, number int) (WatchdogPullRequest, error)
	RunLogFn             func(ctx context.Context, repo string, runID int64) (string, error)
	RerunFn              func(ctx context.Context, repo string, runID int64) error
	ReadFileFn           func(path string) ([]byte, error)
	WriteFileFn          func(path string, data []byte, perm os.FileMode) error
	MkdirAllFn           func(path string, perm os.FileMode) error
	NowFn                func() time.Time
	SleepFn              func(d time.Duration)
}

type WatchdogReport struct {
	Executed     bool            `json:"executed"`
	Repos        []string        `json:"repos"`
	RunnerOnline bool            `json:"runner_online"`
	Metrics      WatchdogMetrics `json:"metrics"`
	Events       []WatchdogEvent `json:"events"`
	Exit         int             `json:"exit"`
}

type WatchdogMetrics struct {
	RunsConsidered  int `json:"runs_considered"`
	RerunsTriggered int `json:"reruns_triggered"`
	RerunsSkipped   int `json:"reruns_skipped"`
}

type WatchdogEvent struct {
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Repo     string `json:"repo,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Runner   string `json:"runner,omitempty"`
	RunID    int64  `json:"run_id,omitempty"`
	HeadSHA  string `json:"head_sha,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Executed bool   `json:"executed"`
	Online   bool   `json:"online,omitempty"`
}

type WatchdogGitHubRunner struct {
	ID     int64    `json:"id,omitempty"`
	Repo   string   `json:"repo"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels,omitempty"`
}

type WatchdogRun struct {
	ID           int64                    `json:"id"`
	HeadSHA      string                   `json:"head_sha"`
	Status       string                   `json:"status"`
	Conclusion   string                   `json:"conclusion"`
	CreatedAt    time.Time                `json:"created_at,omitempty"`
	URL          string                   `json:"url,omitempty"`
	PullRequests []WatchdogPullRequestRef `json:"pull_requests,omitempty"`
}

type WatchdogPullRequestRef struct {
	Number int `json:"number"`
}

type WatchdogPullRequest struct {
	Number         int    `json:"number"`
	State          string `json:"state"`
	MergeableState string `json:"mergeable_state"`
}

type FailureKind string

const (
	FailureUnknown         FailureKind = "unknown"
	FailureNetworkCheckout FailureKind = "network_checkout"
	FailureCode            FailureKind = "code"
	FailureSecret          FailureKind = "secret"
)

type FailureClassification struct {
	Kind      FailureKind `json:"kind"`
	Signature string      `json:"signature,omitempty"`
	Detail    string      `json:"detail,omitempty"`
}

func DefaultWatchdogOptions() WatchdogOptions {
	return WatchdogOptions{
		Execute:        false,
		InferRepos:     true,
		NetworkTimeout: 10 * time.Second,
		RestartDelay:   10 * time.Second,
		IdleProbeDelay: 2 * time.Second,
		MaxRunAge:      DefaultWatchdogMaxRunAge,
		RunLimit:       defaultWatchdogRunLimit,
		MarkerPath:     DefaultWatchdogMarkerPath,
		RunFn:          defaultRun,
		ActivityFn:     idle.DefaultActivities,
		ReadFileFn:     os.ReadFile,
		WriteFileFn:    os.WriteFile,
		MkdirAllFn:     os.MkdirAll,
		NowFn:          time.Now,
		SleepFn:        time.Sleep,
		HookInstallFn:  hook.Install,
	}
}

func Watchdog(ctx context.Context, opts WatchdogOptions) WatchdogReport {
	applyWatchdogDefaults(&opts)
	report := WatchdogReport{Executed: opts.Execute, Exit: 0}
	if err := validateWatchdogOptions(opts); err != nil {
		report.add(WatchdogEvent{Event: "watchdog-invalid", Severity: "critical", Reason: "invalid-options", Detail: err.Error()})
		report.Exit = 2
		return report
	}
	if err := opts.NetworkFn(ctx, opts.NetworkTimeout); err != nil {
		report.add(WatchdogEvent{Event: "network-down", Severity: "warning", Reason: "github-unreachable", Detail: err.Error()})
		report.Exit = 1
		return report
	}

	systemd, err := opts.SystemRunnersFn(ctx)
	if err != nil {
		report.add(WatchdogEvent{Event: "runner-status-unknown", Severity: "warning", Reason: "systemd-list-failed", Detail: err.Error()})
		report.Exit = maxExit(report.Exit, 1)
	}
	systemd = enrichWatchdogSystemdRepos(ctx, opts, systemd)
	repos := append([]string(nil), opts.Repos...)
	if opts.InferRepos && len(repos) == 0 {
		repos = inferWatchdogRepos(systemd)
	}
	report.Repos = repos
	if len(repos) == 0 {
		report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "warning", Reason: "no-repos"})
		report.Exit = maxExit(report.Exit, 1)
		return report
	}

	ghBefore, repoOnline := collectWatchdogGitHubRunners(ctx, opts, repos, &report)
	report.RunnerOnline = anyRepoOnline(repoOnline)

	idleResult := idle.Result{Status: idle.StatusIdle, ExitCode: idle.StatusIdle.ExitCode()}
	if opts.Execute {
		idleResult = idle.Check(ctx, idle.Options{ActivityFn: opts.ActivityFn, ProbeDelay: opts.IdleProbeDelay})
		if idleResult.Status != idle.StatusIdle {
			reason := "host-busy"
			detail := idle.FormatActivities(idleResult.Activities)
			if idleResult.Status == idle.StatusUnknown {
				reason = "host-idle-unknown"
				detail = idleResult.Error
			}
			report.add(WatchdogEvent{Event: "runner-restart-skipped", Severity: "warning", Reason: reason, Detail: detail})
			if opts.RerunNetworkFailures {
				report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "warning", Reason: reason, Detail: detail})
			}
			report.Exit = maxExit(report.Exit, 1)
			return report
		}
	}

	if err := repairWatchdogHooks(ctx, opts, &report); err != nil {
		report.Exit = 2
		return report
	}
	if err := restartWatchdogRunners(ctx, opts, systemd, ghBefore, &report); err != nil {
		report.Exit = 2
		return report
	}

	_, repoOnline = collectWatchdogGitHubRunners(ctx, opts, repos, &report)
	report.RunnerOnline = anyRepoOnline(repoOnline)
	for _, repo := range repos {
		if !repoOnline[repo] {
			report.add(WatchdogEvent{Event: "runner-offline", Severity: "warning", Repo: repo, Reason: "github-not-online"})
			report.Exit = maxExit(report.Exit, 1)
		}
	}

	if !opts.RerunNetworkFailures {
		report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "info", Reason: "disabled"})
		return report
	}
	if err := rerunWatchdogNetworkFailures(ctx, opts, repos, repoOnline, &report); err != nil {
		report.Exit = 2
	}
	return report
}

func repairWatchdogHooks(ctx context.Context, opts WatchdogOptions, report *WatchdogReport) error {
	hookOpts := hook.DefaultInstallOptions()
	hookOpts.Execute = opts.Execute
	hookOpts.RestartRunners = false
	// The watchdog is a light-touch periodic .env repair. The privileged
	// safedelete wrapper + scoped sudoers are a one-time provisioning concern
	// (hook install --execute / bootstrap-everything), not something to re-run
	// (visudo + /etc/sudoers.d rewrite) on every timer tick.
	hookOpts.SkipScopedSudoers = true
	if opts.HooksDir != "" {
		hookOpts.HooksDir = opts.HooksDir
	}
	if opts.RunnerGlob != "" {
		hookOpts.RunnerGlob = opts.RunnerGlob
	}
	if opts.CivmctlPath != "" {
		hookOpts.CivmctlPath = opts.CivmctlPath
	}
	res := opts.HookInstallFn(ctx, hookOpts)
	event := WatchdogEvent{
		Event:    "hooks-repaired",
		Severity: "info",
		Executed: opts.Execute,
		Detail:   fmt.Sprintf("%d runner env file(s)", len(res.RunnerEnvFiles)),
	}
	if res.Error != "" {
		event.Severity = "critical"
		event.Reason = "hook-install-failed"
		event.Detail = res.Error
		report.add(event)
		return errors.New(res.Error)
	}
	report.add(event)
	return nil
}

func restartWatchdogRunners(ctx context.Context, opts WatchdogOptions, systemd []Status, ghByRepo map[string][]WatchdogGitHubRunner, report *WatchdogReport) error {
	candidates := restartCandidates(systemd, ghByRepo)
	for _, unit := range sortedMapKeys(candidates) {
		reason := candidates[unit]
		event := WatchdogEvent{Event: "runner-restarted", Severity: "info", Unit: unit, Reason: reason, Executed: opts.Execute}
		if !opts.Execute {
			report.add(event)
			continue
		}
		if _, err := opts.RunFn(ctx, "sudo", "systemctl", "restart", unit); err != nil {
			event.Severity = "critical"
			event.Detail = fmt.Sprintf("systemctl restart: %v", err)
			report.add(event)
			return err
		}
		opts.SleepFn(opts.RestartDelay)
		out, _ := opts.RunFn(ctx, "systemctl", "is-active", unit)
		if strings.TrimSpace(string(out)) != "active" {
			err := fmt.Errorf("%s is-active=%q", unit, strings.TrimSpace(string(out)))
			event.Severity = "critical"
			event.Detail = err.Error()
			report.add(event)
			return err
		}
		report.add(event)
	}
	return nil
}

func rerunWatchdogNetworkFailures(ctx context.Context, opts WatchdogOptions, repos []string, repoOnline map[string]bool, report *WatchdogReport) error {
	state, err := loadRerunState(opts)
	if err != nil {
		report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "critical", Reason: "marker-read-failed", Detail: err.Error()})
		return err
	}
	mutated := false
	for _, repo := range repos {
		if !repoOnline[repo] {
			report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "warning", Repo: repo, Reason: "runner-not-online"})
			continue
		}
		runs, err := opts.ListRunsFn(ctx, repo, opts.RunLimit)
		if err != nil {
			report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "warning", Repo: repo, Reason: "run-list-failed", Detail: err.Error()})
			report.Exit = maxExit(report.Exit, 1)
			continue
		}
		for _, run := range runs {
			report.Metrics.RunsConsidered++
			if reason := skipRunBeforeLog(opts, repo, run, state); reason != "" {
				report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "info", Repo: repo, RunID: run.ID, HeadSHA: run.HeadSHA, Reason: reason})
				continue
			}
			pr, reason, err := openPullRequestForRun(ctx, opts, repo, run)
			if err != nil {
				report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "warning", Repo: repo, RunID: run.ID, HeadSHA: run.HeadSHA, Reason: "pr-check-failed", Detail: err.Error()})
				report.Exit = maxExit(report.Exit, 1)
				continue
			}
			if reason != "" {
				report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "info", Repo: repo, RunID: run.ID, HeadSHA: run.HeadSHA, Reason: reason})
				continue
			}
			log, err := opts.RunLogFn(ctx, repo, run.ID)
			if err != nil {
				report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "warning", Repo: repo, RunID: run.ID, HeadSHA: run.HeadSHA, Reason: "log-fetch-failed", Detail: err.Error()})
				report.Exit = maxExit(report.Exit, 1)
				continue
			}
			classification := ClassifyFailureLog(log)
			if classification.Kind != FailureNetworkCheckout {
				report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "info", Repo: repo, RunID: run.ID, HeadSHA: run.HeadSHA, Reason: string(classification.Kind), Detail: classification.Detail})
				continue
			}
			event := WatchdogEvent{
				Event:    "rerun-triggered",
				Severity: "info",
				Repo:     repo,
				RunID:    run.ID,
				HeadSHA:  run.HeadSHA,
				Reason:   "network-checkout",
				Detail:   fmt.Sprintf("pr=%d signature=%s", pr.Number, classification.Signature),
				Executed: opts.Execute,
			}
			if opts.Execute {
				if err := opts.RerunFn(ctx, repo, run.ID); err != nil {
					event.Severity = "critical"
					event.Detail = fmt.Sprintf("gh run rerun: %v", err)
					report.add(event)
					return err
				}
				state.Reruns[rerunMarkerKey(run.ID, run.HeadSHA)] = RerunMarker{
					Repo:    repo,
					RunID:   run.ID,
					HeadSHA: run.HeadSHA,
					RerunAt: opts.NowFn().UTC(),
				}
				mutated = true
			}
			report.add(event)
		}
	}
	if mutated {
		if err := writeRerunState(opts, state); err != nil {
			report.add(WatchdogEvent{Event: "rerun-skipped", Severity: "critical", Reason: "marker-write-failed", Detail: err.Error()})
			return err
		}
	}
	return nil
}

func skipRunBeforeLog(opts WatchdogOptions, repo string, run WatchdogRun, state rerunState) string {
	if err := civm.ValidateRepo(repo); err != nil {
		return "repo-invalid"
	}
	if !rerunnableConclusion(run.Conclusion) {
		return "conclusion-not-rerunnable"
	}
	if run.CreatedAt.IsZero() {
		return "run-created-at-missing"
	}
	if opts.NowFn().Sub(run.CreatedAt) > opts.MaxRunAge {
		return "run-too-old"
	}
	if len(run.PullRequests) == 0 {
		return "no-open-pr-association"
	}
	if state.Reruns[rerunMarkerKey(run.ID, run.HeadSHA)].RunID != 0 {
		return "already-rerun"
	}
	return ""
}

func openPullRequestForRun(ctx context.Context, opts WatchdogOptions, repo string, run WatchdogRun) (WatchdogPullRequest, string, error) {
	lastReason := "no-open-pr-association"
	for _, ref := range run.PullRequests {
		if ref.Number <= 0 {
			continue
		}
		pr, err := opts.PullRequestFn(ctx, repo, ref.Number)
		if err != nil {
			return WatchdogPullRequest{}, "", err
		}
		if pr.State != "open" {
			lastReason = "pr-not-open"
			continue
		}
		mergeable := strings.ToLower(strings.TrimSpace(pr.MergeableState))
		if mergeable == "dirty" || mergeable == "conflicting" {
			return pr, "pr-conflicting", nil
		}
		return pr, "", nil
	}
	return WatchdogPullRequest{}, lastReason, nil
}

func ClassifyFailureLog(log string) FailureClassification {
	lower := strings.ToLower(log)
	if sig, idx := firstSignature(lower, secretSignatures); idx >= 0 {
		return FailureClassification{Kind: FailureSecret, Signature: sig, Detail: "secret/auth signature present"}
	}
	sig, networkIdx := firstSignature(lower, networkCheckoutSignatures)
	if networkIdx < 0 {
		if codeSig, idx := firstSignature(lower, codeFailureSignatures); idx >= 0 {
			return FailureClassification{Kind: FailureCode, Signature: codeSig, Detail: "code/test/lint/build signature present"}
		}
		return FailureClassification{Kind: FailureUnknown, Detail: "no network checkout signature"}
	}
	if codeSig, codeIdx := firstSignature(lower, codeFailureSignatures); codeIdx >= 0 && codeIdx < networkIdx {
		return FailureClassification{Kind: FailureCode, Signature: codeSig, Detail: "code/test/lint/build step started before network failure"}
	}
	return FailureClassification{Kind: FailureNetworkCheckout, Signature: sig, Detail: "transient checkout/network signature"}
}

var networkCheckoutSignatures = []string{
	"rpc failed",
	"early eof",
	"invalid index-pack",
	"curl 56",
	"curl 92",
	"gnutls recv error",
	"connection timed out",
	"http/2 cancel",
	"http/2 stream",
	"the remote end hung up unexpectedly",
}

var codeFailureSignatures = []string{
	"run go test",
	"run npm test",
	"run pnpm test",
	"run yarn test",
	"run make test",
	"run go build",
	"run npm run build",
	"run pnpm build",
	"run yarn build",
	"run golangci-lint",
	"run eslint",
	"run npm run lint",
	"run pnpm lint",
	"run yarn lint",
	"run tsc",
	"fail\t",
	"npm err!",
	"eslint",
	"test failed",
	"tests failed",
	"build failed",
	"lint failed",
}

var secretSignatures = []string{
	"bad credentials",
	"authentication failed",
	"could not read username",
	"resource not accessible by integration",
	"permission denied (publickey)",
	"a secret with the name",
	"secret not found",
	"secrets are not passed",
}

func firstSignature(haystack string, signatures []string) (string, int) {
	bestSig := ""
	bestIdx := -1
	for _, sig := range signatures {
		idx := strings.Index(haystack, sig)
		if idx == -1 {
			continue
		}
		if bestIdx == -1 || idx < bestIdx {
			bestIdx = idx
			bestSig = sig
		}
	}
	return bestSig, bestIdx
}

func collectWatchdogGitHubRunners(ctx context.Context, opts WatchdogOptions, repos []string, report *WatchdogReport) (map[string][]WatchdogGitHubRunner, map[string]bool) {
	byRepo := map[string][]WatchdogGitHubRunner{}
	online := map[string]bool{}
	for _, repo := range repos {
		runners, err := opts.GitHubRunnersFn(ctx, repo)
		if err != nil {
			report.add(WatchdogEvent{Event: "runner-online-unknown", Severity: "warning", Repo: repo, Reason: "github-runners-failed", Detail: err.Error()})
			report.Exit = maxExit(report.Exit, 1)
			continue
		}
		byRepo[repo] = runners
		for _, r := range runners {
			if r.Status == "online" && hasWatchdogLabel(r.Labels, "civm") {
				online[repo] = true
				report.add(WatchdogEvent{Event: "runner-online", Severity: "info", Repo: repo, Runner: r.Name, Online: true})
			}
		}
	}
	return byRepo, online
}

func restartCandidates(systemd []Status, ghByRepo map[string][]WatchdogGitHubRunner) map[string]string {
	localByRepoName := map[string]Status{}
	out := map[string]string{}
	for _, s := range systemd {
		localByRepoName[s.Repo+"/"+s.Name] = s
		if s.UnitName == "" {
			continue
		}
		if s.ActiveState != "active" || s.SubState != "running" {
			out[s.UnitName] = "systemd-" + strings.Trim(strings.Join([]string{s.ActiveState, s.SubState}, "-"), "-")
		}
	}
	for repo, runners := range ghByRepo {
		for _, r := range runners {
			if r.Status != "offline" {
				continue
			}
			if local, ok := localByRepoName[repo+"/"+r.Name]; ok && local.UnitName != "" {
				out[local.UnitName] = "github-offline"
			}
		}
	}
	return out
}

func rerunnableConclusion(conclusion string) bool {
	switch conclusion {
	case "failure", "cancelled", "timed_out":
		return true
	default:
		return false
	}
}

type RerunMarker struct {
	Repo    string    `json:"repo"`
	RunID   int64     `json:"run_id"`
	HeadSHA string    `json:"head_sha"`
	RerunAt time.Time `json:"rerun_at"`
}

type rerunState struct {
	Reruns map[string]RerunMarker `json:"reruns"`
}

func loadRerunState(opts WatchdogOptions) (rerunState, error) {
	state := rerunState{Reruns: map[string]RerunMarker{}}
	data, err := opts.ReadFileFn(opts.MarkerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return rerunState{Reruns: map[string]RerunMarker{}}, err
	}
	if state.Reruns == nil {
		state.Reruns = map[string]RerunMarker{}
	}
	return state, nil
}

func writeRerunState(opts WatchdogOptions, state rerunState) error {
	dir := filepath.Dir(opts.MarkerPath)
	if err := opts.MkdirAllFn(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return opts.WriteFileFn(opts.MarkerPath, data, 0644)
}

func rerunMarkerKey(runID int64, headSHA string) string {
	return fmt.Sprintf("%d/%s", runID, headSHA)
}

func applyWatchdogDefaults(opts *WatchdogOptions) {
	defaults := DefaultWatchdogOptions()
	if opts.NetworkTimeout == 0 {
		opts.NetworkTimeout = defaults.NetworkTimeout
	}
	if opts.RestartDelay == 0 {
		opts.RestartDelay = defaults.RestartDelay
	}
	if opts.MaxRunAge == 0 {
		opts.MaxRunAge = defaults.MaxRunAge
	}
	if opts.RunLimit == 0 {
		opts.RunLimit = defaults.RunLimit
	}
	if opts.MarkerPath == "" {
		opts.MarkerPath = defaults.MarkerPath
	}
	if opts.RunFn == nil {
		opts.RunFn = defaults.RunFn
	}
	if opts.ActivityFn == nil {
		opts.ActivityFn = defaults.ActivityFn
	}
	if opts.SystemRunnersFn == nil {
		opts.SystemRunnersFn = func(ctx context.Context) ([]Status, error) {
			listOpts := DefaultListOptions()
			listOpts.RunFn = opts.RunFn
			return List(ctx, listOpts)
		}
	}
	if opts.GitHubRunnersFn == nil {
		opts.GitHubRunnersFn = func(ctx context.Context, repo string) ([]WatchdogGitHubRunner, error) {
			return listWatchdogGitHubRunners(ctx, repo, opts.RunFn)
		}
	}
	if opts.HookInstallFn == nil {
		opts.HookInstallFn = defaults.HookInstallFn
	}
	if opts.ListRunsFn == nil {
		opts.ListRunsFn = func(ctx context.Context, repo string, limit int) ([]WatchdogRun, error) {
			return listWatchdogRuns(ctx, repo, limit, opts.RunFn)
		}
	}
	if opts.PullRequestFn == nil {
		opts.PullRequestFn = func(ctx context.Context, repo string, number int) (WatchdogPullRequest, error) {
			return getWatchdogPullRequest(ctx, repo, number, opts.RunFn)
		}
	}
	if opts.RunLogFn == nil {
		opts.RunLogFn = func(ctx context.Context, repo string, runID int64) (string, error) {
			return getWatchdogRunLog(ctx, repo, runID, opts.RunFn)
		}
	}
	if opts.RerunFn == nil {
		opts.RerunFn = func(ctx context.Context, repo string, runID int64) error {
			_, err := opts.RunFn(ctx, "gh", "run", "rerun", strconv.FormatInt(runID, 10), "--repo", repo, "--failed")
			return err
		}
	}
	if opts.ReadFileFn == nil {
		opts.ReadFileFn = defaults.ReadFileFn
	}
	if opts.WriteFileFn == nil {
		opts.WriteFileFn = defaults.WriteFileFn
	}
	if opts.MkdirAllFn == nil {
		opts.MkdirAllFn = defaults.MkdirAllFn
	}
	if opts.NowFn == nil {
		opts.NowFn = defaults.NowFn
	}
	if opts.SleepFn == nil {
		opts.SleepFn = defaults.SleepFn
	}
	if opts.NetworkFn == nil {
		opts.NetworkFn = func(ctx context.Context, timeout time.Duration) error {
			networkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			_, err := opts.RunFn(networkCtx, "git", "ls-remote", "https://github.com/actions/checkout.git", "HEAD")
			if err != nil {
				return fmt.Errorf("git ls-remote github.com: %w", err)
			}
			return nil
		}
	}
}

func validateWatchdogOptions(opts WatchdogOptions) error {
	for _, repo := range opts.Repos {
		if err := civm.ValidateRepo(repo); err != nil {
			return err
		}
	}
	if opts.NetworkTimeout <= 0 {
		return fmt.Errorf("--network-timeout deve ser >0")
	}
	if opts.RestartDelay < 0 {
		return fmt.Errorf("--restart-delay deve ser >=0")
	}
	if opts.MaxRunAge <= 0 {
		return fmt.Errorf("--max-run-age deve ser >0")
	}
	if opts.RunLimit < 1 || opts.RunLimit > 100 {
		return fmt.Errorf("run limit deve ficar entre 1 e 100")
	}
	if strings.ContainsRune(opts.MarkerPath, 0) || !filepath.IsAbs(filepath.Clean(opts.MarkerPath)) {
		return fmt.Errorf("marker path deve ser absoluto e seguro")
	}
	return nil
}

func listWatchdogGitHubRunners(ctx context.Context, repo string, runFn func(context.Context, string, ...string) ([]byte, error)) ([]WatchdogGitHubRunner, error) {
	out, err := runFn(ctx, "gh", "api", fmt.Sprintf("/repos/%s/actions/runners", repo))
	if err != nil {
		return nil, fmt.Errorf("gh api actions/runners: %w", err)
	}
	var raw struct {
		Runners []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Busy   bool   `json:"busy"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"runners"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse gh runners: %w", err)
	}
	runners := make([]WatchdogGitHubRunner, 0, len(raw.Runners))
	for _, rr := range raw.Runners {
		item := WatchdogGitHubRunner{ID: rr.ID, Repo: repo, Name: rr.Name, Status: rr.Status, Busy: rr.Busy}
		for _, label := range rr.Labels {
			item.Labels = append(item.Labels, label.Name)
		}
		runners = append(runners, item)
	}
	return runners, nil
}

func listWatchdogRuns(ctx context.Context, repo string, limit int, runFn func(context.Context, string, ...string) ([]byte, error)) ([]WatchdogRun, error) {
	endpoint := fmt.Sprintf("/repos/%s/actions/runs?per_page=%d&status=completed", repo, limit)
	out, err := runFn(ctx, "gh", "api", endpoint)
	if err != nil {
		return nil, fmt.Errorf("gh api actions/runs: %w", err)
	}
	var raw struct {
		WorkflowRuns []struct {
			ID           int64     `json:"id"`
			HeadSHA      string    `json:"head_sha"`
			Status       string    `json:"status"`
			Conclusion   string    `json:"conclusion"`
			CreatedAt    time.Time `json:"created_at"`
			URL          string    `json:"html_url"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse gh runs: %w", err)
	}
	runs := make([]WatchdogRun, 0, len(raw.WorkflowRuns))
	for _, rr := range raw.WorkflowRuns {
		run := WatchdogRun{
			ID:         rr.ID,
			HeadSHA:    rr.HeadSHA,
			Status:     rr.Status,
			Conclusion: rr.Conclusion,
			CreatedAt:  rr.CreatedAt,
			URL:        rr.URL,
		}
		for _, pr := range rr.PullRequests {
			run.PullRequests = append(run.PullRequests, WatchdogPullRequestRef{Number: pr.Number})
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func getWatchdogPullRequest(ctx context.Context, repo string, number int, runFn func(context.Context, string, ...string) ([]byte, error)) (WatchdogPullRequest, error) {
	out, err := runFn(ctx, "gh", "api", fmt.Sprintf("/repos/%s/pulls/%d", repo, number))
	if err != nil {
		return WatchdogPullRequest{}, fmt.Errorf("gh api pulls/%d: %w", number, err)
	}
	var raw struct {
		Number         int    `json:"number"`
		State          string `json:"state"`
		MergeableState string `json:"mergeable_state"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return WatchdogPullRequest{}, fmt.Errorf("parse gh pull: %w", err)
	}
	return WatchdogPullRequest{Number: raw.Number, State: raw.State, MergeableState: raw.MergeableState}, nil
}

func getWatchdogRunLog(ctx context.Context, repo string, runID int64, runFn func(context.Context, string, ...string) ([]byte, error)) (string, error) {
	out, err := runFn(ctx, "gh", "run", "view", strconv.FormatInt(runID, 10), "--repo", repo, "--log-failed")
	if err != nil {
		return "", fmt.Errorf("gh run view --log-failed: %w", err)
	}
	return string(out), nil
}

func inferWatchdogRepos(systemd []Status) []string {
	seen := map[string]bool{}
	var repos []string
	for _, status := range systemd {
		if status.Repo == "" || seen[status.Repo] || civm.ValidateRepo(status.Repo) != nil {
			continue
		}
		seen[status.Repo] = true
		repos = append(repos, status.Repo)
	}
	sort.Strings(repos)
	return repos
}

func enrichWatchdogSystemdRepos(ctx context.Context, opts WatchdogOptions, systemd []Status) []Status {
	out := append([]Status(nil), systemd...)
	for i := range out {
		repo, ok := resolveWatchdogRunnerRepo(ctx, opts, out[i])
		if ok {
			out[i].Repo = repo
		}
	}
	return out
}

func resolveWatchdogRunnerRepo(ctx context.Context, opts WatchdogOptions, status Status) (string, bool) {
	if status.UnitName == "" {
		return "", false
	}
	if err := civm.ValidateServiceUnit(status.UnitName); err != nil {
		return "", false
	}
	out, err := opts.RunFn(ctx, "systemctl", "show", status.UnitName, "--property=WorkingDirectory", "--value")
	if err != nil {
		return "", false
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || dir == "-" {
		return "", false
	}
	data, err := opts.ReadFileFn(filepath.Join(dir, ".runner"))
	if err != nil {
		return "", false
	}
	return repoFromRunnerConfig(data)
}

func repoFromRunnerConfig(data []byte) (string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false
	}
	for _, field := range []string{"gitHubUrl", "serverUrl"} {
		valueRaw, ok := raw[field]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(valueRaw, &value); err != nil {
			continue
		}
		if repo, ok := repoFromGitHubURL(value); ok {
			return repo, true
		}
	}
	return "", false
}

func repoFromGitHubURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	repoName := strings.TrimSuffix(parts[1], ".git")
	repo := parts[0] + "/" + repoName
	if civm.ValidateRepo(repo) != nil {
		return "", false
	}
	return repo, true
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func anyRepoOnline(repoOnline map[string]bool) bool {
	for _, online := range repoOnline {
		if online {
			return true
		}
	}
	return false
}

func hasWatchdogLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func maxExit(current, candidate int) int {
	if candidate > current {
		return candidate
	}
	return current
}

func (r *WatchdogReport) add(event WatchdogEvent) {
	if event.Severity == "" {
		event.Severity = "info"
	}
	switch event.Event {
	case "rerun-skipped":
		r.Metrics.RerunsSkipped++
	case "rerun-triggered":
		r.Metrics.RerunsTriggered++
	}
	r.Events = append(r.Events, event)
}

func (r WatchdogReport) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r WatchdogReport) Render(w io.Writer) {
	mode := "DRY-RUN"
	if r.Executed {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "civmctl runner watchdog: %s | exit=%d | runner_online=%v\n", mode, r.Exit, r.RunnerOnline)
	fmt.Fprintf(w, "Rerun metrics: runs_considered=%d reruns_triggered=%d reruns_skipped=%d\n",
		r.Metrics.RunsConsidered, r.Metrics.RerunsTriggered, r.Metrics.RerunsSkipped)
	if len(r.Repos) > 0 {
		fmt.Fprintf(w, "Repos: %s\n", strings.Join(r.Repos, ","))
	}
	for _, event := range r.Events {
		target := event.Repo
		if event.Unit != "" {
			target = event.Unit
		}
		if target == "" && event.Runner != "" {
			target = event.Runner
		}
		if target == "" {
			target = "-"
		}
		detail := event.Detail
		if event.Reason != "" {
			detail = event.Reason
			if event.Detail != "" {
				detail += ": " + event.Detail
			}
		}
		if event.RunID != 0 {
			target = fmt.Sprintf("%s run=%d", target, event.RunID)
		}
		fmt.Fprintf(w, "  %-18s %-8s %-64s %s\n", event.Event, event.Severity, target, detail)
	}
}
