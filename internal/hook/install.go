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
	defaultHooksDir = "/opt/civm/hooks"
	startedWrapper  = "#!/usr/bin/env bash\nexec /usr/local/bin/civmctl hook job-started --execute \"$@\"\n"
	doneWrapper     = "#!/usr/bin/env bash\nexec /usr/local/bin/civmctl hook job-completed --execute \"$@\"\n"
)

type InstallOptions struct {
	Execute        bool
	HooksDir       string
	RunnerGlob     string
	RestartRunners bool
	GlobFn         func(pattern string) ([]string, error)
	ReadFileFn     func(path string) ([]byte, error)
	WriteFileFn    func(path string, data []byte, perm os.FileMode) error
	MkdirAllFn     func(path string, perm os.FileMode) error
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
		RunnerGlob:     "/home/*/actions-runner*",
		GlobFn:         filepath.Glob,
		ReadFileFn:     os.ReadFile,
		WriteFileFn:    os.WriteFile,
		MkdirAllFn:     os.MkdirAll,
		RunFn:          defaultRun,
		RestartRunners: true,
	}
}

func Install(ctx context.Context, opts InstallOptions) InstallResult {
	applyInstallDefaults(&opts)
	res := InstallResult{Executed: opts.Execute, HooksDir: opts.HooksDir}
	if opts.Execute {
		if err := opts.MkdirAllFn(opts.HooksDir, 0755); err != nil {
			return installError(res, err)
		}
		if err := opts.WriteFileFn(filepath.Join(opts.HooksDir, "job-started.sh"), []byte(startedWrapper), 0755); err != nil {
			return installError(res, err)
		}
		if err := opts.WriteFileFn(filepath.Join(opts.HooksDir, "job-completed.sh"), []byte(doneWrapper), 0755); err != nil {
			return installError(res, err)
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
		"ACTIONS_RUNNER_HOOK_JOB_STARTED="+filepath.Join(opts.HooksDir, "job-started.sh"),
		"ACTIONS_RUNNER_HOOK_JOB_COMPLETED="+filepath.Join(opts.HooksDir, "job-completed.sh"),
	)
	return opts.WriteFileFn(envPath, []byte(strings.Join(kept, "\n")+"\n"), 0644)
}

func safeRunnerDir(path string) bool {
	path = filepath.Clean(path)
	return strings.HasPrefix(path, "/home/") && strings.Contains(filepath.Base(path), "actions-runner")
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
	if opts.RunnerGlob == "" {
		opts.RunnerGlob = "/home/*/actions-runner*"
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
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
}
