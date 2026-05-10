package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/emersonbusson/ci-vm/internal/specs"
)

type runCall struct {
	name string
	args []string
}

func newRunner(responses map[string][]byte, errs map[string]error) (func(context.Context, string, ...string) ([]byte, error), *[]runCall) {
	calls := []runCall{}
	fn := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name, args})
		key := name
		if len(args) > 0 {
			key = name + " " + args[0]
		}
		if err, ok := errs[key]; ok {
			return nil, err
		}
		if r, ok := responses[key]; ok {
			return r, nil
		}
		if r, ok := responses[name]; ok {
			return r, nil
		}
		return nil, errors.New("nao mockado: " + key)
	}
	return fn, &calls
}

func okOpts(t *testing.T) Options {
	t.Helper()
	opts := DefaultOptions()
	opts.UID = 0
	opts.OSReader = func() (string, error) {
		return "ID=ubuntu\nVERSION_ID=\"24.04\"\n", nil
	}
	return opts
}

func TestRun_DryRun_Reports_Would_Apply(t *testing.T) {
	t.Parallel()
	opts := okOpts(t)
	opts.Execute = false
	fn, _ := newRunner(map[string][]byte{}, map[string]error{
		"go --version":         errors.New("not installed"),
		"node --version":       errors.New("not installed"),
		"docker --version":     errors.New("not installed"),
		"gh --version":         errors.New("not installed"),
		"dpkg-query":           errors.New("not installed"),
		"systemctl is-enabled": errors.New("disabled"),
	})
	opts.RunFn = fn
	results := Run(context.Background(), opts)
	if len(results) == 0 {
		t.Fatalf("0 results")
	}
	wouldDoCount := 0
	for _, r := range results {
		if r.WouldDo {
			wouldDoCount++
		}
		if r.Executed {
			t.Errorf("%s Executed em dry-run", r.Name)
		}
	}
	if wouldDoCount == 0 {
		t.Errorf("nenhum step com WouldDo=true em dry-run")
	}
}

func TestRun_VerifyOSFails(t *testing.T) {
	t.Parallel()
	opts := okOpts(t)
	opts.OSReader = func() (string, error) {
		return "ID=debian\nVERSION_ID=\"12\"\n", nil
	}
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("nao deveria ser chamado")
	}
	results := Run(context.Background(), opts)
	if results[0].Err == nil {
		t.Errorf("verify_os deveria ter erro com Debian")
	}
}

func TestRun_NotRoot(t *testing.T) {
	t.Parallel()
	opts := okOpts(t)
	opts.UID = 1000
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("nao deveria ser chamado")
	}
	results := Run(context.Background(), opts)
	foundUIDError := false
	for _, r := range results {
		if r.Name == "verify_uid" && r.Err != nil {
			foundUIDError = true
		}
	}
	if !foundUIDError {
		t.Errorf("UID 1000 deveria gerar erro em verify_uid")
	}
}

func TestRun_Execute_AlreadyInstalled_Skips(t *testing.T) {
	t.Parallel()
	opts := okOpts(t)
	opts.Execute = true
	want := opts.Spec.Tools[0].Preferred() // go
	wantNode := "v" + opts.Spec.Tools[1].Preferred()
	wantDocker := opts.Spec.Tools[3].Preferred()
	wantGH := opts.Spec.Tools[5].Preferred()
	responses := map[string][]byte{
		"go version":       []byte("go version go" + want + " linux/amd64\n"),
		"node --version":   []byte(wantNode + "\n"),
		"docker --version": []byte("Docker version " + wantDocker + ", build abc\n"),
		"gh --version":     []byte("gh version " + wantGH + " (2026)\n"),
		"dpkg-query": []byte("build-essential install ok installed\n" +
			"curl install ok installed\n" +
			"wget install ok installed\n" +
			"jq install ok installed\n" +
			"git install ok installed\n" +
			"ca-certificates install ok installed\n"),
		"systemctl is-enabled": []byte("enabled\n"),
	}
	fn, calls := newRunner(responses, nil)
	opts.RunFn = fn
	results := Run(context.Background(), opts)
	for _, r := range results {
		if r.Name == "verify_os" || r.Name == "verify_uid" {
			continue
		}
		if !r.AlreadyDone {
			t.Errorf("%s deveria estar AlreadyDone", r.Name)
		}
		if r.Executed {
			t.Errorf("%s Executed = true mesmo com AlreadyDone", r.Name)
		}
	}
	for _, c := range *calls {
		if c.name == "apt-get" && len(c.args) > 0 && c.args[0] == "install" {
			t.Errorf("apt-get install chamado mesmo com tudo ja instalado")
		}
	}
}

