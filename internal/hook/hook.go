// Package hook implements GitHub Actions self-hosted runner job hooks.
// Runtime is dispatched by small runner hook scripts into civmctl. The policy
// lives here so it is testable.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	Executed   bool   `json:"executed"`
	BytesFound int64  `json:"bytes_found,omitempty"`
	BytesFreed int64  `json:"bytes_freed,omitempty"`
	Error      string `json:"error,omitempty"`
	Warning    string `json:"warning,omitempty"`
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
	WalkDirFn       func(root string, fn fs.WalkDirFunc) error
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
		WalkDirFn:       filepath.WalkDir,
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
// remove os caches em $HOME (go-build, npm, yarn, pnpm) por inteiro e roda
// docker system prune agressivo. purgeCaches=false em modo rotineiro
// (job-completed) faz trim por tamanho/idade (preserva quentes <24h) e usa
// buildx prune mais brando — evita invalidar cache de jobs concorrentes.
func cleanup(opts Options, ctx context.Context, purgeCaches bool) []Action {
	roots := workRoots(opts)
	caps := cacheCaps()
	estCap := len(roots) + len(caps) + 8
	actions := make([]Action, 0, estCap)
	for _, root := range roots {
		actions = append(actions, cleanWorkRoot(opts, root, purgeCaches))
	}
	// Cache trim is age-based in BOTH modes. A wholesale purge of the shared
	// $HOME caches at job-started deletes the hot go-build/npm cache out from
	// under a concurrent sibling job mid-compile ("could not import ...: no
	// such file or directory"). trimCacheByAge protects recently-used files
	// (minProtect); HardFailPct still guards genuinely-full disk.
	for _, c := range caps {
		actions = append(actions, trimCacheByAge(opts, c.path, c.maxBytes, c.minProtect))
	}
	// Docker prune is always age-filtered and best-effort (commandActionWarn,
	// never fatal) in both modes. We must NOT run `docker system prune
	// --volumes` at job-started: its unfiltered content GC corrupts a
	// concurrent `docker pull` on a sibling job ("unable to lease content:
	// lease does not exist"), and on a busy daemon it can be OOM-killed
	// ("docker_prune: signal: killed") — a fatal cleanup error at job-started
	// would then reject the starting job. Filtered prunes free old
	// images/build cache without touching content a sibling is pulling, and
	// HardFailPct still guards genuinely-full disk.
	actions = append(actions, commandActionWarn(opts, ctx, "docker_buildx_prune", "docker", "buildx", "prune", "--force", "--filter", civm.DefaultDockerBuildxPruneFilter))
	actions = append(actions, commandActionWarn(opts, ctx, "docker_image_prune", "docker", "image", "prune", "-af", "--filter", civm.DefaultDockerImagePruneFilter))
	actions = append(actions, commandActionWarn(opts, ctx, "docker_container_prune", "docker", "container", "prune", "-f"))
	actions = append(actions, commandActionWarn(opts, ctx, "docker_volume_prune", "docker", "volume", "prune", "-f"))
	// apt_clean, journal_vacuum and fstrim are opportunistic disk reclaim and
	// must also be best-effort. apt-get clean returns exit 100 when a sibling
	// job holds the dpkg/apt lock, and a fatal cleanup error at job-started
	// would reject the starting job. Never let job-started cleanup fail a job.
	actions = append(actions, commandActionWarn(opts, ctx, "apt_clean", "sudo", "apt-get", "clean"))
	actions = append(actions, commandActionWarn(opts, ctx, "journal_vacuum", "sudo", "journalctl", "--vacuum-time=1d"))
	actions = append(actions, commandActionWarn(opts, ctx, "fstrim", "sudo", "fstrim", "-av"))
	return actions
}

