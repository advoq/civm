// Package hook implements GitHub Actions self-hosted runner job hooks.
// Runtime is dispatched by small runner hook scripts into civmctl. The policy
// lives here so it is testable.
package hook

import (
	"context"
	"encoding/json"
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

	"github.com/advoq/civm/internal/cachetrim"
	"github.com/advoq/civm/internal/civm"
	"github.com/advoq/civm/internal/hostdisk"
	"github.com/advoq/civm/internal/safedelete"
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
	// WorkRoot is the runner's _work directory; it identifies WHICH runner
	// emitted this record on the shared hooks.jsonl, so the runner watchdog can
	// map a broken-runner sentinel to the right systemd unit (RF-6 / ITEM-10).
	WorkRoot string   `json:"work_root,omitempty"`
	Actions  []Action `json:"actions,omitempty"`
	Error    string   `json:"error,omitempty"`
	// HostLevel/HostVFreeGB carry the Hyper-V host volume state read from the
	// delivered host-metrics snapshot. The guest-% gate above (DiskUsedPct) does
	// not see host pressure: the guest can be comfortable while the host V: volume
	// already crossed a floor (the 2026-06 PausedCritical incident). Surfaced on
	// hooks.jsonl for the watchdog/observability.
	HostLevel   string `json:"host_level,omitempty"`
	HostVFreeGB int64  `json:"host_v_free_gb,omitempty"`
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
	// SafeWorkDeleteFn removes one top-level _work entry, escalating to the
	// privileged wrapper only when a root-owned file (a containerized CI step
	// ran as root and wrote into the mounted _work) blocks the unprivileged
	// delete. Without it, EACCES on a root-owned leftover fails job-completed at
	// "Complete runner" and wedges every later job on this runner. The GuardFn
	// scopes the escalation to a direct child of a safeWorkRoot. Injected so
	// unit tests never call real sudo (DT-v2-1/3/9).
	SafeWorkDeleteFn func(ctx context.Context, path string) safedelete.Result
	MkdirAllFn       func(path string, perm os.FileMode) error
	StatfsFn         func(path string) (totalBytes, freeBytes uint64, err error)
	DiscoverRootsFn  func() ([]string, error)
	ReadDirFn        func(path string) ([]os.DirEntry, error)
	WalkDirFn        func(root string, fn fs.WalkDirFunc) error
	// HostDiskFn reads the Hyper-V host volume snapshot delivered to the guest and
	// classifies it (ok/warn/crit). The job-started gate uses it so host V:
	// pressure triggers cleanup/rejection even when the guest filesystem % alone
	// would not. Injected so unit tests never touch the metrics file.
	HostDiskFn func() (hostdisk.Report, error)
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
		LogPath:         civm.DefaultHooksLogPath,
		Now:             time.Now(),
		RunFn:           defaultRun,
		RemoveAllFn:     os.RemoveAll,
		MkdirAllFn:      os.MkdirAll,
		StatfsFn:        defaultStatfs,
		DiscoverRootsFn: discoverRunnerWorkRoots,
		ReadDirFn:       os.ReadDir,
		WalkDirFn:       filepath.WalkDir,
		HostDiskFn:      defaultHostDisk,
	}
}

// defaultHostDisk reads + classifies the delivered host-metrics snapshot. A read
// error is never fatal to the hook: hostdisk.Check folds an absent/unreadable
// file into a fail-safe crit Report (Stale=true), which WantsCleanup but does
// NOT Block — so a missing metrics file forces cleanup without wedging CI.
func defaultHostDisk() (hostdisk.Report, error) {
	return hostdisk.Check(hostdisk.DefaultOptions())
}

// newSafeWorkDelete builds the hook-scoped safedelete closure. It routes the
// unprivileged remove through the hook's own RemoveAllFn (so an injected
// RemoveAllFn keeps capturing the happy path) and escalates to the guarded
// wrapper only for the root-owned _work case. The escalation can only ever
// target a direct child of a safeWorkRoot.
func newSafeWorkDelete(removeAllFn func(string) error) func(context.Context, string) safedelete.Result {
	return func(ctx context.Context, path string) safedelete.Result {
		return safedelete.Remove(ctx, safedelete.Options{
			GuardFn:     workChildGuard,
			RemoveAllFn: removeAllFn,
		}, path)
	}
}

