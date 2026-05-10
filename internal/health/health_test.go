package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func okCollector() *Collector {
	c := NewDefaultCollector("/tmp")
	c.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 50 * (1 << 30), nil
	}
	c.MeminfoFn = func() (int64, error) { return 8 * 1024 * 1024, nil } // 8 GB
	c.RunnerUnitsFn = func(context.Context) ([]string, error) {
		return []string{"actions.runner.foo.service"}, nil
	}
	now := time.Now().Add(-2 * time.Hour)
	c.LastCleanupFn = func(context.Context) (*time.Time, string, error) {
		return &now, "liberados 4.2 GB", nil
	}
	c.TimerStateFn = func(context.Context, string) (TimerState, error) {
		return TimerState{Enabled: "enabled", Active: "active"}, nil
	}
	return c
}

func TestCollect_AllOK(t *testing.T) {
	t.Parallel()
	c := okCollector()
	r := c.Collect(context.Background())
	if r.Exit() != 0 {
		t.Errorf("Exit = %d, want 0", r.Exit())
	}
	if len(r.Checks) != 7 {
		t.Errorf("len(Checks) = %d, want 7", len(r.Checks))
	}
}

func TestCollect_DiskWarn(t *testing.T) {
	t.Parallel()
	c := okCollector()
	c.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 5 * (1 << 30), nil // 5 GB free, warn=10
	}
	r := c.Collect(context.Background())
	if r.Exit() != int(StatusWarn) {
		t.Errorf("Exit = %d, want %d (warn)", r.Exit(), StatusWarn)
	}
}

func TestCollect_DiskCritical(t *testing.T) {
	t.Parallel()
	c := okCollector()
	c.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 * (1 << 30), 1 * (1 << 30), nil // 1 GB free, crit=3
	}
	r := c.Collect(context.Background())
	if r.Exit() != int(StatusCritical) {
		t.Errorf("Exit = %d, want %d (crit)", r.Exit(), StatusCritical)
	}
}

func TestCollect_StatfsError(t *testing.T) {
	t.Parallel()
	c := okCollector()
	c.StatfsFn = func(string) (uint64, uint64, error) {
		return 0, 0, errors.New("statfs explodiu")
	}
	r := c.Collect(context.Background())
	if r.Exit() < int(StatusWarn) {
		t.Errorf("Exit = %d, want >= warn", r.Exit())
	}
	found := false
	for _, ch := range r.Checks {
		if ch.Name == "DISK" && strings.Contains(ch.Detail, "explodiu") {
			found = true
		}
	}
	if !found {
		t.Errorf("erro de statfs nao apareceu no detail")
	}
}

func TestCollect_MemCritical(t *testing.T) {
	t.Parallel()
	c := okCollector()
	c.MeminfoFn = func() (int64, error) { return 100 * 1024, nil } // 100 MB, crit=128
	r := c.Collect(context.Background())
	if r.Exit() != int(StatusCritical) {
		t.Errorf("Exit = %d, want crit", r.Exit())
	}
}

func TestCollect_NoRunnersIsOK(t *testing.T) {
	t.Parallel()
	c := okCollector()
	c.RunnerUnitsFn = func(context.Context) ([]string, error) { return nil, nil }
	r := c.Collect(context.Background())
	if r.Exit() != 0 {
		t.Errorf("Exit = %d, want 0 (rodar fora da VM nao e erro)", r.Exit())
	}
}

func TestCollect_NoLastCleanupIsWarn(t *testing.T) {
	t.Parallel()
	c := okCollector()
	c.LastCleanupFn = func(context.Context) (*time.Time, string, error) {
		return nil, "", nil
	}
	r := c.Collect(context.Background())
	if r.Exit() != int(StatusWarn) {
		t.Errorf("Exit = %d, want warn (sem cleanup historico)", r.Exit())
	}
}

func TestCollect_OldCleanupIsWarn(t *testing.T) {
	t.Parallel()
	c := okCollector()
	old := time.Now().Add(-72 * time.Hour)
	c.LastCleanupFn = func(context.Context) (*time.Time, string, error) {
		return &old, "liberados 1 GB", nil
	}
	r := c.Collect(context.Background())
	if r.Exit() != int(StatusWarn) {
		t.Errorf("Exit = %d, want warn (cleanup >48h)", r.Exit())
	}
}

func TestCollect_TimersMissingAndStale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		timer     string
		state     TimerState
		err       error
		wantExit  int
		wantCheck string
	}{
		{
			name:      "cleanup missing is critical",
			timer:     "civmctl-cleanup.timer",
			err:       errors.New("not found"),
			wantExit:  int(StatusCritical),
			wantCheck: "TIMER_CLEANUP",
		},
		{
			name:      "disk stale is critical",
			timer:     "civmctl-disk-watchdog.timer",
			state:     TimerState{Enabled: "enabled", Active: "inactive"},
			wantExit:  int(StatusCritical),
			wantCheck: "TIMER_DISK",
		},
		{
			name:      "reverse missing is warning",
			timer:     "civmctl-reverse-watchdog.timer",
			err:       errors.New("not found"),
			wantExit:  int(StatusWarn),
			wantCheck: "TIMER_REVERSE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := okCollector()
			c.TimerStateFn = func(_ context.Context, timer string) (TimerState, error) {
				if timer == tt.timer {
					return tt.state, tt.err
				}
				return TimerState{Enabled: "enabled", Active: "active"}, nil
			}
			r := c.Collect(context.Background())
			if r.Exit() != tt.wantExit {
				t.Fatalf("Exit = %d, want %d", r.Exit(), tt.wantExit)
			}
			found := false
			for _, ch := range r.Checks {
				if ch.Name == tt.wantCheck && ch.Status == Status(tt.wantExit) {
					found = true
				}
			}
			if !found {
				t.Fatalf("check %s with status %d not found: %+v", tt.wantCheck, tt.wantExit, r.Checks)
			}
		})
	}
}

