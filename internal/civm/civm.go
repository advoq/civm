// Package civm centralizes shared operational defaults and input validation.
package civm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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
	DefaultCivmctlPath    = "/usr/local/bin/civmctl"
	DefaultRunnerVersion  = "2.334.0"

	DefaultGoLinuxAMD64SHA256      = "2b2cfc7148493da5e73981bffbf3353af381d5f93e789c82c79aff64962eb556"
	DefaultRunnerLinuxX64SHA256    = "048024cd2c848eb6f14d5646d56c13a4def2ae7ee3ad12122bee960c56f3d271"
	DefaultNodeSourceSetup24SHA256 = "6e3d580f5bd7ccf2aa1e8df8d35c60d78e873c3ff8beb282c9bebd914904ad72"
	DefaultYQLinuxAMD64SHA256      = "75d893a0d5940d1019cb7cdc60001d9e876623852c31cfc6267047bc31149fa9"
	DefaultDockerGPGFingerprint    = "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
	DefaultGitHubCLIGPGFingerprint = "2C6106201985B60E6C7AC87323F3D4EA75716059"

	DefaultCleanupTimeoutMinutes      = 30
	DefaultRunnerTimeoutMinutes       = 10
	DefaultRunnerRemoveTimeoutMinutes = 5
	DefaultRestartTimeoutSeconds      = 30
	DefaultHealthTimeoutSeconds       = 5
	DefaultBillingTimeoutSeconds      = 15
	DefaultPreCleanupPct              = 60
	DefaultHardFailPct                = 90
	DefaultWatchdogThresholdPct       = DefaultPreCleanupPct
	DefaultCapacityMaxDiskPct         = DefaultHardFailPct
	DefaultReverseMaxAgeHours         = 2
	DefaultRestartVerifySeconds       = 3
	DefaultUpgradeVerifySeconds       = 5

	// Per-cache size budgets enforced by hook routine cleanup (job-completed).
	// Excedente é removido por mtime ascendente; arquivos com mtime mais novo
	// que DefaultCacheTrimMinProtectHours são preservados.
	DefaultCacheTrimMinProtectHours = 24
	DefaultCacheGoBuildMaxGB        = 5
	DefaultCacheNPMMaxGB            = 3
	DefaultCacheYarnMaxGB           = 3
	DefaultCachePNPMMaxGB           = 5

	// Filtros do docker prune em modo rotineiro. Mantêm layers quentes < 24h
	// e imagens unused < 7 dias, em vez do agressivo system prune --volumes.
	DefaultDockerBuildxPruneFilter = "until=24h"
	DefaultDockerImagePruneFilter  = "until=168h"

	// Timeout por comando dentro do hook cleanup. Evita que um docker travado
	// segure o runner durante todo o TimeoutStartSec do systemd (30 min).
	DefaultRoutineCleanupCmdTimeoutSecs = 120
)

var (
	goLinuxAMD64SHA256 = map[string]string{
		"1.26.3": DefaultGoLinuxAMD64SHA256,
	}
	runnerLinuxX64SHA256 = map[string]string{
		DefaultRunnerVersion: DefaultRunnerLinuxX64SHA256,
	}
	nodeSourceSetupSHA256 = map[string]string{
		"24": DefaultNodeSourceSetup24SHA256,
	}
	yqLinuxAMD64SHA256 = map[string]string{
		"4.52.5": DefaultYQLinuxAMD64SHA256,
	}
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

func GoLinuxAMD64SHA256(version string) (string, bool) {
	value, ok := goLinuxAMD64SHA256[version]
	return value, ok
}

func RunnerLinuxX64SHA256(version string) (string, bool) {
	value, ok := runnerLinuxX64SHA256[version]
	return value, ok
}

func NodeSourceSetupSHA256(major string) (string, bool) {
	value, ok := nodeSourceSetupSHA256[major]
	return value, ok
}

func YQLinuxAMD64SHA256(version string) (string, bool) {
	value, ok := yqLinuxAMD64SHA256[version]
	return value, ok
}

func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func VerifySHA256(actual, expected, label string) error {
	actual = strings.ToLower(strings.TrimSpace(actual))
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return fmt.Errorf("sha256 esperado ausente para %s", label)
	}
	if actual != expected {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", label, actual, expected)
	}
	return nil
}

func VerifyGPGFingerprint(output, expected, label string) error {
	actual := normalizeFingerprint(output)
	expected = normalizeFingerprint(expected)
	if expected == "" {
		return fmt.Errorf("fingerprint esperado ausente para %s", label)
	}
	if !strings.Contains(actual, expected) {
		return fmt.Errorf("fingerprint mismatch for %s: got output without %s", label, expected)
	}
	return nil
}

func normalizeFingerprint(value string) string {
	value = strings.ToUpper(value)
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, value)
}
