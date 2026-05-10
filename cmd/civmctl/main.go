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
  help            Esta mensagem

EXEMPLOS
  civmctl version-pins
  civmctl health
  civmctl cleanup --dry-run
  sudo civmctl bootstrap --execute
  civmctl runner add --token=ghp_xxx --url=https://github.com/owner/repo

DOCUMENTACAO
  PRD/SPEC: docs/specs/civmctl/
  Runbooks: runbooks/MULTI-PROJECT-RUNNER.md
  Source canonico de versoes: actions/runner-images Ubuntu2404-Readme.md
`)
}
