package peerstatus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCollect_HappyPath(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.Repo = "emersonbusson/civm"
	o.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		switch {
		case strings.Contains(key, "actions/runners"):
			return []byte(`{"runners":[{"name":"civm-self","status":"online"},{"name":"civm-2","status":"offline"}]}`), nil
		case strings.Contains(key, "run list"):
			return []byte(`[{"databaseId":12345,"status":"completed","conclusion":"success","createdAt":"2026-05-10T12:00:00Z","url":"https://github.com/x/y/actions/runs/12345"}]`), nil
		case strings.Contains(key, "gh run list --repo"):
			return []byte(`[]`), nil
		default:
			// billing-status delega a billing package que invoca gh run list
			// com diferentes args. Retornar runs healthy:
			return []byte(`[
				{"databaseId":1,"conclusion":"success","startedAt":"2026-05-10T11:50:00Z","updatedAt":"2026-05-10T11:55:00Z"},
				{"databaseId":2,"conclusion":"success","startedAt":"2026-05-10T11:40:00Z","updatedAt":"2026-05-10T11:45:00Z"},
				{"databaseId":3,"conclusion":"success","startedAt":"2026-05-10T11:30:00Z","updatedAt":"2026-05-10T11:35:00Z"}
			]`), nil
		}
	}
	s, err := Collect(context.Background(), o)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if s.Repo != "emersonbusson/civm" {
		t.Errorf("Repo = %s", s.Repo)
	}
	if s.RunnersTotal != 2 {
		t.Errorf("RunnersTotal = %d", s.RunnersTotal)
	}
	if s.RunnersOnline != 1 {
		t.Errorf("RunnersOnline = %d, want 1", s.RunnersOnline)
	}
	if s.LastRun == nil {
		t.Errorf("LastRun nil")
	} else if s.LastRun.DatabaseID != 12345 {
		t.Errorf("LastRun.DatabaseID = %d", s.LastRun.DatabaseID)
	}
}

func TestCollect_RunnersOffline(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.Repo = "x/y"
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"runners":[]}`), nil
	}
	s, _ := Collect(context.Background(), o)
	if s.RunnersTotal != 0 || s.RunnersOnline != 0 {
		t.Errorf("expected 0 runners")
	}
}

func TestCollect_GhFails_Tolerant(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.Repo = "x/y"
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("gh: 401")
	}
	s, err := Collect(context.Background(), o)
	if err != nil {
		t.Errorf("err = %v (esperado: tolerante)", err)
	}
	if s.RunnersTotal != 0 {
		t.Errorf("Runners deveria ser 0 quando gh falha")
	}
	if s.LastRun != nil {
		t.Errorf("LastRun deveria ser nil quando gh falha")
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		repo string
	}{
		{"empty", ""},
		{"no-slash", "noslash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := DefaultOptions()
			o.Repo = c.repo
			if err := validateOptions(o); err == nil {
				t.Errorf("esperava erro pra %q", c.repo)
			}
		})
	}
}

func TestValidate_OK(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.Repo = "owner/repo"
	if err := validateOptions(o); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestRender_Healthy(t *testing.T) {
	t.Parallel()
	s := Status{
		Repo: "x/y", WorkflowFile: "ci.yml",
		BillingStatus: "ok", BillingExitCode: 0,
		RunnersTotal: 1, RunnersOnline: 1,
		RunnerNames: []string{"civm-x"},
		LastRun: &RunSummary{
			DatabaseID: 99, Conclusion: "success",
			CreatedAt: time.Now().Add(-30 * time.Minute),
			URL:       "https://gh/x",
		},
	}
	var buf bytes.Buffer
	s.Render(&buf)
	out := buf.String()
	for _, w := range []string{"x/y", "ok", "1/1 online", "civm-x", "#99", "OK: billing OK"} {
		if !strings.Contains(out, w) {
			t.Errorf("Render omitiu %q", w)
		}
	}
}

func TestRender_BlockedNoRunners(t *testing.T) {
	t.Parallel()
	s := Status{Repo: "x/y", BillingStatus: "blocked", RunnersOnline: 0}
	var buf bytes.Buffer
	s.Render(&buf)
	if !strings.Contains(buf.String(), "ALERTA") {
		t.Errorf("Render omitiu ALERTA")
	}
}

func TestRender_BlockedWithFallback(t *testing.T) {
	t.Parallel()
	s := Status{Repo: "x/y", BillingStatus: "blocked", RunnersOnline: 1}
	var buf bytes.Buffer
	s.Render(&buf)
	if !strings.Contains(buf.String(), "fallback") {
		t.Errorf("Render omitiu mensagem fallback")
	}
}

func TestRender_NoRunners(t *testing.T) {
	t.Parallel()
	s := Status{Repo: "x/y", BillingStatus: "ok", RunnersOnline: 0}
	var buf bytes.Buffer
	s.Render(&buf)
	if !strings.Contains(buf.String(), "WARN") {
		t.Errorf("Render omitiu WARN")
	}
}

func TestRender_NoLastRun(t *testing.T) {
	t.Parallel()
	s := Status{Repo: "x/y", BillingStatus: "ok", RunnersOnline: 1, RunnersTotal: 1}
	var buf bytes.Buffer
	s.Render(&buf)
	if !strings.Contains(buf.String(), "nenhum encontrado") {
		t.Errorf("Render omitiu mensagem 'nenhum encontrado'")
	}
}

func TestRenderJSON_Valid(t *testing.T) {
	t.Parallel()
	s := Status{Repo: "x/y", BillingStatus: "ok", RunnersTotal: 2}
	var buf bytes.Buffer
	if err := s.RenderJSON(&buf); err != nil {
		t.Fatalf("err = %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output nao e JSON valido: %v", err)
	}
	if parsed["repo"] != "x/y" {
		t.Errorf("repo = %v", parsed["repo"])
	}
}

func TestRender_LastRunNoConclusion(t *testing.T) {
	t.Parallel()
	s := Status{
		Repo:          "x/y",
		BillingStatus: "ok",
		RunnersOnline: 1, RunnersTotal: 1,
		LastRun: &RunSummary{
			DatabaseID: 1, Status: "in_progress", Conclusion: "",
			CreatedAt: time.Now().Add(-time.Minute),
		},
	}
	var buf bytes.Buffer
	s.Render(&buf)
	if !strings.Contains(buf.String(), "in_progress") {
		t.Errorf("Render deveria mostrar status quando Conclusion vazio")
	}
}
