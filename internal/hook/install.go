package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultHooksDir   = "/opt/civm/hooks"
	defaultCivmctlBin = "/usr/local/bin/civmctl"
	defaultRunnerGlob = "/home/*/actions-runner*"
	startedHookName   = "job-started.sh"
	completedHookName = "job-completed.sh"

	DefaultHooksDir   = defaultHooksDir
	DefaultCivmctlBin = defaultCivmctlBin
	DefaultRunnerGlob = defaultRunnerGlob
	StartedHookName   = startedHookName
	CompletedHookName = completedHookName
)

type InstallOptions struct {
	Execute        bool
	HooksDir       string
	CivmctlPath    string // binary invoked by job-started.sh / job-completed.sh scripts
	RunnerGlob     string
	RestartRunners bool
	GlobFn         func(pattern string) ([]string, error)
	ReadFileFn     func(path string) ([]byte, error)
	WriteFileFn    func(path string, data []byte, perm os.FileMode) error
	MkdirAllFn     func(path string, perm os.FileMode) error
	RemoveFn       func(path string) error // remove one file or symlink, never recursively
	RunFn          func(ctx context.Context, name string, args ...string) ([]byte, error)
}

type InstallResult struct {
	Executed       bool     `json:"executed"`
	HooksDir       string   `json:"hooks_dir"`
	RunnerEnvFiles []string `json:"runner_env_files"`
	Restarted      bool     `json:"restarted"`
	Error          string   `json:"error,omitempty"`
}

func DefaultInstallOptions() InstallOptions {
	return InstallOptions{
		HooksDir:       defaultHooksDir,
		CivmctlPath:    defaultCivmctlBin,
		RunnerGlob:     defaultRunnerGlob,
		GlobFn:         filepath.Glob,
		ReadFileFn:     os.ReadFile,
		WriteFileFn:    os.WriteFile,
		MkdirAllFn:     os.MkdirAll,
		RemoveFn:       os.Remove,
		RunFn:          defaultRun,
		RestartRunners: true,
	}
}

func Install(ctx context.Context, opts InstallOptions) InstallResult {
	applyInstallDefaults(&opts)
	res := InstallResult{Executed: opts.Execute, HooksDir: opts.HooksDir}
	if err := validateInstallOptions(opts); err != nil {
		return installError(res, err)
	}
	if opts.Execute {
		if err := opts.MkdirAllFn(opts.HooksDir, 0755); err != nil {
			return installError(res, err)
		}
		// Clean up an invalid transition: the runner requires .sh, .ps1 or
		// .js suffixes in ACTIONS_RUNNER_HOOK_* paths.
		for _, legacy := range []string{"job-started", "job-completed"} {
			path := filepath.Join(opts.HooksDir, legacy)
			if err := opts.RemoveFn(path); err != nil && !os.IsNotExist(err) {
				return installError(res, err)
			}
		}
		hooks := []struct {
			name  string
			event Event
		}{
			{startedHookName, EventJobStarted},
			{completedHookName, EventJobCompleted},
		}
		for _, item := range hooks {
			path := filepath.Join(opts.HooksDir, item.name)
			if err := ensureHookScript(opts, path, item.event); err != nil {
				return installError(res, err)
			}
		}
	}
	runners, err := opts.GlobFn(opts.RunnerGlob)
	if err != nil {
		return installError(res, err)
	}
	sort.Strings(runners)
	for _, runner := range runners {
		if !safeRunnerDir(runner) {
			continue
		}
		envPath := filepath.Join(runner, ".env")
		res.RunnerEnvFiles = append(res.RunnerEnvFiles, envPath)
		if opts.Execute {
			if err := upsertEnv(opts, envPath); err != nil {
				return installError(res, err)
			}
		}
	}
	if opts.Execute && opts.RestartRunners {
		if _, err := opts.RunFn(ctx, "systemctl", "restart", "actions.runner.*"); err != nil {
			return installError(res, err)
		}
		res.Restarted = true
	}
	return res
}

