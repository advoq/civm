package runner

import (
	"context"
	"testing"
)

// orgUnit / repoUnit constroem Status como runner.List() os produziria a partir
// dos unit names reais do box (parseRunnerUnit faz o split owner-repo.name).
func mkStatus(unit, active, sub string) Status {
	repo, name := parseRunnerUnit(unit)
	return Status{UnitName: unit, Repo: repo, Name: name, ActiveState: active, SubState: sub}
}

const (
	unitOrgAdvoq  = "actions.runner.advoq.civm-advoq-org.service"
	unitRepoAdvoq = "actions.runner.advoq-advoq.civm-advoq.service"
	unitRepoCivm  = "actions.runner.advoq-civm.civm-self.service"
	unitVitae     = "actions.runner.emersonbusson-vitae.civm-vitae.service"
)

func TestDetectCollisions_AdvoqOrgPlusRepoActive(t *testing.T) {
	t.Parallel()
	// O estado exato que quebrou o #1184: org + repo, ambos active.
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitRepoAdvoq, "active", "running"),
		mkStatus(unitVitae, "active", "running"),
	}
	got := DetectCollisions(units)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 collision (%+v)", len(got), got)
	}
	c := got[0]
	if c.RepoUnit != unitRepoAdvoq {
		t.Errorf("RepoUnit = %q, want %q", c.RepoUnit, unitRepoAdvoq)
	}
	if c.Repo != "advoq/advoq" {
		t.Errorf("Repo = %q, want advoq/advoq", c.Repo)
	}
	if c.Owner != "advoq" {
		t.Errorf("Owner = %q, want advoq", c.Owner)
	}
	if c.OrgUnit != unitOrgAdvoq || c.OrgName != "civm-advoq-org" {
		t.Errorf("OrgUnit/OrgName = %q/%q, want %q/civm-advoq-org", c.OrgUnit, c.OrgName, unitOrgAdvoq)
	}
	if !c.RepoActive {
		t.Errorf("RepoActive = false, want true (runner ainda de pé)")
	}
}

func TestDetectCollisions_DisabledRepoStillCollides(t *testing.T) {
	t.Parallel()
	// O fix manual foi só `systemctl disable`: a unit fica loaded, inactive/dead.
	// Ainda É colisão — o watchdog a ressuscitaria. RepoActive deve ser false
	// para o enforcement saber que basta `config.sh remove` (já parada).
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitRepoAdvoq, "inactive", "dead"),
	}
	got := DetectCollisions(units)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (disabled-but-loaded ainda colide)", len(got))
	}
	if got[0].RepoActive {
		t.Errorf("RepoActive = true, want false para unit inactive/dead")
	}
}

func TestDetectCollisions_OrgOnly_NoCollision(t *testing.T) {
	t.Parallel()
	// Estado durável alvo: só o runner org do advoq sobrevive.
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitVitae, "active", "running"),
	}
	if got := DetectCollisions(units); len(got) != 0 {
		t.Fatalf("len = %d, want 0 (só org = serializado) (%+v)", len(got), got)
	}
}

func TestDetectCollisions_NoOrgRunner_NoCollision(t *testing.T) {
	t.Parallel()
	// Sem runner org, runners por-repo são o padrão legítimo (peers pessoais).
	// Não inventar colisão onde não há org para serializar.
	units := []Status{
		mkStatus(unitRepoAdvoq, "active", "running"),
		mkStatus(unitVitae, "active", "running"),
	}
	if got := DetectCollisions(units); len(got) != 0 {
		t.Fatalf("len = %d, want 0 (sem org runner) (%+v)", len(got), got)
	}
}

func TestDetectCollisions_OrgServesMultipleOwnerRepos(t *testing.T) {
	t.Parallel()
	// O runner org do advoq serve advoq/advoq E advoq/civm. Se ALGUÉM
	// registrasse runners por-repo para os DOIS, ambos colidem.
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitRepoAdvoq, "active", "running"),
		mkStatus(unitRepoCivm, "active", "running"), // repo advoq/civm
		mkStatus(unitVitae, "active", "running"),    // owner emersonbusson: NÃO colide
	}
	got := DetectCollisions(units)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	// Ordem estável por unit: advoq-advoq antes de advoq-civm.
	if got[0].RepoUnit != unitRepoAdvoq || got[1].RepoUnit != unitRepoCivm {
		t.Errorf("ordem inesperada: %q, %q", got[0].RepoUnit, got[1].RepoUnit)
	}
	for _, c := range got {
		if c.Owner != "advoq" {
			t.Errorf("Owner = %q, want advoq", c.Owner)
		}
	}
}

func TestDetectCollisions_DifferentOwnerNotAffected(t *testing.T) {
	t.Parallel()
	// Um runner org do advoq NÃO torna o runner por-repo de outro owner
	// (emersonbusson/vitae) redundante. Sem falso positivo cross-owner.
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitVitae, "active", "running"),
	}
	if got := DetectCollisions(units); len(got) != 0 {
		t.Fatalf("len = %d, want 0 (owner diferente) (%+v)", len(got), got)
	}
}

