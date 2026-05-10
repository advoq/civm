// Package runner orchestrates the full GitHub Actions self-hosted runner
// install flow: download tarball, extract, ./config.sh, svc.sh install,
// svc.sh start. Designed for "1 runner per peer-repo on the same VM".
package runner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// AddOptions controls a single runner installation.
type AddOptions struct {
	Repo          string // "emersonbusson/compexhub"
	Token         string // registration token (efêmero ~1h)
	Short         string // suffix do diretorio: ~/actions-runner-<short>
	Label         string // CSV de labels (default: "civm")
	RunnerVersion string // ex: "2.334.0"
	BaseDir       string // ex: "/home/emdev"
	RunAsUser     string // ex: "emdev" (passa para svc.sh install)
	Execute       bool   // false = dry-run; true = aplica
	RunFn         func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultOptions returns sane production defaults.
//
// Label "civm" alinhado com nome do repo infra (auto-explicativo:
// `runs-on: [self-hosted, civm]` em qualquer peer aponta pra
// runner mantido pelo civm). Migracao 2026-05-10 substituiu label
// legacy "vitae-ci" por "civm" em todos peers + runners.
func DefaultOptions() AddOptions {
	return AddOptions{
		Label:         "civm",
		RunnerVersion: "2.334.0",
		Execute:       false,
		RunFn:         defaultRun,
	}
}

// Step is one action in the install flow.
type Step struct {
	Name        string
	Description string
	WouldDo     string // resumo da acao em dry-run
	Apply       func(ctx context.Context) error
}

// Result captures step outcome.
type Result struct {
	Name        string
	Description string
	Executed    bool
	WouldDo     string
	Err         error
}

// Add runs the full install flow (or simulates it when Execute=false).
func Add(ctx context.Context, opts AddOptions) ([]Result, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	dir := fmt.Sprintf("%s/actions-runner-%s", opts.BaseDir, opts.Short)
	tarball := fmt.Sprintf("https://github.com/actions/runner/releases/download/v%s/actions-runner-linux-x64-%s.tar.gz",
		opts.RunnerVersion, opts.RunnerVersion)
	url := fmt.Sprintf("https://github.com/%s", opts.Repo)
	// Naming padrao: ci-vm-<short>. Ex: ci-vm-compexhub, ci-vm-advoq.
	// Para o proprio repo ci-vm: convencao --short=self -> ci-vm-self.
	name := "ci-vm-" + opts.Short
	steps := []Step{
		{
			Name:        "mkdir_dir",
			Description: "Cria diretorio dedicado " + dir,
			WouldDo:     "mkdir -p " + dir,
			Apply: func(ctx context.Context) error {
				_, err := opts.RunFn(ctx, "mkdir", "-p", dir)
				return err
			},
		},
		{
			Name:        "download_runner",
			Description: "Baixa actions/runner v" + opts.RunnerVersion,
			WouldDo:     "curl -fsSL -o " + dir + "/runner.tar.gz " + tarball,
			Apply: func(ctx context.Context) error {
				_, err := opts.RunFn(ctx, "curl", "-fsSL", "-o", dir+"/runner.tar.gz", tarball)
				return err
			},
		},
		{
			Name:        "extract_runner",
			Description: "Extrai tarball em " + dir,
			WouldDo:     "tar -C " + dir + " -xzf " + dir + "/runner.tar.gz && rm " + dir + "/runner.tar.gz",
			Apply: func(ctx context.Context) error {
				if _, err := opts.RunFn(ctx, "tar", "-C", dir, "-xzf", dir+"/runner.tar.gz"); err != nil {
					return err
				}
				_, err := opts.RunFn(ctx, "rm", dir+"/runner.tar.gz")
				return err
			},
		},
		{
			Name:        "config_runner",
			Description: "config.sh --unattended --labels " + opts.Label + " --name " + name,
			WouldDo:     fmt.Sprintf("(cd %s && ./config.sh --unattended --url %s --token *** --labels %s --name %s --work _work --replace)", dir, url, opts.Label, name),
			Apply: func(ctx context.Context) error {
				_, err := opts.RunFn(ctx, "sh", "-c",
					fmt.Sprintf("cd %s && ./config.sh --unattended --url %s --token %s --labels %s --name %s --work _work --replace",
						shellQuote(dir), shellQuote(url), shellQuote(opts.Token), shellQuote(opts.Label), shellQuote(name)))
				return err
			},
		},
		{
			Name:        "install_service",
			Description: "sudo svc.sh install " + opts.RunAsUser,
			WouldDo:     fmt.Sprintf("(cd %s && sudo ./svc.sh install %s)", dir, opts.RunAsUser),
			Apply: func(ctx context.Context) error {
				_, err := opts.RunFn(ctx, "sh", "-c",
					fmt.Sprintf("cd %s && sudo ./svc.sh install %s", shellQuote(dir), shellQuote(opts.RunAsUser)))
				return err
			},
		},
		{
			Name:        "start_service",
			Description: "sudo svc.sh start",
			WouldDo:     fmt.Sprintf("(cd %s && sudo ./svc.sh start)", dir),
			Apply: func(ctx context.Context) error {
				_, err := opts.RunFn(ctx, "sh", "-c",
					fmt.Sprintf("cd %s && sudo ./svc.sh start", shellQuote(dir)))
				return err
			},
		},
	}
	results := make([]Result, 0, len(steps))
	for _, s := range steps {
		r := Result{Name: s.Name, Description: s.Description, WouldDo: s.WouldDo}
		if !opts.Execute {
			results = append(results, r)
			continue
		}
		if err := s.Apply(ctx); err != nil {
			r.Err = err
			results = append(results, r)
			break
		}
		r.Executed = true
		results = append(results, r)
	}
	return results, nil
}

func validateOptions(opts AddOptions) error {
	if opts.Repo == "" {
		return fmt.Errorf("--repo obrigatorio (formato: owner/repo)")
	}
	if !strings.Contains(opts.Repo, "/") {
		return fmt.Errorf("--repo deve ter formato owner/repo")
	}
	if opts.Token == "" {
		return fmt.Errorf("--token obrigatorio")
	}
	if opts.Short == "" {
		return fmt.Errorf("--short obrigatorio (suffix curto, ex: cmpx, vitae)")
	}
	if opts.RunnerVersion == "" {
		return fmt.Errorf("--runner-version obrigatorio")
	}
	if opts.BaseDir == "" {
		return fmt.Errorf("--base-dir obrigatorio")
	}
	if opts.RunAsUser == "" {
		return fmt.Errorf("--run-as obrigatorio (user que vai rodar o service)")
	}
	return nil
}

// RenderTable writes results human-readable.
func RenderTable(results []Result, opts AddOptions, w io.Writer) {
	mode := "DRY-RUN"
	if opts.Execute {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "Modo: %s\n", mode)
	fmt.Fprintf(w, "Repo: %s | Short: %s | Label: %s | Runner: v%s\n\n",
		opts.Repo, opts.Short, opts.Label, opts.RunnerVersion)
	for _, r := range results {
		status := "(seria-aplicado)"
		switch {
		case r.Err != nil:
			status = "erro: " + r.Err.Error()
		case r.Executed:
			status = "aplicado"
		}
		fmt.Fprintf(w, "  %-18s %s\n", r.Name, status)
		if !opts.Execute {
			fmt.Fprintf(w, "    -> %s\n", r.WouldDo)
		}
	}
	fmt.Fprintln(w)
	if !opts.Execute {
		fmt.Fprintln(w, "Para aplicar: rode novamente com --execute")
	}
}

// shellQuote escapes single quotes for safe shell embedding.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ==== Remove ====

// RemoveOptions controls a runner removal.
type RemoveOptions struct {
	Short     string // suffix do diretorio: ~/actions-runner-<short>
	Token     string // remove-token (efemero, gh api .../runners/remove-token)
	BaseDir   string // ex: "/home/emdev"
	Execute   bool   // false = dry-run
	RunFn     func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultRemoveOptions returns sane defaults.
func DefaultRemoveOptions() RemoveOptions {
	return RemoveOptions{
		Execute: false,
		RunFn:   defaultRun,
	}
}

// Remove undoes Add. All steps are idempotent (best-effort): missing dirs
// are skip, services already stopped are skip. Designed to be safe to
// re-run in cleanup automation.
func Remove(ctx context.Context, opts RemoveOptions) ([]Result, error) {
	if err := validateRemoveOptions(opts); err != nil {
		return nil, err
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	dir := fmt.Sprintf("%s/actions-runner-%s", opts.BaseDir, opts.Short)
	steps := []Step{
		{
			Name:        "stop_service",
			Description: "sudo svc.sh stop",
			WouldDo:     fmt.Sprintf("(cd %s && sudo ./svc.sh stop) || true (idempotente)", dir),
			Apply: func(ctx context.Context) error {
				// best-effort; ignore error if not installed
				_, _ = opts.RunFn(ctx, "sh", "-c",
					fmt.Sprintf("cd %s 2>/dev/null && sudo ./svc.sh stop 2>&1 || true", shellQuote(dir)))
				return nil
			},
		},
		{
			Name:        "uninstall_service",
			Description: "sudo svc.sh uninstall",
			WouldDo:     fmt.Sprintf("(cd %s && sudo ./svc.sh uninstall) || true", dir),
			Apply: func(ctx context.Context) error {
				_, _ = opts.RunFn(ctx, "sh", "-c",
					fmt.Sprintf("cd %s 2>/dev/null && sudo ./svc.sh uninstall 2>&1 || true", shellQuote(dir)))
				return nil
			},
		},
		{
			Name:        "config_remove",
			Description: "config.sh remove --token=*** (deregister no GitHub)",
			WouldDo:     fmt.Sprintf("(cd %s && ./config.sh remove --token=***) || true", dir),
			Apply: func(ctx context.Context) error {
				_, _ = opts.RunFn(ctx, "sh", "-c",
					fmt.Sprintf("cd %s 2>/dev/null && ./config.sh remove --token %s 2>&1 || true",
						shellQuote(dir), shellQuote(opts.Token)))
				return nil
			},
		},
		{
			Name:        "remove_dir",
			Description: "rm -rf " + dir,
			WouldDo:     "rm -rf " + dir,
			Apply: func(ctx context.Context) error {
				_, err := opts.RunFn(ctx, "rm", "-rf", dir)
				return err
			},
		},
	}
	results := make([]Result, 0, len(steps))
	for _, s := range steps {
		r := Result{Name: s.Name, Description: s.Description, WouldDo: s.WouldDo}
		if !opts.Execute {
			results = append(results, r)
			continue
		}
		if err := s.Apply(ctx); err != nil {
			r.Err = err
		} else {
			r.Executed = true
		}
		results = append(results, r)
	}
	return results, nil
}

func validateRemoveOptions(opts RemoveOptions) error {
	if opts.Short == "" {
		return fmt.Errorf("--short obrigatorio (suffix curto, ex: cmpx, vitae)")
	}
	if opts.BaseDir == "" {
		return fmt.Errorf("--base-dir obrigatorio")
	}
	if opts.Token == "" {
		return fmt.Errorf("--token obrigatorio (gh api -X POST .../actions/runners/remove-token)")
	}
	return nil
}

// RenderRemoveTable writes remove results human-readable.
func RenderRemoveTable(results []Result, opts RemoveOptions, w io.Writer) {
	mode := "DRY-RUN"
	if opts.Execute {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "Modo: %s\n", mode)
	fmt.Fprintf(w, "Short: %s | Dir: %s/actions-runner-%s\n\n",
		opts.Short, opts.BaseDir, opts.Short)
	for _, r := range results {
		status := "(seria-aplicado)"
		switch {
		case r.Err != nil:
			status = "erro: " + r.Err.Error()
		case r.Executed:
			status = "aplicado"
		}
		fmt.Fprintf(w, "  %-20s %s\n", r.Name, status)
		if !opts.Execute {
			fmt.Fprintf(w, "    -> %s\n", r.WouldDo)
		}
	}
	fmt.Fprintln(w)
	if !opts.Execute {
		fmt.Fprintln(w, "Para aplicar: rode novamente com --execute")
	}
}
