package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/advoq/civm/internal/specs"
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
	steps := buildBootstrapEverythingSteps("/opt/civm/deploy/systemd", true, true, false)
	if len(steps) == 0 || !bytes.Contains([]byte(steps[0].WouldDo), []byte("/usr/local/bin/civmctl")) {
		t.Fatalf("bootstrap-everything deve validar /usr/local/bin/civmctl, step=%+v", steps)
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()
	got := splitCSV("advoq/civm, emersonbusson/vitae,,")
	if len(got) != 2 || got[0] != "advoq/civm" || got[1] != "emersonbusson/vitae" {
		t.Fatalf("splitCSV = %#v", got)
	}
}

// FuzzSplitCSV enforces the post-condition that splitCSV's output never
// contains empty entries, never contains leading/trailing whitespace, and
// never contains a comma. Downstream callers (runner add/remove, ci local-report)
// pass each entry to civm.ValidateRepo, which is regex-strict; we only test
// the trim+filter contract here.
func FuzzSplitCSV(f *testing.F) {
	seeds := []string{
		"",
		"advoq/civm",
		"advoq/civm,vitae/x",
		",,",
		" advoq/civm , vitae/x ",
		"a,,b,,,c",
		"\tadvoq/civm\n",
		"single-no-slash",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out := splitCSV(raw)
		for i, part := range out {
			if part == "" {
				t.Fatalf("splitCSV(%q)[%d] is empty", raw, i)
			}
			if strings.ContainsRune(part, ',') {
				t.Fatalf("splitCSV(%q)[%d] contains comma: %q", raw, i, part)
			}
			if part != strings.TrimSpace(part) {
				t.Fatalf("splitCSV(%q)[%d] is not trimmed: %q", raw, i, part)
			}
		}
	})
}
