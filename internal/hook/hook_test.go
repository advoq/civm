package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobCompletedCleansWorkspaceButPreservesHotCaches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// DefaultOptionsFromEnv lê RUNNER_TEMP/GITHUB_WORKSPACE/etc. do ambiente;
	// quando este teste roda no runner self-hosted do próprio civm, esses
	// vars apontam para um work root REAL fora do mock — limpa para isolar.
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")
	var removed []string
	var commands []string
	opts := DefaultOptionsFromEnv(EventJobCompleted)
	opts.Execute = true
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 60, nil }
	opts.RemoveAllFn = func(path string) error { removed = append(removed, path); return nil }
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	// Path real sob /home/ é exigido por safeWorkRoot; o conteúdo do dir é
	// simulado via ReadDirFn — não tocamos o filesystem.
	workRoot := "/home/civm-test/actions-runner-civm/_work"
	opts.WorkRoot = workRoot
	opts.ReadDirFn = func(path string) ([]os.DirEntry, error) {
		if path != workRoot {
			return nil, fmt.Errorf("unexpected ReadDir path: %s", path)
		}
		return []os.DirEntry{
			fakeDirEntry("_tool"),
			fakeDirEntry("_actions"),
			fakeDirEntry("_temp"),
			fakeDirEntry("repo"),
		}, nil
	}
	res := Run(context.Background(), opts)
	if res.Decision != DecisionCleanupApplied || res.ExitCode != 0 {
		t.Fatalf("res=%+v", res)
	}
	joined := strings.Join(removed, "\n")
	if strings.Contains(joined, "_tool") || strings.Contains(joined, "_actions") {
		t.Fatalf("removed hot cache: %v", removed)
	}
	if !strings.Contains(joined, "_temp") || !strings.Contains(joined, "repo") {
		t.Fatalf("missing workspace removals: %v", removed)
	}
	if len(commands) == 0 {
		t.Fatal("expected maintenance commands")
	}
}

type fakeEntry string

func (f fakeEntry) Name() string               { return string(f) }
func (fakeEntry) IsDir() bool                  { return true }
func (fakeEntry) Type() os.FileMode            { return os.ModeDir }
func (fakeEntry) Info() (os.FileInfo, error)   { return nil, nil }

func fakeDirEntry(name string) os.DirEntry { return fakeEntry(name) }

// TestJobCompletedPreservesHotCachesUnderHome valida que job-completed
// NÃO remove os caches em $HOME (.cache/go-build, .npm, .yarn, .pnpm-store).
// Esses caches são caros de reconstruir e o wipe a cada job quebrava
// builds concorrentes na VM compartilhada. Disk pressure cleanup (via
// job-started com disco alto) ainda os limpa.
func TestJobCompletedPreservesHotCachesUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")

	// Cria dirs concretos para os caches sob $HOME — se a função tentar
	// removê-los, vamos detectar via RemoveAllFn captura.
	cachePathsUnderHome := []string{
		filepath.Join(home, ".cache", "go-build"),
		filepath.Join(home, ".npm", "_cacache"),
		filepath.Join(home, ".yarn", "cache"),
		filepath.Join(home, ".pnpm-store"),
	}

	var removed []string
	opts := DefaultOptionsFromEnv(EventJobCompleted)
	opts.Execute = true
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 60, nil }
	opts.RemoveAllFn = func(p string) error { removed = append(removed, p); return nil }
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	res := Run(context.Background(), opts)
	if res.Decision != DecisionCleanupApplied {
		t.Fatalf("decision=%v, want cleanup-applied", res.Decision)
	}
	for _, cache := range cachePathsUnderHome {
		for _, r := range removed {
			if r == cache {
				t.Errorf("job-completed removed hot cache %s — go-build em particular invalida builds concorrentes", cache)
			}
		}
	}
}

// TestJobStartedPurgesHotCachesUnderDiskPressure valida o outro lado:
// quando disk pressure está ativa em job-started (acima de pre-cleanup-pct),
// os caches sob $HOME SÃO removidos como medida agressiva para liberar
// espaço antes do job começar.
func TestJobStartedPurgesHotCachesUnderDiskPressure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")

	var removed []string
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	// 80% disco usado, acima de PreCleanupPct → cleanup purga caches
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(p string) error { removed = append(removed, p); return nil }
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	Run(context.Background(), opts)

	wantCache := filepath.Join(home, ".cache", "go-build")
	found := false
	for _, r := range removed {
		if r == wantCache {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("disk pressure cleanup deveria purgar go-build cache; removed=%v", removed)
	}
}

