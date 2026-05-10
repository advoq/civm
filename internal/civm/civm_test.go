package civm

import "testing"

func TestValidateRepo(t *testing.T) {
	t.Parallel()
	for _, repo := range []string{"emersonbusson/civm", "owner/repo.name"} {
		if err := ValidateRepo(repo); err != nil {
			t.Fatalf("ValidateRepo(%q) err = %v", repo, err)
		}
	}
	for _, repo := range []string{"", "owner", "owner/repo;rm", "-bad/repo", "owner/../repo"} {
		if err := ValidateRepo(repo); err == nil {
			t.Fatalf("ValidateRepo(%q) sem erro", repo)
		}
	}
}

func TestValidateShortLabelsAndVersions(t *testing.T) {
	t.Parallel()
	if err := ValidateShort("civm-cmpx_1"); err != nil {
		t.Fatalf("ValidateShort err = %v", err)
	}
	if err := ValidateLabels("civm,linux_x64"); err != nil {
		t.Fatalf("ValidateLabels err = %v", err)
	}
	if err := ValidateSemver("2.334.0", "--runner-version"); err != nil {
		t.Fatalf("ValidateSemver err = %v", err)
	}
	for _, value := range []string{"../x", "x/y", "x y", ""} {
		if err := ValidateShort(value); err == nil {
			t.Fatalf("ValidateShort(%q) sem erro", value)
		}
	}
	if err := ValidateLabels("civm, bad label"); err == nil {
		t.Fatalf("ValidateLabels aceitou label com espaco")
	}
	if err := ValidateSemver("2.334", "--runner-version"); err == nil {
		t.Fatalf("ValidateSemver aceitou versao incompleta")
	}
}

func TestValidateWorkflowAndUnit(t *testing.T) {
	t.Parallel()
	for _, workflow := range []string{"ci.yml", ".github/workflows/ci.yaml"} {
		if err := ValidateWorkflowFile(workflow); err != nil {
			t.Fatalf("ValidateWorkflowFile(%q) err = %v", workflow, err)
		}
	}
	for _, workflow := range []string{"/tmp/ci.yml", "../ci.yml", "ci.sh", "ci.yml;rm"} {
		if err := ValidateWorkflowFile(workflow); err == nil {
			t.Fatalf("ValidateWorkflowFile(%q) sem erro", workflow)
		}
	}
	if err := ValidateServiceUnit("actions.runner.owner-repo.civm.service"); err != nil {
		t.Fatalf("ValidateServiceUnit err = %v", err)
	}
	if err := ValidateServiceUnit("../bad.service"); err == nil {
		t.Fatalf("ValidateServiceUnit aceitou path traversal")
	}
}

func TestCleanDir(t *testing.T) {
	t.Parallel()
	got, err := CleanDir("/opt/civm/../civm/deploy/systemd", "--units-source")
	if err != nil {
		t.Fatalf("CleanDir err = %v", err)
	}
	if got != "/opt/civm/deploy/systemd" {
		t.Fatalf("CleanDir got %q", got)
	}
	for _, value := range []string{"", ".", "a\x00b"} {
		if _, err := CleanDir(value, "--dir"); err == nil {
			t.Fatalf("CleanDir(%q) sem erro", value)
		}
	}
}
