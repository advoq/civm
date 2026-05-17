package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderInstallJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderInstallJSON(&buf, InstallResult{Executed: true, HooksDir: "/opt/civm/hooks"}); err != nil {
		t.Fatal(err)
	}
	var parsed InstallResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !parsed.Executed || parsed.HooksDir != "/opt/civm/hooks" {
		t.Fatalf("roundtrip: %+v", parsed)
	}
}

func TestRenderInstallText(t *testing.T) {
	var buf bytes.Buffer
	RenderInstallText(&buf, InstallResult{
		Executed:       true,
		HooksDir:       "/opt/civm/hooks",
		RunnerEnvFiles: []string{"/home/runner/actions-runner/.env"},
		Restarted:      true,
		Error:          "warn",
	})
	out := buf.String()
	for _, want := range []string{"EXECUTE", "/opt/civm/hooks", "/home/runner/actions-runner/.env", "restarted", "warn"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Fatalf("text missing %q:\n%s", want, out)
		}
	}
}

func TestRenderInstallTextDryRun(t *testing.T) {
	var buf bytes.Buffer
	RenderInstallText(&buf, InstallResult{Executed: false, HooksDir: "/opt/civm/hooks"})
	if !strings.Contains(buf.String(), "DRY-RUN") {
		t.Fatalf("expected DRY-RUN tag: %q", buf.String())
	}
}

func TestInstallDryRunSkipsWrites(t *testing.T) {
	var writes, symlinks, removes []string
	opts := InstallOptions{
		Execute:    false,
		HooksDir:   "/opt/civm/hooks",
		RunnerGlob: "/home/*/actions-runner*",
		GlobFn:     func(string) ([]string, error) { return []string{"/home/runner/actions-runner"}, nil },
		ReadFileFn: func(string) ([]byte, error) { return nil, nil },
		WriteFileFn: func(p string, _ []byte, _ os.FileMode) error {
			writes = append(writes, p)
			return nil
		},
		MkdirAllFn: func(string, os.FileMode) error { return nil },
		SymlinkFn:  func(_, link string) error { symlinks = append(symlinks, link); return nil },
		RemoveFn:   func(p string) error { removes = append(removes, p); return nil },
	}
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("dry-run failed: %s", res.Error)
	}
	if len(writes) != 0 || len(symlinks) != 0 || len(removes) != 0 {
		t.Fatalf("dry-run had side effects: writes=%v symlinks=%v removes=%v", writes, symlinks, removes)
	}
	if len(res.RunnerEnvFiles) != 1 {
		t.Fatalf("expected 1 enumerated env file, got %v", res.RunnerEnvFiles)
	}
}

func TestInstallPropagatesMkdirError(t *testing.T) {
	opts := InstallOptions{
		Execute:    true,
		HooksDir:   "/opt/civm/hooks",
		MkdirAllFn: func(string, os.FileMode) error { return fmt.Errorf("EACCES") },
	}
	res := Install(context.Background(), opts)
	if res.Error == "" {
		t.Fatalf("expected mkdir error to surface")
	}
}

func TestInstallPropagatesGlobError(t *testing.T) {
	opts := InstallOptions{
		Execute:     true,
		HooksDir:    "/opt/civm/hooks",
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		WriteFileFn: func(string, []byte, os.FileMode) error { return nil },
		SymlinkFn:   func(string, string) error { return nil },
		RemoveFn:    func(string) error { return os.ErrNotExist },
		ReadlinkFn:  func(string) (string, error) { return "", os.ErrNotExist },
		GlobFn:      func(string) ([]string, error) { return nil, fmt.Errorf("pattern bad") },
	}
	res := Install(context.Background(), opts)
	if res.Error == "" {
		t.Fatalf("expected glob error to surface")
	}
}

func TestInstallRejectsRelativeMutationPaths(t *testing.T) {
	opts := DefaultInstallOptions()
	opts.Execute = true
	opts.HooksDir = "relative/hooks"
	res := Install(context.Background(), opts)
	if res.Error == "" || !strings.Contains(res.Error, "absolute") {
		t.Fatalf("expected absolute path error, got %q", res.Error)
	}

	opts = DefaultInstallOptions()
	opts.Execute = true
	opts.CivmctlPath = "bin/civmctl"
	res = Install(context.Background(), opts)
	if res.Error == "" || !strings.Contains(res.Error, "absolute") {
		t.Fatalf("expected absolute path error, got %q", res.Error)
	}
}

