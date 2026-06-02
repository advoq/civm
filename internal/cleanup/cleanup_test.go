package cleanup

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/advoq/civm/internal/safedelete"
)

func mkFS(now time.Time) fstest.MapFS {
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-1 * time.Hour)
	mid := now.Add(-10 * 24 * time.Hour)
	return fstest.MapFS{
		"tmp/old.txt":     {Data: []byte("xxxxxxxxxx"), ModTime: old}, // delete (>7d, >2h)
		"tmp/mid.log":     {Data: []byte("yyyy"), ModTime: mid},       // delete (>7d, >2h)
		"tmp/recent.txt":  {Data: []byte("zz"), ModTime: recent},      // skip (<2h, <7d)
		"work/x/_actions": {Data: []byte("aaaaaa"), ModTime: old},     // delete (>14d, >2h)
		"work/y/_actions": {Data: []byte("b"), ModTime: recent},       // skip (<2h)
	}
}

func walkFS(testFS fstest.MapFS) func(root string, fn fs.WalkDirFunc) error {
	return func(root string, fn fs.WalkDirFunc) error {
		return fs.WalkDir(testFS, root, fn)
	}
}

func noActivity(context.Context) ([]Activity, error) {
	return nil, nil
}

// safeDeleteRecorder captures the paths a hermetic SafeDeleteFn was asked to
// remove, so tests assert deletion without ever calling real sudo or os.Remove.
type safeDeleteRecorder struct {
	targets []string
	err     error // returned by every call when set (simulates a stuck delete)
}

func (r *safeDeleteRecorder) fn(_ context.Context, path string) safedelete.Result {
	r.targets = append(r.targets, path)
	return safedelete.Result{Err: r.err}
}

func testExecuteOptions() Options {
	opts := DefaultOptions()
	opts.Execute = true
	opts.ActivityFn = noActivity
	opts.IdleProbeDelay = 0
	// Keep the docker-heavy lock check hermetic: no lock held, reclaim is a
	// no-op. Tests that exercise the defer path override LockActiveFn.
	opts.LockActiveFn = func() (bool, error) { return false, nil }
	opts.ReclaimStaleFn = func() (bool, error) { return false, nil }
	opts.LockHolderFn = func() string { return "" }
	// Hermetic delete by default (records nothing); tests that assert on
	// deletion install their own safeDeleteRecorder.
	opts.SafeDeleteFn = func(context.Context, string) safedelete.Result { return safedelete.Result{} }
	return opts
}

func TestRun_DefersToActiveDockerHeavyLock(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.LockActiveFn = func() (bool, error) { return true, nil }
	opts.LockHolderFn = func() string { return "docker-heavy advoq/advoq#42" }
	reclaimed := false
	opts.ReclaimStaleFn = func() (bool, error) { reclaimed = true; return false, nil }
	actions := Run(context.Background(), opts)
	if len(actions) != 1 || actions[0].Name != deferredByDockerHeavyLock {
		t.Fatalf("expected single deferred action, got %+v", actions)
	}
	if actions[0].Err != nil {
		t.Fatalf("defer must not be an error (exit 0): %v", actions[0].Err)
	}
	if actions[0].Path != "docker-heavy advoq/advoq#42" {
		t.Fatalf("holder not surfaced in deferred action: %q", actions[0].Path)
	}
	if reclaimed {
		t.Fatalf("must NOT reclaim a fresh/active lock")
	}
}

func TestRun_ReclaimsStaleLockWhenNotActive(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.WorkDir = t.TempDir()
	opts.TmpDir = t.TempDir()
	opts.DockerPrune = false
	opts.AptClean = false
	reclaimed := false
	opts.LockActiveFn = func() (bool, error) { return false, nil }
	opts.ReclaimStaleFn = func() (bool, error) { reclaimed = true; return true, nil }
	actions := Run(context.Background(), opts)
	if !reclaimed {
		t.Fatalf("stale/absent lock must trigger ReclaimStale before proceeding")
	}
	for _, a := range actions {
		if a.Name == deferredByDockerHeavyLock {
			t.Fatalf("must not defer when lock is not active")
		}
	}
}

func TestRun_LockReadErrorDoesNotBlockCleanup(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.WorkDir = t.TempDir()
	opts.TmpDir = t.TempDir()
	opts.DockerPrune = false
	opts.AptClean = false
	opts.LockActiveFn = func() (bool, error) { return false, errors.New("read .hb failed") }
	actions := Run(context.Background(), opts)
	for _, a := range actions {
		if a.Name == deferredByDockerHeavyLock {
			t.Fatalf("lock read error must not defer (DT-v2-16): %+v", actions)
		}
	}
}

