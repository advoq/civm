package runner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func validOpts() AddOptions {
	o := DefaultOptions()
	o.Repo = "emersonbusson/compexhub"
	o.Token = "AAAA1234"
	o.Short = "cmpx"
	o.BaseDir = "/home/emdev"
	o.RunAsUser = "emdev"
	return o
}

func TestValidate_AllRequiredFieldsChecked(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(*AddOptions)
	}{
		{"no repo", func(o *AddOptions) { o.Repo = "" }},
		{"bad repo (no slash)", func(o *AddOptions) { o.Repo = "compexhub" }},
		{"no token", func(o *AddOptions) { o.Token = "" }},
		{"no short", func(o *AddOptions) { o.Short = "" }},
		{"bad short", func(o *AddOptions) { o.Short = "../cmpx" }},
		{"bad label", func(o *AddOptions) { o.Label = "civm, bad label" }},
		{"no runner-version", func(o *AddOptions) { o.RunnerVersion = "" }},
		{"bad runner-version", func(o *AddOptions) { o.RunnerVersion = "2.334" }},
		{"no base-dir", func(o *AddOptions) { o.BaseDir = "" }},
		{"base-dir not clean", func(o *AddOptions) { o.BaseDir = "/home/emdev/.." }},
		{"no run-as", func(o *AddOptions) { o.RunAsUser = "" }},
		{"bad run-as", func(o *AddOptions) { o.RunAsUser = "emdev;root" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := validOpts()
			c.mut(&o)
			if err := validateOptions(o); err == nil {
				t.Errorf("esperava erro para caso %q", c.name)
			}
		})
	}
}

func TestValidate_ValidPasses(t *testing.T) {
	t.Parallel()
	if err := validateOptions(validOpts()); err != nil {
		t.Errorf("opts validas falharam: %v", err)
	}
}

func TestAdd_DryRun_NoExecute(t *testing.T) {
	t.Parallel()
	called := false
	o := validOpts()
	o.Execute = false
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	results, err := Add(context.Background(), o)
	if err != nil {
		t.Fatalf("Add err = %v", err)
	}
	if called {
		t.Errorf("RunFn chamado em dry-run")
	}
	if len(results) != 6 {
		t.Errorf("len(results) = %d, want 6 steps", len(results))
	}
	for _, r := range results {
		if r.Executed {
			t.Errorf("%s Executed em dry-run", r.Name)
		}
		if r.WouldDo == "" {
			t.Errorf("%s sem WouldDo", r.Name)
		}
	}
}

func TestAdd_Execute_RunsAllSteps(t *testing.T) {
	t.Parallel()
	calls := 0
	var gotShell bool
	o := validOpts()
	o.Execute = true
	o.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if name == "sh" {
			gotShell = true
		}
		return nil, nil
	}
	results, err := Add(context.Background(), o)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, r := range results {
		if !r.Executed {
			t.Errorf("%s nao Executed", r.Name)
		}
		if r.Err != nil {
			t.Errorf("%s err = %v", r.Name, r.Err)
		}
	}
	if calls < 6 {
		t.Errorf("calls = %d, esperava ao menos 6 (1+ por step)", calls)
	}
	if gotShell {
		t.Errorf("Add nao deveria usar sh -c")
	}
}

func TestAdd_StopsOnError(t *testing.T) {
	t.Parallel()
	o := validOpts()
	o.Execute = true
	step := 0
	o.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		step++
		if step == 2 {
			return nil, errors.New("download falhou")
		}
		return nil, nil
	}
	results, err := Add(context.Background(), o)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	hadErr := false
	executed := 0
	for _, r := range results {
		if r.Err != nil {
			hadErr = true
		}
		if r.Executed {
			executed++
		}
	}
	if !hadErr {
		t.Errorf("esperava propagar erro do download")
	}
	// mkdir success + download fail -> 1 executed
	if executed != 1 {
		t.Errorf("executed = %d, want 1 (mkdir antes do erro)", executed)
	}
}

func TestAdd_TokenNotInDryRunOutput(t *testing.T) {
	t.Parallel()
	o := validOpts()
	o.Execute = false
	results, _ := Add(context.Background(), o)
	for _, r := range results {
		if strings.Contains(r.WouldDo, o.Token) {
			t.Errorf("%s WouldDo expoe token: %q", r.Name, r.WouldDo)
		}
	}
}

func TestRenderTable_DryRun(t *testing.T) {
	t.Parallel()
	o := validOpts()
	results, _ := Add(context.Background(), o)
	var buf bytes.Buffer
	RenderTable(results, o, &buf)
	out := buf.String()
	for _, want := range []string{"DRY-RUN", "compexhub", "cmpx", "civm", "(seria-aplicado)", "--execute"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderTable omitiu %q", want)
		}
	}
}

func TestRenderTable_Execute(t *testing.T) {
	t.Parallel()
	o := validOpts()
	o.Execute = true
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	results, _ := Add(context.Background(), o)
	var buf bytes.Buffer
	RenderTable(results, o, &buf)
	out := buf.String()
	if !strings.Contains(out, "EXECUTE") {
		t.Errorf("output sem EXECUTE")
	}
	if strings.Contains(out, "--execute") {
		t.Errorf("dica --execute apareceu em modo execute")
	}
}

