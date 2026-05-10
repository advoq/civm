// Package bootstrap provisions an Ubuntu 24.04 VM to be a GitHub Actions
// self-hosted runner with parity to ubuntu-latest. All steps are
// idempotent: each Step.Check returns true when already applied.
package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/emersonbusson/ci-vm/internal/specs"
)

// Result is the outcome of one Step.
type Result struct {
	Name        string
	Description string
	AlreadyDone bool
	Executed    bool
	WouldDo     bool // true in dry-run mode if Apply would run
	Err         error
}

// Step is one idempotent provisioning action.
type Step struct {
	Name        string
	Description string
	Check       func(ctx context.Context) (bool, error)
	Apply       func(ctx context.Context) error
}

// Options control the bootstrap run.
type Options struct {
	Execute       bool
	Spec             specs.RunnerImageSpec
	UID              int
	WatchdogTimer    bool   // habilita civmctl-disk-watchdog.timer
	ReverseWatchdog  bool   // habilita civmctl-reverse-watchdog.timer (alarm-of-alarm)
	InstallUnitsFrom string // se nao-vazio, copia .service/.timer de PATH para /etc/systemd/system/ antes de enable
	OSReader         func() (string, error)
	RunFn            func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// DefaultOptions returns sane production defaults.
func DefaultOptions() Options {
	return Options{
		Execute:          false,
		Spec:             specs.Ubuntu2404(),
		UID:              defaultUID(),
		WatchdogTimer:    true, // default: install all timers
		ReverseWatchdog:  true,
		InstallUnitsFrom: "", // assume admin ja copiou; pode setar pra automatizar
		OSReader:         defaultOSReader,
		RunFn:            defaultRun,
	}
}

// Run executes every step (or simulates it when Execute=false).
func Run(ctx context.Context, opts Options) []Result {
	if opts.OSReader == nil {
		opts.OSReader = defaultOSReader
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	steps := buildSteps(opts)
	out := make([]Result, 0, len(steps))
	for _, s := range steps {
		r := Result{Name: s.Name, Description: s.Description}
		done, err := s.Check(ctx)
		if err != nil {
			r.Err = err
			out = append(out, r)
			continue
		}
		r.AlreadyDone = done
		if done {
			out = append(out, r)
			continue
		}
		if !opts.Execute {
			r.WouldDo = true
			out = append(out, r)
			continue
		}
		if err := s.Apply(ctx); err != nil {
			r.Err = err
		} else {
			r.Executed = true
		}
		out = append(out, r)
	}
	return out
}

func buildSteps(opts Options) []Step {
	goVersion, _ := opts.Spec.FindTool("go")
	nodeVersion, _ := opts.Spec.FindTool("node")
	ghVersion, _ := opts.Spec.FindTool("gh")

	return []Step{
		{
			Name:        "verify_os",
			Description: "Confirma Ubuntu 24.04 LTS",
			Check: func(ctx context.Context) (bool, error) {
				out, err := opts.OSReader()
				if err != nil {
					return false, err
				}
				if strings.Contains(out, "VERSION_ID=\"24.04\"") {
					return true, nil
				}
				return false, fmt.Errorf("OS nao e Ubuntu 24.04: %s", firstLine(out))
			},
			Apply: func(ctx context.Context) error {
				return fmt.Errorf("verify_os e read-only; nao ha apply (instale Ubuntu 24.04)")
			},
		},
		{
			Name:        "verify_uid",
			Description: "Confirma execucao como root (UID=0)",
			Check: func(ctx context.Context) (bool, error) {
				if opts.UID == 0 {
					return true, nil
				}
				return false, fmt.Errorf("bootstrap exige sudo (UID atual=%d)", opts.UID)
			},
			Apply: func(ctx context.Context) error {
				return fmt.Errorf("rode novamente com sudo")
			},
		},
		{
			Name:        "apt_base_packages",
			Description: "Instala build-essential, curl, wget, jq, yq, git, ca-certificates",
			Check:       packagesInstalled(opts, "build-essential", "curl", "wget", "jq", "git", "ca-certificates"),
			Apply: func(ctx context.Context) error {
				if _, err := opts.RunFn(ctx, "apt-get", "update", "-y"); err != nil {
					return err
				}
				_, err := opts.RunFn(ctx, "apt-get", "install", "-y",
					"build-essential", "curl", "wget", "jq", "git", "ca-certificates",
					"gnupg", "lsb-release", "software-properties-common")
				return err
			},
		},
		{
			Name:        "install_go",
			Description: "Instala Go " + goVersion.Preferred() + " em /usr/local/go",
			Check: func(ctx context.Context) (bool, error) {
				out, err := opts.RunFn(ctx, "go", "version")
				if err != nil {
					return false, nil
				}
				want := goVersion.Preferred()
				return strings.Contains(string(out), "go"+want), nil
			},
			Apply: func(ctx context.Context) error {
				return installGoTarball(ctx, opts, goVersion.Preferred())
			},
		},
		{
			Name:        "install_node",
			Description: "Instala Node (any version; setup-node sobrepoe no job)",
			Check: func(ctx context.Context) (bool, error) {
				// Aceita qualquer versao instalada — peer workflows usam
				// actions/setup-node@v5 que sobrepoe no /opt/hostedtoolcache.
				_, err := opts.RunFn(ctx, "node", "--version")
				return err == nil, nil
			},
			Apply: func(ctx context.Context) error {
				return installNodeViaNodeSource(ctx, opts, nodeVersion.Preferred())
			},
		},
		{
			Name:        "install_docker",
			Description: "Instala Docker CE (any version; nunca downgrade)",
			Check: func(ctx context.Context) (bool, error) {
				// Aceita qualquer Docker funcional. Nunca fazer downgrade
				// se ja existe (poderia matar containers em execucao).
				_, err := opts.RunFn(ctx, "docker", "--version")
				return err == nil, nil
			},
			Apply: func(ctx context.Context) error {
				return installDockerCE(ctx, opts)
			},
		},
		{
			Name:        "install_gh",
			Description: "Instala GitHub CLI " + ghVersion.Preferred(),
			Check: func(ctx context.Context) (bool, error) {
				out, err := opts.RunFn(ctx, "gh", "--version")
				if err != nil {
					return false, nil
				}
				return strings.Contains(string(out), ghVersion.Preferred()), nil
			},
			Apply: func(ctx context.Context) error {
				return installGHCLI(ctx, opts)
			},
		},
		{
			Name:        "install_systemd_timers",
			Description: "Instala timers: cleanup (diario), disk-watchdog (hourly), reverse-watchdog (4h)",
			Check: func(ctx context.Context) (bool, error) {
				timers := timerList(opts)
				for _, t := range timers {
					out, err := opts.RunFn(ctx, "systemctl", "is-enabled", t)
					if err != nil || !strings.Contains(string(out), "enabled") {
						return false, nil
					}
				}
				return true, nil
			},
			Apply: func(ctx context.Context) error {
				// Optional: copy units from InstallUnitsFrom antes de enable
				if opts.InstallUnitsFrom != "" {
					if _, err := opts.RunFn(ctx, "sh", "-c",
						fmt.Sprintf("cp %s/civmctl-*.{service,timer} /etc/systemd/system/ 2>&1",
							opts.InstallUnitsFrom)); err != nil {
						return fmt.Errorf("cp units from %s: %w", opts.InstallUnitsFrom, err)
					}
				}
				if _, err := opts.RunFn(ctx, "systemctl", "daemon-reload"); err != nil {
					return err
				}
				timers := timerList(opts)
				for _, t := range timers {
					// Idempotente: enable --now em timer ja-enabled e no-op
					if _, err := opts.RunFn(ctx, "systemctl", "enable", "--now", t); err != nil {
						return fmt.Errorf("enable %s: %w", t, err)
					}
				}
				return nil
			},
		},
	}
}

// timerList retorna lista de systemd timers a instalar conforme opts.
func timerList(opts Options) []string {
	timers := []string{"civmctl-cleanup.timer"}
	if opts.WatchdogTimer {
		timers = append(timers, "civmctl-disk-watchdog.timer")
	}
	if opts.ReverseWatchdog {
		timers = append(timers, "civmctl-reverse-watchdog.timer")
	}
	return timers
}

func packagesInstalled(opts Options, names ...string) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		args := append([]string{"-W", "-f", "${Package} ${Status}\n"}, names...)
		out, err := opts.RunFn(ctx, "dpkg-query", args...)
		if err != nil {
			return false, nil
		}
		txt := string(out)
		for _, n := range names {
			if !strings.Contains(txt, n+" install ok installed") {
				return false, nil
			}
		}
		return true, nil
	}
}

