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

// SPECv3 DT-v3-3 (red-team Finding 5): the two reclaim scripts use SEPARATE
// per-script locks and historically did not exclude EACH OTHER, so a supervised
// optimize and the (now pressure-cadenced) autoreclaim could Stop-VM / Optimize
// the same VHDX concurrently. Both must acquire one canonical shared lock before
// any Stop-VM, and the watchdog must consult it too. The #98 watchdog fix did
// NOT make the reclaimers mutually exclusive — this closes that gap.
func TestReclaimersShareCanonicalLock(t *testing.T) {
	const canonical = "civm-reclaim.lock"

	for _, name := range []string{"civm-vhdx-autoreclaim.ps1", "civm-vhdx-optimize.ps1"} {
		body := readWindowsScript(t, name)
		if !strings.Contains(body, canonical) {
			t.Errorf("%s must acquire the canonical %s before Stop-VM (SPECv3 DT-v3-3 mutual exclusion)",
				name, canonical)
		}
		if !strings.Contains(body, "reclaim_skip_other_active") {
			t.Errorf("%s must exit-skip (reclaim_skip_other_active) when the other reclaimer holds %s",
				name, canonical)
		}
	}

	wd := readWindowsScript(t, "register-civm-vhdx-optimize.ps1")
	if !strings.Contains(wd, canonical) {
		t.Errorf("the optimize-watchdog $MaintenanceLocks must include %s so a held canonical "+
			"lock also makes the watchdog back off (SPECv3 DT-v3-3)", canonical)
	}
}

// SPECv3 DT-v3-1 (red-team Findings 1+2): the below-headroom emergency reclaim is
// admission-gated, not floored by a guess, and the Optimize-VHD it runs is
// UNINTERRUPTIBLE. The autoreclaim must (a) gate on the measured scratch budget
// vs the hard floor (abort_insufficient_slack), (b) label the admitted run
// emergency_reclaim_start, and (c) NEVER contain a Stop-Job — Stop-Job does not
// abort CompactVirtualDisk, so trying to abort mid-flight is the exact unsafe
// mechanism the red-team killed.
func TestAutoreclaimAdmissionGate(t *testing.T) {
	body := readWindowsScript(t, "civm-vhdx-autoreclaim.ps1")

	for _, token := range []string{
		"autoreclaim_abort_insufficient_slack", // the gate refusal
		"emergency_reclaim_start",              // the admitted-run label
		"$ScratchBudgetGB",                     // measured budget wired in
		"$HardFloorGB",                         // absolute hard floor wired in
	} {
		if !strings.Contains(body, token) {
			t.Errorf("civm-vhdx-autoreclaim.ps1 must contain %q for the SPECv3 admission gate (DT-v3-1)", token)
		}
	}

	// Match a Stop-Job CALL, not a comment that explains its absence. Optimize-VHD
	// is uninterruptible (red-team Finding 2): Stop-Job does not abort the native
	// CompactVirtualDisk, so the emergency path must be admission-gated and never
	// attempt a mid-compaction abort.
	for i, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "Stop-Job") {
			t.Errorf("civm-vhdx-autoreclaim.ps1:%d must NOT call Stop-Job — Optimize-VHD is "+
				"uninterruptible (red-team Finding 2); the emergency path is admission-gated, "+
				"never aborted mid-compaction: %s", i+1, strings.TrimSpace(line))
		}
	}
}
