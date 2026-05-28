package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/advoq/civm/internal/civm"
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

func (f fakeEntry) Name() string             { return string(f) }
func (fakeEntry) IsDir() bool                { return true }
func (fakeEntry) Type() os.FileMode          { return os.ModeDir }
func (fakeEntry) Info() (os.FileInfo, error) { return nil, nil }

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

// TestJobStartedUnderPressureTrimsCachesByAge valida que, sob disk pressure,
// job-started faz trim por idade dos caches ($HOME) em vez de wipe total. O
// wipe total apagava o go-build cache quente de um job concorrente em
// compilação ("could not import ...: no such file or directory").
func TestJobStartedUnderPressureTrimsCachesByAge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")

	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	// 80% disco usado, acima de PreCleanupPct → cleanup roda.
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	res := Run(context.Background(), opts)

	var hasTrim, hasWholesalePurge bool
	for _, a := range res.Actions {
		switch a.Name {
		case "cache_trim":
			hasTrim = true
		case "cache":
			hasWholesalePurge = true
		}
	}
	if !hasTrim {
		t.Errorf("job-started under pressure should age-trim caches; actions=%+v", res.Actions)
	}
	if hasWholesalePurge {
		t.Errorf("job-started must not wholesale-purge shared caches (races concurrent builds); actions=%+v", res.Actions)
	}
}

// TestJobStartedPreservesActiveWorkspaceUnderDiskPressure valida que, sob disk
// pressure em job-started, o cleanup NÃO apaga o GITHUB_WORKSPACE que o runner
// acabou de criar para o job que está começando — senão o job falha com
// "working directory ... No such file or directory". Outras entradas stale de
// _work (ex.: _temp) ainda são limpas e os caches sob $HOME ainda purgados.
func TestJobStartedPreservesActiveWorkspaceUnderDiskPressure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUNNER_TEMP", "")
	workRoot := "/home/civm-test/actions-runner-advoq/_work"
	t.Setenv("GITHUB_WORKSPACE", workRoot+"/advoq/advoq")
	t.Setenv("GITHUB_REPOSITORY", "advoq/advoq")
	t.Setenv("GITHUB_RUN_ID", "1")

	var removed []string
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	// 80% usado, acima de PreCleanupPct → disk pressure cleanup.
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(p string) error { removed = append(removed, p); return nil }
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = workRoot
	opts.ReadDirFn = func(path string) ([]os.DirEntry, error) {
		if path != workRoot {
			return nil, fmt.Errorf("unexpected ReadDir path: %s", path)
		}
		return []os.DirEntry{
			fakeDirEntry("_tool"),
			fakeDirEntry("_actions"),
			fakeDirEntry("_temp"),
			fakeDirEntry("advoq"),
		}, nil
	}

	Run(context.Background(), opts)

	joined := strings.Join(removed, "\n")
	if strings.Contains(joined, filepath.Join(workRoot, "advoq")) {
		t.Fatalf("disk-pressure cleanup apagou o workspace ativo %q — quebra o job iniciando; removed=%v", filepath.Join(workRoot, "advoq"), removed)
	}
	if !strings.Contains(joined, filepath.Join(workRoot, "_temp")) {
		t.Fatalf("esperava limpar _temp stale; removed=%v", removed)
	}
	if strings.Contains(joined, filepath.Join(workRoot, "_tool")) || strings.Contains(joined, filepath.Join(workRoot, "_actions")) {
		t.Fatalf("removeu cache quente _tool/_actions; removed=%v", removed)
	}
}