func TestRun_DryRun_DetectsOldFiles(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	mfs := mkFS(now)
	opts := DefaultOptions()
	opts.WorkDir = "work"
	opts.TmpDir = "tmp"
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.RunFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}
	opts.DockerPrune = false
	opts.AptClean = false

	actions := Run(context.Background(), opts)
	if len(actions) != 2 {
		t.Fatalf("len(actions) = %d, want 2 (tmp_old + work_old)", len(actions))
	}
	for _, a := range actions {
		if a.Err != nil {
			t.Errorf("%s erro = %v", a.Name, a.Err)
		}
		if a.Executed {
			t.Errorf("%s Executed = true em dry-run", a.Name)
		}
		if a.BytesFreed != 0 {
			t.Errorf("%s BytesFreed = %d em dry-run", a.Name, a.BytesFreed)
		}
		if a.BytesFound == 0 {
			t.Errorf("%s BytesFound = 0; esperava detectar arquivo antigo", a.Name)
		}
	}
}

func TestRun_DefaultWorkDirDiscoversRunnerWorkDirs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	mfs := fstest.MapFS{
		"tmp/fresh.txt": {Data: []byte("x"), ModTime: now.Add(-30 * time.Minute)},
		"home/emdev/actions-runner-a/_work/old.txt":    {Data: []byte("aaaa"), ModTime: old},
		"home/emdev/actions-runner-b/_work/cache.bin":  {Data: []byte("bbbbbb"), ModTime: old},
		"home/emdev/actions-runner-b/_work/recent.bin": {Data: []byte("cc"), ModTime: now.Add(-30 * time.Minute)},
	}
	opts := DefaultOptions()
	opts.TmpDir = "tmp"
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.GlobFn = func(pattern string) ([]string, error) {
		if pattern != "/home/*/actions-runner-*/_work" {
			t.Fatalf("glob pattern = %q", pattern)
		}
		return []string{
			"home/emdev/actions-runner-b/_work",
			"home/emdev/actions-runner-a/_work",
		}, nil
	}
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	opts.DockerPrune = false
	opts.AptClean = false

	actions := Run(context.Background(), opts)
	if len(actions) != 2 {
		t.Fatalf("len(actions) = %d, want 2", len(actions))
	}
	work := actions[1]
	if work.Name != "work_old" {
		t.Fatalf("second action = %s, want work_old", work.Name)
	}
	if work.Err != nil {
		t.Fatalf("work_old err = %v", work.Err)
	}
	if work.BytesFound != 10 {
		t.Fatalf("work_old BytesFound = %d, want 10", work.BytesFound)
	}
	if !strings.Contains(work.Path, "actions-runner-a/_work") || !strings.Contains(work.Path, "actions-runner-b/_work") {
		t.Fatalf("work_old Path omitiu roots descobertos: %s", work.Path)
	}
}