func TestRender_ContainsExitLine(t *testing.T) {
	t.Parallel()
	c := okCollector()
	r := c.Collect(context.Background())
	var buf bytes.Buffer
	r.Render(&buf)
	if !strings.Contains(buf.String(), "EXIT:") {
		t.Errorf("Render() omitiu EXIT line")
	}
	if !strings.Contains(buf.String(), "DISK") {
		t.Errorf("Render() omitiu DISK row")
	}
}

func TestRenderJSON_StructValid(t *testing.T) {
	t.Parallel()
	c := okCollector()
	r := c.Collect(context.Background())
	var buf bytes.Buffer
	if err := r.RenderJSON(&buf); err != nil {
		t.Fatalf("err = %v", err)
	}
	var parsed struct {
		Checks []map[string]any `json:"checks"`
		Exit   int              `json:"exit"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output nao e JSON valido: %v", err)
	}
	if len(parsed.Checks) != 7 {
		t.Errorf("Checks len = %d, want 7", len(parsed.Checks))
	}
	if parsed.Exit != 0 {
		t.Errorf("Exit = %d, want 0", parsed.Exit)
	}
}

func TestStatusString(t *testing.T) {
	t.Parallel()
	cases := map[Status]string{StatusOK: "OK", StatusWarn: "WARN", StatusCritical: "CRIT"}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
	if got := Status(99).String(); got != "?" {
		t.Errorf("Status(99) = %q, want ?", got)
	}
}

func TestRoundDur(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{2 * time.Hour, "2h"},
		{72 * time.Hour, "3d"},
	}
	for _, c := range cases {
		if got := roundDur(c.d); got != c.want {
			t.Errorf("roundDur(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestParseMemAvailable(t *testing.T) {
	t.Parallel()
	in := "MemTotal:       16384000 kB\nMemFree:         2048000 kB\nMemAvailable:    8192000 kB\n"
	got, err := parseMemAvailableKB(in)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != 8192000 {
		t.Errorf("got %d, want 8192000", got)
	}
}

func TestParseMemAvailable_Missing(t *testing.T) {
	t.Parallel()
	if _, err := parseMemAvailableKB("MemTotal: 1 kB\n"); err == nil {
		t.Errorf("esperado erro quando MemAvailable ausente")
	}
}

func TestParseMemAvailable_Malformed(t *testing.T) {
	t.Parallel()
	if _, err := parseMemAvailableKB("MemAvailable:\n"); err == nil {
		t.Errorf("esperado erro com linha malformada")
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("got %q, want abc", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcd…" {
		t.Errorf("got %q, want abcd…", got)
	}
}

func TestDefaultStatfs_RealDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	total, free, err := defaultStatfs(dir)
	if err != nil {
		t.Fatalf("defaultStatfs(%s) erro = %v", dir, err)
	}
	if total == 0 {
		t.Errorf("total = 0; tmpfs/local FS deveria ter espaço")
	}
	if free > total {
		t.Errorf("free %d > total %d", free, total)
	}
}

func TestDefaultStatfs_NonExistent(t *testing.T) {
	t.Parallel()
	if _, _, err := defaultStatfs("/this/path/does/not/exist/xyz"); err == nil {
		t.Errorf("esperado erro em path inexistente")
	}
}

// Tests below touch the package-global osReadFile or run real OS commands.
// They are not Parallel-safe; keep them serial.

func TestDefaultMeminfo_Real(t *testing.T) {
	kb, err := defaultMeminfo()
	if err != nil {
		t.Skipf("defaultMeminfo() falhou em ambiente nao-Linux: %v", err)
	}
	if kb <= 0 {
		t.Errorf("MemAvailable = %d; esperado > 0 em sistema Linux normal", kb)
	}
}

func TestDefaultMeminfo_BrokenFile(t *testing.T) {
	orig := osReadFile
	osReadFile = func(string) ([]byte, error) {
		return []byte("MemTotal: 1 kB\n"), nil
	}
	defer func() { osReadFile = orig }()
	if _, err := defaultMeminfo(); err == nil {
		t.Errorf("esperado erro com /proc/meminfo sem MemAvailable")
	}
}

func TestDefaultMeminfo_FileError(t *testing.T) {
	orig := osReadFile
	osReadFile = func(string) ([]byte, error) {
		return nil, errors.New("read falhou")
	}
	defer func() { osReadFile = orig }()
	if _, err := defaultMeminfo(); err == nil {
		t.Errorf("esperado propagar erro de leitura")
	}
}

func TestDefaultRunnerUnits_NonNil(t *testing.T) {
	// Em ambiente sem systemctl ou sem actions.runner.*, deve retornar nil sem erro.
	units, err := defaultRunnerUnits(context.Background())
	if err != nil {
		t.Errorf("erro inesperado: %v", err)
	}
	_ = units // pode ser nil ou vazio em dev machine
}

func TestDefaultLastCleanup_NoData(t *testing.T) {
	// Sem journalctl ou sem unit civmctl-cleanup, retorna nil sem erro.
	when, _, err := defaultLastCleanup(context.Background())
	if err != nil {
		t.Errorf("erro inesperado: %v", err)
	}
	_ = when
}
