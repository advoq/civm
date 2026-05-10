package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/emersonbusson/civm/internal/specs"
)

func TestRenderSpecJSONValid(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if code := renderSpecJSON(specs.Ubuntu2404(), &buf); code != 0 {
		t.Fatalf("renderSpecJSON code = %d", code)
	}
	var parsed struct {
		OSDistro string `json:"os_distro"`
		Tools    []struct {
			Name      string `json:"name"`
			Preferred string `json:"preferred"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json invalido: %v", err)
	}
	if parsed.OSDistro != "Ubuntu" || len(parsed.Tools) == 0 {
		t.Fatalf("json inesperado: distro=%q tools=%d", parsed.OSDistro, len(parsed.Tools))
	}
}

func TestRunVersionPinsBadFlag(t *testing.T) {
	t.Parallel()
	if code := runVersionPins([]string{"--bad"}); code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
}

func TestRunBillingRejectsBadRepoBeforeGh(t *testing.T) {
	t.Parallel()
	if code := runBilling([]string{"--repo=bad/repo;rm"}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestBootstrapEverythingHelpers(t *testing.T) {
	t.Parallel()
	if got := execFlag(true); got != "--execute" {
		t.Fatalf("execFlag(true) = %q", got)
	}
	if got := execFlag(false); got != "" {
		t.Fatalf("execFlag(false) = %q", got)
	}
	if got := boolStr(true); got != "true" {
		t.Fatalf("boolStr(true) = %q", got)
	}
	if got := joinArgs([]string{"cp", "a", "b"}); got != "cp a b" {
		t.Fatalf("joinArgs = %q", got)
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()
	got := splitCSV("emersonbusson/civm, emersonbusson/vitae,,")
	if len(got) != 2 || got[0] != "emersonbusson/civm" || got[1] != "emersonbusson/vitae" {
		t.Fatalf("splitCSV = %#v", got)
	}
}