func TestRun_WorkCleanupPreservesRunnerToolAndActionCaches(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	mfs := fstest.MapFS{
		"work/_actions":                       {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/_actions/actions/checkout/main": {Data: []byte("aaaaaaaaaa"), ModTime: old},
		"work/_temp":                          {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/_temp/old.tmp":                  {Data: []byte("bbbb"), ModTime: old},
		"work/_tool":                          {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/_tool/go/1.26.3/bin/go":         {Data: []byte("cccccc"), ModTime: old},
		"work/repo":                           {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/repo/file.txt":                  {Data: []byte("ddddd"), ModTime: old},
	}
	rec := &safeDeleteRecorder{}
	opts := testExecuteOptions()
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.SafeDeleteFn = rec.fn

	a := scanAndMaybeDelete(context.Background(), opts, "work_old", "work", 14*24*time.Hour)
	if a.Err != nil {
		t.Fatalf("work_old err = %v", a.Err)
	}
	if a.BytesFound != 9 || a.BytesFreed != 9 {
		t.Fatalf("BytesFound=%d BytesFreed=%d, want 9", a.BytesFound, a.BytesFreed)
	}
	joined := strings.Join(rec.targets, "\n")
	for _, protected := range []string{"work/_tool", "work/_actions"} {
		if strings.Contains(joined, protected) {
			t.Fatalf("protected cache %s removido: %v", protected, rec.targets)
		}
	}
	for _, want := range []string{"work/_temp", "work/repo"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("delete targets omitiu %s: %v", want, rec.targets)
		}
	}
}

func TestRun_Execute_CallsRm(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	mfs := mkFS(now)
	rec := &safeDeleteRecorder{}
	opts := testExecuteOptions()
	opts.WorkDir = "work"
	opts.TmpDir = "tmp"
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.DockerPrune = false
	opts.AptClean = false
	opts.SafeDeleteFn = rec.fn
	actions := Run(context.Background(), opts)
	if len(rec.targets) == 0 {
		t.Errorf("nenhuma remoção solicitada; esperava deletar pelo menos 1 caminho")
	}
	executedAny := false
	for _, a := range actions {
		if a.Executed {
			executedAny = true
		}
	}
	if !executedAny {
		t.Errorf("nenhuma action ficou Executed")
	}
}

func TestScanAndMaybeDelete_AccumulatesFirstErrorAndContinues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	// Three deletable top-level entries; one of them (a root-owned leftover the
	// escalation could not reclaim) fails. The sweep must record the first error
	// but still attempt and free the others (DT-v2-9: no break on first error).
	mfs := fstest.MapFS{
		"work/a":       {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/a/x.txt": {Data: []byte("aaaaa"), ModTime: old},
		"work/b":       {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/b/y.txt": {Data: []byte("bbbbb"), ModTime: old},
		"work/c":       {Mode: fs.ModeDir | 0755, ModTime: old},
		"work/c/z.txt": {Data: []byte("ccccc"), ModTime: old},
	}
	opts := testExecuteOptions()
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	stuck := errors.New("root-owned, escalation failed")
	var attempted []string
	opts.SafeDeleteFn = func(_ context.Context, path string) safedelete.Result {
		attempted = append(attempted, path)
		if strings.HasSuffix(path, "/b") {
			return safedelete.Result{Escalated: true, Err: stuck}
		}
		return safedelete.Result{}
	}

	a := scanAndMaybeDelete(context.Background(), opts, "work_old", "work", 14*24*time.Hour)
	if a.Err == nil {
		t.Fatalf("expected the stuck candidate error to be surfaced")
	}
	if len(attempted) != 3 {
		t.Fatalf("attempted %d deletes, want 3 (no break on first error): %v", len(attempted), attempted)
	}
	// Two of three succeeded (5 bytes each) -> 10 freed; the stuck one is excluded.
	if a.BytesFreed != 10 {
		t.Fatalf("BytesFreed = %d, want 10 (only the two reclaimable trees)", a.BytesFreed)
	}
}

func TestCleanupChildGuardScopesEscalation(t *testing.T) {
	t.Parallel()
	// The guard mirrors validateCleanupRoot on the PARENT (DT-v2-9): it rejects
	// candidates whose parent is a dangerous root (/, /home, /root, a bare home),
	// not arbitrary depth. In production scanAndMaybeDelete only ever passes
	// direct children of the swept root, so this is the defense-in-depth floor.
	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"direct child of tmp", "/tmp/leftover", false},
		{"child of a work root", "/home/runner/actions-runner/_work/repo", false},
		{"parent is filesystem root", "/etc", true},
		{"parent is /home (bare)", "/home/someuser", true},
		{"root itself", "/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := cleanupChildGuard(tc.path)
			if tc.wantErr && err == nil {
				t.Fatalf("cleanupChildGuard(%q) = nil, want error", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("cleanupChildGuard(%q) = %v, want nil", tc.path, err)
			}
		})
	}
}

func TestRun_Execute_BytesFreedTracksEachPathOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	mfs := fstest.MapFS{
		"tmp/a.txt": {Data: []byte("aaaa"), ModTime: now.Add(-30 * 24 * time.Hour)},
		"tmp/b.txt": {Data: []byte("bb"), ModTime: now.Add(-30 * 24 * time.Hour)},
	}
	opts := testExecuteOptions()
	opts.TmpDir = "tmp"
	opts.WorkDir = "tmp"
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.DockerPrune = false
	opts.AptClean = false
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }

	actions := Run(context.Background(), opts)
	for _, a := range actions {
		if a.BytesFound != a.BytesFreed {
			t.Fatalf("%s BytesFound=%d BytesFreed=%d, want equal", a.Name, a.BytesFound, a.BytesFreed)
		}
		if a.BytesFreed != 6 {
			t.Fatalf("%s BytesFreed=%d, want 6", a.Name, a.BytesFreed)
		}
	}
}