func TestJobStartedDemotesCacheDeleteRaceWhenDiskDropsBelowHardFail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")

	now := time.Now()
	statCalls := 0
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.Now = now
	opts.PreCleanupPct = 70
	opts.HardFailPct = 90
	opts.StatfsFn = func(string) (uint64, uint64, error) {
		statCalls++
		if statCalls == 1 {
			return 100, 20, nil // 80% used, triggers pressure cleanup.
		}
		return 100, 35, nil // 65% used after cleanup, below hard fail.
	}
	// A large, old cache file the age-trim must remove, but the remove races a
	// concurrent writer ("directory not empty") — must demote to a warning.
	opts.WalkDirFn = walkCacheFiles([]cacheFile{
		{path: "/home/.cache/go-build/old", size: 8 * (int64(1) << 30), mtime: now.Add(-72 * time.Hour)},
	})
	opts.RemoveAllFn = func(p string) error {
		return fmt.Errorf("remove %s: directory not empty", p)
	}
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	res := Run(context.Background(), opts)
	if res.Decision != DecisionCleanupApplied || res.ExitCode != 0 {
		t.Fatalf("res=%+v", res)
	}
	foundWarning := false
	for _, action := range res.Actions {
		if action.Warning != "" && strings.Contains(action.Warning, "directory not empty") {
			foundWarning = true
		}
		if action.Error != "" {
			t.Fatalf("cache race should be warning, got action error: %+v", action)
		}
	}
	if !foundWarning {
		t.Fatalf("missing cache race warning: %+v", res.Actions)
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
	a := cleanWorkRoot(Options{Execute: true}, "/etc/passwd", false)
	if a.Error == "" {
		t.Fatalf("expected unsafe error, got %+v", a)
	}
}

func TestCleanWorkRootHandlesReadDirError(t *testing.T) {
	opts := Options{
		Execute:   true,
		ReadDirFn: func(string) ([]os.DirEntry, error) { return nil, fmt.Errorf("EACCES") },
	}
	a := cleanWorkRoot(opts, "/home/x/actions-runner/_work", false)
	if a.Error == "" {
		t.Fatalf("expected ReadDir error to propagate, got %+v", a)
	}
}