func TestDetectCollisions_Empty(t *testing.T) {
	t.Parallel()
	if got := DetectCollisions(nil); got != nil {
		t.Fatalf("DetectCollisions(nil) = %+v, want nil", got)
	}
	if got := DetectCollisions([]Status{}); len(got) != 0 {
		t.Fatalf("DetectCollisions([]) len = %d, want 0", len(got))
	}
}

func TestIsOrgRunner(t *testing.T) {
	t.Parallel()
	cases := []struct {
		unit string
		want bool
	}{
		{unitOrgAdvoq, true},   // org: nome -org + repo sem barra
		{unitRepoAdvoq, false}, // por-repo: repo advoq/advoq
		{unitVitae, false},     // por-repo pessoal
		{"actions.runner.advoq-org.civm-x.service", false}, // repo "advoq/org" (tem barra) — não é org runner
	}
	for _, c := range cases {
		got := isOrgRunner(mkStatus(c.unit, "active", "running"))
		if got != c.want {
			repo, name := parseRunnerUnit(c.unit)
			t.Errorf("isOrgRunner(%q -> repo=%q name=%q) = %v, want %v", c.unit, repo, name, got, c.want)
		}
	}
}

// TestDetectCollisions_FromListSnapshot prova o caminho ponta-a-ponta a partir
// da saída crua do systemctl, exatamente como o doctor o consome via runner.List.
func TestDetectCollisions_FromListSnapshot(t *testing.T) {
	t.Parallel()
	out := "" +
		"  actions.runner.advoq.civm-advoq-org.service        loaded active running GitHub Actions Runner (advoq.civm-advoq-org)\n" +
		"  actions.runner.advoq-advoq.civm-advoq.service      loaded active running GitHub Actions Runner (advoq-advoq.civm-advoq)\n" +
		"  actions.runner.emersonbusson-vitae.civm-vitae.service loaded active running GitHub Actions Runner (vitae)\n"
	o := DefaultListOptions()
	o.RunFn = func(context.Context, string, ...string) ([]byte, error) { return []byte(out), nil }
	items, err := List(context.Background(), o)
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	got := DetectCollisions(items)
	if len(got) != 1 || got[0].Repo != "advoq/advoq" {
		t.Fatalf("DetectCollisions sobre snapshot real = %+v, want 1 colisão advoq/advoq", got)
	}
}

// unitGateAdvoq representa um runner do pool civm-gate registrado para o advoq.
// Nomeado "civm-advoq-gate" (convenção: civm-<owner>-gate), label `civm-gate`.
// Unit: actions.runner.advoq-advoq.civm-advoq-gate.service — mesmo owner/repo
// que o runner por-repo padrão, mas sufixo "-gate" no nome.
const unitGateAdvoq = "actions.runner.advoq-advoq.civm-advoq-gate.service"

func TestDetectCollisions_GateRunnerPlusOrg_NoCollision(t *testing.T) {
	t.Parallel()
	// Um runner civm-gate coexistindo com o runner org NÃO deve ser reportado
	// como colisão: ele atende somente jobs `[self-hosted, civm-gate]` e não
	// realiza Docker/disco, portanto não viola o invariante de serialização
	// (incidente #1184).
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitGateAdvoq, "active", "running"),
		mkStatus(unitVitae, "active", "running"),
	}
	if got := DetectCollisions(units); len(got) != 0 {
		t.Fatalf("len = %d, want 0 (gate runner não é colisão) (%+v)", len(got), got)
	}
}

func TestDetectCollisions_GateRunnerDoesNotSuppressPlainRepoCollision(t *testing.T) {
	t.Parallel()
	// A exclusão do runner gate NÃO deve suprimir a colisão do runner por-repo
	// padrão: quando org + gate + repo-plain coexistem, só o repo-plain colide.
	units := []Status{
		mkStatus(unitOrgAdvoq, "active", "running"),
		mkStatus(unitGateAdvoq, "active", "running"),
		mkStatus(unitRepoAdvoq, "active", "running"),
		mkStatus(unitVitae, "active", "running"),
	}
	got := DetectCollisions(units)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (só o runner por-repo padrão colide) (%+v)", len(got), got)
	}
	c := got[0]
	if c.RepoUnit != unitRepoAdvoq {
		t.Errorf("RepoUnit = %q, want %q (runner gate não deve aparecer)", c.RepoUnit, unitRepoAdvoq)
	}
	if c.RepoName != "civm-advoq" {
		t.Errorf("RepoName = %q, want civm-advoq", c.RepoName)
	}
	if c.Owner != "advoq" {
		t.Errorf("Owner = %q, want advoq", c.Owner)
	}
}