func TestRun_RejectsDangerousCleanupRoots(t *testing.T) {
	t.Parallel()
	for _, root := range []string{"", "/", "/home", "/root", "/home/runner"} {
		opts := DefaultOptions()
		opts.TmpDir = root
		opts.WorkDir = "work"
		opts.DockerPrune = false
		opts.AptClean = false
		opts.WalkFn = func(string, fs.WalkDirFunc) error { return nil }
		actions := Run(context.Background(), opts)
		if actions[0].Err == nil {
			t.Fatalf("root %q sem erro", root)
		}
	}
}

func TestRun_SkipsRecentFiles(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	recentOnlyFS := fstest.MapFS{
		"tmp/fresh.txt": {Data: []byte("x"), ModTime: now.Add(-30 * time.Minute)},
	}
	opts := DefaultOptions()
	opts.WorkDir = "tmp"
	opts.TmpDir = "tmp"
	opts.Now = now
	opts.WalkFn = walkFS(recentOnlyFS)
	opts.DockerPrune = false
	opts.AptClean = false
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }

	actions := Run(context.Background(), opts)
	for _, a := range actions {
		if a.BytesFound != 0 {
			t.Errorf("%s detectou %d bytes em arquivos recentes (deveria ser 0)", a.Name, a.BytesFound)
		}
	}
}

func TestDockerPrune_DryRun(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Execute = false
	opts.RunFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "docker" {
			t.Errorf("comando inesperado: %s", name)
		}
		return []byte("4.2GB (100%)\n"), nil
	}
	a := dockerPrune(context.Background(), opts)
	if a.BytesFound == 0 {
		t.Errorf("BytesFound = 0; esperava parsear 4.2GB")
	}
	if a.Executed {
		t.Errorf("Executed = true em dry-run")
	}
}

func TestDockerPrune_Execute(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.RunFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Total reclaimed space: 3.5GB\n"), nil
	}
	a := dockerPrune(context.Background(), opts)
	if a.BytesFreed == 0 {
		t.Errorf("BytesFreed = 0; esperava parsear 3.5GB")
	}
	if !a.Executed {
		t.Errorf("Executed = false")
	}
}

func TestDockerPrune_Error(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.RunFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("docker daemon not running")
	}
	a := dockerPrune(context.Background(), opts)
	if a.Err == nil {
		t.Errorf("esperava erro propagado")
	}
}

func TestAptClean(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	calls := []string{}
	opts.RunFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	a := aptClean(context.Background(), opts)
	if !a.Executed {
		t.Errorf("Executed = false")
	}
	if len(calls) != 2 {
		t.Errorf("len(calls) = %d, want 2 (clean + autoremove)", len(calls))
	}
}

func TestAptClean_DryRun(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Execute = false
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) {
		t.Errorf("RunFn nao deveria ter sido chamado em dry-run")
		return nil, nil
	}
	a := aptClean(context.Background(), opts)
	if a.Executed {
		t.Errorf("Executed = true em dry-run")
	}
}

func TestParseHumanBytes(t *testing.T) {
	t.Parallel()
	cases := map[string]int64{
		"1GB":   1 << 30,
		"1.5GB": int64(1.5 * float64(1<<30)),
		"512MB": 512 * (1 << 20),
		"100kB": 100 * (1 << 10),
		"50B":   50,
		"":      0,
		"junk":  0,
	}
	for in, want := range cases {
		got := parseHumanBytes(in)
		// Allow tolerance for float multiplication (1.5GB)
		diff := got - want
		if diff < 0 {
			diff = -diff
		}
		if diff > 1024 {
			t.Errorf("parseHumanBytes(%q) = %d, want %d (diff %d)", in, got, want, diff)
		}
	}
}

