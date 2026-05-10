// Package civm centralizes shared operational defaults and input validation.
package civm

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	DefaultWorkDir        = "/home/runner/_work"
	DefaultHealthDiskPath = "/"
	DefaultTmpDir         = "/tmp"
	DefaultSystemdDir     = "/etc/systemd/system"
	DefaultUnitsSourceDir = "/opt/civm/deploy/systemd"
	DefaultRunnerVersion  = "2.334.0"

	DefaultCleanupTimeoutMinutes      = 30
	DefaultRunnerTimeoutMinutes       = 10
	DefaultRunnerRemoveTimeoutMinutes = 5
	DefaultRestartTimeoutSeconds      = 30
	DefaultHealthTimeoutSeconds       = 5
	DefaultBillingTimeoutSeconds      = 15
	DefaultWatchdogThresholdPct       = 80
	DefaultReverseMaxAgeHours         = 2
	DefaultRestartVerifySeconds       = 3
	DefaultUpgradeVerifySeconds       = 5
)

var (
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	shortRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	labelRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	semverRe   = regexp.MustCompile(`^[0-9]+[.][0-9]+[.][0-9]+$`)
	unitRe     = regexp.MustCompile(`^[A-Za-z0-9_.@-]+[.]service$`)
	userRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
	workflowRe = regexp.MustCompile(`^[A-Za-z0-9.][A-Za-z0-9._/-]{0,127}[.]ya?ml$`)
)

// ValidateRepo enforces a GitHub owner/repo shape without shell metacharacters.
func ValidateRepo(repo string) error {
	if !repoRe.MatchString(repo) {
		return fmt.Errorf("repo deve ter formato owner/repo seguro, got %q", repo)
	}
	return nil
}

// ValidateShort accepts only a directory-safe runner suffix.
func ValidateShort(short string) error {
	if !shortRe.MatchString(short) {
		return fmt.Errorf("short deve conter apenas letras, numeros, _ ou -, got %q", short)
	}
	return nil
}

// ValidateLabels accepts a comma-separated allowlist for GitHub runner labels.
func ValidateLabels(labels string) error {
	if labels == "" {
		return fmt.Errorf("label obrigatorio")
	}
	for _, label := range strings.Split(labels, ",") {
		label = strings.TrimSpace(label)
		if !labelRe.MatchString(label) {
			return fmt.Errorf("label invalido %q", label)
		}
	}
	return nil
}

// ValidateSemver rejects runner versions that are not plain x.y.z.
func ValidateSemver(value, field string) error {
	if !semverRe.MatchString(value) {
		return fmt.Errorf("%s deve usar semver x.y.z, got %q", field, value)
	}
	return nil
}

// ValidateServiceUnit restricts user-provided systemd unit names.
func ValidateServiceUnit(unit string) error {
	if !unitRe.MatchString(unit) || strings.Contains(unit, "..") {
		return fmt.Errorf("unit invalida: %q", unit)
	}
	return nil
}

// ValidateUserName restricts --run-as to local Unix-style user names.
func ValidateUserName(name string) error {
	if !userRe.MatchString(name) {
		return fmt.Errorf("run-as invalido: %q", name)
	}
	return nil
}

// ValidateWorkflowFile restricts gh workflow selectors to local YAML filenames.
func ValidateWorkflowFile(workflow string) error {
	if !workflowRe.MatchString(workflow) ||
		strings.HasPrefix(workflow, "/") ||
		strings.Contains(workflow, "..") {
		return fmt.Errorf("workflow deve ser arquivo .yml/.yaml seguro, got %q", workflow)
	}
	return nil
}

// CleanDir validates and normalizes a user-provided directory path.
func CleanDir(path, field string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s obrigatorio", field)
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("%s contem byte NUL", field)
	}
	clean := filepath.Clean(path)
	if clean == "." {
		return "", fmt.Errorf("%s nao pode ser diretorio atual", field)
	}
	return clean, nil
}