func validateInstallOptions(opts InstallOptions) error {
	if strings.ContainsRune(opts.HooksDir, 0) || !filepath.IsAbs(filepath.Clean(opts.HooksDir)) {
		return fmt.Errorf("hooks-dir must be an absolute path")
	}
	if strings.ContainsRune(opts.CivmctlPath, 0) || !filepath.IsAbs(filepath.Clean(opts.CivmctlPath)) {
		return fmt.Errorf("civmctl-path must be an absolute path")
	}
	if strings.ContainsRune(opts.RunnerGlob, 0) {
		return fmt.Errorf("runner-glob contains NUL byte")
	}
	return nil
}

func upsertEnv(opts InstallOptions, envPath string) error {
	data, err := opts.ReadFileFn(envPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, line := range lines {
		if strings.HasPrefix(line, "ACTIONS_RUNNER_HOOK_JOB_STARTED=") ||
			strings.HasPrefix(line, "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=") ||
			strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	kept = append(kept,
		"ACTIONS_RUNNER_HOOK_JOB_STARTED="+filepath.Join(opts.HooksDir, startedHookName),
		"ACTIONS_RUNNER_HOOK_JOB_COMPLETED="+filepath.Join(opts.HooksDir, completedHookName),
	)
	return opts.WriteFileFn(envPath, []byte(strings.Join(kept, "\n")+"\n"), 0644)
}

func ensureHookScript(opts InstallOptions, path string, event Event) error {
	if err := opts.RemoveFn(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing %s: %w", path, err)
	}
	if err := opts.WriteFileFn(path, []byte(ScriptContent(opts.CivmctlPath, event)), 0755); err != nil {
		return fmt.Errorf("write hook script %s: %w", path, err)
	}
	return nil
}

func ScriptContent(civmctlPath string, event Event) string {
	return fmt.Sprintf("#!/usr/bin/env bash\nset -euo pipefail\nexec %s hook %s --execute \"$@\"\n", shellQuote(civmctlPath), event)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// IsRunnerDirCandidate returns true for absolute GitHub runner directories
// that are safe for hook .env reconciliation.
func IsRunnerDirCandidate(path string) bool {
	if strings.ContainsRune(path, 0) {
		return false
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == string(os.PathSeparator) {
		return false
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "actions-runner") {
		return false
	}
	sep := string(os.PathSeparator)
	blockedRoots := []string{"/bin", "/boot", "/dev", "/etc", "/proc", "/run", "/sys", "/tmp", "/usr", "/var/tmp"}
	for _, root := range blockedRoots {
		if path == root || strings.HasPrefix(path, root+sep) {
			return false
		}
	}
	return true
}

func safeRunnerDir(path string) bool {
	return IsRunnerDirCandidate(path)
}

func installError(res InstallResult, err error) InstallResult {
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func RenderInstallJSON(w io.Writer, r InstallResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func RenderInstallText(w io.Writer, r InstallResult) {
	mode := "DRY-RUN"
	if r.Executed {
		mode = "EXECUTE"
	}
	fmt.Fprintf(w, "civm hook install: %s\nHooks dir: %s\n", mode, r.HooksDir)
	for _, env := range r.RunnerEnvFiles {
		fmt.Fprintf(w, "  env %s\n", env)
	}
	if r.Restarted {
		fmt.Fprintln(w, "Runners restarted")
	}
	if r.Error != "" {
		fmt.Fprintf(w, "Error: %s\n", r.Error)
	}
}

func applyInstallDefaults(opts *InstallOptions) {
	if opts.HooksDir == "" {
		opts.HooksDir = defaultHooksDir
	}
	if opts.CivmctlPath == "" {
		opts.CivmctlPath = defaultCivmctlBin
	}
	if opts.RunnerGlob == "" {
		opts.RunnerGlob = defaultRunnerGlob
	}
	if opts.GlobFn == nil {
		opts.GlobFn = filepath.Glob
	}
	if opts.ReadFileFn == nil {
		opts.ReadFileFn = os.ReadFile
	}
	if opts.WriteFileFn == nil {
		opts.WriteFileFn = os.WriteFile
	}
	if opts.MkdirAllFn == nil {
		opts.MkdirAllFn = os.MkdirAll
	}
	if opts.RemoveFn == nil {
		opts.RemoveFn = os.Remove
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
}