func installGoTarball(ctx context.Context, opts Options, version string) error {
	url := fmt.Sprintf("https://go.dev/dl/go%s.linux-amd64.tar.gz", version)
	tmp := "/tmp/go-" + version + ".tar.gz"
	if _, err := opts.RunFn(ctx, "curl", "-fsSL", "-o", tmp, url); err != nil {
		return err
	}
	if _, err := opts.RunFn(ctx, "rm", "-rf", "/usr/local/go"); err != nil {
		return err
	}
	if _, err := opts.RunFn(ctx, "tar", "-C", "/usr/local", "-xzf", tmp); err != nil {
		return err
	}
	_, err := opts.RunFn(ctx, "ln", "-sf", "/usr/local/go/bin/go", "/usr/local/bin/go")
	return err
}

func installNodeViaNodeSource(ctx context.Context, opts Options, version string) error {
	major := strings.SplitN(version, ".", 2)[0]
	url := fmt.Sprintf("https://deb.nodesource.com/setup_%s.x", major)
	tmp := "/tmp/nodesource-" + major + ".sh"
	if _, err := opts.RunFn(ctx, "curl", "-fsSL", "-o", tmp, url); err != nil {
		return err
	}
	if _, err := opts.RunFn(ctx, "bash", tmp); err != nil {
		return err
	}
	_, err := opts.RunFn(ctx, "apt-get", "install", "-y", "nodejs")
	return err
}