func TestCleanWorkRootSkipsMissingDir(t *testing.T) {
	opts := Options{
		Execute:   true,
		ReadDirFn: func(string) ([]os.DirEntry, error) { return nil, os.ErrNotExist },
	}
	a := cleanWorkRoot(opts, "/home/x/actions-runner/_work", false)
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
	a := cleanWorkRoot(opts, "/home/x/actions-runner/_work", false)
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

type cacheFile struct {
	path  string
	size  int64
	mtime time.Time
}

func walkCacheFiles(files []cacheFile) func(string, fs.WalkDirFunc) error {
	return func(root string, fn fs.WalkDirFunc) error {
		for _, f := range files {
			d := dirEntryFile{name: filepath.Base(f.path), size: f.size, mtime: f.mtime}
			if err := fn(f.path, d, nil); err != nil {
				return err
			}
		}
		return nil
	}
}

type dirEntryFile struct {
	name  string
	size  int64
	mtime time.Time
}

func (d dirEntryFile) Name() string               { return d.name }
func (dirEntryFile) IsDir() bool                  { return false }
func (dirEntryFile) Type() fs.FileMode            { return 0 }
func (d dirEntryFile) Info() (fs.FileInfo, error) { return fileInfoStub(d), nil }

type fileInfoStub dirEntryFile

func (f fileInfoStub) Name() string       { return f.name }
func (f fileInfoStub) Size() int64        { return f.size }
func (fileInfoStub) Mode() fs.FileMode    { return 0 }
func (f fileInfoStub) ModTime() time.Time { return f.mtime }
func (fileInfoStub) IsDir() bool          { return false }
func (fileInfoStub) Sys() any             { return nil }

func TestTrimCacheByAge(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	const KiB = int64(1024)
	tests := []struct {
		name       string
		files      []cacheFile
		maxBytes   int64
		minProtect time.Duration
		wantFound  int64
		wantFreed  int64
		wantKept   []string // file paths that must NOT be removed
		wantGone   []string // file paths that MUST be removed
	}{
		{
			name:       "empty cache is no-op",
			files:      nil,
			maxBytes:   10 * KiB,
			minProtect: time.Hour,
			wantFound:  0,
			wantFreed:  0,
		},
		{
			name: "under cap is no-op",
			files: []cacheFile{
				{path: "/cache/a", size: 2 * KiB, mtime: now.Add(-7 * 24 * time.Hour)},
				{path: "/cache/b", size: 3 * KiB, mtime: now.Add(-1 * time.Hour)},
			},
			maxBytes:   10 * KiB,
			minProtect: time.Hour,
			wantFound:  5 * KiB,
			wantFreed:  0,
			wantKept:   []string{"/cache/a", "/cache/b"},
		},
		{
			name: "over cap removes oldest first",
			files: []cacheFile{
				{path: "/cache/oldest", size: 4 * KiB, mtime: now.Add(-30 * 24 * time.Hour)},
				{path: "/cache/mid", size: 4 * KiB, mtime: now.Add(-7 * 24 * time.Hour)},
				{path: "/cache/new", size: 4 * KiB, mtime: now.Add(-2 * time.Hour)},
			},
			maxBytes:   8 * KiB,
			minProtect: time.Hour, // new is 2h old → not protected
			wantFound:  12 * KiB,
			wantFreed:  4 * KiB,
			wantGone:   []string{"/cache/oldest"},
			wantKept:   []string{"/cache/mid", "/cache/new"},
		},
		{
			name: "protect window keeps hot files even if cap requires them",
			files: []cacheFile{
				{path: "/cache/hot1", size: 4 * KiB, mtime: now.Add(-10 * time.Minute)},
				{path: "/cache/hot2", size: 4 * KiB, mtime: now.Add(-30 * time.Minute)},
				{path: "/cache/cold", size: 4 * KiB, mtime: now.Add(-7 * 24 * time.Hour)},
			},
			maxBytes:   1 * KiB, // cap is way below; only cold can be removed
			minProtect: time.Hour,
			wantFound:  12 * KiB,
			wantFreed:  4 * KiB,
			wantGone:   []string{"/cache/cold"},
			wantKept:   []string{"/cache/hot1", "/cache/hot2"},
		},
		{
			name: "stops once target met",
			files: []cacheFile{
				{path: "/cache/a", size: 4 * KiB, mtime: now.Add(-30 * 24 * time.Hour)},
				{path: "/cache/b", size: 4 * KiB, mtime: now.Add(-20 * 24 * time.Hour)},
				{path: "/cache/c", size: 4 * KiB, mtime: now.Add(-10 * 24 * time.Hour)},
			},
			maxBytes:   9 * KiB, // need to free 3 KiB → only removes "a" (4 KiB)
			minProtect: time.Hour,
			wantFound:  12 * KiB,
			wantFreed:  4 * KiB,
			wantGone:   []string{"/cache/a"},
			wantKept:   []string{"/cache/b", "/cache/c"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var removed []string
			opts := Options{
				Execute:     true,
				Now:         now,
				WalkDirFn:   walkCacheFiles(tc.files),
				RemoveAllFn: func(p string) error { removed = append(removed, p); return nil },
			}
			a := trimCacheByAge(opts, "/cache", tc.maxBytes, tc.minProtect)
			if a.Error != "" {
				t.Fatalf("unexpected error: %s", a.Error)
			}
			if a.BytesFound != tc.wantFound {
				t.Errorf("BytesFound=%d, want %d", a.BytesFound, tc.wantFound)
			}
			if a.BytesFreed != tc.wantFreed {
				t.Errorf("BytesFreed=%d, want %d", a.BytesFreed, tc.wantFreed)
			}
			for _, want := range tc.wantGone {
				found := false
				for _, r := range removed {
					if r == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %s to be removed, removed=%v", want, removed)
				}
			}
			for _, want := range tc.wantKept {
				for _, r := range removed {
					if r == want {
						t.Errorf("expected %s to be kept, but it was removed", want)
					}
				}
			}
		})
	}
}

func TestTrimCacheByAgeDryRunDoesNotRemove(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	files := []cacheFile{
		{path: "/cache/old", size: 10 * 1024, mtime: now.Add(-30 * 24 * time.Hour)},
	}
	var removed []string
	opts := Options{
		Execute:     false,
		Now:         now,
		WalkDirFn:   walkCacheFiles(files),
		RemoveAllFn: func(p string) error { removed = append(removed, p); return nil },
	}
	a := trimCacheByAge(opts, "/cache", 1, time.Hour)
	if len(removed) != 0 {
		t.Fatalf("dry-run should not remove anything, got %v", removed)
	}
	// BytesFreed still accounts for what would be freed
	if a.BytesFreed == 0 {
		t.Errorf("dry-run BytesFreed should be > 0 (estimate), got 0")
	}
}

func TestTrimCacheByAgeRejectsUnsafePath(t *testing.T) {
	t.Setenv("HOME", "/home/example")
	for _, p := range []string{"", "  ", "/", "/home/example"} {
		a := trimCacheByAge(Options{Execute: true, Now: time.Now()}, p, 1024, time.Hour)
		if a.Error == "" {
			t.Errorf("expected unsafe path error for %q, got %+v", p, a)
		}
	}
}

func TestTrimCacheByAgeHandlesMissingCache(t *testing.T) {
	opts := Options{
		Execute: true,
		Now:     time.Now(),
		WalkDirFn: func(root string, fn fs.WalkDirFunc) error {
			// Mimic filepath.WalkDir when root is absent: returns the lstat error.
			return &fs.PathError{Op: "lstat", Path: root, Err: fs.ErrNotExist}
		},
	}
	a := trimCacheByAge(opts, "/cache/missing", 1024, time.Hour)
	if a.Error != "" {
		t.Errorf("missing cache should be silent, got %s", a.Error)
	}
	if a.BytesFound != 0 || a.BytesFreed != 0 {
		t.Errorf("missing cache should yield zero stats, got found=%d freed=%d", a.BytesFound, a.BytesFreed)
	}
}

func TestJobCompletedUsesGentleDockerSequence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")
	var commands []string
	opts := DefaultOptionsFromEnv(EventJobCompleted)
	opts.Execute = true
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 60, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	Run(context.Background(), opts)

	joined := strings.Join(commands, "\n")
	for _, want := range []string{
		"docker buildx prune --force --filter until=24h",
		"docker image prune -af --filter until=168h",
		"docker container prune -f",
		"docker volume prune -f",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("job-completed missing gentle docker step %q\nGot:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "docker system prune") {
		t.Errorf("job-completed should not run aggressive docker system prune, got:\n%s", joined)
	}
}

func TestJobCompletedDemotesCommandFailureToWarning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")

	opts := DefaultOptionsFromEnv(EventJobCompleted)
	opts.Execute = true
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 60, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		// Simula buildx ausente: docker buildx prune falha.
		if name == "docker" && len(args) > 0 && args[0] == "buildx" {
			return nil, fmt.Errorf(`exec: "docker buildx": executable file not found in $PATH`)
		}
		// Simula sudo sem NOPASSWD: fstrim falha.
		if name == "sudo" && len(args) > 0 && args[0] == "fstrim" {
			return nil, fmt.Errorf("sudo: a password is required")
		}
		return nil, nil
	}
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	res := Run(context.Background(), opts)

	if res.Decision != DecisionCleanupApplied {
		t.Fatalf("decision = %v, want cleanup-applied (warnings shouldn't change decision)", res.Decision)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (warnings shouldn't fail the hook)", res.ExitCode)
	}
	var sawBuildxWarn, sawFstrimWarn bool
	for _, a := range res.Actions {
		if a.Error != "" {
			t.Errorf("routine action %s should have used Warning, got Error=%s", a.Name, a.Error)
		}
		if a.Name == "docker_buildx_prune" && a.Warning != "" {
			sawBuildxWarn = true
		}
		if a.Name == "fstrim" && a.Warning != "" {
			sawFstrimWarn = true
		}
	}
	if !sawBuildxWarn {
		t.Errorf("expected docker_buildx_prune Warning, actions=%+v", res.Actions)
	}
	if !sawFstrimWarn {
		t.Errorf("expected fstrim Warning, actions=%+v", res.Actions)
	}
}