func TestJobStartedRejectsWhenDiskStillTooHigh(t *testing.T) {
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = false
	opts.PreCleanupPct = 70
	opts.HardFailPct = 90
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 5, nil }
	opts.LogPath = ""
	res := Run(context.Background(), opts)
	if res.Decision != DecisionRejected || res.ExitCode == 0 {
		t.Fatalf("res=%+v", res)
	}
}

func TestRunErrorsWhenStatfsFails(t *testing.T) {
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 0, 0, fmt.Errorf("EIO") }
	opts.LogPath = ""
	res := Run(context.Background(), opts)
	if res.Decision != DecisionError || res.ExitCode == 0 || res.Error == "" {
		t.Fatalf("res=%+v", res)
	}
}

func TestRunErrorsOnUnsupportedEvent(t *testing.T) {
	opts := DefaultOptionsFromEnv(Event("bogus"))
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 50, nil }
	opts.LogPath = ""
	res := Run(context.Background(), opts)
	if res.Decision != DecisionError {
		t.Fatalf("expected error on unknown event, got %+v", res)
	}
}

func TestCleanWorkRootRejectsUnsafeRoot(t *testing.T) {
	a := cleanWorkRoot(Options{Execute: true}, "/etc/passwd")
	if a.Error == "" {
		t.Fatalf("expected unsafe error, got %+v", a)
	}
}

func TestCleanWorkRootHandlesReadDirError(t *testing.T) {
	opts := Options{
		Execute:   true,
		ReadDirFn: func(string) ([]os.DirEntry, error) { return nil, fmt.Errorf("EACCES") },
	}
	a := cleanWorkRoot(opts, "/home/x/actions-runner/_work")
	if a.Error == "" {
		t.Fatalf("expected ReadDir error to propagate, got %+v", a)
	}
}

func TestCleanWorkRootSkipsMissingDir(t *testing.T) {
	opts := Options{
		Execute:   true,
		ReadDirFn: func(string) ([]os.DirEntry, error) { return nil, os.ErrNotExist },
	}
	a := cleanWorkRoot(opts, "/home/x/actions-runner/_work")
	if a.Error != "" {
		t.Fatalf("missing dir should be silent, got %+v", a)
	}
}

func TestCleanWorkRootPropagatesRemoveError(t *testing.T) {
	opts := Options{
		Execute: true,
		ReadDirFn: func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeDirEntry("repo")}, nil
		},
		RemoveAllFn: func(string) error { return fmt.Errorf("EBUSY") },
	}
	a := cleanWorkRoot(opts, "/home/x/actions-runner/_work")
	if a.Error == "" {
		t.Fatalf("expected RemoveAll error to propagate, got %+v", a)
	}
}

func TestRemovePathBlocksUnsafe(t *testing.T) {
	for _, p := range []string{"", "  ", "/"} {
		a := removePath(Options{Execute: true}, p, "cache")
		if a.Error == "" {
			t.Fatalf("expected unsafe error for %q, got %+v", p, a)
		}
	}
}

func TestRemovePathPropagatesError(t *testing.T) {
	opts := Options{Execute: true, RemoveAllFn: func(string) error { return fmt.Errorf("ENOSPC") }}
	a := removePath(opts, "/tmp/cache", "cache")
	if a.Error == "" {
		t.Fatalf("expected RemoveAll error, got %+v", a)
	}
}

func TestRenderJSONHook(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, Result{Event: EventJobStarted, Decision: DecisionOK}); err != nil {
		t.Fatal(err)
	}
	var parsed Result
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	if parsed.Event != EventJobStarted || parsed.Decision != DecisionOK {
		t.Fatalf("roundtrip: %+v", parsed)
	}
}

func TestRenderTextHookShowsActions(t *testing.T) {
	var buf bytes.Buffer
	RenderText(&buf, Result{
		Event:    EventJobCompleted,
		Decision: DecisionCleanupApplied,
		Error:    "oops",
		Actions: []Action{
			{Name: "ok-action", Path: "/x", Executed: true},
			{Name: "dry-action", Path: "/y", Executed: false},
			{Name: "fail-action", Path: "/z", Error: "boom"},
		},
	})
	out := buf.String()
	for _, want := range []string{"job-completed", "Error: oops", "ok-action", "dry-action", "fail-action", "boom"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text missing %q:\n%s", want, out)
		}
	}
}

