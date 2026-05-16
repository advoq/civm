// civmctl: zero-effort CLI to provision and maintain the civm self-hosted
// GitHub Actions runner. See docs/specs/civmctl/PRD.md for design.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const exitUsage = 64

// hookEventFromArgv0 detects when civmctl was invoked as a runner hook
// (via symlink job-started or job-completed in /opt/civm/hooks). Returns
// the event name and true when the basename matches; otherwise false.
// The legacy ".sh" suffix is tolerated for transitional installs.
func hookEventFromArgv0(arg0 string) (string, bool) {
	base := strings.TrimSuffix(filepath.Base(arg0), ".sh")
	switch base {
	case "job-started", "job-completed":
		return base, true
	}
	return "", false
}

func main() {
	// Hook dispatch via argv[0]: symlinks job-started/job-completed (instalados
	// em /opt/civm/hooks/) apontam para este binário; o nome do invocador
	// determina o evento. Eliminamos os shell wrappers antigos.
	if event, ok := hookEventFromArgv0(os.Args[0]); ok {
		os.Exit(runHook(append([]string{event, "--execute"}, os.Args[1:]...)))
	}
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(exitUsage)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "version-pins":
		os.Exit(runVersionPins(args))
	case "parity":
		os.Exit(runParity(args))
	case "health":
		os.Exit(runHealth(args))
	case "doctor":
		os.Exit(runDoctor(args))
	case "cleanup":
		os.Exit(runCleanup(args))
	case "bootstrap":
		os.Exit(runBootstrap(args))
	case "runner":
		os.Exit(runRunner(args))
	case "drift":
		os.Exit(runDrift(args))
	case "billing-status":
		os.Exit(runBilling(args))
	case "disk-watchdog":
		os.Exit(runDiskWatchdog(args))
	case "idle-check":
		os.Exit(runIdleCheck(args))
	case "ci":
		os.Exit(runCI(args))
	case "hook":
		os.Exit(runHook(args))
	case "capacity":
		os.Exit(runCapacity(args))
	case "metrics":
		os.Exit(runMetrics(args))
	case "reverse-watchdog":
		os.Exit(runReverseWatchdog(args))
	case "bootstrap-everything":
		os.Exit(runBootstrapEverything(args))
	case "peer-status":
		os.Exit(runPeerStatus(args))
	case "-h", "--help", "help":
		printHelp()
		os.Exit(0)
	case "-v", "--version":
		fmt.Println("civmctl dev (sem release tag ainda)")
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido: %s\n\n", cmd)
		printHelp()
		os.Exit(exitUsage)
	}
}

func printHelp() {
	fmt.Print(`civmctl — provisionamento zero-esforco da VM civm

USO
  civmctl <comando> [flags]

COMANDOS
  version-pins    Imprime as versoes alvo (paridade com ubuntu-latest)
  parity          Valida ferramentas instaladas vs pins ubuntu-latest
  bootstrap       Provisiona Ubuntu 24.04 com tools alvo (idempotente)
  cleanup         Limpa Docker, /tmp, _work, apt cache (cron diario)
  health          Health check (disk, mem, runners, ultimo cleanup)
  doctor          Diagnostico read-only consolidado host + GitHub runners
  runner          Gerencia runners GitHub Actions self-hosted
  drift           Detecta versoes pinadas vs upstream actions/runner-images
  billing-status  Detecta billing-block heuristico (3 runs failure <10s)
  disk-watchdog   Trigger cleanup agressivo se disk >threshold (default 80%%)
  idle-check      Read-only: 0=idle, 1=busy, 2=unknown
  ci              Subcomandos CI cross-peer (local-report)
  hook            GitHub Actions job hooks (started/completed)
  capacity        Status JSON estável para Busson/integrações
  metrics         Prometheus textfile dump (node_exporter collector)
  reverse-watchdog Alerta se disk-watchdog nao disparou em >MaxAge (default 2h)
  bootstrap-everything  Wrapper: cp systemd units + daemon-reload + bootstrap --execute
  peer-status     Consolida billing + runners + last run em 1 view por peer-repo
  help            Esta mensagem

EXEMPLOS
  civmctl version-pins
  civmctl parity
  civmctl health
  civmctl doctor --json
  civmctl cleanup --dry-run
  sudo civmctl bootstrap --execute
  civmctl drift
  civmctl billing-status --repo=owner/repo
  civmctl billing-status --repo=owner/repo --json
  civmctl runner add --repo=owner/repo --token=$(gh api ...) --short=cmpx
  civmctl runner add --repo=owner/repo --token=... --short=cmpx --execute
  civmctl runner remove --short=cmpx --token=$(gh api -X POST .../remove-token) --execute
  civmctl runner list --json | jq '.runners[] | select(.repo == "advoq/civm")'
  civmctl runner restart --short=civm-1 --execute
  civmctl runner upgrade --short=cmpx --new-version=2.335.0 --execute
  civmctl reverse-watchdog --max-age-hours=2
  sudo civmctl bootstrap-everything --units-source=/opt/civm/deploy/systemd --execute
  civmctl peer-status --repo=emersonbusson/compexhub
  civmctl health --json | jq '.exit'
  civmctl reverse-watchdog --max-age-hours=4
  civmctl idle-check --json
  sudo civmctl bootstrap-everything --units-source=/opt/civm/deploy/systemd --execute
  civmctl disk-watchdog --threshold-pct=80 --execute
  civmctl ci local-report --repo=owner/repo --sha=abc... --state=success --context="Local VM CI"
  civmctl capacity --json
  civmctl metrics dump --stdout
  civmctl metrics dump --out=/var/lib/node_exporter/textfile_collector/civm.prom
  civmctl hook job-completed --execute --json

DOCUMENTACAO
  PRD/SPEC: docs/specs/civmctl/
  Runbooks: runbooks/MULTI-PROJECT-RUNNER.md
  Source canonico de versoes: actions/runner-images Ubuntu2404-Readme.md
`)
}
