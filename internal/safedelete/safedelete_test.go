package safedelete

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/advoq/civm/internal/civm"
)

const (
	runnerUID    = 1000
	rootUID      = 0
	safeWorkRoot = "/home/runner/actions-runner/_work"
	safeChild    = safeWorkRoot + "/repo"
	wrapperPath  = civm.DefaultSafeDeleteWrapperPath
)

// fakeInfo is a minimal fs.FileInfo so LstatFn results need no real files.
type fakeInfo struct{ name string }

func (f fakeInfo) Name() string     { return f.name }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return true }
func (fakeInfo) Sys() any           { return nil }

// recorder captures every wrapper invocation so tests assert the exact argv,
// including that "--" never reaches the Go layer (the wrapper places it on the
// root side) and that escalation order is chown-then-rm.
type recorder struct {
	calls   [][]string
	failOps map[string]error // op ("chown"/"rm") -> error to return
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	// argv is: sudo -n <wrapper> <op> <args...>
	if len(args) >= 3 {
		op := args[2]
		if err, ok := r.failOps[op]; ok && err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (r *recorder) ops() []string {
	var ops []string
	for _, c := range r.calls {
		if len(c) >= 4 {
			ops = append(ops, c[3]) // sudo -n wrapper OP
		}
	}
	return ops
}

// baseOptions returns hermetic options: the child is owned by the runner, the
// guard accepts only direct children of safeWorkRoot, and EvalSymlinks is
// identity. Individual tests override the pieces they exercise.
func baseOptions(rec *recorder) Options {
	return Options{
		WrapperPath:    wrapperPath,
		GuardFn:        childOfSafeWorkRoot,
		RunFn:          rec.run,
		RemoveAllFn:    func(string) error { return nil },
		EvalSymlinksFn: func(p string) (string, error) { return p, nil },
		LstatFn:        func(string) (fs.FileInfo, error) { return fakeInfo{"repo"}, nil },
		OwnerUIDFn:     func() int { return runnerUID },
		FileOwnerUIDFn: func(fs.FileInfo) (int, bool) { return runnerUID, true },
	}
}

func childOfSafeWorkRoot(path string) error {
	if path == safeChild {
		return nil
	}
	return errors.New("not a direct child of a safe _work root")
}

func TestRemoveUnprivilegedSuccessNeverEscalates(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	removed := ""
	opts.RemoveAllFn = func(p string) error { removed = p; return nil }

	res := Remove(context.Background(), opts, safeChild)
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
	if res.Escalated {
		t.Fatalf("Escalated = true, want false for an owner-removable path")
	}
	if removed != safeChild {
		t.Fatalf("RemoveAllFn target = %q, want %q", removed, safeChild)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("sudo invoked %d times for a clean delete: %v", len(rec.calls), rec.calls)
	}
}

func TestRemoveEscalatesOnPermissionDenied(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	calls := 0
	// First unprivileged remove hits EACCES; after chown, the retry succeeds.
	opts.RemoveAllFn = func(string) error {
		calls++
		if calls == 1 {
			return fs.ErrPermission
		}
		return nil
	}

	res := Remove(context.Background(), opts, safeChild)
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil after successful escalation", res.Err)
	}
	if !res.Escalated {
		t.Fatalf("Escalated = false, want true")
	}
	if got := rec.ops(); len(got) != 1 || got[0] != "chown" {
		t.Fatalf("escalation ops = %v, want [chown] (retry succeeded, no rm)", got)
	}
	// The wrapper is the only binary sudo runs, and the path is the LAST arg —
	// "--" is the wrapper's responsibility on the root side, never the Go layer.
	last := rec.calls[0]
	if last[0] != "sudo" || last[1] != "-n" || last[2] != wrapperPath {
		t.Fatalf("argv prefix = %v, want [sudo -n %s ...]", last[:3], wrapperPath)
	}
	if last[len(last)-1] != safeChild {
		t.Fatalf("path is not the final argv element: %v", last)
	}
}

func TestRemoveFallsBackToWrapperRmWhenChownInsufficient(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	// Unprivileged remove always fails with EACCES; chown succeeds but the
	// retry still EACCES (e.g. immutable bit) -> wrapper rm.
	opts.RemoveAllFn = func(string) error { return fs.ErrPermission }

	res := Remove(context.Background(), opts, safeChild)
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil after wrapper rm", res.Err)
	}
	if !res.Escalated {
		t.Fatalf("Escalated = false, want true")
	}
	if got := rec.ops(); len(got) != 2 || got[0] != "chown" || got[1] != "rm" {
		t.Fatalf("escalation ops = %v, want [chown rm]", got)
	}
}

