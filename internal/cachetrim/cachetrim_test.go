package cachetrim

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/advoq/civm/internal/civm"
)

// TestCapsGlobsNamedDirsAcrossHomes is the regression of the 2026-06 incident:
// the named per-workflow cache dirs (go-build-advoq-services, yarn-advoq-web)
// must be capped, and the family budget divided among matched dirs.
func TestCapsGlobsNamedDirsAcrossHomes(t *testing.T) {
	root := t.TempDir()
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// two runner homes (cleanup runs as root over /home/*), each with named caches.
	home1 := filepath.Join(root, "home", "emdev")
	home2 := filepath.Join(root, "home", "runner")
	gbA := mk("home", "emdev", ".cache", "go-build-advoq-services")
	gbB := mk("home", "emdev", ".cache", "go-build-advoq-devctl")
	gbC := mk("home", "runner", ".cache", "go-build-advoq-web")
	yarn1 := mk("home", "emdev", ".cache", "yarn-advoq-web")
	lint := mk("home", "emdev", ".cache", "golangci-lint")
	npm := mk("home", "emdev", ".npm", "_cacache")

	caps := Caps([]string{home1, home2}, Deps{})
	byPath := make(map[string]Cap, len(caps))
	for _, c := range caps {
		byPath[c.Path] = c
	}
	for _, p := range []string{gbA, gbB, gbC, yarn1, lint, npm} {
		if _, ok := byPath[p]; !ok {
			t.Errorf("Caps() missing named dir %s — would grow unbounded", p)
		}
	}
	// 3 go-build dirs across both homes share the family budget (familyGB/3).
	const giB = int64(1) << 30
	wantPer := int64(civm.DefaultCacheGoBuildMaxGB) * giB / 3
	if got := byPath[gbA].MaxBytes; got != wantPer {
		t.Errorf("go-build per-dir cap=%d, want family/3=%d", got, wantPer)
	}
	// Paths derives 1:1.
	if len(Paths(caps)) != len(caps) {
		t.Errorf("Paths len mismatch")
	}
}

// TestCapsEmptyHomeNoPanic: a non-existent / empty home yields no globbed caps
// (npm/pnpm still added per home but absent → harmless no-op downstream).
func TestCapsEmptyHomeNoPanic(t *testing.T) {
	caps := Caps([]string{filepath.Join(t.TempDir(), "nope")}, Deps{})
	for _, c := range caps {
		if c.MaxBytes <= 0 {
			t.Errorf("cap %s non-positive budget", c.Path)
		}
	}
}

// TestTrimByAgeRemovesOldestPreservesHot proves the core trim: over-cap dir gets
// its oldest files removed down to the cap, but files newer than MinProtect stay.
func TestTrimByAgeRemovesOldestPreservesHot(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mkfile := func(name string, size int64, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := mkfile("old.o", 8<<20, 72*time.Hour) // 8MB, 3 days old → trimmable
	hot := mkfile("hot.o", 8<<20, 1*time.Hour)  // 8MB, 1h old → protected
	mid := mkfile("mid.o", 8<<20, 48*time.Hour) // 8MB, 2 days old → trimmable

	var removed []string
	opts := Options{
		Execute:     true,
		Now:         now,
		RemoveAllFn: func(p string) error { removed = append(removed, p); return nil },
	}
	// cap 12MB: total 24MB → must free ~12MB from oldest (old + mid), never hot.
	c := Cap{Path: dir, MaxBytes: 12 << 20, MinProtect: 24 * time.Hour}
	r := TrimByAge(opts, c)
	if r.Err != nil {
		t.Fatalf("unexpected err: %v", r.Err)
	}
	if r.BytesFound != 24<<20 {
		t.Errorf("BytesFound=%d, want 24MB", r.BytesFound)
	}
	hotRemoved := false
	for _, p := range removed {
		if p == hot {
			hotRemoved = true
		}
	}
	if hotRemoved {
		t.Errorf("trim removed the hot (<MinProtect) file %s", hot)
	}
	if len(removed) == 0 {
		t.Errorf("over-cap dir should have trimmed oldest; removed nothing (old=%s mid=%s)", old, mid)
	}
}

func TestTrimByAgeNoOpUnderCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	r := TrimByAge(Options{Execute: true, Now: time.Now()}, Cap{Path: dir, MaxBytes: 5 << 20, MinProtect: time.Hour})
	if r.BytesFreed != 0 {
		t.Errorf("under-cap dir must not trim; freed=%d", r.BytesFreed)
	}
}

func TestTrimByAgeUnsafePath(t *testing.T) {
	for _, p := range []string{"", "/"} {
		r := TrimByAge(Options{Execute: true, Now: time.Now()}, Cap{Path: p, MaxBytes: 1, MinProtect: time.Hour})
		if r.Err == nil {
			t.Errorf("path %q must be rejected as unsafe", p)
		}
	}
}

func TestTrimByAgeMissingDirIsNoError(t *testing.T) {
	r := TrimByAge(Options{Execute: true, Now: time.Now()}, Cap{Path: filepath.Join(t.TempDir(), "absent"), MaxBytes: 1, MinProtect: time.Hour})
	if r.Err != nil {
		t.Errorf("absent cache dir must be a no-op, got err %v", r.Err)
	}
}
