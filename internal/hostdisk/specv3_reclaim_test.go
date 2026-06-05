package hostdisk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWindowsScript(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "deploy", "windows", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// SPECv3 DT-v3-2: the supervised optimize is the measurement campaign vehicle.
// It must sample LIVE V: free (Get-PSDrive, never the 10-min metrics JSON —
// red-team Finding 3) during the compaction and record scratch_high_water_gb,
// which is what calibrates the emergency admission budget. Without this number
// the emergency floor would be a guess.
func TestOptimizeScriptMeasuresScratchHighWater(t *testing.T) {
	body := readWindowsScript(t, "civm-vhdx-optimize.ps1")

	if !strings.Contains(body, "scratch_high_water_gb") {
		t.Errorf("civm-vhdx-optimize.ps1 must log scratch_high_water_gb (DT-v3-2 measurement)")
	}
	if !strings.Contains(body, "Get-PSDrive V") {
		t.Errorf("civm-vhdx-optimize.ps1 must sample LIVE V: free via Get-PSDrive during " +
			"the optimize, not the stale 10-min metrics JSON (red-team Finding 3)")
	}
}