func TestParseReclaimable(t *testing.T) {
	t.Parallel()
	in := "1GB (100%)\n500MB (50%)\n\n"
	got := parseReclaimable(in)
	want := int64(1<<30) + 500*(1<<20)
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestParseTotalReclaimed(t *testing.T) {
	t.Parallel()
	in := "Some logs\nDeleted Containers\nTotal reclaimed space: 2.5GB\n"
	got := parseTotalReclaimed(in)
	if got == 0 {
		t.Errorf("nao parseou; got 0")
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   int64
		want string
	}{
		{1 << 30, "1.0 GB"},
		{1 << 20, "1.0 MB"},
		{1 << 10, "1.0 kB"},
		{500, "500 B"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderTable_DryRun(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Execute = false
	actions := []Action{
		{Name: "tmp_old", Path: "/tmp", BytesFound: 1 << 30},
		{Name: "docker_prune", Path: "(docker)", BytesFound: 2 << 30},
	}
	var buf bytes.Buffer
	RenderTable(actions, opts, &buf)
	s := buf.String()
	if !strings.Contains(s, "DRY-RUN") {
		t.Errorf("output sem DRY-RUN")
	}
	if !strings.Contains(s, "TOTAL") {
		t.Errorf("output sem TOTAL")
	}
	if !strings.Contains(s, "--execute") {
		t.Errorf("output sem hint --execute")
	}
}

func TestRenderTable_Execute(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.Execute = true
	actions := []Action{
		{Name: "tmp_old", Path: "/tmp", BytesFreed: 1 << 30, Executed: true},
	}
	var buf bytes.Buffer
	RenderTable(actions, opts, &buf)
	s := buf.String()
	if !strings.Contains(s, "EXECUTE") {
		t.Errorf("output sem EXECUTE")
	}
	if strings.Contains(s, "--execute") {
		t.Errorf("dica --execute apareceu em modo execute")
	}
}

func TestRenderTable_Error(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	actions := []Action{
		{Name: "x", Path: "/tmp", Err: errors.New("boom")},
	}
	var buf bytes.Buffer
	RenderTable(actions, opts, &buf)
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("erro nao apareceu na tabela")
	}
}

func TestTruncatePath(t *testing.T) {
	t.Parallel()
	if got := truncatePath("/a/b", 10); got != "/a/b" {
		t.Errorf("got %q, want unchanged", got)
	}
	if got := truncatePath("/very/long/path/that/exceeds", 10); got != "…t/exceeds" {
		t.Errorf("got %q, want truncated tail", got)
	}
}

func TestRun_WalkError(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	opts.WorkDir = "x"
	opts.TmpDir = "x"
	opts.DockerPrune = false
	opts.AptClean = false
	opts.WalkFn = func(string, fs.WalkDirFunc) error {
		return errors.New("walk falhou")
	}
	opts.RunFn = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	actions := Run(context.Background(), opts)
	for _, a := range actions {
		if a.Err == nil {
			t.Errorf("%s sem erro propagado", a.Name)
		}
	}
}

// TestRun_ExecuteBusyReclaimsUnusedDockerAndDefersPrivilegedCleanup is the
// positive replacement for the old "blocks everything when busy" test (issue
// #70). It asserts the PURPOSE, not the happy path: while a Runner.Worker is
// active, the privileged file cleanup and the aggressive system prune/apt MUST
// NOT run, but the unused-only docker prune MUST run (it is safe by
// construction), and the host-busy outcome MUST NOT be an error.
func TestRun_ExecuteBusyReclaimsUnusedDockerAndDefersPrivilegedCleanup(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.WorkDir = "work"
	opts.TmpDir = "tmp"
	opts.DockerPrune = true
	opts.AptClean = true
	opts.ActivityFn = func(context.Context) ([]Activity, error) {
		return []Activity{{PID: 1234, Command: "/home/emdev/actions-runner/bin/Runner.Worker run"}}, nil
	}
	var ranImagePrune, ranBuilderPrune, sawDangerous bool
	opts.RunFn = func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "image prune"):
			ranImagePrune = true
			return []byte("Total reclaimed space: 1GB\n"), nil
		case strings.Contains(joined, "builder prune"):
			ranBuilderPrune = true
			return []byte("Total:  2GB\n"), nil
		case strings.Contains(joined, "system prune") || name == "apt-get":
			sawDangerous = true
		}
		return nil, nil
	}
	rec := &safeDeleteRecorder{}
	opts.SafeDeleteFn = rec.fn

	actions := Run(context.Background(), opts)

	if len(rec.targets) != 0 {
		t.Fatalf("privileged file delete ran while host busy: %v", rec.targets)
	}
	if sawDangerous {
		t.Fatalf("aggressive system prune / apt ran while host busy")
	}
	if !ranImagePrune || !ranBuilderPrune {
		t.Fatalf("safe docker prune did not run when busy: image=%v builder=%v", ranImagePrune, ranBuilderPrune)
	}
	for _, a := range actions {
		if a.Err != nil {
			t.Fatalf("busy host surfaced an error action (should be a benign deferral): %+v", a)
		}
	}
	var safe, deferred *Action
	for i := range actions {
		switch actions[i].Name {
		case "docker_prune_safe":
			safe = &actions[i]
		case deferredByHostBusy:
			deferred = &actions[i]
		}
	}
	if safe == nil || !safe.Executed || safe.BytesFreed != 3*(1<<30) {
		t.Fatalf("docker_prune_safe missing/not executed/wrong bytes: %+v", safe)
	}
	if deferred == nil {
		t.Fatalf("expected %q deferral action, got %+v", deferredByHostBusy, actions)
	}
}