func TestAppendLogWritesSlogJSON(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "hooks.jsonl")
	opts := Options{
		Execute:    true,
		LogPath:    logPath,
		MkdirAllFn: os.MkdirAll,
	}
	res := Result{
		Event: EventJobStarted, Decision: DecisionOK,
		Repository: "advoq/civm", RunID: "12345",
		DiskUsedPct: 42,
	}
	if err := appendLog(opts, res); err != nil {
		t.Fatalf("appendLog: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// slog.JSONHandler adds time/level/msg + attrs as flat keys.
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	for _, key := range []string{"time", "level", "msg", "event", "decision", "repository", "run_id", "disk_used_pct"} {
		if _, ok := rec[key]; !ok {
			t.Errorf("missing slog field %q in %v", key, rec)
		}
	}
	if rec["event"] != "job-started" || rec["msg"] != "hook event" || rec["level"] != "INFO" {
		t.Errorf("unexpected slog record: %v", rec)
	}
}

func TestAppendLogPromotesErrorLevel(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "hooks.jsonl")
	opts := Options{Execute: true, LogPath: logPath, MkdirAllFn: os.MkdirAll}
	cases := map[Decision]string{
		DecisionError:    "ERROR",
		DecisionRejected: "WARN",
		DecisionOK:       "INFO",
	}
	for dec, wantLevel := range cases {
		_ = os.Remove(logPath)
		if err := appendLog(opts, Result{Event: EventJobStarted, Decision: dec}); err != nil {
			t.Fatalf("appendLog(%s): %v", dec, err)
		}
		data, _ := os.ReadFile(logPath)
		var rec map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
			t.Fatalf("invalid JSON for %s: %v", dec, err)
		}
		if rec["level"] != wantLevel {
			t.Errorf("decision=%s level=%v, want %s", dec, rec["level"], wantLevel)
		}
	}
}

func TestAppendLogNoopWhenDisabled(t *testing.T) {
	if err := appendLog(Options{LogPath: ""}, Result{}); err != nil {
		t.Fatalf("empty path should be noop, got %v", err)
	}
	if err := appendLog(Options{LogPath: "/tmp/x", Execute: false}, Result{}); err != nil {
		t.Fatalf("dry-run should be noop, got %v", err)
	}
}

func TestSafeWorkRoot(t *testing.T) {
	valid := "/home/emdev/actions-runner-advoq/_work"
	if !safeWorkRoot(valid) {
		t.Fatalf("expected safe: %s", valid)
	}
	for _, root := range []string{"/", "/home/emdev", "/tmp/_work", "/home/emdev/actions-runner/_tool"} {
		if safeWorkRoot(root) {
			t.Fatalf("expected unsafe: %s", root)
		}
	}
}

// FuzzSafeWorkRoot enforces the safety invariant of safeWorkRoot for arbitrary
// input. Anything safeWorkRoot accepts must, after filepath.Clean, contain
// "/home/" and "/actions-runner", and end in "/_work" — i.e. no path the
// fuzzer constructs may escape the runner work-root whitelist.
func FuzzSafeWorkRoot(f *testing.F) {
	seeds := []string{
		"/home/emdev/actions-runner-advoq/_work",
		"/home/runner/actions-runner-1/_work",
		"/home/emdev/actions-runner/_work/../../etc",
		"/home/../home/runner/actions-runner/_work",
		"/etc/passwd",
		"/tmp/_work",
		"/home/emdev/actions-runner/_tool",
		"",
		"//home//emdev//actions-runner//_work",
		"/home/emdev/actions-runner/_work\x00/etc",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, root string) {
		if !safeWorkRoot(root) {
			return
		}
		clean := filepath.Clean(root)
		if !filepath.IsAbs(clean) {
			t.Fatalf("safeWorkRoot accepted non-absolute %q (clean=%q)", root, clean)
		}
		if !strings.HasPrefix(clean, "/home/") ||
			!strings.Contains(clean, "/actions-runner") ||
			!strings.HasSuffix(clean, "/_work") {
			t.Fatalf("safeWorkRoot accepted %q but clean=%q breaks invariant", root, clean)
		}
		for _, part := range strings.Split(clean, "/") {
			if part == ".." {
				t.Fatalf("safeWorkRoot accepted %q with traversal component after Clean: %q", root, clean)
			}
		}
	})
}