func TestRenderTable_Snapshot(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Execute = false
	results := []Result{
		{Name: "verify_os", Description: "x", AlreadyDone: true},
		{Name: "install_go", Description: "Instala Go 1.25.9", WouldDo: true},
		{Name: "broken", Description: "y", Err: errors.New("falha")},
	}
	var buf bytes.Buffer
	RenderTable(results, opts, &buf)
	out := buf.String()
	for _, want := range []string{"DRY-RUN", "ja-instalado", "(seria-aplicado)", "erro:", "Resumo:"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderTable omitiu %q\noutput:\n%s", want, out)
		}
	}
}

func TestRenderTable_Execute(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Execute = true
	results := []Result{{Name: "x", Description: "y", Executed: true}}
	var buf bytes.Buffer
	RenderTable(results, opts, &buf)
	if !strings.Contains(buf.String(), "EXECUTE") {
		t.Errorf("output sem EXECUTE")
	}
}

func TestSpecHasGoNodeDocker(t *testing.T) {
	t.Parallel()
	s := specs.Ubuntu2404()
	for _, name := range []string{"go", "node", "docker", "gh"} {
		if _, ok := s.FindTool(name); !ok {
			t.Errorf("spec sem %s", name)
		}
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()
	if got := firstLine("a\nb"); got != "a" {
		t.Errorf("got %q, want a", got)
	}
	if got := firstLine("noNewline"); got != "noNewline" {
		t.Errorf("got %q, want noNewline", got)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("got %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcd…" {
		t.Errorf("got %q", got)
	}
}

func TestRun_WatchdogTimer_OnlyCleanup(t *testing.T) {
	t.Parallel()
	// WatchdogTimer=false: só checa civmctl-cleanup.timer, ignora disk-watchdog
	opts := okOpts(t)
	opts.WatchdogTimer = false
	opts.Execute = false
	calls := []string{}
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := name
		if len(args) > 0 {
			key = name + " " + strings.Join(args, " ")
		}
		calls = append(calls, key)
		if strings.Contains(key, "is-enabled civmctl-cleanup.timer") {
			return []byte("enabled\n"), nil
		}
		// outros: erro
		return nil, errors.New("not installed")
	}
	results := Run(context.Background(), opts)
	for _, r := range results {
		if r.Name == "install_systemd_timers" {
			if !r.AlreadyDone {
				t.Errorf("WatchdogTimer=false + cleanup enabled: AlreadyDone esperado true")
			}
		}
	}
	for _, c := range calls {
		if strings.Contains(c, "civmctl-disk-watchdog.timer") {
			t.Errorf("WatchdogTimer=false NAO deveria chamar disk-watchdog.timer; got: %q", c)
		}
	}
}

func TestRun_WatchdogTimer_BothRequired(t *testing.T) {
	t.Parallel()
	// WatchdogTimer=true: ambos timers checados
	opts := okOpts(t)
	opts.WatchdogTimer = true
	opts.Execute = false
	calls := []string{}
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := name
		if len(args) > 0 {
			key = name + " " + strings.Join(args, " ")
		}
		calls = append(calls, key)
		// Cleanup enabled mas watchdog não
		if strings.Contains(key, "is-enabled civmctl-cleanup.timer") {
			return []byte("enabled\n"), nil
		}
		return nil, errors.New("not installed")
	}
	results := Run(context.Background(), opts)
	for _, r := range results {
		if r.Name == "install_systemd_timers" {
			if r.AlreadyDone {
				t.Errorf("watchdog ausente: AlreadyDone deveria ser false")
			}
			if !r.WouldDo {
				t.Errorf("watchdog ausente em dry-run: WouldDo esperado true")
			}
		}
	}
}

func TestRun_StepError_Propagates(t *testing.T) {
	t.Parallel()
	opts := okOpts(t)
	opts.Execute = true
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "go" || name == "node" || name == "docker" || name == "gh" || name == "dpkg-query" || name == "systemctl" {
			return nil, errors.New("not installed")
		}
		if name == "apt-get" && len(args) > 0 && args[0] == "update" {
			return nil, errors.New("apt-get update falhou")
		}
		return nil, nil
	}
	results := Run(context.Background(), opts)
	hasError := false
	for _, r := range results {
		if r.Err != nil {
			hasError = true
		}
	}
	if !hasError {
		t.Errorf("esperava propagar erro de apt-get update")
	}
}
