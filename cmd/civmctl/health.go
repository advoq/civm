package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/emersonbusson/ci-vm/internal/health"
)

func runHealth(args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workDir := fs.String("work-dir", "/home/runner/_work", "diretorio do runner para checar disco")
	timeoutSec := fs.Int("timeout", 5, "timeout em segundos para coleta")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de health:", err)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	c := health.NewDefaultCollector(*workDir)
	r := c.Collect(ctx)
	r.Render(os.Stdout)
	return r.Exit()
}