func TestInstallCreatesSymlinks(t *testing.T) {
	var symlinks [][2]string
	opts := InstallOptions{
		Execute:     true,
		HooksDir:    "/opt/civm/hooks",
		CivmctlPath: "/usr/local/bin/civmctl",
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		SymlinkFn: func(target, link string) error {
			symlinks = append(symlinks, [2]string{target, link})
			return nil
		},
		RemoveFn:   func(string) error { return os.ErrNotExist },
		ReadlinkFn: func(string) (string, error) { return "", os.ErrNotExist },
		GlobFn:     func(string) ([]string, error) { return nil, nil },
	}
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("install error: %s", res.Error)
	}
	want := map[string]string{
		"/opt/civm/hooks/job-started":   "/usr/local/bin/civmctl",
		"/opt/civm/hooks/job-completed": "/usr/local/bin/civmctl",
	}
	if len(symlinks) != 2 {
		t.Fatalf("symlinks created = %v, want 2", symlinks)
	}
	for _, pair := range symlinks {
		if want[pair[1]] != pair[0] {
			t.Fatalf("unexpected symlink %s -> %s", pair[1], pair[0])
		}
	}
}

func TestInstallReusesCorrectSymlink(t *testing.T) {
	var symlinks, removes []string
	opts := InstallOptions{
		Execute:     true,
		HooksDir:    "/opt/civm/hooks",
		CivmctlPath: "/usr/local/bin/civmctl",
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		// Já aponta para o lugar certo — não deve remover nem recriar.
		ReadlinkFn: func(string) (string, error) { return "/usr/local/bin/civmctl", nil },
		SymlinkFn:  func(_, link string) error { symlinks = append(symlinks, link); return nil },
		RemoveFn:   func(p string) error { removes = append(removes, p); return nil },
		GlobFn:     func(string) ([]string, error) { return nil, nil },
	}
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("install error: %s", res.Error)
	}
	if len(symlinks) != 0 {
		t.Fatalf("expected no new symlinks (already correct), got %v", symlinks)
	}
	// Legacy .sh paths são sempre limpos (ensureSymlink só pula se já correto;
	// o loop de cleanup roda sempre antes).
	if len(removes) != 2 {
		t.Fatalf("expected 2 legacy removes, got %v", removes)
	}
}

func TestInstallCleansLegacyShWrappers(t *testing.T) {
	var removes []string
	opts := InstallOptions{
		Execute:     true,
		HooksDir:    "/opt/civm/hooks",
		CivmctlPath: "/usr/local/bin/civmctl",
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		ReadlinkFn:  func(string) (string, error) { return "", os.ErrNotExist },
		SymlinkFn:   func(string, string) error { return nil },
		RemoveFn:    func(p string) error { removes = append(removes, p); return nil },
		GlobFn:      func(string) ([]string, error) { return nil, nil },
	}
	Install(context.Background(), opts)
	wantLegacy := []string{
		"/opt/civm/hooks/job-started.sh",
		"/opt/civm/hooks/job-completed.sh",
	}
	for _, w := range wantLegacy {
		found := false
		for _, r := range removes {
			if r == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("legacy %s not cleaned (removes=%v)", w, removes)
		}
	}
}

func TestInstallSkipsUnsafeRunnerDirs(t *testing.T) {
	opts := DefaultInstallOptions()
	opts.Execute = false
	opts.GlobFn = func(string) ([]string, error) {
		return []string{"/etc/passwd", "/home/runner/actions-runner-x"}, nil
	}
	opts.ReadFileFn = func(string) ([]byte, error) { return nil, nil }
	opts.WriteFileFn = func(string, []byte, os.FileMode) error { return nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	res := Install(context.Background(), opts)
	if len(res.RunnerEnvFiles) != 1 {
		t.Fatalf("expected unsafe path skipped, got %v", res.RunnerEnvFiles)
	}
	if !strings.HasPrefix(res.RunnerEnvFiles[0], "/home/runner/") {
		t.Fatalf("expected /home/runner path, got %q", res.RunnerEnvFiles[0])
	}
}

func TestInstallAllowsConfiguredRunnerRoot(t *testing.T) {
	opts := DefaultInstallOptions()
	opts.Execute = false
	opts.RunnerGlob = "/srv/ci/actions-runner*"
	opts.GlobFn = func(pattern string) ([]string, error) {
		if pattern != "/srv/ci/actions-runner*" {
			t.Fatalf("pattern = %q", pattern)
		}
		return []string{"/srv/ci/actions-runner-team"}, nil
	}
	opts.ReadFileFn = func(string) ([]byte, error) { return nil, nil }
	opts.WriteFileFn = func(string, []byte, os.FileMode) error { return nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("install error: %s", res.Error)
	}
	if len(res.RunnerEnvFiles) != 1 || res.RunnerEnvFiles[0] != "/srv/ci/actions-runner-team/.env" {
		t.Fatalf("runner env files = %v", res.RunnerEnvFiles)
	}
}

func TestRunnerDirCandidateRejectsSystemAndTemporaryRoots(t *testing.T) {
	for _, path := range []string{"/etc/actions-runner", "/usr/local/actions-runner", "/tmp/actions-runner", "actions-runner"} {
		if IsRunnerDirCandidate(path) {
			t.Fatalf("IsRunnerDirCandidate(%q) = true, want false", path)
		}
	}
}

func TestInstallUpsertsHookEnv(t *testing.T) {
	files := map[string][]byte{
		"/home/emdev/actions-runner/.env": []byte("FOO=bar\nACTIONS_RUNNER_HOOK_JOB_COMPLETED=/old\n"),
	}
	var writes []string
	opts := DefaultInstallOptions()
	opts.Execute = true
	opts.RestartRunners = false
	opts.GlobFn = func(string) ([]string, error) { return []string{"/home/emdev/actions-runner"}, nil }
	opts.MkdirAllFn = func(string, os.FileMode) error { return nil }
	opts.ReadFileFn = func(path string) ([]byte, error) { return files[path], nil }
	opts.WriteFileFn = func(path string, data []byte, perm os.FileMode) error {
		writes = append(writes, path+"="+string(data))
		files[path] = data
		return nil
	}
	opts.SymlinkFn = func(string, string) error { return nil }
	opts.RemoveFn = func(string) error { return os.ErrNotExist }
	opts.ReadlinkFn = func(string) (string, error) { return "", os.ErrNotExist }
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("install error: %s", res.Error)
	}
	env := string(files["/home/emdev/actions-runner/.env"])
	for _, want := range []string{"FOO=bar", "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/civm/hooks/job-started", "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/opt/civm/hooks/job-completed"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q:\n%s", want, env)
		}
	}
	if len(writes) < 1 {
		t.Fatalf("expected at least one env write, got %v", writes)
	}
}

