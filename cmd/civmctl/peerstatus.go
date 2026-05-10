package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/emersonbusson/ci-vm/internal/peerstatus"
)

func runPeerStatus(args []string) int {
	fs := flag.NewFlagSet("peer-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "owner/repo (ex: emersonbusson/compexhub)")
	workflow := fs.String("workflow", "ci.yml", "nome do workflow file")
	jsonOut := fs.Bool("json", false, "saida JSON")
	timeoutSec := fs.Int("timeout", 20, "timeout em segundos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de peer-status:", err)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	opts := peerstatus.DefaultOptions()
	opts.Repo = *repo
	opts.WorkflowFile = *workflow
	s, err := peerstatus.Collect(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return exitUsage
	}
	if *jsonOut {
		if err := s.RenderJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "erro ao gerar JSON:", err)
			return 2
		}
	} else {
		s.Render(os.Stdout)
	}
	// Exit code: 0 OK, 1 warn (no runners OR billing blocked com runner), 2 critical
	if s.BillingStatus == "blocked" && s.RunnersOnline == 0 {
		return 2
	}
	if s.RunnersOnline == 0 || s.BillingStatus == "blocked" {
		return 1
	}
	return 0
}
