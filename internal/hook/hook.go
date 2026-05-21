// Package hook implements GitHub Actions self-hosted runner job hooks.
// Runtime is dispatched by small runner hook scripts into civmctl. The policy
// lives here so it is testable.
package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/advoq/civm/internal/civm"
)

type Event string

const (
	EventJobStarted   Event = "job-started"
	EventJobCompleted Event = "job-completed"
)

type Decision string

const (
	DecisionOK             Decision = "ok"
	DecisionCleanupApplied Decision = "cleanup-applied"
	DecisionRejected       Decision = "rejected"
	DecisionError          Decision = "error"
)

type Action struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Executed bool   `json:"executed"`
	Error    string `json:"error,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

type Result struct {
	Event       Event    `json:"event"`
	Decision    Decision `json:"decision"`
	ExitCode    int      `json:"exit_code"`
	Repository  string   `json:"repository,omitempty"`
	RunID       string   `json:"run_id,omitempty"`
	DiskUsedPct int      `json:"disk_used_pct"`
	Actions     []Action `json:"actions,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type Options struct {
	Event           Event
	Execute         bool
	PreCleanupPct   int
	HardFailPct     int
	FilesystemPath  string
	WorkRoot        string
	RunnerTemp      string
	GitHubWorkspace string
	Repository      string
	RunID           string
	LogPath         string
	Now             time.Time
	RunFn           func(ctx context.Context, name string, args ...string) ([]byte, error)
	RemoveAllFn     func(path string) error
	MkdirAllFn      func(path string, perm os.FileMode) error
	StatfsFn        func(path string) (totalBytes, freeBytes uint64, err error)
	DiscoverRootsFn func() ([]string, error)
	ReadDirFn       func(path string) ([]os.DirEntry, error)
}

func DefaultOptionsFromEnv(event Event) Options {
	return Options{
		Event:           event,
		PreCleanupPct:   civm.DefaultPreCleanupPct,
		HardFailPct:     civm.DefaultHardFailPct,
		FilesystemPath:  "/",
		RunnerTemp:      os.Getenv("RUNNER_TEMP"),
		GitHubWorkspace: os.Getenv("GITHUB_WORKSPACE"),
		Repository:      os.Getenv("GITHUB_REPOSITORY"),
		RunID:           os.Getenv("GITHUB_RUN_ID"),
		LogPath:         "/var/log/civm/hooks.jsonl",
		Now:             time.Now(),
		RunFn:           defaultRun,
		RemoveAllFn:     os.RemoveAll,
		MkdirAllFn:      os.MkdirAll,
		StatfsFn:        defaultStatfs,
		DiscoverRootsFn: discoverRunnerWorkRoots,
		ReadDirFn:       os.ReadDir,
	}
}

func Run(ctx context.Context, opts Options) Result {
	applyDefaults(&opts)
	res := Result{Event: opts.Event, Repository: opts.Repository, RunID: opts.RunID, Decision: DecisionOK, ExitCode: 0}
	usedPct, err := diskUsedPct(opts)
	if err != nil {
		return finish(opts, errorResult(res, err))
	}
	res.DiskUsedPct = usedPct

	switch opts.Event {
	case EventJobStarted:
		if usedPct >= opts.PreCleanupPct {
			res.Decision = DecisionCleanupApplied
			// Disk pressure mode: purga caches ($HOME/.cache/go-build, npm, etc.)
			// para liberar espaço suficiente.
			res.Actions = append(res.Actions, cleanup(opts, ctx, true)...)
			if usedAfter, err := diskUsedPct(opts); err == nil {
				res.DiskUsedPct = usedAfter
			}
			if err := firstActionError(res.Actions); err != nil {
				if onlyIgnorableCacheDeleteRaces(res.Actions) {
					demoteIgnorableCacheDeleteRaces(res.Actions)
				} else {
					return finish(opts, errorResult(res, err))
				}
			}
		}
		if res.DiskUsedPct >= opts.HardFailPct {
			res.Decision = DecisionRejected
			res.ExitCode = 75
			res.Error = fmt.Sprintf("disk usage %d%% >= hard fail threshold %d%%", res.DiskUsedPct, opts.HardFailPct)
		}
	case EventJobCompleted:
		res.Decision = DecisionCleanupApplied
		// Modo rotineiro: preserva caches hot ($HOME/.cache/go-build, etc.)
		// para evitar invalidar build caches entre jobs concorrentes na VM.
		// Go build cache especialmente é caro de reconstruir (~minutos por
		// stdlib + deps), e wipe a cada job-completed quebrava lint
		// concorrente quando outro PR estava em fila.
		res.Actions = append(res.Actions, cleanup(opts, ctx, false)...)
		if err := firstActionError(res.Actions); err != nil {
			return finish(opts, errorResult(res, err))
		}
		if usedAfter, err := diskUsedPct(opts); err == nil {
			res.DiskUsedPct = usedAfter
		}
	default:
		return finish(opts, errorResult(res, fmt.Errorf("unsupported hook event %q", opts.Event)))
	}
	return finish(opts, res)
}