// workChildGuard rejects any path that is not a direct child of a valid
// safeWorkRoot. safeWorkRoot validates the ROOT (under /home, /actions-runner,
// trailing /_work); this adapter derives that root from the candidate and
// confirms the parent/child relation, symmetric to the cleanup guard (DT-v2-9).
func workChildGuard(path string) error {
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	if !safeWorkRoot(parent) {
		return fmt.Errorf("parent %q is not a safe _work root", parent)
	}
	if filepath.Base(clean) == "" {
		return fmt.Errorf("%q is not a direct child of a _work root", clean)
	}
	return nil
}

func Run(ctx context.Context, opts Options) Result {
	applyDefaults(&opts)
	res := Result{Event: opts.Event, Repository: opts.Repository, RunID: opts.RunID, WorkRoot: opts.WorkRoot, Decision: DecisionOK, ExitCode: 0}
	usedPct, err := diskUsedPct(opts)
	if err != nil {
		return finish(opts, errorResult(res, err))
	}
	res.DiskUsedPct = usedPct

	switch opts.Event {
	case EventJobStarted:
		// Host-aware (Bug A do incidente 2026-06): o gate guest-% não vê a pressão
		// do volume V: do host. Lê o snapshot de host-metrics entregue ao guest; um
		// erro de leitura vira um Report crit fail-safe (Stale=true) — força
		// cleanup, mas não bloqueia (telemetria ausente != disco cheio).
		host, _ := opts.HostDiskFn()
		res.HostLevel = host.Level
		res.HostVFreeGB = host.VFreeGB
		// Cleanup dispara por pressão do guest-% OU do host V: (warn/crit). A
		// metade host-aware pega o caso que o guest-% perde: guest enxuto, V: cheio.
		if usedPct >= opts.PreCleanupPct || host.WantsCleanup() {
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
		// Rejeita por hard-fail do guest OU host crit FRESCO (Blocks): V: realmente
		// cheio com telemetria atual. Snapshot stale nunca bloqueia (gap de infra,
		// não prova) — só forçou cleanup acima. O cleanup do guest ajuda o próximo
		// Optimize do host a recuperar; o block evita iniciar run e cair em
		// PausedCritical antes disso.
		switch {
		case res.DiskUsedPct >= opts.HardFailPct:
			res.Decision = DecisionRejected
			res.ExitCode = 75
			res.Error = fmt.Sprintf("disk usage %d%% >= hard fail threshold %d%%", res.DiskUsedPct, opts.HardFailPct)
		case host.Blocks():
			res.Decision = DecisionRejected
			res.ExitCode = 75
			res.Error = fmt.Sprintf("host V: free %dGB at crit floor (level=%s) — refusing job to avoid PausedCritical", host.VFreeGB, host.Level)
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
		actions = append(actions, cleanWorkRoot(ctx, opts, root, purgeCaches))
	}
	// Cache trim is age-based in BOTH modes. A wholesale purge of the shared
	// $HOME caches at job-started deletes the hot go-build/npm cache out from
	// under a concurrent sibling job mid-compile ("could not import ...: no
	// such file or directory"). trimCacheByAge protects recently-used files
	// (minProtect); HardFailPct still guards genuinely-full disk.
	for _, c := range caps {
		actions = append(actions, trimCacheByAge(opts, c.path, c.maxBytes, c.minProtect))
	}
	// Docker prune is always best-effort (commandActionWarn, never fatal) in
	// both modes. Two concurrency hazards on the shared runner:
	//
	//  1. `docker system prune --volumes` unfiltered content GC corrupts a
	//     concurrent `docker pull` on a sibling job ("unable to lease content:
	//     lease does not exist") — so we never run it here.
	//  2. `docker image prune -a` removes ALL unused TAGGED images. The age
	//     `--filter until=` matches on image CREATED date (the vendor build
	//     date), not pull date, so a recently-pulled but old vendor image
	//     (redis, minio, alpine, clamav, postgres base) is deleted out from
	//     under a sibling job that is mid `compose up --build` — the sibling
	//     then fails with "No such image". So job cleanup prunes DANGLING
	//     images only (`-f`, no `-a`): untagged superseded layers are safe to
	//     remove and are the bulk of build churn, while tagged images another
	//     job pulled or built are never touched.
	//
	// Build cache (the largest disk consumer) is still trimmed by buildx prune
	// (until=24h), and HardFailPct guards genuinely-full disk.
	actions = append(actions, commandActionWarn(opts, ctx, "docker_buildx_prune", "docker", "buildx", "prune", "--force", "--filter", civm.DefaultDockerBuildxPruneFilter))
	actions = append(actions, commandActionWarn(opts, ctx, "docker_image_prune", "docker", "image", "prune", "-f"))
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

func cleanWorkRoot(ctx context.Context, opts Options, root string, preserveActiveWorkspace bool) Action {
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
			// A CI Docker step that ran as root may have written files into the
			// mounted _work that this user cannot unlink (EACCES on unlinkat).
			// safedelete tries the unprivileged remove first and escalates to
			// the guarded wrapper only for that root-owned case, so the runner
			// never wedges at "Complete runner". A terminal error (escalation
			// itself unavailable) surfaces here and stays fatal at job-completed
			// — it is never silently swallowed (DT-v2-1/3/12).
			if res := opts.SafeWorkDeleteFn(ctx, path); res.Err != nil {
				a.Error = res.Err.Error()
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
// cacheCap mirrors cachetrim.Cap with this package's field names so the existing
// hook cleanup flow and tests keep their shape; the policy itself lives in the
// shared internal/cachetrim (one source of truth, also used by internal/cleanup).
type cacheCap struct {
	path       string
	maxBytes   int64
	minProtect time.Duration
}

// cacheCaps delegates to the shared cachetrim policy for the runner user's home.
// The hook runs AS the runner user, so $HOME is the right (single) home. The
// disk-pressure cleanup (internal/cleanup, runs as root) applies the SAME policy
// across every /home/* runner home — both go through internal/cachetrim, so the
// named-dir glob + family budget live in exactly one place.
func cacheCaps() []cacheCap {
	shared := cachetrim.Caps([]string{os.Getenv("HOME")}, cachetrim.Deps{})
	out := make([]cacheCap, len(shared))
	for i, c := range shared {
		out[i] = cacheCap{path: c.Path, maxBytes: c.MaxBytes, minProtect: c.MinProtect}
	}
	return out
}

// trimCacheByAge delegates one cache dir's age/size trim to cachetrim and maps
// the result to a hook Action.
func trimCacheByAge(opts Options, root string, maxBytes int64, minProtect time.Duration) Action {
	r := cachetrim.TrimByAge(cachetrim.Options{
		Execute:     opts.Execute,
		Now:         opts.Now,
		WalkDirFn:   opts.WalkDirFn,
		RemoveAllFn: opts.RemoveAllFn,
	}, cachetrim.Cap{Path: root, MaxBytes: maxBytes, MinProtect: minProtect})
	a := Action{Name: "cache_trim", Path: r.Path, BytesFound: r.BytesFound, BytesFreed: r.BytesFreed, Executed: r.Executed}
	if r.Err != nil {
		a.Error = r.Err.Error()
	}
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

// workRootGlob is the single canonical shape of a runner _work root:
// /home/<user>/actions-runner*/_work. Discovery and the safeWorkRoot guard both
// match it via filepath.Match, whose '*' never crosses a path separator — a
// path-SEGMENT match, not a substring, so a decoy like
// /home/x/sub/actions-runnerEVIL/deep/_work cannot slip past the privileged
// delete guard (DT-v2-7; testing.md "guard text must match guard behavior").
const workRootGlob = "/home/*/actions-runner*/_work"

func safeWorkRoot(root string) bool {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) {
		return false
	}
	ok, err := filepath.Match(workRootGlob, clean)
	return err == nil && ok
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
		slog.String("work_root", res.WorkRoot),
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
	if opts.SafeWorkDeleteFn == nil {
		opts.SafeWorkDeleteFn = newSafeWorkDelete(opts.RemoveAllFn)
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
	if opts.HostDiskFn == nil {
		opts.HostDiskFn = defaultHostDisk
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
}

func discoverRunnerWorkRoots() ([]string, error) {
	return filepath.Glob(workRootGlob)
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
