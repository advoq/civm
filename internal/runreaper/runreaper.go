// Package runreaper cancels GitHub Actions workflow runs that are still
// queued / in_progress for pull requests that are no longer open. On a shared
// self-hosted fleet a closed or merged PR leaves its runs waiting for the
// runner forever (GitHub does not auto-cancel them), starving the runs of the
// PRs that are still open. The reaper is the durable, periodic answer: it keeps
// the runner queue scoped to work that still matters.
//
// Safety rails:
//   - Only event=pull_request / pull_request_target runs are ever cancelled —
//     push (main CI), schedule (crons) and workflow_dispatch are never touched.
//   - A run is reaped only when its head branch has NO open PR in that repo.
//   - Dry-run by default (Execute=false only reports candidates).
//   - Per-repo cancel cap (anti-runaway); the excess is logged, never silent.
//
// Stdlib-only; all GitHub access goes through injected funcs so the logic is
// hermetically testable.
package runreaper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/advoq/civm/internal/civm"
)

// DefaultMaxCancelPerRepo bounds how many runs a single tick may cancel per
// repo. A real backlog of hundreds is handled across successive ticks; the cap
// is a blast-radius guard against a logic bug, not a throughput limit.
const DefaultMaxCancelPerRepo = 400

// Run is a queued or in_progress workflow run considered for reaping.
type Run struct {
	ID        int64     `json:"id"`
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	Event     string    `json:"event"`
	Status    string    `json:"status"`
	Workflow  string    `json:"workflow"`
	CreatedAt time.Time `json:"created_at"`
}

// Options configures Reap. The Fn fields are injected in tests; production
// defaults are wired by applyDefaults.
type Options struct {
	Repos             []string
	Execute           bool
	MaxCancelPerRepo  int
	RunFn             func(ctx context.Context, name string, args ...string) ([]byte, error)
	OpenBranchesFn    func(ctx context.Context, repo string) (map[string]bool, error)
	ActiveRunsFn      func(ctx context.Context, repo string) ([]Run, error)
	CancelFn          func(ctx context.Context, repo string, runID int64) error
	NowFn             func() time.Time
}

// Event is one structured line in the report.
type Event struct {
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Repo     string `json:"repo,omitempty"`
	RunID    int64  `json:"run_id,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Workflow string `json:"workflow,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Executed bool   `json:"executed"`
}

// Report is the result of a reap pass.
type Report struct {
	Executed   bool     `json:"executed"`
	Repos      []string `json:"repos"`
	Scanned    int      `json:"scanned"`
	Candidates int      `json:"candidates"`
	Cancelled  int      `json:"cancelled"`
	Events     []Event  `json:"events"`
	Exit       int      `json:"exit"`
}

// Reap scans the configured repos and cancels (or, in dry-run, reports) every
// queued/in_progress pull_request run whose head branch has no open PR.
func Reap(ctx context.Context, opts Options) Report {
	applyDefaults(&opts)
	report := Report{Executed: opts.Execute}
	if len(opts.Repos) == 0 {
		report.add(Event{Event: "reap-skipped", Severity: "warning", Reason: "no-repos"})
		report.Exit = 1
		return report
	}
	for _, repo := range opts.Repos {
		if err := civm.ValidateRepo(repo); err != nil {
			report.add(Event{Event: "reap-skipped", Severity: "warning", Repo: repo, Reason: "repo-invalid", Detail: err.Error()})
			report.Exit = maxInt(report.Exit, 1)
			continue
		}
		report.Repos = append(report.Repos, repo)
		reapRepo(ctx, opts, repo, &report)
	}
	return report
}

