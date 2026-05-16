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
	var writes []string
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
	}
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("dry-run failed: %s", res.Error)
	}
	if len(writes) != 0 {
		t.Fatalf("dry-run wrote files: %v", writes)
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
		GlobFn:      func(string) ([]string, error) { return nil, fmt.Errorf("pattern bad") },
	}
	res := Install(context.Background(), opts)
	if res.Error == "" {
		t.Fatalf("expected glob error to surface")
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
	res := Install(context.Background(), opts)
	if res.Error != "" {
		t.Fatalf("install error: %s", res.Error)
	}
	env := string(files["/home/emdev/actions-runner/.env"])
	for _, want := range []string{"FOO=bar", "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/civm/hooks/job-started.sh", "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/opt/civm/hooks/job-completed.sh"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q:\n%s", want, env)
		}
	}
	if len(writes) < 3 {
		t.Fatalf("expected wrapper and env writes, got %v", writes)
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
	wantStarted := "ACTIONS_RUNNER_HOOK_JOB_STARTED=" + filepath.Join(defaultHooksDir, "job-started.sh")
	wantCompleted := "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=" + filepath.Join(defaultHooksDir, "job-completed.sh")

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