func cleanWorkRoot(opts Options, root string, preserveActiveWorkspace bool) Action {
	a := Action{Name: "work_root", Path: root, Executed: opts.Execute}
	if !safeWorkRoot(root) {
		a.Error = "unsafe work root"
		return a
	}
	// At job-started the runner has already created the active job's
	// GITHUB_WORKSPACE under this root. Deleting it frees almost nothing but
	// breaks the starting job ("working directory ... No such file or
	// directory"). job-completed still cleans it once the job is done.
	protected := ""
	if preserveActiveWorkspace {
		protected = activeWorkspaceEntry(root, opts.GitHubWorkspace)
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
		if protected != "" && name == protected {
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

// activeWorkspaceEntry returns the top-level entry under root that contains the
// active GITHUB_WORKSPACE, or "" when workspace is empty or not under root.
// Example: root=.../_work, ws=.../_work/advoq/advoq -> "advoq". Used at
// job-started so disk-pressure cleanup never deletes the workspace the runner
// just created for the starting job.
func activeWorkspaceEntry(root, workspace string) string {
	if strings.TrimSpace(workspace) == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(workspace))
	if err != nil {
		return ""
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if parts[0] == "" {
		return ""
	}
	return parts[0]
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

// cacheCap describes a per-cache size budget for routine trim.
type cacheCap struct {
	path     string
	maxBytes int64
	// minProtect protege arquivos com mtime > Now-minProtect contra remoção,
	// para não invalidar cache quente de um job que acabou de gravar.
	minProtect time.Duration
}

// cacheCaps lista os caches sob $HOME com seus limites de tamanho.
// Em cleanup rotineiro (job-completed) usamos trim por idade/tamanho em vez de
// wipe total: preserva Go build cache até X GB, depois descarta os mais velhos.
// Constantes vivem em internal/civm/civm.go para uma fonte única de verdade.
func cacheCaps() []cacheCap {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	const giB = int64(1) << 30
	protect := time.Duration(civm.DefaultCacheTrimMinProtectHours) * time.Hour
	return []cacheCap{
		{filepath.Join(home, ".cache", "go-build"), int64(civm.DefaultCacheGoBuildMaxGB) * giB, protect},
		{filepath.Join(home, ".npm", "_cacache"), int64(civm.DefaultCacheNPMMaxGB) * giB, protect},
		{filepath.Join(home, ".yarn", "cache"), int64(civm.DefaultCacheYarnMaxGB) * giB, protect},
		{filepath.Join(home, ".pnpm-store"), int64(civm.DefaultCachePNPMMaxGB) * giB, protect},
	}
}

// trimCacheByAge walks root, sorts arquivos por mtime asc, e remove os mais
// velhos até total <= maxBytes. Arquivos com mtime > Now-minProtect são
// preservados — protege cache quente do job atual contra invalidação.
// No-op se cache não existe ou já está abaixo da tampa.
func trimCacheByAge(opts Options, root string, maxBytes int64, minProtect time.Duration) Action {
	a := Action{Name: "cache_trim", Path: root, Executed: opts.Execute}
	if strings.TrimSpace(root) == "" || root == "/" || root == os.Getenv("HOME") {
		a.Error = "unsafe cache path"
		return a
	}
	type entry struct {
		path  string
		size  int64
		mtime time.Time
	}
	var entries []entry
	var total int64
	walkErr := opts.WalkDirFn(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		entries = append(entries, entry{path: p, size: info.Size(), mtime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return a
		}
		a.Error = walkErr.Error()
		return a
	}
	a.BytesFound = total
	if total <= maxBytes {
		return a
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.Before(entries[j].mtime) })
	protectCutoff := opts.Now.Add(-minProtect)
	target := total - maxBytes
	var freed int64
	for _, e := range entries {
		if freed >= target {
			break
		}
		if minProtect > 0 && e.mtime.After(protectCutoff) {
			continue
		}
		if opts.Execute {
			if err := opts.RemoveAllFn(e.path); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				a.Error = err.Error()
				return a
			}
		}
		freed += e.size
	}
	a.BytesFreed = freed
	return a
}

// commandActionWarn é a variante tolerante: falha de comando vira Warning,
// não Error. Usada no modo rotineiro (job-completed) para que ferramentas
// ausentes (docker buildx em hosts antigos, fstrim em FS sem suporte) não
// derrubem o hook. O cleanup é best-effort entre jobs; o que importa é o
// hook retornar exit 0 para o runner continuar normalmente.
func commandActionWarn(opts Options, ctx context.Context, actionName, name string, args ...string) Action {
	return runWithTimeout(opts, ctx, actionName, true /*errorAsWarning*/, name, args...)
}

// runWithTimeout aplica DefaultRoutineCleanupCmdTimeoutSecs por comando.
// Evita que um docker travado segure o hook durante todo o TimeoutStartSec
// do systemd (30 min). Cada comando tem orçamento próprio.
func runWithTimeout(opts Options, ctx context.Context, actionName string, errorAsWarning bool, name string, args ...string) Action {
	a := Action{Name: actionName, Executed: opts.Execute}
	if !opts.Execute {
		return a
	}
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(civm.DefaultRoutineCleanupCmdTimeoutSecs)*time.Second)
	defer cancel()
	if _, err := opts.RunFn(cmdCtx, name, args...); err != nil {
		msg := err.Error()
		if errorAsWarning {
			a.Warning = msg
		} else {
			a.Error = msg
		}
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

// cachePaths deriva a lista de paths de cacheCaps() — fonte única de verdade.
// Usada pelo modo disk-pressure (wipe total) e por testes que validam o set.
func cachePaths() []string {
	caps := cacheCaps()
	if len(caps) == 0 {
		return nil
	}
	paths := make([]string, len(caps))
	for i, c := range caps {
		paths[i] = c.path
	}
	return paths
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
	if a.Name != "cache" && a.Name != "cache_trim" {
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
	if opts.WalkDirFn == nil {
		opts.WalkDirFn = filepath.WalkDir
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
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
