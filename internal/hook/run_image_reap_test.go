package hook

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/advoq/civm/internal/idle"
)

// Estes testes travam o lever de redução-na-FONTE da issue #137: o job-completed
// reapa as imagens taggeadas do PRÓPRIO run que acabou, em vez de deixá-las
// acumular durante a rajada. O reap é escopado ao compose project deste runner
// (`com.docker.compose.project=<slot>` e `<slot>-<run_id>`), que é box-único por
// runner (multi-project-isolation) — um sibling NUNCA compartilha esse label, e
// imagens de vendor pull (redis/minio/postgres) não carregam label de compose,
// então o "No such image" race que o age-guard consertou não volta.

// reapTestRunFn captura os comandos docker emitidos e responde no-op.
type reapTestRunFn struct {
	commands []string
	failOn   string // se != "", retorna erro quando o comando contém esta substring
}

func (r *reapTestRunFn) fn(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, cmd)
	if r.failOn != "" && strings.Contains(cmd, r.failOn) {
		return nil, fmt.Errorf("simulated failure for %q", r.failOn)
	}
	return nil, nil
}

// baseReapOpts monta opts hermeticos de job-completed com o slot deste runner
// injetado no env (como o .env do runner faz em produção via install.go).
func baseReapOpts(t *testing.T, run *reapTestRunFn) Options {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "advoq/advoq")
	t.Setenv("GITHUB_RUN_ID", "555")
	t.Setenv("CIVM_RUNNER_SLOT", "advoq")
	t.Setenv("COMPOSE_PROJECT_NAME", "advoq")
	opts := DefaultOptionsFromEnv(EventJobCompleted)
	opts.Execute = true
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 60, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }
	// Hermetico: força o cenario idle para o cache-trim não deferir na box de CI.
	opts.ActivityFn = func(context.Context) ([]idle.Activity, error) { return nil, nil }
	opts.RunFn = run.fn
	return opts
}

// TestJobCompletedReapsOwnRunImages prova o reap escopado: o job-completed roda
// `image prune -a -f --filter label=com.docker.compose.project=<scope>` para o
// slot bare E para o `<slot>-<run_id>` (a forma que o consumidor usa). Ambos os
// labels são deste runner — nunca de um sibling.
func TestJobCompletedReapsOwnRunImages(t *testing.T) {
	run := &reapTestRunFn{}
	opts := baseReapOpts(t, run)

	Run(context.Background(), opts)

	joined := strings.Join(run.commands, "\n")
	wantSlot := "docker image prune -a -f --filter label=com.docker.compose.project=advoq"
	wantRun := "docker image prune -a -f --filter label=com.docker.compose.project=advoq-555"
	if !strings.Contains(joined, wantSlot) {
		t.Errorf("job-completed deve reapar as imagens do compose project do slot %q\nGot:\n%s", wantSlot, joined)
	}
	if !strings.Contains(joined, wantRun) {
		t.Errorf("job-completed deve reapar as imagens do compose project per-run %q\nGot:\n%s", wantRun, joined)
	}
}

// TestRunImageReapNeverUnscopedPruneAll é o guard de segurança central: o reap
// SÓ pode usar `image prune -a` COM um filtro de label. Um `image prune -a` sem
// label removeria qualquer imagem taggeada sem container — incluindo a base de
// vendor que um sibling acabou de pull e ainda não subiu container (o "No such
// image" race que o PR #135 removeu de vez do path online).
func TestRunImageReapNeverUnscopedPruneAll(t *testing.T) {
	run := &reapTestRunFn{}
	opts := baseReapOpts(t, run)

	Run(context.Background(), opts)

	for _, cmd := range run.commands {
		if strings.Contains(cmd, "image prune -a") && !strings.Contains(cmd, "--filter label=com.docker.compose.project=") {
			t.Fatalf("image prune -a só pode rodar com filtro de label de compose project, got: %q", cmd)
		}
	}
	// O floor dangling-only (`image prune -f`, sem -a) permanece — não foi removido.
	if !strings.Contains(strings.Join(run.commands, "\n"), "docker image prune -f") {
		t.Errorf("o image prune dangling-only (-f sem -a) deve continuar rodando como floor")
	}
}

