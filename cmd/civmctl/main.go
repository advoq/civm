// civmctl: zero-effort CLI to provision and maintain the ci-vm self-hosted
// GitHub Actions runner. See docs/specs/civmctl/PRD.md for design.
package main

import (
	"fmt"
	"os"
)

const exitUsage = 64

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(exitUsage)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "version-pins":
		os.Exit(runVersionPins(args))
	case "health":
		os.Exit(runHealth(args))
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
	case "ci":
		os.Exit(runCI(args))
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
	fmt.Print(`civmctl — provisionamento zero-esforco da VM ci-vm

USO
  civmctl <comando> [flags]

COMANDOS
  version-pins    Imprime as versoes alvo (paridade com ubuntu-latest)
  bootstrap       Provisiona Ubuntu 24.04 com tools alvo (idempotente)
  cleanup         Limpa Docker, /tmp, _work, apt cache (cron diario)
  health          Health check (disk, mem, runners, ultimo cleanup)
  runner          Gerencia runners GitHub Actions self-hosted
  drift           Detecta versoes pinadas vs upstream actions/runner-images
  billing-status  Detecta billing-block heuristico (3 runs failure <10s)
  disk-watchdog   Trigger cleanup agressivo se disk >threshold (default 80%%)
  ci              Subcomandos CI cross-peer (local-report)
  reverse-watchdog Alerta se disk-watchdog nao disparou em >MaxAge (default 2h)
  bootstrap-everything  Wrapper: cp systemd units + daemon-reload + bootstrap --execute
  peer-status     Consolida billing + runners + last run em 1 view por peer-repo
  help            Esta mensagem

EXEMPLOS
  civmctl version-pins
  civmctl health
  civmctl cleanup --dry-run
  sudo civmctl bootstrap --execute
  civmctl drift
  civmctl billing-status --repo=owner/repo
  civmctl billing-status --repo=owner/repo --json
  civmctl runner add --repo=owner/repo --token=$(gh api ...) --short=cmpx
  civmctl runner add --repo=owner/repo --token=... --short=cmpx --execute
  civmctl runner remove --short=cmpx --token=$(gh api -X POST .../remove-token) --execute
  civmctl runner list --json | jq '.runners[] | select(.repo == "emersonbusson/ci-vm")'
  civmctl runner restart --short=civm-1 --execute
  civmctl runner upgrade --short=cmpx --new-version=2.335.0 --execute
  civmctl reverse-watchdog --max-age-hours=2
  sudo civmctl bootstrap-everything --units-source=/opt/ci-vm/deploy/systemd --execute
  civmctl peer-status --repo=emersonbusson/compexhub
  civmctl health --json | jq '.exit'
  civmctl reverse-watchdog --max-age-hours=4
  sudo civmctl bootstrap --install-units-from=/opt/ci-vm/deploy/systemd --execute
  civmctl disk-watchdog --threshold-pct=80 --execute
  civmctl ci local-report --repo=owner/repo --sha=abc... --state=success --context="Local VM CI"

DOCUMENTACAO
  PRD/SPEC: docs/specs/civmctl/
  Runbooks: runbooks/MULTI-PROJECT-RUNNER.md
  Source canonico de versoes: actions/runner-images Ubuntu2404-Readme.md
`)
}
