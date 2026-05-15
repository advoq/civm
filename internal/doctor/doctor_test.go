package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/advoq/civm/internal/health"
	"github.com/advoq/civm/internal/runner"
)

func TestClassifyRunner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		runner GitHubRunner
		class  string
		sev    Severity
	}{
		{
			name: "canonical",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", Name: "civm-vitae",
				Status: "online", Labels: []string{"self-hosted", "civm"}},
			class: "canonical", sev: SeverityOK,
		},
		{
			name: "legacy offline",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", ID: 123, Name: "vitae-ci-1",
				Status: "offline", Labels: []string{"self-hosted", "vitae-ci"}},
			class: "legacy_stale", sev: SeverityWarning,
		},
		{
			name: "online without civm label",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", Name: "custom",
				Status: "online", Labels: []string{"self-hosted"}},
			class: "ambiguous", sev: SeverityWarning,
		},
		{
			name: "busy canonical",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", Name: "civm-vitae",
				Status: "online", Busy: true, Labels: []string{"self-hosted", "civm"}},
			class: "canonical", sev: SeverityWarning,
		},
		{
			name: "compatible civm label with noncanonical name",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", Name: "custom-vm",
				Status: "online", Labels: []string{"self-hosted", "civm"}},
			class: "compatible", sev: SeverityOK,
		},
		{
			name: "offline generic",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", Name: "custom-vm",
				Status: "offline", Labels: []string{"self-hosted", "civm"}},
			class: "offline", sev: SeverityWarning,
		},
		{
			name: "unknown state",
			runner: GitHubRunner{Repo: "emersonbusson/vitae", Name: "custom-vm",
				Status: "draining", Labels: []string{"self-hosted", "civm"}},
			class: "unknown", sev: SeverityWarning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyRunner(tt.runner)
			if got.Classification != tt.class || got.Severity != tt.sev {
				t.Fatalf("got class=%s severity=%s, want class=%s severity=%s", got.Classification, got.Severity, tt.class, tt.sev)
			}
			if tt.class == "legacy_stale" && !strings.Contains(got.ManualRemoveCommand, "/repos/emersonbusson/vitae/actions/runners/123") {
				t.Fatalf("manual remove command missing runner id: %+v", got)
			}
		})
	}
}

func TestListGitHubRunnersParsesLabels(t *testing.T) {
	t.Parallel()
	runFn := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "gh" {
			t.Fatalf("name = %q, want gh", name)
		}
		gotArgs := strings.Join(args, " ")
		if gotArgs != "api /repos/emersonbusson/vitae/actions/runners" {
			t.Fatalf("args = %q", gotArgs)
		}
		return []byte(`{
			"runners": [{
				"id": 123,
				"name": "civm-vitae",
				"status": "online",
				"busy": true,
				"labels": [{"name": "self-hosted"}, {"name": "civm"}]
			}]
		}`), nil
	}

	got, err := listGitHubRunners(context.Background(), "emersonbusson/vitae", runFn)
	if err != nil {
		t.Fatalf("listGitHubRunners err = %v", err)
	}
	if len(got) != 1 || got[0].ID != 123 || got[0].Repo != "emersonbusson/vitae" || !got[0].Busy {
		t.Fatalf("runners = %+v", got)
	}
	if strings.Join(got[0].Labels, ",") != "self-hosted,civm" {
		t.Fatalf("labels = %+v", got[0].Labels)
	}
}

func TestListGitHubRunnersErrors(t *testing.T) {
	t.Parallel()
	_, err := listGitHubRunners(context.Background(), "emersonbusson/vitae",
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("network")
		})
	if err == nil || !strings.Contains(err.Error(), "gh api actions/runners") {
		t.Fatalf("run error = %v", err)
	}

	_, err = listGitHubRunners(context.Background(), "emersonbusson/vitae",
		func(context.Context, string, ...string) ([]byte, error) {
			return []byte(`{bad-json`), nil
		})
	if err == nil || !strings.Contains(err.Error(), "parse gh runners") {
		t.Fatalf("json error = %v", err)
	}
}

func TestClassifyRepoMissingAndUnknown(t *testing.T) {
	t.Parallel()
	missing := ClassifyRepo("advoq/advoq", nil, nil)
	if missing.Severity != SeverityWarning || missing.Runners[0].Classification != "missing" {
		t.Fatalf("missing = %+v", missing)
	}
	unknown := ClassifyRepo("advoq/advoq", nil, errors.New("gh auth"))
	if unknown.Severity != SeverityWarning || unknown.Runners[0].Classification != "unknown" {
		t.Fatalf("unknown = %+v", unknown)
	}
}

func TestCollectAndRenderJSON(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Repos = []string{"advoq/civm", "emersonbusson/vitae"}
	opts.HealthFn = func(context.Context) health.Report {
		return health.Report{Checks: []health.Check{
			{Name: "DISK", Detail: "50 GB free", Status: health.StatusOK},
			{Name: "TIMER_DISK", Detail: "enabled+active", Status: health.StatusOK},
		}}
	}
	opts.SystemRunnersFn = func(context.Context) ([]runner.Status, error) {
		return []runner.Status{{UnitName: "actions.runner.emersonbusson-vitae.civm-vitae.service", Repo: "emersonbusson/vitae", Name: "civm-vitae", ActiveState: "active", SubState: "running"}}, nil
	}
	opts.GitHubRunnersFn = func(_ context.Context, repo string) ([]GitHubRunner, error) {
		if repo == "advoq/civm" {
			return []GitHubRunner{{Repo: repo, Name: "civm-self", Status: "online", Labels: []string{"self-hosted", "civm"}}}, nil
		}
		return []GitHubRunner{{Repo: repo, Name: "vitae-ci-1", Status: "offline", Labels: []string{"self-hosted", "vitae-ci"}}}, nil
	}
	report, err := Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	if report.Exit != 1 {
		t.Fatalf("Exit = %d, want warning 1", report.Exit)
	}
	var buf bytes.Buffer
	if err := report.RenderJSON(&buf); err != nil {
		t.Fatalf("RenderJSON err = %v", err)
	}
	var parsed Report
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed.GitHubRepos) != 2 || parsed.GitHubRepos[1].Runners[0].Classification != "legacy_stale" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestRenderHumanIncludesManualLegacyCleanup(t *testing.T) {
	t.Parallel()
	r := Report{
		WorkflowFile: "ci.yml",
		HostChecks:   []HostCheck{{Name: "DISK", Severity: "ok", Detail: "50 GB free"}},
		GitHubRepos: []RepoDiagnosis{{
			Repo: "emersonbusson/vitae", Severity: SeverityWarning,
			Runners: []RunnerDiagnosis{{
				Repo: "emersonbusson/vitae", ID: 55, Name: "vitae-ci-1", Status: "offline",
				Classification: "legacy_stale", Severity: SeverityWarning,
				ManualRemoveCommand: "gh api -X DELETE /repos/emersonbusson/vitae/actions/runners/55",
			}},
		}},
		Exit: 1,
	}
	var buf bytes.Buffer
	r.Render(&buf)
	for _, want := range []string{"HOST", "GITHUB RUNNERS", "LEGACY OFFLINE CLEANUP", "gh api -X DELETE"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("Render omitted %q:\n%s", want, buf.String())
		}
	}
}

func TestCollectRejectsBadRepo(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Repos = []string{"bad/repo;rm"}
	if _, err := Collect(context.Background(), opts); err == nil {
		t.Fatalf("expected bad repo validation error")
	}
}
