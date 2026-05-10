package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/emersonbusson/civm/internal/doctor"
)

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reposRaw := fs.String("repos", strings.Join(doctor.DefaultRepos, ","), "repos owner/repo separados por virgula")
	workflow := fs.String("workflow", "ci.yml", "nome do workflow file")
	jsonOut := fs.Bool("json", false, "saida JSON estruturada")
	timeoutSec := fs.Int("timeout", 20, "timeout em segundos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de doctor:", err)
		return exitUsage
	}
	repos := splitCSV(*reposRaw)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()
	opts := doctor.DefaultOptions()
	opts.Repos = repos
	opts.WorkflowFile = *workflow
	report, err := doctor.Collect(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return exitUsage
	}
	if *jsonOut {
		if err := report.RenderJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "erro ao gerar JSON:", err)
			return 2
		}
	} else {
		report.Render(os.Stdout)
	}
	return report.Exit
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
