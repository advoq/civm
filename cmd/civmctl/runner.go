package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"time"

	"github.com/emersonbusson/ci-vm/internal/runner"
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
	repo := fs.String("repo", "", "owner/repo (ex: emersonbusson/compexhub)")
	token := fs.String("token", "", "registration token (efemero ~1h via gh api)")
	short := fs.String("short", "", "suffix curto do diretorio (ex: cmpx, vitae)")
	label := fs.String("label", "vitae-ci", "labels CSV")
	runnerVersion := fs.String("runner-version", "2.334.0", "versao do actions/runner")
	baseDir := fs.String("base-dir", "", "base dir (default: \\$HOME do user atual)")
	runAs := fs.String("run-as", "", "user que vai rodar o service (default: user atual)")
	execute := fs.Bool("execute", false, "aplicar (default: dry-run)")
	timeoutMin := fs.Int("timeout", 10, "timeout total em minutos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de runner add:", err)
		return exitUsage
	}
	if *baseDir == "" {
		*baseDir = userHomeOrDefault()
	}
	if *runAs == "" {
		*runAs = userNameOrDefault()
	}
	opts := runner.DefaultOptions()
	opts.Repo = *repo
	opts.Token = *token
	opts.Short = *short
	opts.Label = *label
	opts.RunnerVersion = *runnerVersion
	opts.BaseDir = *baseDir
	opts.RunAsUser = *runAs
	opts.Execute = *execute
	if *execute {
		fmt.Fprintln(os.Stderr, "AVISO: --execute vai modificar o sistema (mkdir, curl, tar, sudo svc.sh).")
		fmt.Fprintln(os.Stderr, "Pressione Ctrl+C em 3s para abortar...")
		time.Sleep(3 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutMin)*time.Minute)
	defer cancel()
	results, err := runner.Add(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return exitUsage
	}
	runner.RenderTable(results, opts, os.Stdout)
	for _, r := range results {
		if r.Err != nil {
			return 1
		}
	}
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
	fs := flag.NewFlagSet("runner remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	short := fs.String("short", "", "suffix curto (ex: cmpx, vitae, advoq)")
	token := fs.String("token", "", "remove-token (gh api -X POST /repos/.../actions/runners/remove-token)")
	baseDir := fs.String("base-dir", "", "base dir (default: $HOME)")
	execute := fs.Bool("execute", false, "aplicar (default: dry-run)")
	timeoutMin := fs.Int("timeout", 5, "timeout em minutos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de runner remove:", err)
		return exitUsage
	}
	if *baseDir == "" {
		*baseDir = userHomeOrDefault()
	}
	opts := runner.DefaultRemoveOptions()
	opts.Short = *short
	opts.Token = *token
	opts.BaseDir = *baseDir
	opts.Execute = *execute
	if *execute {
		fmt.Fprintln(os.Stderr, "AVISO: --execute vai parar service + remover diretorio.")
		fmt.Fprintln(os.Stderr, "Pressione Ctrl+C em 3s para abortar...")
		time.Sleep(3 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutMin)*time.Minute)
	defer cancel()
	results, err := runner.Remove(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return exitUsage
	}
	runner.RenderRemoveTable(results, opts, os.Stdout)
	for _, r := range results {
		if r.Err != nil {
			return 1
		}
	}
	return 0
}

func userHomeOrDefault() string {
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/home/runner"
}

func userNameOrDefault() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if h := os.Getenv("USER"); h != "" {
		return h
	}
	return "runner"
}