func TestRun_ExecuteRechecksBeforeDeleting(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	mfs := fstest.MapFS{
		"tmp/a.txt": {Data: []byte("aaaa"), ModTime: now.Add(-30 * 24 * time.Hour)},
	}
	opts := testExecuteOptions()
	opts.WorkDir = "tmp"
	opts.TmpDir = "tmp"
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.DockerPrune = false
	opts.AptClean = false
	activityCalls := 0
	opts.ActivityFn = func(context.Context) ([]Activity, error) {
		activityCalls++
		if activityCalls == 1 {
			return nil, nil
		}
		return []Activity{{PID: 5678, Command: "/home/emdev/actions-runner/_work/repo/repo/script.sh"}}, nil
	}
	rec := &safeDeleteRecorder{}
	opts.SafeDeleteFn = rec.fn

	actions := Run(context.Background(), opts)
	if len(rec.targets) != 0 {
		t.Fatalf("delete targets = %v, want none (idle re-check must block deletion)", rec.targets)
	}
	if len(actions) == 0 || actions[0].Err == nil {
		t.Fatalf("esperava erro no primeiro cleanup action: %+v", actions)
	}
}

func TestParseActiveProcessesDetectsWorkersAndWorkdir(t *testing.T) {
	t.Parallel()
	ps := `
 100 1 Sl Runner.Listener /home/emdev/actions-runner/bin/Runner.Listener run --startuptype service
 101 100 Sl Runner.Worker /home/emdev/actions-runner/bin/Runner.Worker run
 102 100 S bash /home/emdev/actions-runner/_work/repo/repo/build.sh
 103 1 S sleep sleep 10
`
	got := parseActiveProcesses(ps, 999)
	if len(got) != 2 {
		t.Fatalf("len(active) = %d, want 2: %+v", len(got), got)
	}
	if got[0].PID != 101 || got[1].PID != 102 {
		t.Fatalf("active pids = %+v", got)
	}
}

func TestActiveBuildProcessIgnoresCleanupCommandItself(t *testing.T) {
	t.Parallel()
	if isActiveBuildProcess("civmctl", "/usr/local/bin/civmctl cleanup --execute --work-dir=/home/emdev/actions-runner/_work") {
		t.Fatalf("cleanup command should not self-block on its own --work-dir arg")
	}
	if !isActiveBuildProcess("docker", "docker buildx build .") {
		t.Fatalf("docker buildx build should be active")
	}
}

func TestEnsureIdleDoubleProbeCatchesNewActivity(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.IdleProbeDelay = time.Nanosecond
	calls := 0
	opts.ActivityFn = func(context.Context) ([]Activity, error) {
		calls++
		if calls == 1 {
			return nil, nil
		}
		return []Activity{{PID: 99, Command: "docker compose up"}}, nil
	}
	if err := ensureIdle(context.Background(), opts); err == nil {
		t.Fatalf("ensureIdle sem erro no segundo probe")
	}
}

func TestEnsureIdleFailsClosedOnProbeError(t *testing.T) {
	t.Parallel()
	opts := testExecuteOptions()
	opts.ActivityFn = func(context.Context) ([]Activity, error) {
		return nil, errors.New("ps unavailable")
	}
	err := ensureIdle(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "nao foi possivel provar") {
		t.Fatalf("ensureIdle err = %v", err)
	}
}

func TestFormatActivitiesLimitsOutput(t *testing.T) {
	t.Parallel()
	activities := []Activity{
		{PID: 1, Command: strings.Repeat("a", 100)},
		{PID: 2, Command: "b"},
		{PID: 3, Command: "c"},
		{PID: 4, Command: "d"},
	}
	got := formatActivities(activities)
	for _, want := range []string{"pid=1", "pid=2", "pid=3", "+1 outro"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatActivities omitiu %q: %s", want, got)
		}
	}
}

func TestDefaultActivitiesRealPSDoesNotError(t *testing.T) {
	// Não usa Parallel: toca ps real.
	if _, err := defaultActivities(context.Background()); err != nil {
		t.Skipf("ps indisponivel neste ambiente: %v", err)
	}
}