// cleanup orchestra a limpeza. purgeCaches=true em disk pressure mode
// remove os caches em $HOME (go-build, npm, yarn, pnpm); false em modo
// rotineiro (job-completed) os preserva.
func cleanup(opts Options, ctx context.Context, purgeCaches bool) []Action {
	roots := workRoots(opts)
	hotCaches := cachePaths()
	estCap := len(roots) + 4
	if purgeCaches {
		estCap += len(hotCaches)
	}
	actions := make([]Action, 0, estCap)
	for _, root := range roots {
		actions = append(actions, cleanWorkRoot(opts, root))
	}
	if purgeCaches {
		for _, path := range hotCaches {
			actions = append(actions, removePath(opts, path, "cache"))
		}
	}
	actions = append(actions, commandAction(opts, ctx, "docker_prune", "docker", "system", "prune", "-af", "--volumes"))
	actions = append(actions, commandAction(opts, ctx, "apt_clean", "sudo", "apt-get", "clean"))
	actions = append(actions, commandAction(opts, ctx, "journal_vacuum", "sudo", "journalctl", "--vacuum-time=1d"))
	actions = append(actions, commandAction(opts, ctx, "fstrim", "sudo", "fstrim", "-av"))
	return actions
}

func cleanWorkRoot(opts Options, root string) Action {
	a := Action{Name: "work_root", Path: root, Executed: opts.Execute}
	if !safeWorkRoot(root) {
		a.Error = "unsafe work root"
		return a
	}
	entries, err := opts.ReadDirFn(root)
	if err != nil {
		if os.IsNotExist(err) {
			return a
		}
		a.Error = err.Error()
		return a
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "_tool" || name == "_actions" {
			continue
		}
		path := filepath.Join(root, name)
		if opts.Execute {
			if err := opts.RemoveAllFn(path); err != nil {
				a.Error = err.Error()
				return a
			}
		}
	}
	return a
}

func removePath(opts Options, path, name string) Action {
	a := Action{Name: name, Path: path, Executed: opts.Execute}
	if strings.TrimSpace(path) == "" || path == "/" || path == os.Getenv("HOME") {
		a.Error = "unsafe cache path"
		return a
	}
	if opts.Execute {
		if err := opts.RemoveAllFn(path); err != nil {
			a.Error = err.Error()
		}
	}
	return a
}

func commandAction(opts Options, ctx context.Context, actionName, name string, args ...string) Action {
	a := Action{Name: actionName, Executed: opts.Execute}
	if !opts.Execute {
		return a
	}
	if _, err := opts.RunFn(ctx, name, args...); err != nil {
		a.Error = err.Error()
	}
	return a
}

func workRoots(opts Options) []string {
	seen := map[string]struct{}{}
	var roots []string
	add := func(root string) {
		root = filepath.Clean(strings.TrimSpace(root))
		if safeWorkRoot(root) {
			if _, ok := seen[root]; !ok {
				seen[root] = struct{}{}
				roots = append(roots, root)
			}
		}
	}
	if opts.WorkRoot != "" {
		add(opts.WorkRoot)
	}
	if opts.RunnerTemp != "" {
		add(filepath.Dir(opts.RunnerTemp))
	}
	if opts.GitHubWorkspace != "" {
		add(filepath.Dir(filepath.Dir(opts.GitHubWorkspace)))
	}
	if len(roots) == 0 && opts.DiscoverRootsFn != nil {
		discovered, _ := opts.DiscoverRootsFn()
		for _, root := range discovered {
			add(root)
		}
	}
	sort.Strings(roots)
	return roots
}

func safeWorkRoot(root string) bool {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) {
		return false
	}
	return strings.HasPrefix(clean, "/home/") &&
		strings.Contains(clean, "/actions-runner") &&
		strings.HasSuffix(clean, "/_work")
}

func cachePaths() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".cache", "go-build"),
		filepath.Join(home, ".npm", "_cacache"),
		filepath.Join(home, ".yarn", "cache"),
		filepath.Join(home, ".pnpm-store"),
	}
}