func TestJobStartedCleanupCommandFailureIsNonFatal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")

	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	// All job-started cleanup is best-effort. apt-get clean returns exit 100
	// when a sibling job holds the dpkg/apt lock; a fatal cleanup error must
	// not reject the starting job. Only HardFailPct (genuinely full disk)
	// fails closed at job-started.
	opts.RunFn = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "sudo" {
			return nil, fmt.Errorf("apt-get clean failed")
		}
		return nil, nil
	}
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	res := Run(context.Background(), opts)

	if res.Decision == DecisionError {
		t.Fatalf("job-started cleanup command failure must not be fatal, actions=%+v", res.Actions)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (cleanup warnings must not fail the hook)", res.ExitCode)
	}
	var sawAptWarn bool
	for _, a := range res.Actions {
		if a.Error != "" {
			t.Errorf("job-started cleanup action %s should warn, got Error=%s", a.Name, a.Error)
		}
		if a.Name == "apt_clean" && a.Warning != "" {
			sawAptWarn = true
		}
	}
	if !sawAptWarn {
		t.Errorf("expected apt_clean Warning, actions=%+v", res.Actions)
	}
}

func TestRunWithTimeoutCancelsHungCommand(t *testing.T) {
	originalTimeout := civm.DefaultRoutineCleanupCmdTimeoutSecs
	_ = originalTimeout
	// Mock RunFn that respects ctx and hangs forever otherwise.
	hung := make(chan struct{})
	defer close(hung)
	opts := Options{
		Execute: true,
		RunFn: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-hung:
				return nil, nil
			}
		},
	}
	// Wrap with a parent ctx that has a short deadline so the test doesn't
	// wait the full DefaultRoutineCleanupCmdTimeoutSecs (which is 120s).
	parent, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	a := commandActionWarn(opts, parent, "hung_cmd", "hang")
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("commandActionWarn took %v, expected to honor parent ctx deadline", elapsed)
	}
	if a.Warning == "" {
		t.Errorf("hung command should produce Warning, got %+v", a)
	}
	if a.Error != "" {
		t.Errorf("hung command in warn mode should not produce Error, got %+v", a)
	}
}