// FuzzUpsertEnv enforces the idempotency invariant of upsertEnv: regardless
// of prior content, the resulting .env must contain exactly one well-formed
// ACTIONS_RUNNER_HOOK_JOB_STARTED= line and exactly one well-formed
// ACTIONS_RUNNER_HOOK_JOB_COMPLETED= line — and a second pass must produce
// the same bytes (idempotent).
func FuzzUpsertEnv(f *testing.F) {
	seeds := []string{
		"",
		"FOO=bar\n",
		"FOO=bar\nACTIONS_RUNNER_HOOK_JOB_COMPLETED=/old\n",
		"ACTIONS_RUNNER_HOOK_JOB_STARTED=/a\nACTIONS_RUNNER_HOOK_JOB_STARTED=/b\n",
		"URL=http://a=b\n",
		"line-without-equals\n",
		"FOO=bar\r\nBAZ=qux\r\n",
		"\n\n\n",
		"ACTIONS_RUNNER_HOOK_JOB_STARTED=/a\nACTIONS_RUNNER_HOOK_JOB_COMPLETED=/b\nFOO=keep\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	const envPath = "/tmp/fuzz.env"
	wantStarted := "ACTIONS_RUNNER_HOOK_JOB_STARTED=" + filepath.Join(defaultHooksDir, startedHookName)
	wantCompleted := "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=" + filepath.Join(defaultHooksDir, completedHookName)

	f.Fuzz(func(t *testing.T, initial string) {
		files := map[string][]byte{envPath: []byte(initial)}
		opts := InstallOptions{
			HooksDir: defaultHooksDir,
			ReadFileFn: func(p string) ([]byte, error) {
				return files[p], nil
			},
			WriteFileFn: func(p string, data []byte, _ os.FileMode) error {
				files[p] = data
				return nil
			},
		}
		if err := upsertEnv(opts, envPath); err != nil {
			t.Fatalf("first upsert: %v", err)
		}
		first := string(files[envPath])
		if got := countPrefix(first, "ACTIONS_RUNNER_HOOK_JOB_STARTED="); got != 1 {
			t.Fatalf("first pass: started line count = %d, want 1\n%s", got, first)
		}
		if got := countPrefix(first, "ACTIONS_RUNNER_HOOK_JOB_COMPLETED="); got != 1 {
			t.Fatalf("first pass: completed line count = %d, want 1\n%s", got, first)
		}
		if !containsLine(first, wantStarted) {
			t.Fatalf("first pass: missing %q\n%s", wantStarted, first)
		}
		if !containsLine(first, wantCompleted) {
			t.Fatalf("first pass: missing %q\n%s", wantCompleted, first)
		}
		// Idempotency: a second pass must produce the same bytes.
		if err := upsertEnv(opts, envPath); err != nil {
			t.Fatalf("second upsert: %v", err)
		}
		if got := string(files[envPath]); got != first {
			t.Fatalf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, got)
		}
	})
}

func countPrefix(content, prefix string) int {
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func containsLine(content, want string) bool {
	for _, line := range strings.Split(content, "\n") {
		if line == want {
			return true
		}
	}
	return false
}
