package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/emersonbusson/ci-vm/internal/billing"
)

func runBilling(args []string) int {
	fs := flag.NewFlagSet("billing-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "owner/repo (ex: emersonbusson/compexhub)")
	workflow := fs.String("workflow", "ci.yml", "nome do workflow file (ex: ci.yml)")
	limit := fs.Int("limit", 5, "numero de runs a fetchar")
	thresholdSec := fs.Int("threshold-sec", 10, "duracao maxima (segundos) pra considerar 'morto cedo'")
	minBlocked := fs.Int("min-blocked", 3, "min consecutive blocked runs pra StatusBlocked")
	jsonOut := fs.Bool("json", false, "saida JSON")
	timeoutSec := fs.Int("timeout", 15, "timeout em segundos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de billing-status:", err)
		return exitUsage
	}
	opts := billing.DefaultOptions()
	opts.Repo = *repo
	opts.WorkflowFile = *workflow
	opts.Limit = *limit
	opts.Threshold = time.Duration(*thresholdSec) * time.Second
	opts.MinBlocked = *minBlocked
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	status, runs, err := billing.Detect(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return status.ExitCode()
	}
	if *jsonOut {
		if err := billing.RenderJSON(status, runs, opts, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "erro ao gerar JSON:", err)
			return 2
		}
	} else {
		billing.Render(status, runs, opts, os.Stdout)
	}
	return status.ExitCode()
}