func TestRemoveChownFailureStillTriesWrapperRm(t *testing.T) {
	rec := &recorder{failOps: map[string]error{"chown": errors.New("chown denied")}}
	opts := baseOptions(rec)
	opts.RemoveAllFn = func(string) error { return fs.ErrPermission }

	res := Remove(context.Background(), opts, safeChild)
	if !res.Escalated {
		t.Fatalf("Escalated = false, want true")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil (rm rescued after chown failed)", res.Err)
	}
	if got := rec.ops(); len(got) != 2 || got[0] != "chown" || got[1] != "rm" {
		t.Fatalf("ops = %v, want [chown rm] (chown failure must not skip rm)", got)
	}
}

func TestRemoveBothEscalationStepsFailSurfacesError(t *testing.T) {
	rec := &recorder{failOps: map[string]error{
		"chown": errors.New("chown denied"),
		"rm":    errors.New("rm denied"),
	}}
	opts := baseOptions(rec)
	opts.RemoveAllFn = func(string) error { return fs.ErrPermission }

	res := Remove(context.Background(), opts, safeChild)
	if !res.Escalated {
		t.Fatalf("Escalated = false, want true")
	}
	if res.Err == nil {
		t.Fatalf("Err = nil, want a clear error when both chown and rm fail")
	}
	if !strings.Contains(res.Err.Error(), "chown failed") || !strings.Contains(res.Err.Error(), "rm failed") {
		t.Fatalf("error should mention both failures: %v", res.Err)
	}
}