func TestRenderTable_Error(t *testing.T) {
	t.Parallel()
	o := validOpts()
	o.Execute = true
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}
	results, _ := Add(context.Background(), o)
	var buf bytes.Buffer
	RenderTable(results, o, &buf)
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("erro nao apareceu na tabela")
	}
}

func TestAdd_InvalidOptions(t *testing.T) {
	t.Parallel()
	o := validOpts()
	o.Repo = ""
	if _, err := Add(context.Background(), o); err == nil {
		t.Errorf("esperava erro de validacao")
	}
}

func TestDefaultOptions_HasSaneDefaults(t *testing.T) {
	t.Parallel()
	d := DefaultOptions()
	if d.Label != "civm" {
		t.Errorf("Label default = %q, want civm", d.Label)
	}
	if d.RunnerVersion == "" {
		t.Errorf("RunnerVersion vazio")
	}
	if d.Execute {
		t.Errorf("Execute default = true; deveria ser false (dry-run)")
	}
}

// ==== Remove tests ====

func validRemoveOpts() RemoveOptions {
	return RemoveOptions{
		Short:   "cmpx",
		Token:   "REMOVE-TOKEN-XYZ",
		BaseDir: "/home/emdev",
		Execute: false,
		RunFn:   func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	}
}

func TestRemove_DryRun_NoExecute(t *testing.T) {
	t.Parallel()
	called := false
	o := validRemoveOpts()
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	results, err := Remove(context.Background(), o)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Errorf("RunFn chamado em dry-run")
	}
	if len(results) != 4 {
		t.Errorf("len(results) = %d, want 4 steps (stop, uninstall, config_remove, remove_dir)", len(results))
	}
	for _, r := range results {
		if r.Executed {
			t.Errorf("%s Executed em dry-run", r.Name)
		}
	}
}

func TestRemove_Execute_RunsAllSteps(t *testing.T) {
	t.Parallel()
	calls := 0
	o := validRemoveOpts()
	o.Execute = true
	o.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return nil, nil
	}
	results, err := Remove(context.Background(), o)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, r := range results {
		if !r.Executed {
			t.Errorf("%s nao Executed", r.Name)
		}
		if r.Err != nil {
			t.Errorf("%s err = %v (steps best-effort, nao deveriam erro)", r.Name, r.Err)
		}
	}
	if calls < 4 {
		t.Errorf("calls = %d, esperava ao menos 4", calls)
	}
}

func TestRemove_TokenNotInWouldDo(t *testing.T) {
	t.Parallel()
	o := validRemoveOpts()
	results, _ := Remove(context.Background(), o)
	for _, r := range results {
		if strings.Contains(r.WouldDo, o.Token) {
			t.Errorf("%s WouldDo expoe token: %q", r.Name, r.WouldDo)
		}
	}
}

func TestRemove_ConfigRemoveErrorIsReported(t *testing.T) {
	t.Parallel()
	o := validRemoveOpts()
	o.Execute = true
	o.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if strings.HasSuffix(name, "/config.sh") {
			return nil, errors.New("config remove falhou")
		}
		return nil, nil
	}
	results, _ := Remove(context.Background(), o)
	var gotConfigErr bool
	for _, r := range results {
		if r.Name == "config_remove" && r.Err != nil {
			gotConfigErr = true
		}
	}
	if !gotConfigErr {
		t.Errorf("config_remove deveria reportar erro")
	}
}

func TestValidateRemove_RequiredFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(*RemoveOptions)
	}{
		{"no short", func(o *RemoveOptions) { o.Short = "" }},
		{"bad short", func(o *RemoveOptions) { o.Short = "x/y" }},
		{"no base-dir", func(o *RemoveOptions) { o.BaseDir = "" }},
		{"base-dir not clean", func(o *RemoveOptions) { o.BaseDir = "/home/emdev/.." }},
		{"no token", func(o *RemoveOptions) { o.Token = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := validRemoveOpts()
			c.mut(&o)
			if err := validateRemoveOptions(o); err == nil {
				t.Errorf("esperava erro pra %q", c.name)
			}
		})
	}
}

func TestRenderRemoveTable_DryRun(t *testing.T) {
	t.Parallel()
	o := validRemoveOpts()
	results, _ := Remove(context.Background(), o)
	var buf bytes.Buffer
	RenderRemoveTable(results, o, &buf)
	out := buf.String()
	for _, want := range []string{"DRY-RUN", "cmpx", "stop_service", "remove_dir", "--execute"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderRemoveTable omitiu %q", want)
		}
	}
}

func TestRenderRemoveTable_Execute(t *testing.T) {
	t.Parallel()
	o := validRemoveOpts()
	o.Execute = true
	results, _ := Remove(context.Background(), o)
	var buf bytes.Buffer
	RenderRemoveTable(results, o, &buf)
	if !strings.Contains(buf.String(), "EXECUTE") {
		t.Errorf("output sem EXECUTE")
	}
}

func TestRemove_InvalidOpts(t *testing.T) {
	t.Parallel()
	o := validRemoveOpts()
	o.Short = ""
	if _, err := Remove(context.Background(), o); err == nil {
		t.Errorf("esperava erro de validacao")
	}
}

func TestDefaultRemoveOptions(t *testing.T) {
	t.Parallel()
	d := DefaultRemoveOptions()
	if d.Execute {
		t.Errorf("Execute default = true; deveria ser false (dry-run)")
	}
}