func reapRepo(ctx context.Context, opts Options, repo string, report *Report) {
	open, err := opts.OpenBranchesFn(ctx, repo)
	if err != nil {
		report.add(Event{Event: "reap-skipped", Severity: "warning", Repo: repo, Reason: "open-prs-failed", Detail: err.Error()})
		report.Exit = maxInt(report.Exit, 1)
		return
	}
	runs, err := opts.ActiveRunsFn(ctx, repo)
	if err != nil {
		report.add(Event{Event: "reap-skipped", Severity: "warning", Repo: repo, Reason: "active-runs-failed", Detail: err.Error()})
		report.Exit = maxInt(report.Exit, 1)
		return
	}
	report.Scanned += len(runs)

	candidates := make([]Run, 0, len(runs))
	for _, run := range runs {
		if !isPullRequestEvent(run.Event) {
			continue // never touch push / schedule / workflow_dispatch
		}
		if run.Branch == "" || open[run.Branch] {
			continue // branch still has an open PR (or unknown) → keep
		}
		candidates = append(candidates, run)
	}
	// Oldest first: those are the ones GitHub hands the runner next, so
	// cancelling them first frees the queue head soonest.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CreatedAt.Before(candidates[j].CreatedAt) })

	for i, run := range candidates {
		report.Candidates++
		ev := Event{Event: "run-reaped", Severity: "info", Repo: repo, RunID: run.ID, Branch: run.Branch, Workflow: run.Workflow, Reason: "pr-not-open", Executed: opts.Execute}
		if i >= opts.MaxCancelPerRepo {
			report.add(Event{Event: "reap-capped", Severity: "warning", Repo: repo, Reason: "max-cancel-reached", Detail: fmt.Sprintf("%d candidates, cap %d — remainder deferred to next tick", len(candidates), opts.MaxCancelPerRepo)})
			break
		}
		if !opts.Execute {
			report.add(ev)
			continue
		}
		if err := opts.CancelFn(ctx, repo, run.ID); err != nil {
			ev.Severity = "warning"
			ev.Reason = "cancel-failed"
			ev.Detail = err.Error()
			report.add(ev)
			report.Exit = maxInt(report.Exit, 1)
			continue
		}
		report.Cancelled++
		report.add(ev)
	}
}

// isPullRequestEvent gates reaping to PR-triggered runs only.
func isPullRequestEvent(event string) bool {
	switch event {
	case "pull_request", "pull_request_target":
		return true
	default:
		return false
	}
}