func TestCacheCapsUsesCivmConstants(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	caps := cacheCaps()
	if len(caps) != 4 {
		t.Fatalf("expected 4 caps, got %d", len(caps))
	}
	wantProtect := time.Duration(civm.DefaultCacheTrimMinProtectHours) * time.Hour
	for _, c := range caps {
		if c.minProtect != wantProtect {
			t.Errorf("cap %s minProtect=%v, want %v", c.path, c.minProtect, wantProtect)
		}
		if c.maxBytes <= 0 {
			t.Errorf("cap %s has non-positive maxBytes %d", c.path, c.maxBytes)
		}
	}
	// cachePaths must be derived from caps — one source of truth.
	paths := cachePaths()
	if len(paths) != len(caps) {
		t.Fatalf("cachePaths len=%d, caps len=%d — must match", len(paths), len(caps))
	}
	for i, p := range paths {
		if p != caps[i].path {
			t.Errorf("cachePaths[%d]=%s, caps[%d].path=%s — must derive", i, p, i, caps[i].path)
		}
	}
}

func TestJobStartedUnderPressureUsesFilteredDockerPrune(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")
	var commands []string
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	Run(context.Background(), opts)

	joined := strings.Join(commands, "\n")
	// Unfiltered `docker system prune --volumes` is forbidden: it GCs content a
	// sibling job is actively pulling and can be OOM-killed.
	if strings.Contains(joined, "docker system prune") {
		t.Errorf("job-started must not run unfiltered docker system prune, got:\n%s", joined)
	}
	if !strings.Contains(joined, "docker image prune -af --filter") {
		t.Errorf("job-started should run age-filtered docker image prune, got:\n%s", joined)
	}
}

// TestJobStartedDockerPruneErrorIsNonFatal proves the regression that broke CI:
// a docker prune that errors or is killed at job-started ("signal: killed")
// must NOT fail the hook and reject the starting job. Docker maintenance is
// best-effort; HardFailPct is the only gate that rejects a job for disk.
func TestJobStartedDockerPruneErrorIsNonFatal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNNER_TEMP", "")
	t.Setenv("GITHUB_WORKSPACE", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_RUN_ID", "")
	opts := DefaultOptionsFromEnv(EventJobStarted)
	opts.Execute = true
	opts.PreCleanupPct = 50
	opts.HardFailPct = 95
	opts.StatfsFn = func(string) (uint64, uint64, error) { return 100, 20, nil }
	opts.RemoveAllFn = func(string) error { return nil }
	opts.RunFn = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "docker" {
			return nil, fmt.Errorf("signal: killed")
		}
		return nil, nil
	}
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.LogPath = ""
	opts.WorkRoot = "/home/civm-test/actions-runner/_work"
	opts.ReadDirFn = func(string) ([]os.DirEntry, error) { return nil, nil }

	res := Run(context.Background(), opts)
	if res.Decision == DecisionError || res.ExitCode != 0 {
		t.Fatalf("docker prune error at job-started must not fail the hook, got %+v", res)
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
