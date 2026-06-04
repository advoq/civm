package runreaper

import (
	"context"
	"strings"
	"testing"
	"time"
)

func ts(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// fixture builds an Options wired with in-memory fakes and records cancels.
func fixture(open map[string]bool, runs []Run, cancelled *[]int64) Options {
	return Options{
		Repos:   []string{"advoq/advoq"},
		Execute: true,
		OpenBranchesFn: func(_ context.Context, _ string) (map[string]bool, error) {
			return open, nil
		},
		ActiveRunsFn: func(_ context.Context, _ string) ([]Run, error) {
			return runs, nil
		},
		CancelFn: func(_ context.Context, _ string, id int64) error {
			*cancelled = append(*cancelled, id)
			return nil
		},
	}
}

func TestReapCancelsClosedPRRunsOnly(t *testing.T) {
	open := map[string]bool{"feature/open-1": true}
	runs := []Run{
		{ID: 1, Event: "pull_request", Status: "queued", Branch: "feature/open-1"},      // keep: open PR
		{ID: 2, Event: "pull_request", Status: "queued", Branch: "fix/closed-pr"},        // reap
		{ID: 3, Event: "pull_request_target", Status: "in_progress", Branch: "old/gone"}, // reap
		{ID: 4, Event: "push", Status: "queued", Branch: "main"},                         // keep: not a PR event
		{ID: 5, Event: "schedule", Status: "queued", Branch: "nightly"},                  // keep: cron
		{ID: 6, Event: "pull_request", Status: "queued", Branch: ""},                     // keep: no branch
	}
	var cancelled []int64
	report := Reap(context.Background(), fixture(open, runs, &cancelled))

	if report.Exit != 0 {
		t.Fatalf("exit = %d, want 0", report.Exit)
	}
	if got, want := report.Cancelled, 2; got != want {
		t.Fatalf("cancelled = %d, want %d (events: %+v)", got, want, report.Events)
	}
	gotIDs := map[int64]bool{}
	for _, id := range cancelled {
		gotIDs[id] = true
	}
	if !gotIDs[2] || !gotIDs[3] {
		t.Errorf("expected runs 2 and 3 cancelled, got %v", cancelled)
	}
	for _, keep := range []int64{1, 4, 5, 6} {
		if gotIDs[keep] {
			t.Errorf("run %d should not have been cancelled", keep)
		}
	}
}

func TestReapDryRunCancelsNothing(t *testing.T) {
	runs := []Run{{ID: 7, Event: "pull_request", Status: "queued", Branch: "fix/closed"}}
	var cancelled []int64
	opts := fixture(nil, runs, &cancelled)
	opts.Execute = false
	report := Reap(context.Background(), opts)

	if len(cancelled) != 0 {
		t.Fatalf("dry-run cancelled %v, want none", cancelled)
	}
	if report.Candidates != 1 {
		t.Fatalf("candidates = %d, want 1", report.Candidates)
	}
	if report.Cancelled != 0 {
		t.Fatalf("cancelled count = %d, want 0", report.Cancelled)
	}
}

func TestReapOldestFirstAndCap(t *testing.T) {
	runs := []Run{
		{ID: 10, Event: "pull_request", Status: "queued", Branch: "c", CreatedAt: ts("2026-06-04T12:00:00Z")},
		{ID: 11, Event: "pull_request", Status: "queued", Branch: "c", CreatedAt: ts("2026-06-04T10:00:00Z")},
		{ID: 12, Event: "pull_request", Status: "queued", Branch: "c", CreatedAt: ts("2026-06-04T11:00:00Z")},
	}
	var cancelled []int64
	opts := fixture(nil, runs, &cancelled)
	opts.MaxCancelPerRepo = 2
	report := Reap(context.Background(), opts)

	if len(cancelled) != 2 {
		t.Fatalf("cancelled %d, want 2 (cap)", len(cancelled))
	}
	// Oldest two first: 11 (10:00) then 12 (11:00).
	if cancelled[0] != 11 || cancelled[1] != 12 {
		t.Errorf("cap order = %v, want [11 12] (oldest first)", cancelled)
	}
	if report.Cancelled != 2 {
		t.Errorf("report.Cancelled = %d, want 2", report.Cancelled)
	}
	var capped bool
	for _, ev := range report.Events {
		if ev.Event == "reap-capped" {
			capped = true
		}
	}
	if !capped {
		t.Error("expected a reap-capped event")
	}
}

func TestReapNoReposExits1(t *testing.T) {
	report := Reap(context.Background(), Options{})
	if report.Exit != 1 {
		t.Fatalf("exit = %d, want 1", report.Exit)
	}
}

func TestReapInvalidRepoSkipped(t *testing.T) {
	report := Reap(context.Background(), Options{Repos: []string{"not-a-repo"}})
	if report.Exit != 1 {
		t.Fatalf("exit = %d, want 1", report.Exit)
	}
	if report.Candidates != 0 {
		t.Fatalf("candidates = %d, want 0", report.Candidates)
	}
}

func TestReapCancelFailureSetsExit(t *testing.T) {
	runs := []Run{{ID: 9, Event: "pull_request", Status: "queued", Branch: "fix/closed"}}
	opts := Options{
		Repos:          []string{"advoq/advoq"},
		Execute:        true,
		OpenBranchesFn: func(_ context.Context, _ string) (map[string]bool, error) { return nil, nil },
		ActiveRunsFn:   func(_ context.Context, _ string) ([]Run, error) { return runs, nil },
		CancelFn:       func(_ context.Context, _ string, _ int64) error { return context.DeadlineExceeded },
	}
	report := Reap(context.Background(), opts)
	if report.Exit != 1 {
		t.Fatalf("exit = %d, want 1 on cancel failure", report.Exit)
	}
	if report.Cancelled != 0 {
		t.Fatalf("cancelled = %d, want 0", report.Cancelled)
	}
}

func TestParseActiveRunsMultiPage(t *testing.T) {
	// Two concatenated pages as gh --paginate emits them.
	page := `{"workflow_runs":[{"id":1,"head_branch":"a","event":"pull_request","status":"queued","name":"Go CI","created_at":"2026-06-04T10:00:00Z"}]}` +
		`{"workflow_runs":[{"id":2,"head_branch":"b","event":"push","status":"in_progress","name":"Web CI","created_at":"2026-06-04T11:00:00Z"}]}`
	runs, err := parseActiveRuns([]byte(page), "advoq/advoq")
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	if runs[0].ID != 1 || runs[0].Branch != "a" || runs[0].Workflow != "Go CI" {
		t.Errorf("run0 = %+v", runs[0])
	}
	if runs[1].ID != 2 || runs[1].Event != "push" {
		t.Errorf("run1 = %+v", runs[1])
	}
}

func TestRenderJSONRoundTrips(t *testing.T) {
	report := Report{Executed: true, Repos: []string{"advoq/advoq"}, Cancelled: 1}
	var sb strings.Builder
	if err := report.RenderJSON(&sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(sb.String(), "\"cancelled\": 1") {
		t.Errorf("json missing cancelled field: %s", sb.String())
	}
}
