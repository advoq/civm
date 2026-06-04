package hostdisk

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// dangerousMaxMin matches the [math]::Max(0, ... ) / [math]::Min(0, ... ) form
// where the clamp literal is a bare Int32 0.
//
// The civm-vhdx-autoreclaim worker once called [math]::Max(0, <int64 bytes>).
// A bare 0 is Int32, which pins .NET overload resolution to Max(int, int) and
// throws "Valor era muito grande ou muito pequeno para Int32" on any byte value
// above Int32.MaxValue (~2 GiB). That aborted every reclaim run, so the dynamic
// VHDX was never compacted and the Hyper-V host volume (V:) silently filled
// until the runner wedged. Always clamp with [int64]0 (or 0L) so the
// Max(long, long) overload is selected. This guard keeps the Int32 form from
// ever returning to any deploy/windows script.
var dangerousMaxMin = regexp.MustCompile(`\[math\]::(Max|Min)\(\s*0\s*,`)

func TestWindowsScriptsHaveNoInt32MaxMinLiteral(t *testing.T) {
	dir := filepath.Join("..", "..", "deploy", "windows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read deploy/windows: %v", err)
	}
	scanned := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ps1") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		scanned++
		for i, line := range strings.Split(string(data), "\n") {
			if dangerousMaxMin.MatchString(line) {
				t.Errorf("%s:%d clamps with a bare Int32 0 literal, which overflows "+
					"on byte values >2 GiB; cast to [int64]0 to force Max(long,long): %s",
					entry.Name(), i+1, strings.TrimSpace(line))
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no .ps1 files scanned under deploy/windows")
	}
}
