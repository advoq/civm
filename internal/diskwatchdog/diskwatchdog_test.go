package diskwatchdog

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheck_OK_BelowThreshold(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 50 * (1 << 30), nil // 50% used
	}
	o.ThresholdPct = 80
	r := Check(context.Background(), o)
	if r.Decision != DecisionOK {
		t.Errorf("Decision = %v, want OK", r.Decision)
	}
	if r.UsedPct != 50 {
		t.Errorf("UsedPct = %d, want 50", r.UsedPct)
	}
	if r.ExitCode() != 0 {
		t.Errorf("Exit = %d, want 0", r.ExitCode())
	}
	if len(r.CleanupActions) != 0 {
		t.Errorf("cleanup nao deveria ter rodado abaixo do threshold")
	}
}

func TestCheck_TriggersAboveThreshold(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 5 * (1 << 30), nil // 95% used
	}
	o.ThresholdPct = 80
	o.Execute = false // dry-run cleanup
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("0B (0%)\n"), nil // empty docker
	}
	r := Check(context.Background(), o)
	if r.Decision != DecisionCleanupTriggered {
		t.Errorf("Decision = %v, want CleanupTriggered", r.Decision)
	}
	if r.UsedPct != 95 {
		t.Errorf("UsedPct = %d, want 95", r.UsedPct)
	}
	if r.ExitCode() != 0 {
		t.Errorf("Exit = %d, want 0 (cleanup ok)", r.ExitCode())
	}
	if len(r.CleanupActions) == 0 {
		t.Errorf("CleanupActions vazio")
	}
}

func TestCheck_StatfsError(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 0, 0, errors.New("statfs explodiu")
	}
	r := Check(context.Background(), o)
	if r.Decision != DecisionError {
		t.Errorf("Decision = %v, want Error", r.Decision)
	}
	if r.ExitCode() != 2 {
		t.Errorf("Exit = %d, want 2", r.ExitCode())
	}
}

func TestCheck_StatfsZeroTotal(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 0, 0, nil // weird empty filesystem
	}
	r := Check(context.Background(), o)
	if r.Decision != DecisionError {
		t.Errorf("Decision = %v, want Error", r.Decision)
	}
}

func TestCheck_DefaultsApplied(t *testing.T) {
	t.Parallel()
	o := Options{
		StatfsFn: func(string) (uint64, uint64, error) {
			return 100 * (1 << 30), 50 * (1 << 30), nil
		},
	}
	// Path empty + ThresholdPct 0 -> defaults applied
	r := Check(context.Background(), o)
	if r.Path != "/" {
		t.Errorf("Path default = %q, want /", r.Path)
	}
	if r.ThresholdPct != 80 {
		t.Errorf("ThresholdPct default = %d, want 80", r.ThresholdPct)
	}
}

func TestRender_Triggered(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 5 * (1 << 30), nil
	}
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return nil, nil
	}
	r := Check(context.Background(), o)
	var buf bytes.Buffer
	r.Render(&buf)
	out := buf.String()
	for _, want := range []string{"Disk watchdog", "95", "Threshold", "cleanup-triggered", "Aggressive cleanup"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render omitiu %q", want)
		}
	}
}

func TestRender_OK(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 50 * (1 << 30), nil
	}
	r := Check(context.Background(), o)
	var buf bytes.Buffer
	r.Render(&buf)
	if !strings.Contains(buf.String(), "ok") {
		t.Errorf("Render(OK) sem 'ok'")
	}
	if strings.Contains(buf.String(), "Aggressive") {
		t.Errorf("Render(OK) com Aggressive section (cleanup nao deveria ter rodado)")
	}
}

func TestRender_Error(t *testing.T) {
	t.Parallel()
	o := DefaultOptions()
	o.StatfsFn = func(string) (uint64, uint64, error) {
		return 0, 0, errors.New("disk on fire")
	}
	r := Check(context.Background(), o)
	var buf bytes.Buffer
	r.Render(&buf)
	if !strings.Contains(buf.String(), "disk on fire") {
		t.Errorf("Render(Error) sem mensagem de erro")
	}
}

func TestDecisionString(t *testing.T) {
	t.Parallel()
	cases := map[Decision]string{
		DecisionOK:               "ok",
		DecisionCleanupTriggered: "cleanup-triggered",
		DecisionError:            "error",
		Decision(99):             "?",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("Decision(%d).String() = %q, want %q", d, got, want)
		}
	}
}

func TestExitCode_AllDecisions(t *testing.T) {
	t.Parallel()
	cases := map[Decision]int{
		DecisionOK:               0,
		DecisionCleanupTriggered: 0,
		DecisionError:            2,
		Decision(99):             1,
	}
	for d, want := range cases {
		r := Result{Decision: d}
		if got := r.ExitCode(); got != want {
			t.Errorf("Decision(%d) Exit = %d, want %d", d, got, want)
		}
	}
}

func TestCheck_DefaultStatfsRealFS(t *testing.T) {
	// Não usa Parallel — toca FS real
	o := DefaultOptions()
	r := Check(context.Background(), o)
	if r.Decision == DecisionError {
		t.Skipf("statfs real falhou em ambiente: %v", r.Err)
	}
	if r.TotalGB == 0 {
		t.Errorf("Total = 0 GB; ambiente Linux normal deveria ter espaço")
	}
}
