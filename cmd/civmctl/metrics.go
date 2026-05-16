package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/advoq/civm/internal/capacity"
	"github.com/advoq/civm/internal/civm"
	"github.com/advoq/civm/internal/metrics"
)

const defaultMetricsPath = "/var/lib/node_exporter/textfile_collector/civm.prom"

func runMetrics(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "uso: civmctl metrics dump [--out=path]")
		return exitUsage
	}
	if args[0] == "dump" {
		return runMetricsDump(args[1:])
	}
	fmt.Fprintf(os.Stderr, "metrics: subcomando desconhecido %q\n", args[0])
	return exitUsage
}

func runMetricsDump(args []string) int {
	fs := flag.NewFlagSet("metrics dump", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", defaultMetricsPath, "destino do textfile prometheus")
	stdout := fs.Bool("stdout", false, "imprime no stdout em vez de gravar no arquivo (debug)")
	timeoutSec := fs.Int("timeout", civm.DefaultHealthTimeoutSeconds, "timeout em segundos")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "erro nos args de metrics dump:", err)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	r := capacity.Check(ctx, capacity.DefaultOptions())
	samples := buildSamples(r)
	if *stdout {
		if err := metrics.Render(os.Stdout, samples); err != nil {
			fmt.Fprintln(os.Stderr, "erro ao renderizar metrics:", err)
			return 2
		}
		return 0
	}
	if err := metrics.WriteTextfile(*out, samples); err != nil {
		fmt.Fprintln(os.Stderr, "erro ao gravar metrics:", err)
		return 1
	}
	return 0
}

func buildSamples(r capacity.Report) []metrics.Metric {
	accepting := 0.0
	if r.AcceptingJobs {
		accepting = 1
	}
	return []metrics.Metric{
		{Name: "civm_disk_used_pct", Help: "Percentual de disco utilizado no path monitorado", Type: metrics.TypeGauge, Value: float64(r.DiskUsedPct)},
		{Name: "civm_disk_free_gb", Help: "Disco livre em GB", Type: metrics.TypeGauge, Value: float64(r.DiskFreeGB)},
		{Name: "civm_disk_total_gb", Help: "Disco total em GB", Type: metrics.TypeGauge, Value: float64(r.DiskTotalGB)},
		{Name: "civm_runner_services_active", Help: "Quantidade de services actions.runner.*", Type: metrics.TypeGauge, Value: float64(r.RunnerServices)},
		{Name: "civm_runner_workers_active", Help: "Quantidade de workers Runner.Worker em execução", Type: metrics.TypeGauge, Value: float64(r.RunnerWorkers)},
		{Name: "civm_accepting_jobs", Help: "1 se runner está aceitando jobs (disco abaixo do threshold); 0 caso contrário", Type: metrics.TypeGauge, Value: accepting},
	}
}
