package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/emersonbusson/ci-vm/internal/bootstrap"
)

// runBootstrapEverything wrappa bootstrap + cp dos systemd units.
// Pré-requisito: ci-vm repo clonado em --units-source (ou /opt/ci-vm
// como default). civmctl ja deve estar em /usr/local/bin (use
// 'go build -o' ou install standalone antes).
//
// Diferença vs `civmctl bootstrap`: garante que os arquivos
// .service/.timer estao em /etc/systemd/system/ ANTES de tentar
// systemctl enable. Atual `bootstrap` assume que admin ja copiou.
func runBootstrapEverything(args []string) int {
	fs := flag.NewFlagSet("bootstrap-everything", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	unitsSource := fs.String("units-source", "/opt/ci-vm/deploy/systemd",
		"diretorio com .service/.timer files (ex: /opt/ci-vm/deploy/systemd)")
	execute := fs.Bool("execute", false, "aplicar (default: dry-run)")
	watchdog := fs.Bool("watchdog", true, "habilitar disk-watchdog timer")
	timeoutMin := fs.Int("timeout", 30, "timeout total em minutos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args:", err)
		return exitUsage
	}

	if *execute {
		fmt.Fprintln(os.Stderr, "AVISO: --execute vai modificar /etc/systemd/system/, apt install,")
		fmt.Fprintln(os.Stderr, "instalar Go em /usr/local/go, habilitar timers. Ctrl+C em 5s...")
		time.Sleep(5 * time.Second)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutMin)*time.Minute)
	defer cancel()

	steps := buildBootstrapEverythingSteps(*unitsSource, *watchdog, *execute)
	results := runEverythingSteps(ctx, steps, *execute)
	renderEverythingTable(results, *execute, os.Stdout)

	for _, r := range results {
		if r.Err != nil {
			return 1
		}
	}
	return 0
}

type everythingStep struct {
	Name     string
	WouldDo  string
	Apply    func(ctx context.Context) error
}

type everythingResult struct {
	Name     string
	WouldDo  string
	Executed bool
	Err      error
}

func buildBootstrapEverythingSteps(unitsSource string, watchdog, execute bool) []everythingStep {
	systemdDest := "/etc/systemd/system"
	timerNames := []string{"civmctl-cleanup"}
	if watchdog {
		timerNames = append(timerNames, "civmctl-disk-watchdog")
	}

	steps := []everythingStep{
		{
			Name:    "verify_civmctl",
			WouldDo: "which civmctl (precisa estar em /usr/local/bin antes deste comando)",
			Apply: func(ctx context.Context) error {
				if _, err := exec.LookPath("civmctl"); err != nil {
					return fmt.Errorf("civmctl nao encontrado no PATH: %w", err)
				}
				return nil
			},
		},
		{
			Name:    "verify_units_source",
			WouldDo: "ls " + unitsSource + "/*.{service,timer}",
			Apply: func(ctx context.Context) error {
				if _, err := os.Stat(unitsSource); err != nil {
					return fmt.Errorf("units-source %s nao existe: %w (use --units-source pra customizar)", unitsSource, err)
				}
				return nil
			},
		},
	}

	for _, name := range timerNames {
		name := name
		steps = append(steps,
			everythingStep{
				Name:    "cp_" + name + "_service",
				WouldDo: fmt.Sprintf("sudo cp %s/%s.service %s/", unitsSource, name, systemdDest),
				Apply: func(ctx context.Context) error {
					src := filepath.Join(unitsSource, name+".service")
					dst := filepath.Join(systemdDest, name+".service")
					return runShell(ctx, fmt.Sprintf("sudo cp %s %s", shellQuote(src), shellQuote(dst)))
				},
			},
			everythingStep{
				Name:    "cp_" + name + "_timer",
				WouldDo: fmt.Sprintf("sudo cp %s/%s.timer %s/", unitsSource, name, systemdDest),
				Apply: func(ctx context.Context) error {
					src := filepath.Join(unitsSource, name+".timer")
					dst := filepath.Join(systemdDest, name+".timer")
					return runShell(ctx, fmt.Sprintf("sudo cp %s %s", shellQuote(src), shellQuote(dst)))
				},
			},
		)
	}

	steps = append(steps,
		everythingStep{
			Name:    "daemon_reload",
			WouldDo: "sudo systemctl daemon-reload",
			Apply: func(ctx context.Context) error {
				return runShell(ctx, "sudo systemctl daemon-reload")
			},
		},
		everythingStep{
			Name:    "bootstrap_run",
			WouldDo: "civmctl bootstrap " + execFlag(execute) + " --watchdog=" + boolStr(watchdog),
			Apply: func(ctx context.Context) error {
				opts := bootstrap.DefaultOptions()
				opts.Execute = true
				opts.WatchdogTimer = watchdog
				results := bootstrap.Run(ctx, opts)
				for _, r := range results {
					if r.Err != nil {
						return fmt.Errorf("bootstrap step %s: %w", r.Name, r.Err)
					}
				}
				return nil
			},
		},
	)

	return steps
}

func runEverythingSteps(ctx context.Context, steps []everythingStep, execute bool) []everythingResult {
	out := make([]everythingResult, 0, len(steps))
	for _, s := range steps {
		r := everythingResult{Name: s.Name, WouldDo: s.WouldDo}
		if !execute {
			out = append(out, r)
			continue
		}
		if err := s.Apply(ctx); err != nil {
			r.Err = err
			out = append(out, r)
			break
		}
		r.Executed = true
		out = append(out, r)
	}
	return out
}

func renderEverythingTable(results []everythingResult, execute bool, w io.Writer) {
	mode := "DRY-RUN"
	if execute {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "Modo: %s (bootstrap-everything)\n\n", mode)
	for _, r := range results {
		status := "(seria-aplicado)"
		switch {
		case r.Err != nil:
			status = "erro: " + r.Err.Error()
		case r.Executed:
			status = "aplicado"
		}
		fmt.Fprintf(w, "  %-26s %s\n", r.Name, status)
		if !execute {
			fmt.Fprintf(w, "    -> %s\n", r.WouldDo)
		}
	}
	fmt.Fprintln(w)
	if !execute {
		fmt.Fprintln(w, "Para aplicar: sudo civmctl bootstrap-everything --execute")
	}
}

func runShell(ctx context.Context, cmd string) error {
	out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (output: %s)", cmd, err, string(out))
	}
	return nil
}

func shellQuote(s string) string {
	// Simple single-quote escape (consistent with internal/runner.shellQuote).
	return "'" + replaceAll(s, "'", `'\''`) + "'"
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func execFlag(execute bool) string {
	if execute {
		return "--execute"
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
