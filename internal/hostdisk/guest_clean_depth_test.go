package hostdisk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGuestFullCleanIsDeep trava a profundidade do full-clean do guest.
//
// O Invoke-GuestFullClean so removia ~/.cache + docker prune. Mas o que nunca era
// limpo — _diag (logs do runner), os checkouts em _work, journal e /tmp — crescia
// run a run, e o piso "limpo" do disco caia de ~51 pra ~47. A E2E builda ~35GB de
// imagens num job; com piso 47, 47-35=12 < 18 (panic floor) e o panic matava o
// job. Incidente 2026-06-18: a main CI perdeu E2E + Go CI por panic_disk.
//
// O deep-clean preserva _tool (hosted node/go cache) — caro de re-baixar. Este
// guard impede que alguem remova os alvos novos e reabra o death-spiral de disco.
func TestGuestFullCleanIsDeep(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "windows", "civm-vm-orchestrator.ps1")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read civm-vm-orchestrator.ps1: %v", err)
	}
	src := string(data)
	mustContain := map[string]string{
		"_diag/*":                  "limpar os logs _diag do runner",
		"_work":                    "limpar os checkouts _work",
		"! -name _tool":            "preservar o hosted tool cache _tool",
		"journalctl --vacuum":      "fazer vacuum do journal",
		"docker builder prune -af": "podar o build cache agressivamente",
	}
	for needle, why := range mustContain {
		if !strings.Contains(src, needle) {
			t.Errorf("Invoke-GuestFullClean deve %s (faltou %q no $remote)", why, needle)
		}
	}
}
