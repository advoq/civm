# Security policy

civm is operational infrastructure: a self-hosted GitHub Actions runner
provisioning toolkit (`civmctl`) plus systemd timers and runner hook
binaries. This doc describes the threat surface, the validations that
defend it, and how to report issues.

## Reporting a vulnerability

For anything that could let an unprivileged actor escalate to runner
privileges or compromise the VM, contact the maintainer privately first
— **do not** open a public issue. The maintainer is `@emersonbusson` on
GitHub; use a private channel (email, DM) to share details before
public disclosure.

For ordinary bugs that are not security-relevant, regular GitHub issues
are fine.

## Threat model

The civm runner is a **shared resource** across peer repos (`compexhub`,
`vitae`, `advoq`, etc.). Multiple jobs from different repos can run
concurrently on the same VM. Each job ships with whatever code its
authors push — so untrusted input includes:

- Repository source code at checkout time
- Action payloads (`actions/checkout`, third-party actions)
- Environment variables propagated by the GitHub Actions runner
- Files written under `_work` during the job
- Anything an action chooses to run via shell

The trusted set is:

- `civmctl` binary at `/usr/local/bin/civmctl` (only operators can
  install or replace; see `civmctl self-upgrade`)
- systemd unit files in `/etc/systemd/system/civmctl-*.{service,timer}`
- Target-state hook symlinks at `/opt/civm/hooks/job-{started,completed}`
  pointing at the trusted binary. Some legacy VMs can still have `.sh`
  wrappers until `civmctl hook install --execute` is run with a fresh
  binary; see `runbooks/MULTI-PROJECT-RUNNER.md`.

Implicit assumptions:

- The runner OS is Ubuntu 24.04 LTS; `civmctl bootstrap` enforces this
  before any apt operation.
- The hook process runs with the runner user's privileges, escalated
  via `sudo` only for specific allowed commands (apt-get clean,
  journalctl --vacuum-time, fstrim).

## Defended surfaces

### Path traversal in hook workspace cleanup

