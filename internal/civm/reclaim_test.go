package civm

import "testing"

// EmergencyAdmits is the SPECv3 admission gate (DT-v3-1). Optimize-VHD is
// uninterruptible, so the below-headroom emergency reclaim may start ONLY when
// the live free space minus the hard floor covers the measured worst-case
// scratch budget. A zero budget (no measurement campaign yet, DT-v3-2) disables
// the emergency path entirely so the normal 8 GB headroom keeps applying — no
// regression, and the deadlock is not relocated to a guessed lower floor.
func TestEmergencyAdmits(t *testing.T) {
	const hard = DefaultHostVolumeHardFloorGB // 1
	tests := []struct {
		name     string
		liveFree int64
		budget   int64
		want     bool
	}{
		{"budget zero disables even with ample free", 100, 0, false},
		{"negative budget disables", 100, -1, false},
		{"ample slack admits", 8, 3, true},
		{"exact boundary admits (slack == budget)", 4, 3, true},      // 4-1 == 3
		{"one below boundary refuses", 3, 3, false},                  // 3-1 == 2 < 3
		{"free below hard floor refuses", 1, 3, false},               // 1-1 == 0 < 3
		{"free at hard floor with tiny budget refuses", 1, 1, false}, // 1-1 == 0 < 1
		{"just enough with budget 1", 2, 1, true},                    // 2-1 == 1 >= 1
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EmergencyAdmits(tt.liveFree, hard, tt.budget)
			if got != tt.want {
				t.Errorf("EmergencyAdmits(free=%d, hard=%d, budget=%d) = %v, want %v",
					tt.liveFree, hard, tt.budget, got, tt.want)
			}
		})
	}
}

// The SPECv3 constants must keep the safety ordering: a run can never be admitted
// below the hard floor, the emergency path lives under the normal headroom, and
// the pressure cadence triggers above the headroom. The scratch budget ships at
// zero until the host measurement campaign (DT-v3-2) sets it by explicit commit.
func TestReclaimConstantsOrdering(t *testing.T) {
	if DefaultHostVolumeHardFloorGB >= DefaultHostVolumeHeadroomGB {
		t.Errorf("HardFloor (%d) must be < Headroom (%d)",
			DefaultHostVolumeHardFloorGB, DefaultHostVolumeHeadroomGB)
	}
	if DefaultHostVolumeHeadroomGB >= DefaultAutoreclaimPressureGB {
		t.Errorf("Headroom (%d) must be < Pressure (%d)",
			DefaultHostVolumeHeadroomGB, DefaultAutoreclaimPressureGB)
	}
	if DefaultHostVolumeScratchBudgetGB != 0 {
		t.Errorf("ScratchBudget must ship at 0 (emergency disabled until measured), got %d",
			DefaultHostVolumeScratchBudgetGB)
	}
	if DefaultReclaimMinIntervalMin <= 0 {
		t.Errorf("ReclaimMinIntervalMin must be positive, got %d", DefaultReclaimMinIntervalMin)
	}
}
