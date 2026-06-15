// Package cachetrim bounds the size of regenerable build/dependency caches under
// the runner home(s) so a shared CI runner's caches cannot grow unbounded and
// fill the host VHDX volume (the 2026-06 PausedCritical incident: the advoq
// workflows point GOCACHE/yarn cache-folder to named per-workflow dirs —
// ~/.cache/go-build-advoq-services hit 13GB — that the old fixed-path cap never
// matched). It is the SINGLE SOURCE of the cache-cap policy, consumed by both
// the job hooks (internal/hook, runs as the runner user) and the disk-pressure
// cleanup (internal/cleanup, runs as root over all /home/* runner homes).
package cachetrim

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/advoq/civm/internal/civm"
)

// Cap is a per-directory size budget: trim Path to at most MaxBytes by removing
// the oldest files, preserving anything modified within MinProtect.
type Cap struct {
	Path       string
	MaxBytes   int64
	MinProtect time.Duration
}

// Deps injects filesystem access so callers and tests stay hermetic.
type Deps struct {
	GlobFn func(pattern string) ([]string, error)
	StatFn func(path string) (os.FileInfo, error)
}

func (d Deps) withDefaults() Deps {
	if d.GlobFn == nil {
		d.GlobFn = filepath.Glob
	}
	if d.StatFn == nil {
		d.StatFn = os.Stat
	}
	return d
}

func (d Deps) glob(pattern string) []string {
	m, _ := d.GlobFn(pattern)
	return m
}