// TestRunImageReapNoSlotIsNoop prova o fail-safe: sem CIVM_RUNNER_SLOT nem
// COMPOSE_PROJECT_NAME no env (env degradado) não há como escopar o reap a este
// runner com segurança, então NENHUM `image prune -a` roda. Reapar sem escopo
// reabriria o race cross-runner.
func TestRunImageReapNoSlotIsNoop(t *testing.T) {
	run := &reapTestRunFn{}
	opts := baseReapOpts(t, run)
	// Simula .env sem as chaves de isolamento: DefaultOptionsFromEnv lê o env uma
	// vez, então o escopo degradado é o ComposeProject vazio nas Options.
	opts.ComposeProject = ""

	Run(context.Background(), opts)

	for _, cmd := range run.commands {
		if strings.Contains(cmd, "image prune -a") {
			t.Fatalf("sem slot/projeto no env, nenhum image prune -a pode rodar (fail-safe), got: %q", cmd)
		}
	}
}

// TestJobStartedDoesNotReapRunImages prova que o reap é só do modo rotineiro
// (job-completed). No job-started o run mal começou — suas imagens vão ser
// usadas/reusadas, reapá-las seria contraproducente.
func TestJobStartedDoesNotReapRunImages(t *testing.T) {
	run := &reapTestRunFn{}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "advoq/advoq")
	t.Setenv("GITHUB_RUN_ID", "555")
	t.Setenv("CIVM_RUNNER_SLOT", "advoq")
	t.Setenv("COMPOSE_PROJECT_NAME", "advoq")
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	// 80% usado → o disk-pressure cleanup roda no job-started.
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }
	opts.ActivityFn = func(context.Context) ([]idle.Activity, error) { return nil, nil }
	opts.RunFn = run.fn

	Run(context.Background(), opts)

	for _, cmd := range run.commands {
		if strings.Contains(cmd, "--filter label=com.docker.compose.project=") {
			t.Fatalf("job-started não deve reapar imagens de run por label de compose, got: %q", cmd)
		}
	}
}

// TestComposeProjectFromEnv prova a fonte do escopo: prefere CIVM_RUNNER_SLOT
// (identidade estável do runner) e cai no COMPOSE_PROJECT_NAME; ambos vazios →
// string vazia (fail-safe a jusante no reap).
func TestComposeProjectFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		slot    string
		project string
		want    string
	}{
		{"slot tem precedencia", "advoq", "outro", "advoq"},
		{"fallback para COMPOSE_PROJECT_NAME", "", "advoq-web", "advoq-web"},
		{"trim de espaco no slot", "  advoq  ", "", "advoq"},
		{"ambos vazios -> vazio", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CIVM_RUNNER_SLOT", tt.slot)
			t.Setenv("COMPOSE_PROJECT_NAME", tt.project)
			if got := composeProjectFromEnv(); got != tt.want {
				t.Errorf("composeProjectFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRunImageReapScopes prova a derivação dos escopos: slot bare sempre, e
// `<slot>-<run_id>` quando há RunID; sem project → nil (no-op).
func TestRunImageReapScopes(t *testing.T) {
	tests := []struct {
		name    string
		project string
		runID   string
		want    []string
	}{
		{"project + run_id -> ambos", "advoq", "555", []string{"advoq", "advoq-555"}},
		{"project sem run_id -> so o slot", "advoq", "", []string{"advoq"}},
		{"sem project -> nil", "", "555", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runImageReapScopes(Options{ComposeProject: tt.project, RunID: tt.runID})
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("runImageReapScopes = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRunImageReapFailureIsNonFatal prova que o reap é best-effort: se o
// `image prune -a` falhar (daemon down, etc.), o job-completed continua exit 0
// com a falha como Warning — higiene pós-job nunca falha o job que a precede.
func TestRunImageReapFailureIsNonFatal(t *testing.T) {
	run := &reapTestRunFn{failOn: "image prune -a"}
	opts := baseReapOpts(t, run)

	res := Run(context.Background(), opts)

	if res.ExitCode != 0 {
		t.Fatalf("falha de reap não pode falhar o job: exit=%d res=%+v", res.ExitCode, res)
	}
	if res.Decision != DecisionCleanupApplied {
		t.Fatalf("decision = %q, want %q (warning não muda decision)", res.Decision, DecisionCleanupApplied)
	}
	var sawReapWarn bool
	for _, a := range res.Actions {
		if a.Name == "docker_run_image_reap" {
			if a.Error != "" {
				t.Errorf("falha de reap deve ser Warning, não Error: %+v", a)
			}
			if a.Warning != "" {
				sawReapWarn = true
			}
		}
	}
	if !sawReapWarn {
		t.Errorf("falha de reap deve ficar visível como Warning, actions=%+v", res.Actions)
	}
}