`internal/hook/safeWorkRoot` validates every candidate work-root path
before `os.RemoveAll`. The historical bug (caught by `FuzzSafeWorkRoot`
in PR #26) was that `filepath.Clean` does **not** resolve `..` at the
start of a relative path — so `../home/x/actions-runner/_work` slipped
through a `strings.Contains(clean, "/home/")` check. The fix enforces:

1. `filepath.IsAbs(clean)` — must be absolute
2. `strings.HasPrefix(clean, "/home/")` — prefix, not substring
3. `strings.Contains(clean, "/actions-runner")` — runner-shaped
4. `strings.HasSuffix(clean, "/_work")` — work-root literal

The fuzz harness asserts no traversal component (`..` as a path element,
not as a substring like `..0`) survives in the cleaned path. The
crashing input that uncovered the bug is committed at
`internal/hook/testdata/fuzz/FuzzSafeWorkRoot/`.

### Subprocess argument injection

`internal/civm` exposes `Validate*` regex functions that gate any CLI
flag value that ever appears in a subprocess argv:

| Validator | Pattern | Used by |
|-----------|---------|---------|
| `ValidateRepo` | `^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$` | runner, billing, cireport, peerstatus |
| `ValidateShort` | `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$` | runner directory suffix |
| `ValidateLabels` | comma-split `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$` | runner labels |
| `ValidateSemver` | `^[0-9]+[.][0-9]+[.][0-9]+$` | runner version, Go version |
| `ValidateServiceUnit` | `^[A-Za-z0-9_.@-]+[.]service$` (no `..`) | systemctl restart targets |
| `ValidateUserName` | `^[A-Za-z_][A-Za-z0-9_-]{0,63}$` | `--run-as` user |
| `ValidateWorkflowFile` | `^[A-Za-z0-9.][A-Za-z0-9._/-]{0,127}[.]ya?ml$` (no `/` prefix, no `..`) | `gh workflow` selectors |

Any caller skipping validation is a regression — `gosec G204` is
acknowledged but excluded globally because subprocess argv is always
gated by these validators (see `.golangci.yml` for the rationale).

### Privilege boundary

Hook policy in `internal/hook` is exercised through symlinks at
`/opt/civm/hooks/job-{started,completed}` that resolve to civmctl.
civmctl detects the basename via `os.Args[0]` and dispatches to the
hook subcommand with `--execute` — never as a separate shell wrapper.
This means:

- The runner can never invoke arbitrary code via the hook env vars,
  only the validated civmctl binary.
- A compromise of the hook binary path requires write access to
  `/usr/local/bin/` (root-only).
- `civmctl self-upgrade` performs the binary swap via `os.Rename`
  inside the same directory (atomic per POSIX) so concurrent
  invocations never see a half-written file.

### Disk hygiene without destroying valuable state

`internal/hook/cleanup` differentiates routine cleanup (`job-completed`)
from disk-pressure cleanup (`job-started` with disk >= threshold). The
former preserves `$HOME/.cache/go-build` and similar build caches; the
latter purges them. Conflating the two cost recurring CI failures
(PR #31 fixed it). Tests
`TestJobCompletedPreservesHotCachesUnderHome` and
`TestJobStartedPurgesHotCachesUnderDiskPressure` lock that behavior.

### Hook event log

`/var/log/civm/hooks.jsonl` is emitted via `slog.JSONHandler` with
`level` derived from decision (ERROR for `error`, WARN for `rejected`,
INFO otherwise). World-readable (`0644`) by design — operators and log
shippers (Vector/Loki) consume it. `//nolint:gosec` annotation on the
open call documents the intent.

## Linter excludes (justified)

`.golangci.yml` excludes a small set of `gosec` rules with rationale:

| Rule | Reason |
|------|--------|
| G115 | Disk arithmetic (`uint64 → int` percent, `uint64 → int64` GB) is bounded by realistic filesystem sizes and percent ranges. |
| G204 | All subprocess argv values are validated by `internal/civm.Validate*` regexes before reaching `exec.CommandContext`. |
| G304 | Path traversal: paths come from validated CLI flags (`CleanDir`) or from a whitelisted glob in `internal/hook` (`safeWorkRoot`/`safeRunnerDir`). |

When in doubt, prefer a per-line `//nolint:gosec // motivo` annotation
over expanding the global exclude list.

## Operational response

If a deployed civmctl version is found to have a security issue:

1. **Stop the bleed.** On the runner host, downgrade by disabling the
   hook env vars in every `/home/*/actions-runner*/.env`:
   ```bash
   sudo sed -i '/^ACTIONS_RUNNER_HOOK_/s/^/# /' /home/*/actions-runner*/.env
   sudo systemctl restart actions.runner.*
   ```
   The runner keeps working without the hook (no cleanup between jobs,
   but no compromised hook either).
2. **Fix.** Land the patch on `main`. Conventional Commits + the
   `release-please` automation produces a release PR.
3. **Roll forward.** Once a release with the fix is cut:
   ```bash
   cd /opt/civm && git pull --ff-only
   sudo civmctl self-upgrade --execute
   ```
   If the host predates `self-upgrade` or `/opt/civm` is not a Git
   checkout, first verify the runner is idle (`civmctl idle-check`),
   build the release binary from a trusted checkout, copy it to the VM,
   and install it atomically with `sudo install -m 0755 <binary>
   /usr/local/bin/civmctl`. Then run `sudo civmctl hook install
   --execute` to replace legacy `.sh` hook wrappers with symlinks to the
   trusted binary.
4. **Re-enable hooks.** Reverse step 1 on each runner.

## Known operational notes

- `release-please` requires either repo setting **"Allow GitHub Actions
  to create and approve pull requests"** to be enabled, or a PAT with
  `repo` scope stored as secret `RELEASE_PLEASE_TOKEN`. The workflow at
  `.github/workflows/release.yml` reads the secret with fallback.
- The hook is intentionally idempotent and tolerant: every
  `civmctl hook install --execute` is safe to re-run; legacy `.sh`
  wrappers from before PR #26 are cleaned up automatically.