func applyDefaults(opts *Options) {
	if opts.MaxCancelPerRepo <= 0 {
		opts.MaxCancelPerRepo = DefaultMaxCancelPerRepo
	}
	if opts.NowFn == nil {
		opts.NowFn = time.Now
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.OpenBranchesFn == nil {
		opts.OpenBranchesFn = func(ctx context.Context, repo string) (map[string]bool, error) {
			return listOpenBranches(ctx, repo, opts.RunFn)
		}
	}
	if opts.ActiveRunsFn == nil {
		opts.ActiveRunsFn = func(ctx context.Context, repo string) ([]Run, error) {
			return listActiveRuns(ctx, repo, opts.RunFn)
		}
	}
	if opts.CancelFn == nil {
		opts.CancelFn = func(ctx context.Context, repo string, runID int64) error {
			return cancelRun(ctx, repo, runID, opts.RunFn)
		}
	}
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func listOpenBranches(ctx context.Context, repo string, runFn func(context.Context, string, ...string) ([]byte, error)) (map[string]bool, error) {
	out, err := runFn(ctx, "gh", "pr", "list", "--repo", repo, "--state", "open", "--limit", "1000", "--json", "headRefName")
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []struct {
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	open := make(map[string]bool, len(prs))
	for _, pr := range prs {
		if pr.HeadRefName != "" {
			open[pr.HeadRefName] = true
		}
	}
	return open, nil
}

func listActiveRuns(ctx context.Context, repo string, runFn func(context.Context, string, ...string) ([]byte, error)) ([]Run, error) {
	var all []Run
	for _, status := range []string{"queued", "in_progress"} {
		endpoint := fmt.Sprintf("/repos/%s/actions/runs?status=%s&per_page=100", repo, status)
		out, err := runFn(ctx, "gh", "api", "--paginate", endpoint)
		if err != nil {
			return nil, fmt.Errorf("gh api actions/runs?status=%s: %w", status, err)
		}
		runs, err := parseActiveRuns(out, repo)
		if err != nil {
			return nil, err
		}
		all = append(all, runs...)
	}
	return all, nil
}

// parseActiveRuns decodes one or more concatenated `actions/runs` JSON objects
// (gh --paginate emits one object per page) into Run values.
func parseActiveRuns(out []byte, repo string) ([]Run, error) {
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var runs []Run
	for {
		var page struct {
			WorkflowRuns []struct {
				ID         int64     `json:"id"`
				HeadBranch string    `json:"head_branch"`
				Event      string    `json:"event"`
				Status     string    `json:"status"`
				Name       string    `json:"name"`
				CreatedAt  time.Time `json:"created_at"`
			} `json:"workflow_runs"`
		}
		if err := dec.Decode(&page); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse gh runs: %w", err)
		}
		for _, r := range page.WorkflowRuns {
			runs = append(runs, Run{
				ID:        r.ID,
				Repo:      repo,
				Branch:    r.HeadBranch,
				Event:     r.Event,
				Status:    r.Status,
				Workflow:  r.Name,
				CreatedAt: r.CreatedAt,
			})
		}
	}
	return runs, nil
}

// cancelRun stops a workflow run. It tries force-cancel FIRST: a plain
// POST .../cancel is accepted (202) but NOT applied to a run stuck queued
// waiting for a busy self-hosted runner — GitHub only delivers that
// cancellation when the runner finally picks the job, so closed-PR runs linger
// in the queue indefinitely. POST .../force-cancel dislodges them immediately.
// GitHub may reject force-cancel on a run that has not yet been regular-cancelled
// (or too soon after); in that case fall back to the plain cancel so the next
// reaper tick can force-cancel it. Returns nil if either call is accepted.
func cancelRun(ctx context.Context, repo string, runID int64, runFn func(context.Context, string, ...string) ([]byte, error)) error {
	id := strconv.FormatInt(runID, 10)
	force := fmt.Sprintf("/repos/%s/actions/runs/%s/force-cancel", repo, id)
	if _, err := runFn(ctx, "gh", "api", "-X", "POST", force, "--silent"); err == nil {
		return nil
	}
	normal := fmt.Sprintf("/repos/%s/actions/runs/%s/cancel", repo, id)
	if _, err := runFn(ctx, "gh", "api", "-X", "POST", normal, "--silent"); err != nil {
		return fmt.Errorf("gh api cancel/force-cancel run %s: %w", id, err)
	}
	return nil
}

func maxInt(a, b int) int {
	if b > a {
		return b
	}
	return a
}

func (r *Report) add(ev Event) {
	if ev.Severity == "" {
		ev.Severity = "info"
	}
	r.Events = append(r.Events, ev)
}

// RenderJSON writes the report as indented JSON.
func (r Report) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Render writes a human-readable summary.
func (r Report) Render(w io.Writer) {
	mode := "DRY-RUN"
	if r.Executed {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "civmctl reap-runs: %s | exit=%d | scanned=%d candidates=%d cancelled=%d\n", mode, r.Exit, r.Scanned, r.Candidates, r.Cancelled)
	if len(r.Repos) > 0 {
		fmt.Fprintf(w, "Repos: %s\n", strings.Join(r.Repos, ","))
	}
	for _, ev := range r.Events {
		target := ev.Repo
		if ev.RunID != 0 {
			target = fmt.Sprintf("%s run=%d", target, ev.RunID)
		}
		detail := ev.Reason
		if ev.Detail != "" {
			detail += ": " + ev.Detail
		}
		if ev.Branch != "" {
			detail = fmt.Sprintf("%s [%s]", detail, ev.Branch)
		}
		fmt.Fprintf(w, "  %-14s %-8s %-40s %s\n", ev.Event, ev.Severity, target, detail)
	}
}
