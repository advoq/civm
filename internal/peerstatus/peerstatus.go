// Package peerstatus consolida billing-status + runner online + last
// workflow run em uma view única por peer-repo. Útil pra dashboards
// rápidos sem invocar 3 ferramentas separadas. Stdlib-only.
package peerstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/emersonbusson/ci-vm/internal/billing"
)

// Status agrega informação operacional de um peer-repo.
type Status struct {
	Repo            string             `json:"repo"`
	WorkflowFile    string             `json:"workflow_file"`
	BillingStatus   string             `json:"billing_status"`   // ok/blocked/unknown
	BillingExitCode int                `json:"billing_exit_code"`
	RunnersTotal    int                `json:"runners_total"`
	RunnersOnline   int                `json:"runners_online"`
	RunnerNames     []string           `json:"runner_names"`
	LastRun         *RunSummary        `json:"last_run,omitempty"`
}

// RunSummary é o último workflow run.
type RunSummary struct {
	DatabaseID int64     `json:"database_id"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
	URL        string    `json:"url"`
}

// Options control the check.
type Options struct {
	Repo         string
	WorkflowFile string // ex: "ci.yml"
	RunFn        func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultOptions returns sane defaults.
func DefaultOptions() Options {
	return Options{
		WorkflowFile: "ci.yml",
		RunFn:        defaultRun,
	}
}

// Collect consolida billing + runners + last run para o peer-repo opts.Repo.
func Collect(ctx context.Context, opts Options) (Status, error) {
	if err := validateOptions(opts); err != nil {
		return Status{}, err
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	s := Status{Repo: opts.Repo, WorkflowFile: opts.WorkflowFile}

	// 1. billing-status (delega ao package billing)
	bOpts := billing.DefaultOptions()
	bOpts.Repo = opts.Repo
	bOpts.WorkflowFile = opts.WorkflowFile
	bOpts.RunFn = opts.RunFn
	bStatus, _, _ := billing.Detect(ctx, bOpts)
	s.BillingStatus = string(bStatus)
	s.BillingExitCode = bStatus.ExitCode()

	// 2. runners online (gh api)
	type runnerJSON struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	type runnersResp struct {
		Runners []runnerJSON `json:"runners"`
	}
	out, err := opts.RunFn(ctx, "gh", "api",
		fmt.Sprintf("/repos/%s/actions/runners", opts.Repo))
	if err == nil {
		var rr runnersResp
		if json.Unmarshal(out, &rr) == nil {
			s.RunnersTotal = len(rr.Runners)
			for _, r := range rr.Runners {
				s.RunnerNames = append(s.RunnerNames, r.Name)
				if r.Status == "online" {
					s.RunnersOnline++
				}
			}
		}
	}

	// 3. last workflow run
	type runJSON struct {
		DatabaseID int64     `json:"databaseId"`
		Status     string    `json:"status"`
		Conclusion string    `json:"conclusion"`
		CreatedAt  time.Time `json:"createdAt"`
		URL        string    `json:"url"`
	}
	out, err = opts.RunFn(ctx, "gh", "run", "list",
		"--repo", opts.Repo,
		"--workflow", opts.WorkflowFile,
		"--limit", "1",
		"--json", "databaseId,status,conclusion,createdAt,url")
	if err == nil {
		var runs []runJSON
		if json.Unmarshal(out, &runs) == nil && len(runs) > 0 {
			r := runs[0]
			s.LastRun = &RunSummary{
				DatabaseID: r.DatabaseID,
				Status:     r.Status,
				Conclusion: r.Conclusion,
				CreatedAt:  r.CreatedAt,
				URL:        r.URL,
			}
		}
	}

	return s, nil
}

func validateOptions(opts Options) error {
	if opts.Repo == "" {
		return fmt.Errorf("--repo obrigatorio (formato: owner/repo)")
	}
	if !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("--repo deve ter formato owner/repo, got %q", opts.Repo)
	}
	return nil
}

// Render writes a human-readable summary.
func (s Status) Render(w io.Writer) {
	fmt.Fprintf(w, "Peer:        %s\n", s.Repo)
	fmt.Fprintf(w, "Workflow:    %s\n", s.WorkflowFile)
	fmt.Fprintf(w, "Billing:     %s (exit %d)\n", s.BillingStatus, s.BillingExitCode)
	fmt.Fprintf(w, "Runners:     %d/%d online\n", s.RunnersOnline, s.RunnersTotal)
	for _, n := range s.RunnerNames {
		fmt.Fprintf(w, "             - %s\n", n)
	}
	if s.LastRun != nil {
		conc := s.LastRun.Conclusion
		if conc == "" {
			conc = s.LastRun.Status
		}
		age := time.Since(s.LastRun.CreatedAt).Round(time.Minute)
		fmt.Fprintf(w, "Last run:    #%d %s (%s ago)\n", s.LastRun.DatabaseID, conc, age)
		fmt.Fprintf(w, "             %s\n", s.LastRun.URL)
	} else {
		fmt.Fprintln(w, "Last run:    (nenhum encontrado)")
	}
	fmt.Fprintln(w)
	switch {
	case s.BillingStatus == "blocked" && s.RunnersOnline == 0:
		fmt.Fprintln(w, "ALERTA: billing-block + nenhum runner self-hosted online = workflow nao roda.")
	case s.BillingStatus == "blocked" && s.RunnersOnline > 0:
		fmt.Fprintln(w, "OK: billing-block detectado, mas vitae-ci self-hosted serve fallback.")
	case s.RunnersOnline == 0:
		fmt.Fprintln(w, "WARN: nenhum runner self-hosted online; depende 100 percent de billing-hosted.")
	default:
		fmt.Fprintln(w, "OK: billing OK + runners online.")
	}
}

// RenderJSON writes machine-readable output.
func (s Status) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