func installDockerCE(ctx context.Context, opts Options) error {
	steps := [][]string{
		{"install", "-m", "0755", "-d", "/etc/apt/keyrings"},
		{"sh", "-c", "curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc"},
		{"chmod", "a+r", "/etc/apt/keyrings/docker.asc"},
		{"sh", "-c", `echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list`},
		{"apt-get", "update", "-y"},
		{"apt-get", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin"},
	}
	for _, s := range steps {
		if _, err := opts.RunFn(ctx, s[0], s[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func installGHCLI(ctx context.Context, opts Options) error {
	steps := [][]string{
		{"sh", "-c", "curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg"},
		{"sh", "-c", `echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list`},
		{"apt-get", "update", "-y"},
		{"apt-get", "install", "-y", "gh"},
	}
	for _, s := range steps {
		if _, err := opts.RunFn(ctx, s[0], s[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// RenderTable writes a human-readable table.
func RenderTable(results []Result, opts Options, w io.Writer) {
	mode := "DRY-RUN"
	if opts.Execute {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "Modo: %s\n", mode)
	fmt.Fprintf(w, "Spec alvo: %s %s (%s)\n\n", opts.Spec.OSDistro, opts.Spec.OSVersion, opts.Spec.UpstreamURL)
	fmt.Fprintf(w, "%-22s %-50s %s\n", "STEP", "DESCRICAO", "STATUS")
	fmt.Fprintln(w, strings.Repeat("-", 90))
	doneCount, applyCount, errCount := 0, 0, 0
	for _, r := range results {
		status := "?"
		switch {
		case r.Err != nil:
			status = "erro: " + truncate(r.Err.Error(), 30)
			errCount++
		case r.AlreadyDone:
			status = "ja-instalado"
			doneCount++
		case r.Executed:
			status = "aplicado"
			applyCount++
		case r.WouldDo:
			status = "(seria-aplicado)"
		}
		fmt.Fprintf(w, "%-22s %-50s %s\n", r.Name, truncate(r.Description, 50), status)
	}
	fmt.Fprintln(w, strings.Repeat("-", 90))
	fmt.Fprintf(w, "Resumo: %d ja-instalados, %d aplicados, %d erros\n", doneCount, applyCount, errCount)
	if !opts.Execute && errCount == 0 {
		fmt.Fprintln(w, "Para aplicar: rode novamente com sudo + --execute")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ---- defaults ----

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