// existingDirs keeps only existing directories, deduplicated by cleaned path.
// Glob already returns existing entries; the fixed extras (e.g. ~/.yarn/cache)
// pass through the same filter so a missing dir does not skew the family budget
// division.
func (d Deps) existingDirs(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		clean := filepath.Clean(p)
		if _, ok := seen[clean]; ok {
			continue
		}
		fi, err := d.StatFn(clean)
		if err != nil || !fi.IsDir() {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

// Caps returns the family-capped cache dirs across the given home roots. Each
// family budget (civm.DefaultCache*MaxGB) is divided among the named variants
// found by glob, so the family total stays bounded regardless of how many
// per-workflow dirs exist. The hook passes a single home (the runner user); the
// root-run cleanup passes every /home/* runner home so caches are bounded even
// when no job is starting (closes the disk-watchdog gap).
func Caps(homes []string, deps Deps) []Cap {
	deps = deps.withDefaults()
	const giB = int64(1) << 30
	protect := time.Duration(civm.DefaultCacheTrimMinProtectHours) * time.Hour
	var caps []Cap
	// family expands a glob (plus optional fixed sub-paths) across every home into
	// one Cap per existing dir, splitting the family budget evenly so the family
	// total is bounded no matter how many named variants exist.
	family := func(familyMaxGB int, glob string, extraSubs ...string) {
		var dirs []string
		for _, home := range homes {
			if home == "" {
				continue
			}
			dirs = append(dirs, deps.glob(filepath.Join(home, glob))...)
			for _, sub := range extraSubs {
				dirs = append(dirs, filepath.Join(home, sub))
			}
		}
		dirs = deps.existingDirs(dirs)
		if len(dirs) == 0 {
			return
		}
		per := int64(familyMaxGB) * giB / int64(len(dirs))
		if per < 1 {
			per = 1
		}
		for _, d := range dirs {
			caps = append(caps, Cap{Path: d, MaxBytes: per, MinProtect: protect})
		}
	}
	family(civm.DefaultCacheGoBuildMaxGB, ".cache/go-build*")
	family(civm.DefaultCacheYarnMaxGB, ".cache/yarn*", ".yarn/cache")
	family(civm.DefaultCacheGolangciLintMaxGB, ".cache/golangci-lint*")
	// npm/pnpm use a single well-known dir per home (no named variants) and the
	// budget is not divided, so no division skew — they enter per home
	// unconditionally (trim/wipe on an absent dir is a no-op).
	for _, home := range homes {
		if home == "" {
			continue
		}
		caps = append(caps,
			Cap{Path: filepath.Join(home, ".npm", "_cacache"), MaxBytes: int64(civm.DefaultCacheNPMMaxGB) * giB, MinProtect: protect},
			Cap{Path: filepath.Join(home, ".pnpm-store"), MaxBytes: int64(civm.DefaultCachePNPMMaxGB) * giB, MinProtect: protect},
		)
	}
	return caps
}

// Paths returns just the paths of caps — the wipe-mode set (disk-pressure purge).
func Paths(caps []Cap) []string {
	if len(caps) == 0 {
		return nil
	}
	paths := make([]string, len(caps))
	for i, c := range caps {
		paths[i] = c.Path
	}
	return paths
}

// Options control one trim run.
type Options struct {
	Execute     bool
	Now         time.Time
	WalkDirFn   func(root string, fn fs.WalkDirFunc) error
	RemoveAllFn func(path string) error
}

func (o Options) withDefaults() Options {
	if o.WalkDirFn == nil {
		o.WalkDirFn = filepath.WalkDir
	}
	if o.RemoveAllFn == nil {
		o.RemoveAllFn = os.RemoveAll
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	return o
}

// Result is one cache dir's trim outcome.
type Result struct {
	Path       string
	BytesFound int64
	BytesFreed int64
	Executed   bool
	Err        error
}

type cacheEntry struct {
	path  string
	size  int64
	mtime time.Time
}

// TrimByAge walks c.Path, sorts files by mtime ascending, and removes the oldest
// until total <= c.MaxBytes. Files newer than Now-c.MinProtect are preserved —
// protects the hot cache of a job that just wrote. No-op if the cache is absent
// or already under the cap.
func TrimByAge(opts Options, c Cap) Result {
	opts = opts.withDefaults()
	r := Result{Path: c.Path, Executed: opts.Execute}
	if strings.TrimSpace(c.Path) == "" || c.Path == "/" || c.Path == os.Getenv("HOME") {
		r.Err = errors.New("unsafe cache path")
		return r
	}
	var entries []cacheEntry
	var total int64
	walkErr := opts.WalkDirFn(c.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		entries = append(entries, cacheEntry{path: p, size: info.Size(), mtime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return r
		}
		r.Err = walkErr
		return r
	}
	r.BytesFound = total
	if total <= c.MaxBytes {
		return r
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.Before(entries[j].mtime) })
	protectCutoff := opts.Now.Add(-c.MinProtect)
	target := total - c.MaxBytes
	var freed int64
	removed := make([]bool, len(entries))

	// MaxBytes e um TETO HARD (a garantia anti-enchimento). trimPass percorre do
	// mais antigo ao mais novo. Pass 1 (allowProtected=false) preserva os arquivos
	// quentes (acessados dentro de MinProtect). Se isso NAO alcanca o cap — o caso
	// do cache de CI sob carga continua, onde TODO arquivo e recente e o cap nunca
	// se aplicava (yarn-advoq-* cresceu a 18GB, incidente 2026-06-15) — Pass 2 trima
	// os protegidos tambem, do mais antigo ao mais novo, ate o cap. A protecao de
	// disco vence a temperatura do cache (Kahneman #16: o fail-safe e o disco).
	trimPass := func(allowProtected bool) error {
		for i := range entries {
			if freed >= target {
				return nil
			}
			if removed[i] {
				continue
			}
			if !allowProtected && c.MinProtect > 0 && entries[i].mtime.After(protectCutoff) {
				continue
			}
			if opts.Execute {
				if err := opts.RemoveAllFn(entries[i].path); err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						removed[i] = true
						continue
					}
					r.Err = err
					return err
				}
			}
			removed[i] = true
			freed += entries[i].size
		}
		return nil
	}
	if err := trimPass(false); err != nil {
		r.Err = err
		r.BytesFreed = freed
		return r
	}
	if freed < target {
		if err := trimPass(true); err != nil {
			r.Err = err
		}
	}
	r.BytesFreed = freed
	return r
}
