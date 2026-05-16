package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// civmctlBin is the path to a built civmctl binary, set up once per
// package by TestMain. Integration tests use this binary plus symlinks
// to exercise argv[0]-based hook dispatch end to end.
var civmctlBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "civmctl-int-*")
	if err != nil {
		panic(err)
	}
	civmctlBin = filepath.Join(tmp, "civmctl")
	build := exec.Command("go", "build", "-o", civmctlBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		panic("build civmctl: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// TestIntegration_JobStartedSymlinkDispatch valida que invocar civmctl via
// symlink "job-started" no /opt/civm/hooks roteia para o subcomando de
// hook com --execute, retornando JSON estruturado em vez de mensagem de
// help genérica. Thresholds altos garantem que cleanup não dispara.
func TestIntegration_JobStartedSymlinkDispatch(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "job-started")
	if err := os.Symlink(civmctlBin, link); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(link,
		"--pre-cleanup-pct=99",
		"--hard-fail-pct=100",
		"--json",
	)
	cmd.Env = []string{
		"HOME=" + dir,
		"PATH=/usr/bin:/bin",
		// RUNNER_TEMP sob tmpdir falha safeWorkRoot → cleanup não enumera roots reais.
		"RUNNER_TEMP=" + filepath.Join(dir, "fake-runner-temp"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("symlink invocation failed: %v\n%s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res["event"] != "job-started" {
		t.Errorf("event=%v, want job-started\n%s", res["event"], out)
	}
	if res["decision"] != "ok" {
		t.Errorf("decision=%v, want ok\n%s", res["decision"], out)
	}
}

// TestIntegration_LegacyShSymlinkDispatch valida o caminho de compat com
// instalações antigas que usavam wrappers .sh — civmctl tolera o sufixo
// no basename para não quebrar runners ainda não re-instalados.
func TestIntegration_LegacyShSymlinkDispatch(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "job-started.sh")
	if err := os.Symlink(civmctlBin, link); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(link,
		"--pre-cleanup-pct=99",
		"--hard-fail-pct=100",
		"--json",
	)
	cmd.Env = []string{
		"HOME=" + dir,
		"PATH=/usr/bin:/bin",
		"RUNNER_TEMP=" + filepath.Join(dir, "fake-runner-temp"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy .sh symlink failed: %v\n%s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res["event"] != "job-started" {
		t.Errorf("legacy .sh basename did not dispatch: event=%v\n%s", res["event"], out)
	}
}

// TestIntegration_NoSymlinkRunsAsRegularCLI valida que invocações normais
// (não via symlink) seguem o switch padrão e NÃO caem no dispatch de hook.
// Sem isso, qualquer rename acidental do binário viraria modo hook.
func TestIntegration_NoSymlinkRunsAsRegularCLI(t *testing.T) {
	cmd := exec.Command(civmctlBin, "version-pins")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version-pins failed: %v\n%s", err, out)
	}
	// version-pins emite versões alvo Ubuntu 2404; deve mencionar Go.
	if !strings.Contains(string(out), "Go") && !strings.Contains(string(out), "go") {
		t.Errorf("version-pins output sem menção a Go:\n%s", out)
	}
}

// TestIntegration_UnrelatedBasenameDoesNotDispatch valida que basenames
// fora da whitelist (job-started/job-completed) NÃO ativam modo hook.
// Importante: o binário renomeado para "civmctl-foo" deve mostrar help.
func TestIntegration_UnrelatedBasenameDoesNotDispatch(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "civmctl-foo")
	if err := os.Symlink(civmctlBin, link); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(link)
	cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin"}
	out, _ := cmd.CombinedOutput()
	// Sem args, civmctl mostra help e sai com exitUsage. Não deve mostrar JSON.
	if strings.Contains(string(out), `"event"`) {
		t.Errorf("non-whitelisted basename triggered hook dispatch:\n%s", out)
	}
	if !strings.Contains(string(out), "civmctl — provisionamento") {
		t.Errorf("expected help message, got:\n%s", out)
	}
}
