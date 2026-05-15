// Package doctor consolidates read-only civm host and GitHub runner state.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/advoq/civm/internal/civm"
	"github.com/advoq/civm/internal/health"
	"github.com/advoq/civm/internal/runner"
)

var DefaultRepos = []string{
	"advoq/civm",
	"emersonbusson/compexhub",
	"emersonbusson/vitae",
	"advoq/advoq",
}

type Severity string

const (
	SeverityOK       Severity = "ok"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) ExitCode() int {
	switch s {
	case SeverityOK:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

type HostCheck struct {
	Name     string `json:"name"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"`
}

type SystemdRunner struct {
	UnitName    string `json:"unit_name"`
	Repo        string `json:"repo"`
	Name        string `json:"name"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
}

type GitHubRunner struct {
	ID     int64    `json:"id,omitempty"`
	Repo   string   `json:"repo"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels"`
}

type RunnerDiagnosis struct {
	ID                  int64    `json:"id,omitempty"`
	Repo                string   `json:"repo"`
	Name                string   `json:"name"`
	Status              string   `json:"status"`
	Busy                bool     `json:"busy"`
	Labels              []string `json:"labels,omitempty"`
	Classification      string   `json:"classification"`
	Severity            Severity `json:"severity"`
	Detail              string   `json:"detail"`
	ManualRemoveCommand string   `json:"manual_remove_command,omitempty"`
}

type RepoDiagnosis struct {
	Repo     string            `json:"repo"`
	Severity Severity          `json:"severity"`
	Runners  []RunnerDiagnosis `json:"runners"`
}

type Report struct {
	WorkflowFile   string          `json:"workflow_file"`
	HostChecks     []HostCheck     `json:"host_checks"`
	SystemdRunners []SystemdRunner `json:"systemd_runners"`
	GitHubRepos    []RepoDiagnosis `json:"github_repos"`
	Exit           int             `json:"exit"`
}

type Options struct {
	Repos        []string
	WorkflowFile string
	WorkDir      string

	HealthFn        func(ctx context.Context) health.Report
	SystemRunnersFn func(ctx context.Context) ([]runner.Status, error)
	GitHubRunnersFn func(ctx context.Context, repo string) ([]GitHubRunner, error)
	RunFn           func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func DefaultOptions() Options {
	return Options{
		Repos:        append([]string(nil), DefaultRepos...),
		WorkflowFile: "ci.yml",
		WorkDir:      civm.DefaultHealthDiskPath,
		RunFn:        defaultRun,
	}
}

func Collect(ctx context.Context, opts Options) (Report, error) {
	applyDefaults(&opts)
	if err := validateOptions(opts); err != nil {
		return Report{}, err
	}
	report := Report{
		WorkflowFile:   opts.WorkflowFile,
		HostChecks:     []HostCheck{},
		SystemdRunners: []SystemdRunner{},
		GitHubRepos:    []RepoDiagnosis{},
	}

	host := opts.HealthFn(ctx)
	worst := SeverityOK
	for _, c := range host.Checks {
		sev := severityFromHealth(c.Status)
		report.HostChecks = append(report.HostChecks, HostCheck{
			Name:     c.Name,
			Detail:   c.Detail,
			Severity: string(sev),
		})
		worst = maxSeverity(worst, sev)
	}

	systemd, err := opts.SystemRunnersFn(ctx)
	if err != nil {
		worst = maxSeverity(worst, SeverityWarning)
		report.SystemdRunners = append(report.SystemdRunners, SystemdRunner{
			UnitName:    "(unknown)",
			ActiveState: "unknown",
			SubState:    err.Error(),
		})
	} else {
		for _, r := range systemd {
			report.SystemdRunners = append(report.SystemdRunners, SystemdRunner{
				UnitName: r.UnitName, Repo: r.Repo, Name: r.Name,
				ActiveState: r.ActiveState, SubState: r.SubState,
			})
		}
	}

	for _, repo := range opts.Repos {
		runners, err := opts.GitHubRunnersFn(ctx, repo)
		diag := ClassifyRepo(repo, runners, err)
		report.GitHubRepos = append(report.GitHubRepos, diag)
		worst = maxSeverity(worst, diag.Severity)
	}

	report.Exit = worst.ExitCode()
	return report, nil
}

func applyDefaults(opts *Options) {
	if len(opts.Repos) == 0 {
		opts.Repos = append([]string(nil), DefaultRepos...)
	}
	if opts.WorkflowFile == "" {
		opts.WorkflowFile = "ci.yml"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = civm.DefaultHealthDiskPath
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.HealthFn == nil {
		opts.HealthFn = func(ctx context.Context) health.Report {
			return health.NewDefaultCollector(opts.WorkDir).Collect(ctx)
		}
	}
	if opts.SystemRunnersFn == nil {
		opts.SystemRunnersFn = func(ctx context.Context) ([]runner.Status, error) {
			listOpts := runner.DefaultListOptions()
			listOpts.RunFn = opts.RunFn
			return runner.List(ctx, listOpts)
		}
	}
	if opts.GitHubRunnersFn == nil {
		opts.GitHubRunnersFn = func(ctx context.Context, repo string) ([]GitHubRunner, error) {
			return listGitHubRunners(ctx, repo, opts.RunFn)
		}
	}
}

func validateOptions(opts Options) error {
	for _, repo := range opts.Repos {
		if err := civm.ValidateRepo(repo); err != nil {
			return err
		}
	}
	return civm.ValidateWorkflowFile(opts.WorkflowFile)
}

func ClassifyRepo(repo string, runners []GitHubRunner, err error) RepoDiagnosis {
	out := RepoDiagnosis{Repo: repo, Severity: SeverityOK, Runners: []RunnerDiagnosis{}}
	if err != nil {
		out.Severity = SeverityWarning
		out.Runners = append(out.Runners, RunnerDiagnosis{
			Repo: repo, Name: "(unknown)", Status: "unknown",
			Classification: "unknown", Severity: SeverityWarning,
			Detail: fmt.Sprintf("nao foi possivel consultar GitHub runners: %v", err),
		})
		return out
	}
	if len(runners) == 0 {
		out.Severity = SeverityWarning
		out.Runners = append(out.Runners, RunnerDiagnosis{
			Repo: repo, Name: "(none)", Status: "missing",
			Classification: "missing", Severity: SeverityWarning,
			Detail: "nenhum runner GitHub registrado para este repo",
		})
		return out
	}
	for _, r := range runners {
		r.Repo = repo
		diag := ClassifyRunner(r)
		out.Runners = append(out.Runners, diag)
		out.Severity = maxSeverity(out.Severity, diag.Severity)
	}
	return out
}

func ClassifyRunner(r GitHubRunner) RunnerDiagnosis {
	diag := RunnerDiagnosis{
		ID: r.ID, Repo: r.Repo, Name: r.Name, Status: r.Status,
		Busy: r.Busy, Labels: append([]string(nil), r.Labels...),
		Severity: SeverityWarning,
	}
	hasCivm := hasLabel(r.Labels, "civm")
	switch {
	case r.Status == "online" && strings.HasPrefix(r.Name, "civm-") && hasCivm:
		diag.Classification = "canonical"
		diag.Severity = SeverityOK
		diag.Detail = "runner civm canonico online"
	case r.Status == "offline" && strings.HasPrefix(r.Name, "vitae-ci-"):
		diag.Classification = "legacy_stale"
		diag.Detail = "runner legacy vitae-ci offline; remover manualmente depois de confirmar que nao e usado"
		if r.ID != 0 && r.Repo != "" {
			diag.ManualRemoveCommand = fmt.Sprintf("gh api -X DELETE /repos/%s/actions/runners/%d", r.Repo, r.ID)
		}
	case r.Status == "online" && !hasCivm:
		diag.Classification = "ambiguous"
		diag.Detail = "runner online sem label civm; jobs runs-on [self-hosted, civm] nao devem depender dele"
	case r.Status == "online" && hasCivm:
		diag.Classification = "compatible"
		diag.Severity = SeverityOK
		diag.Detail = "runner online com label civm"
	case r.Status == "offline":
		diag.Classification = "offline"
		diag.Detail = "runner offline reportado; civmctl nao remove automaticamente"
	default:
		diag.Classification = "unknown"
		diag.Detail = "estado de runner nao reconhecido"
	}
	if r.Busy {
		diag.Severity = maxSeverity(diag.Severity, SeverityWarning)
		diag.Detail += "; busy=true"
	}
	return diag
}

func listGitHubRunners(ctx context.Context, repo string, runFn func(context.Context, string, ...string) ([]byte, error)) ([]GitHubRunner, error) {
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
	runners := make([]GitHubRunner, 0, len(raw.Runners))
	for _, rr := range raw.Runners {
		item := GitHubRunner{ID: rr.ID, Repo: repo, Name: rr.Name, Status: rr.Status, Busy: rr.Busy}
		for _, label := range rr.Labels {
			item.Labels = append(item.Labels, label.Name)
		}
		runners = append(runners, item)
	}
	return runners, nil
}

func (r Report) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r Report) Render(w io.Writer) {
	fmt.Fprintf(w, "civm doctor | workflow=%s | exit=%d\n\n", r.WorkflowFile, r.Exit)
	fmt.Fprintln(w, "HOST")
	fmt.Fprintf(w, "%-16s %-10s %s\n", "CHECK", "SEVERITY", "DETAIL")
	for _, c := range r.HostChecks {
		fmt.Fprintf(w, "%-16s %-10s %s\n", c.Name, c.Severity, c.Detail)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "SYSTEMD RUNNERS")
	if len(r.SystemdRunners) == 0 {
		fmt.Fprintln(w, "  nenhum actions.runner.* encontrado")
	} else {
		fmt.Fprintf(w, "%-54s %-22s %-10s %s\n", "UNIT", "REPO", "STATE", "NAME")
		for _, s := range r.SystemdRunners {
			state := s.ActiveState
			if s.SubState != "" {
				state += "/" + s.SubState
			}
			fmt.Fprintf(w, "%-54s %-22s %-10s %s\n", truncate(s.UnitName, 54), truncate(s.Repo, 22), state, s.Name)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "GITHUB RUNNERS")
	fmt.Fprintf(w, "%-24s %-18s %-10s %-5s %-14s %-9s %s\n", "REPO", "NAME", "STATUS", "BUSY", "CLASS", "SEVERITY", "LABELS")
	var manual []string
	for _, repo := range r.GitHubRepos {
		for _, gr := range repo.Runners {
			fmt.Fprintf(w, "%-24s %-18s %-10s %-5t %-14s %-9s %s\n",
				truncate(repo.Repo, 24), truncate(gr.Name, 18), gr.Status, gr.Busy,
				gr.Classification, gr.Severity, strings.Join(gr.Labels, ","))
			if gr.ManualRemoveCommand != "" {
				manual = append(manual, gr.ManualRemoveCommand+" # "+gr.Name)
			}
		}
	}
	if len(manual) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "LEGACY OFFLINE CLEANUP (manual; civmctl nao executa)")
		for _, cmd := range manual {
			fmt.Fprintf(w, "  %s\n", cmd)
		}
	}
}

func severityFromHealth(s health.Status) Severity {
	switch s {
	case health.StatusOK:
		return SeverityOK
	case health.StatusWarn:
		return SeverityWarning
	default:
		return SeverityCritical
	}
}

func maxSeverity(a, b Severity) Severity {
	if b.ExitCode() > a.ExitCode() {
		return b
	}
	return a
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 2 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