func diskUsedPct(opts Options) (int, error) {
	total, free, err := opts.StatfsFn(opts.FilesystemPath)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, fmt.Errorf("statfs total = 0")
	}
	return int((total - free) * 100 / total), nil
}

func finish(opts Options, res Result) Result {
	if res.ExitCode == 0 && (res.Decision == DecisionError || res.Decision == DecisionRejected) {
		res.ExitCode = 1
	}
	_ = appendLog(opts, res)
	return res
}

func errorResult(res Result, err error) Result {
	res.Decision = DecisionError
	res.ExitCode = 1
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func firstActionError(actions []Action) error {
	for _, a := range actions {
		if a.Error != "" {
			return fmt.Errorf("%s: %s", a.Name, a.Error)
		}
	}
	return nil
}

func onlyIgnorableCacheDeleteRaces(actions []Action) bool {
	hasIgnorable := false
	for _, a := range actions {
		if a.Error == "" {
			continue
		}
		if !isIgnorableCacheDeleteRace(a) {
			return false
		}
		hasIgnorable = true
	}
	return hasIgnorable
}

func demoteIgnorableCacheDeleteRaces(actions []Action) {
	for i := range actions {
		if isIgnorableCacheDeleteRace(actions[i]) {
			actions[i].Warning = actions[i].Error
			actions[i].Error = ""
		}
	}
}

func isIgnorableCacheDeleteRace(a Action) bool {
	if a.Name != "cache" {
		return false
	}
	return strings.Contains(strings.ToLower(a.Error), "directory not empty")
}

func appendLog(opts Options, res Result) error {
	if opts.LogPath == "" || !opts.Execute {
		return nil
	}
	if err := opts.MkdirAllFn(filepath.Dir(opts.LogPath), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644) //nolint:gosec // G302: hook log intencionalmente world-readable para ops/observabilidade
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
	level := slog.LevelInfo
	switch res.Decision {
	case DecisionError:
		level = slog.LevelError
	case DecisionRejected:
		level = slog.LevelWarn
	}
	logger.LogAttrs(context.Background(), level, "hook event",
		slog.String("event", string(res.Event)),
		slog.String("decision", string(res.Decision)),
		slog.Int("exit_code", res.ExitCode),
		slog.Int("disk_used_pct", res.DiskUsedPct),
		slog.String("repository", res.Repository),
		slog.String("run_id", res.RunID),
		slog.String("error", res.Error),
		slog.Any("actions", res.Actions),
	)
	return nil
}

func RenderJSON(w io.Writer, res Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func RenderText(w io.Writer, res Result) {
	fmt.Fprintf(w, "civm hook %s: %s (exit %d, disk=%d%%)\n", res.Event, res.Decision, res.ExitCode, res.DiskUsedPct)
	if res.Error != "" {
		fmt.Fprintf(w, "Error: %s\n", res.Error)
	}
	for _, a := range res.Actions {
		status := "dry-run"
		if a.Executed {
			status = "ok"
		}
		if a.Error != "" {
			status = "error: " + a.Error
		} else if a.Warning != "" {
			status = "warn: " + a.Warning
		}
		fmt.Fprintf(w, "  %-14s %-50s %s\n", a.Name, a.Path, status)
	}
}

func applyDefaults(opts *Options) {
	if opts.PreCleanupPct == 0 {
		opts.PreCleanupPct = civm.DefaultPreCleanupPct
	}
	if opts.HardFailPct == 0 {
		opts.HardFailPct = civm.DefaultHardFailPct
	}
	if opts.FilesystemPath == "" {
		opts.FilesystemPath = "/"
	}
	if opts.RunFn == nil {
		opts.RunFn = defaultRun
	}
	if opts.RemoveAllFn == nil {
		opts.RemoveAllFn = os.RemoveAll
	}
	if opts.MkdirAllFn == nil {
		opts.MkdirAllFn = os.MkdirAll
	}
	if opts.StatfsFn == nil {
		opts.StatfsFn = defaultStatfs
	}
	if opts.DiscoverRootsFn == nil {
		opts.DiscoverRootsFn = discoverRunnerWorkRoots
	}
	if opts.ReadDirFn == nil {
		opts.ReadDirFn = os.ReadDir
	}
}

func discoverRunnerWorkRoots() ([]string, error) {
	return filepath.Glob("/home/*/actions-runner*/_work")
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func defaultStatfs(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return uint64(st.Blocks) * uint64(st.Bsize), uint64(st.Bavail) * uint64(st.Bsize), nil
}
