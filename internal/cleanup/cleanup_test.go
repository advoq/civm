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
)

type fakeEntry struct {
	name    string
	mtime   time.Time
	isDir   bool
	size    int64
}

func (f fakeEntry) Name() string       { return f.name }
func (f fakeEntry) Size() int64        { return f.size }
func (f fakeEntry) Mode() fs.FileMode  { return 0644 }
func (f fakeEntry) ModTime() time.Time { return f.mtime }
func (f fakeEntry) IsDir() bool        { return f.isDir }
func (f fakeEntry) Sys() any           { return nil }

func mkFS(now time.Time) fstest.MapFS {
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-1 * time.Hour)
	mid := now.Add(-10 * 24 * time.Hour)
	return fstest.MapFS{
		"tmp/old.txt":     {Data: []byte("xxxxxxxxxx"), ModTime: old},     // delete (>7d, >2h)
		"tmp/mid.log":     {Data: []byte("yyyy"), ModTime: mid},            // delete (>7d, >2h)
		"tmp/recent.txt":  {Data: []byte("zz"), ModTime: recent},           // skip (<2h, <7d)
		"work/x/_actions": {Data: []byte("aaaaaa"), ModTime: old},          // delete (>14d, >2h)
		"work/y/_actions": {Data: []byte("b"), ModTime: recent},            // skip (<2h)
	}
}

func walkFS(testFS fstest.MapFS) func(root string, fn fs.WalkDirFunc) error {
	return func(root string, fn fs.WalkDirFunc) error {
		return fs.WalkDir(testFS, root, fn)
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

func TestRun_Execute_CallsRm(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	mfs := mkFS(now)
	rmCalls := 0
	opts := DefaultOptions()
	opts.WorkDir = "work"
	opts.TmpDir = "tmp"
	opts.Execute = true
	opts.Now = now
	opts.WalkFn = walkFS(mfs)
	opts.DockerPrune = false
	opts.AptClean = false
	opts.RunFn = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "rm" {
			rmCalls++
		}
		return []byte(""), nil
	}
	actions := Run(context.Background(), opts)
	if rmCalls == 0 {
		t.Errorf("nenhum rm chamado; esperava deletar pelo menos 1 caminho")
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
	opts := DefaultOptions()
	opts.Execute = true
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
	opts := DefaultOptions()
	opts.Execute = true
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
	opts := DefaultOptions()
	opts.Execute = true
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