func TestRemoveNonPermissionErrorNeverEscalates(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	ebusy := errors.New("device or resource busy")
	opts.RemoveAllFn = func(string) error { return ebusy }

	res := Remove(context.Background(), opts, safeChild)
	if res.Escalated {
		t.Fatalf("Escalated = true; a non-permission error must NOT trigger sudo")
	}
	if !errors.Is(res.Err, ebusy) {
		t.Fatalf("Err = %v, want the underlying EBUSY surfaced for the caller classifier", res.Err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("sudo invoked for a non-permission error: %v", rec.calls)
	}
}

func TestRemoveGuardRejectionNeverInvokesWrapper(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	removeCalled := false
	opts.RemoveAllFn = func(string) error { removeCalled = true; return nil }

	// safeWorkRoot itself is not a direct CHILD, so the guard rejects it.
	res := Remove(context.Background(), opts, safeWorkRoot)
	if !errors.Is(res.Err, ErrUnsafePath) {
		t.Fatalf("Err = %v, want ErrUnsafePath", res.Err)
	}
	if res.Escalated {
		t.Fatalf("Escalated = true after guard rejection")
	}
	if removeCalled {
		t.Fatalf("RemoveAllFn called despite guard rejection")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("wrapper invoked despite guard rejection: %v", rec.calls)
	}
}

func TestRemoveNilGuardIsHardReject(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	opts.GuardFn = nil

	res := Remove(context.Background(), opts, safeChild)
	if !errors.Is(res.Err, ErrUnsafePath) {
		t.Fatalf("nil GuardFn must reject with ErrUnsafePath, got %v", res.Err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("wrapper invoked with nil GuardFn: %v", rec.calls)
	}
}

func TestRemoveRejectsDangerousPaths(t *testing.T) {
	t.Setenv("HOME", "/home/runner")
	cases := []struct {
		name string
		path string
	}{
		{"root", "/"},
		{"home env", "/home/runner"},
		{"bare home", "/home/someone"},
		{"relative", "actions-runner/_work/repo"},
		{"empty", ""},
		{"whitespace", "   "},
		{"nul byte", "/home/runner/actions-runner/_work/re\x00po"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			opts := baseOptions(rec)
			// Accept anything at the guard so the FIXED validation is what blocks.
			opts.GuardFn = func(string) error { return nil }
			removeCalled := false
			opts.RemoveAllFn = func(string) error { removeCalled = true; return nil }

			res := Remove(context.Background(), opts, tc.path)
			if !errors.Is(res.Err, ErrUnsafePath) {
				t.Fatalf("path %q: Err = %v, want ErrUnsafePath", tc.path, res.Err)
			}
			if removeCalled {
				t.Fatalf("path %q: RemoveAllFn called for a dangerous path", tc.path)
			}
			if len(rec.calls) != 0 {
				t.Fatalf("path %q: wrapper invoked for a dangerous path: %v", tc.path, rec.calls)
			}
		})
	}
}

func TestRemoveRejectsSymlinkEscapingTree(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	// The candidate passes the guard but resolves (via symlink) to /etc.
	opts.EvalSymlinksFn = func(string) (string, error) { return "/etc", nil }
	removeCalled := false
	opts.RemoveAllFn = func(string) error { removeCalled = true; return nil }

	res := Remove(context.Background(), opts, safeChild)
	if !errors.Is(res.Err, ErrUnsafePath) {
		t.Fatalf("Err = %v, want ErrUnsafePath for a symlink escape", res.Err)
	}
	if removeCalled || len(rec.calls) != 0 {
		t.Fatalf("delete/escalation ran for a symlink escaping the tree")
	}
}

func TestRemoveRejectsSymlinkToRoot(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	opts.EvalSymlinksFn = func(string) (string, error) { return "/", nil }

	res := Remove(context.Background(), opts, safeChild)
	if !errors.Is(res.Err, ErrUnsafePath) {
		t.Fatalf("Err = %v, want ErrUnsafePath for a symlink to /", res.Err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("wrapper invoked for a symlink to /: %v", rec.calls)
	}
}

func TestRemoveRejectsRootOwnedResolvedTarget(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	// The resolved target is owned by root, not the runner: refuse before sudo.
	opts.FileOwnerUIDFn = func(fs.FileInfo) (int, bool) { return rootUID, true }

	res := Remove(context.Background(), opts, safeChild)
	if !errors.Is(res.Err, ErrUnsafePath) {
		t.Fatalf("Err = %v, want ErrUnsafePath for a non-runner-owned target", res.Err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("wrapper invoked for a non-runner-owned target: %v", rec.calls)
	}
}

func TestRemoveRejectsUnknowableOwner(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	// Non-Unix FileInfo: ownership unknowable -> fail closed.
	opts.FileOwnerUIDFn = func(fs.FileInfo) (int, bool) { return 0, false }

	res := Remove(context.Background(), opts, safeChild)
	if !errors.Is(res.Err, ErrUnsafePath) {
		t.Fatalf("Err = %v, want ErrUnsafePath when owner is unknowable", res.Err)
	}
}

func TestRemoveAlreadyGoneIsClean(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	// EvalSymlinks ENOENT (already deleted) -> keep cleaned path; RemoveAll
	// no-ops -> clean success, no escalation.
	opts.EvalSymlinksFn = func(string) (string, error) { return "", fs.ErrNotExist }
	opts.RemoveAllFn = func(string) error { return nil }

	res := Remove(context.Background(), opts, safeChild)
	if res.Err != nil || res.Escalated {
		t.Fatalf("res = %+v, want clean no-op for an already-gone path", res)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("wrapper invoked for an already-gone path: %v", rec.calls)
	}
}

func TestRemovePropagatesContextCancellation(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	opts.RemoveAllFn = func(string) error { return fs.ErrPermission }
	canceled := errors.New("context canceled")
	opts.RunFn = func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canceled
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Remove(ctx, opts, safeChild)
	if res.Err == nil {
		t.Fatalf("Err = nil, want a propagated cancellation error")
	}
	if !res.Escalated {
		t.Fatalf("Escalated = false, want true (escalation attempted under canceled ctx)")
	}
}

func TestRemoveUsesConfiguredWrapperPath(t *testing.T) {
	rec := &recorder{}
	opts := baseOptions(rec)
	opts.WrapperPath = "/opt/civm/bin/custom-safedelete"
	opts.RemoveAllFn = func(string) error { return fs.ErrPermission }

	_ = Remove(context.Background(), opts, safeChild)
	if len(rec.calls) == 0 {
		t.Fatalf("expected at least one wrapper invocation")
	}
	if rec.calls[0][2] != "/opt/civm/bin/custom-safedelete" {
		t.Fatalf("wrapper path = %q, want the configured override", rec.calls[0][2])
	}
}

func TestValidateFixedTable(t *testing.T) {
	t.Setenv("HOME", "/home/runner")
	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"clean child", safeChild, false},
		{"root", "/", true},
		{"home", "/home/runner", true},
		{"bare home other", "/home/x", true},
		{"relative", "rel/path", true},
		// Clean collapses ../ to /home/etc, a bare /home/<x> dir -> rejected by
		// isBareHome even though the literal string mentioned _work.
		{"dotdot collapses to bare home", "/home/runner/actions-runner/_work/../../../etc", true},
		{"empty", "", true},
		{"nul", "/a\x00b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateFixed(tc.path)
			if tc.wantErr && err == nil {
				t.Fatalf("validateFixed(%q) = nil, want error", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateFixed(%q) = %v, want nil", tc.path, err)
			}
		})
	}
}
