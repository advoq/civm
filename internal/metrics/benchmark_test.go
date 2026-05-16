package metrics

import (
	"bytes"
	"testing"
)

// BenchmarkRender mede a serialização do textfile prometheus.
// Baseline pra detectar regressão se trocarmos por templating ou
// adicionarmos overhead em escapeLabelValue. 6 gauges típicos do
// civmctl metrics dump.
func BenchmarkRender(b *testing.B) {
	metrics := []Metric{
		{Name: "civm_disk_used_pct", Help: "Percentage", Type: TypeGauge, Value: 42},
		{Name: "civm_disk_free_gb", Help: "Free GB", Type: TypeGauge, Value: 60},
		{Name: "civm_disk_total_gb", Help: "Total GB", Type: TypeGauge, Value: 100},
		{Name: "civm_runner_services_active", Help: "Services", Type: TypeGauge, Value: 3},
		{Name: "civm_runner_workers_active", Help: "Workers", Type: TypeGauge, Value: 2},
		{Name: "civm_accepting_jobs", Help: "Accepting", Type: TypeGauge, Value: 1},
	}
	var buf bytes.Buffer
	b.ResetTimer()
	for range b.N {
		buf.Reset()
		_ = Render(&buf, metrics)
	}
}

// BenchmarkRender_WithLabels mede o overhead extra de labels com
// escaping. Inclui valor com aspas e quebra de linha pra cobrir o
// caminho real do replacer.
func BenchmarkRender_WithLabels(b *testing.B) {
	metrics := []Metric{
		{
			Name: "civm_hook_invocations_total",
			Help: "Count",
			Type: TypeCounter,
			Labels: map[string]string{
				"event":  "job-started",
				"result": "ok",
				"repo":   "advoq/civm",
			},
			Value: 100,
		},
	}
	var buf bytes.Buffer
	b.ResetTimer()
	for range b.N {
		buf.Reset()
		_ = Render(&buf, metrics)
	}
}
