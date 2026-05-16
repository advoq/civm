package capacity

import (
	"context"
	"testing"
)

// BenchmarkCheck mede o hot path do dump de métricas (chamado por
// civmctl-metrics.timer a cada 1 minuto). Mocks neutralizam syscalls
// reais; mede só a orquestração + parse de saída de gh/systemctl/pgrep.
func BenchmarkCheck(b *testing.B) {
	opts := DefaultOptions()
	opts.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 << 30, 60 << 30, nil
	}
	opts.RunFn = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "systemctl" {
			return []byte("actions.runner.foo.service loaded active running\nactions.runner.bar.service loaded active running\n"), nil
		}
		return []byte("2\n"), nil
	}
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		_ = Check(ctx, opts)
	}
}
