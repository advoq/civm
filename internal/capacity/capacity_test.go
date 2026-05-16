package capacity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCheckBlocksWhenDiskHigh(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxDiskPct = 80
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 10, nil }
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return []byte("0"), nil }
	r := Check(context.Background(), opts)
	if r.AcceptingJobs || r.DiskUsedPct != 90 {
		t.Fatalf("report=%+v", r)
	}
}

func TestCheckAcceptsWhenDiskOK(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxDiskPct = 85
	opts.StatfsFn = func(string) (uint64, uint64, error) {
		return 100 << 30, 60 << 30, nil // 100GB total, 60GB free → 40% used
	}
	calls := map[string]int{}
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls[name]++
		switch name {
		case "systemctl":
			return []byte("actions.runner.foo.service loaded\nactions.runner.bar.service loaded\n"), nil
		case "pgrep":
			return []byte("3\n"), nil
		}
		return nil, nil
	}
	r := Check(context.Background(), opts)
	if !r.AcceptingJobs {
		t.Fatalf("expected accepting, got %+v", r)
	}
	if r.DiskUsedPct != 40 || r.DiskFreeGB != 60 || r.DiskTotalGB != 100 {
		t.Fatalf("disk fields = %+v", r)
	}
	if r.RunnerServices != 2 || r.RunnerWorkers != 3 {
		t.Fatalf("runners = %+v", r)
	}
}

func TestCheckBlocksWhenStatfsFails(t *testing.T) {
	opts := DefaultOptions()
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 0, 0, errors.New("EIO") }
	r := Check(context.Background(), opts)
	if r.AcceptingJobs || r.Reason == "" {
		t.Fatalf("expected blocked with reason: %+v", r)
	}
}

func TestCheckBlocksWhenTotalZero(t *testing.T) {
	opts := DefaultOptions()
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 0, 0, nil }
	r := Check(context.Background(), opts)
	if r.AcceptingJobs {
		t.Fatalf("expected blocked when total=0: %+v", r)
	}
}

func TestCheckAppliesDefaultsForEmptyOptions(t *testing.T) {
	r := Check(context.Background(), Options{
		StatfsFn: func(string) (uint64, uint64, error) { return 100, 50, nil },
		RunFn:    func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
	})
	if r.DiskPath != "/" {
		t.Fatalf("Path default = %q", r.DiskPath)
	}
}

func TestCountRunnerServicesOnRunFail(t *testing.T) {
	got := countRunnerServices(context.Background(),
		func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("boom") })
	if got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestCountRunnerWorkersHandlesInvalidOutput(t *testing.T) {
	got := countRunnerWorkers(context.Background(),
		func(context.Context, string, ...string) ([]byte, error) { return []byte("not-a-number"), nil })
	if got != 0 {
		t.Fatalf("got %d, want 0 for invalid pgrep output", got)
	}
}

func TestRenderJSONShape(t *testing.T) {
	var buf bytes.Buffer
	r := Report{DiskPath: "/", DiskUsedPct: 42, AcceptingJobs: true}
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var parsed Report
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	if parsed.DiskUsedPct != 42 || !parsed.AcceptingJobs {
		t.Fatalf("roundtrip: %+v", parsed)
	}
}

func TestRenderTextIncludesStateAndReason(t *testing.T) {
	var buf bytes.Buffer
	RenderText(&buf, Report{AcceptingJobs: false, Reason: "disk full"})
	out := buf.String()
	if !strings.Contains(out, "blocked") || !strings.Contains(out, "disk full") {
		t.Fatalf("text missing fields: %q", out)
	}
}
