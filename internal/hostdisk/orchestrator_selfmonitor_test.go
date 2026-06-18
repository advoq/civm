package hostdisk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrchestratorMonitorsOwnRepo protege o invariante de auto-monitoramento.
//
// O Get-RunCount do orchestrator so consulta a fila dos repos listados em
// $Repos. Se advoq/civm — o proprio repo — nao estiver la, a CI do civm, que e
// self-hosted e roda na propria box, fica INVISIVEL pro orchestrator: ele ve
// queued=0, conclui que nao ha trabalho, nunca sobe a VM, e o check fica
// QUEUED pra sempre.
//
// Incidente 2026-06-18: o #138 (este mesmo PR) ficou com "Detect changes"
// QUEUED no runner self-hosted enquanto o orchestrator logava queued=0; a box
// nao subiu e a CI pendurou. A box DEVE monitorar o proprio repo, senao nao
// consegue rodar a propria CI — a cobra mordendo o proprio rabo.
func TestOrchestratorMonitorsOwnRepo(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "windows", "civm-vm-orchestrator.ps1")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read civm-vm-orchestrator.ps1: %v", err)
	}
	if !strings.Contains(string(data), "'advoq/civm'") {
		t.Error("civm-vm-orchestrator.ps1 $Repos must include 'advoq/civm' so the box can run " +
			"its own self-hosted CI; without it Get-RunCount sees queued=0 and never starts the VM")
	}
}
