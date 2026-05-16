package hook

import (
	"context"
	"os"
	"testing"
)

// BenchmarkSafeWorkRoot estabelece baseline para o check de path
// traversal — chamado em cada add() do workRoots(). Sensível porque é
// pure-string ops em hot path.
func BenchmarkSafeWorkRoot(b *testing.B) {
	valid := "/home/emdev/actions-runner-advoq/_work"
	invalid := "../home/actions-runner/_work"
	b.ResetTimer()
	for range b.N {
		_ = safeWorkRoot(valid)
		_ = safeWorkRoot(invalid)
	}
}

// BenchmarkCleanup_RoutineNoPressure captura o caminho job-completed
// (purgeCaches=false): cleanWorkRoot + 4 commandActions, todos com
// mocks no-op. Detecta regressão em append/alloc.
func BenchmarkCleanup_RoutineNoPressure(b *testing.B) {
	opts := Options{
		Execute:     true,
		WorkRoot:    "/home/civm-test/actions-runner/_work",
		RemoveAllFn: func(string) error { return nil },
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		ReadDirFn:   func(string) ([]os.DirEntry, error) { return nil, nil },
		RunFn:       func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
		StatfsFn:    func(string) (uint64, uint64, error) { return 100, 60, nil },
	}
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		_ = cleanup(opts, ctx, false)
	}
}

// BenchmarkCleanup_DiskPressure captura job-started com purgeCaches=true:
// cleanWorkRoot + 4 cachePaths + 4 commandActions. Diferença vs routine
// estima o custo do cache wipe.
func BenchmarkCleanup_DiskPressure(b *testing.B) {
	b.Setenv("HOME", "/home/civm-bench")
	opts := Options{
		Execute:     true,
		WorkRoot:    "/home/civm-test/actions-runner/_work",
		RemoveAllFn: func(string) error { return nil },
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		ReadDirFn:   func(string) ([]os.DirEntry, error) { return nil, nil },
		RunFn:       func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
		StatfsFn:    func(string) (uint64, uint64, error) { return 100, 5, nil },
	}
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		_ = cleanup(opts, ctx, true)
	}
}

// BenchmarkAppendLog mede o overhead da emissão slog do hook event.
// Chamado uma vez por hook invocation; baseline ajuda a notar regressões
// se evoluirmos para multiple handlers (journald, etc.).
func BenchmarkAppendLog(b *testing.B) {
	dir := b.TempDir()
	opts := Options{
		Execute:    true,
		LogPath:    dir + "/hooks.jsonl",
		MkdirAllFn: os.MkdirAll,
	}
	res := Result{
		Event: EventJobCompleted, Decision: DecisionCleanupApplied,
		Repository: "advoq/civm", RunID: "12345",
		DiskUsedPct: 42,
		Actions: []Action{
			{Name: "work_root", Path: "/home/x/actions-runner/_work", Executed: true},
			{Name: "docker_prune", Executed: true},
		},
	}
	b.ResetTimer()
	for range b.N {
		_ = appendLog(opts, res)
	}
}
