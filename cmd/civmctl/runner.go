package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func runRunner(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "uso: civmctl runner <add|list|remove> [flags]")
		return exitUsage
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "add":
		return runRunnerAdd(rest)
	case "list":
		return runRunnerList(rest)
	case "remove":
		return runRunnerRemove(rest)
	default:
		fmt.Fprintf(os.Stderr, "subcomando desconhecido: %s\n", sub)
		return exitUsage
	}
}

func runRunnerAdd(args []string) int {
	fs := flag.NewFlagSet("runner add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", "", "URL do repositorio (https://github.com/owner/repo)")
	token := fs.String("token", "", "registration token (gere em Settings > Actions > Runners)")
	labels := fs.String("labels", "vitae-ci", "labels CSV")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de runner add:", err)
		return exitUsage
	}
	if *url == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "erro: --url e --token sao obrigatorios")
		fmt.Fprintln(os.Stderr, "obtenha o token em: <repo>/settings/actions/runners/new")
		return exitUsage
	}
	fmt.Println("Configurando runner self-hosted...")
	fmt.Println("Esta operacao requer que ./config.sh do actions/runner esteja no PATH")
	fmt.Printf("URL:    %s\n", *url)
	fmt.Printf("Labels: %s\n", *labels)

	cmd := exec.Command("./config.sh",
		"--unattended",
		"--url", *url,
		"--token", *token,
		"--labels", *labels,
		"--name", hostnameOrDefault(),
		"--work", "_work",
		"--replace",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "erro: config.sh falhou: %v\n", err)
		fmt.Fprintln(os.Stderr, "verifique que voce esta no diretorio do runner extraido")
		return 1
	}
	fmt.Println("OK. Para iniciar: sudo ./svc.sh install && sudo ./svc.sh start")
	return 0
}

func runRunnerList(args []string) int {
	cmd := exec.Command("systemctl", "list-units", "--type=service", "--no-pager", "actions.runner.*")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "nenhum runner ativo (ou systemctl indisponivel)")
		return 1
	}
	return 0
}

func runRunnerRemove(args []string) int {
	fmt.Fprintln(os.Stderr, "remove ainda nao implementado; use ./config.sh remove --token=<remove-token> manualmente")
	return 1
}

func hostnameOrDefault() string {
	h, err := os.Hostname()
	if err != nil {
		return "ci-vm-runner"
	}
	return h
}
