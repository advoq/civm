# Runbook — Windows host orchestrator (scale-to-zero)

> Day-0 host path for Hyper-V. **Secrets never enter git.** Product/org fleet is
> **operator config**, not repository defaults (open-source hygiene).

## Topology

| Layer | Role |
| --- | --- |
| Guest Linux (`civmctl`) | Runners, cleanup, reaper, doctor |
| Host Windows (this runbook) | Scale-to-zero: start/stop VM, `Optimize-VHD`, GitHub queue poll |
| Optional sibling **civm-host** | C# rewrite; production remains PowerShell until F4 cutover |

## Preconditions

1. Hyper-V VM exists (name default `gha-ubuntu-2404`).
2. Volume for VHDX (default `V:`) with headroom.
3. **SYSTEM-readable** SSH key: `C:\ProgramData\civm\ssh\id_ed25519` (ACL: SYSTEM only).
4. One fine-grained PAT per GitHub owner with **actions:read**, as files:
   `C:\ProgramData\civm\gh-token-<owner>.txt` (single line, no log echo).
5. Guest user + IP/DNS reachable from SYSTEM SSH (prefer stable LAN IP if Tailscale DNS is stale).

## Deploy scripts (atomic)

From an elevated PowerShell, in the repo `deploy/windows` (or a staging copy):

```powershell
# Copies decision/reclaim/pr-queue/orchestrator/host-metrics into C:\civm-deploy
# and registers SYSTEM task. Default registration uses -Observe (safe).
pwsh -NoProfile -ExecutionPolicy Bypass -File .\activate-orchestrator.ps1
```

`register-orchestrator.ps1` alone registers **Observe** only. Production EnforceQueue
must pass `-EnforceQueue` **and** non-empty `Repos` + `TokenPaths`.

## Host-local lab wrapper pattern (recommended)

Public `civm-vm-orchestrator.ps1` defaults:

- `Repos = @()`
- `TokenPaths = @{}`
- `GuestSshTarget = 'emdev@gha-ubuntu-2404'` (example host; override)

Keep **lab fleet out of git**. Example host-only wrapper (not committed):

```powershell
# C:\civm-deploy\civm-vm-orchestrator-lab.ps1
param([switch]$Observe, [switch]$EnforceQueue)
$invoke = @{
  TokenPaths     = @{ 'myorg' = 'C:\ProgramData\civm\gh-token-myorg.txt' }
  Repos          = @('myorg/app', 'myorg/other')
  GuestSshTarget = 'myuser@192.168.0.50'   # stable reachability
}
if ($Observe)      { $invoke['Observe'] = $true }
if ($EnforceQueue) { $invoke['EnforceQueue'] = $true }
& "$PSScriptRoot\civm-vm-orchestrator.ps1" @invoke
```

Register the wrapper as the Scheduled Task action (SYSTEM, every ~2 min, boot trigger).
Disable legacy `civm-vhdx-autoreclaim` / `civm-vhdx-optimize` / `*-watchdog` so **one owner**
holds stop/compact (Kahneman #15).

## host-metrics

```powershell
# Register metrics task (separate cadence) — override GuestSshTarget if needed:
pwsh -File C:\civm-deploy\civm-host-metrics.ps1 -GuestSshTarget 'myuser@192.168.0.50'
```

Snapshot: `V:\civm-host-metrics.json`. When the VM is Off, guest `df` over SSH fails by design;
the orchestrator treats missing/`<=0` guest free as **999 (unknown)** — does not block admit.

## Validate (numbers before adjectives)

| Check | Expect |
| --- | --- |
| Task `civm-vm-orchestrator` LastTaskResult | `0` |
| Log `V:\civm-orchestrator.log` | `tick` JSON lines; no perpetual `token ausente` for configured owners |
| Idle ≥ `IdleStopMinutes` (default 10) + empty queue | `reclaim_start` → VM Off; optional `reclaim_mount_retry` then `reclaim_done` |
| Queue while Off | `vm_started` → guest runners online |
| Lock | Only one of PS orch / civm-host **active** holds `V:\civm-reclaim.lock` |

## Rollback trigger

- `orchestrator_error` rate dominates ticks, or VM thrash (start/stop loop) under normal queue.
- Dual active reclaimers (PS + `civm-host --active`) without F4 cutover — disable one immediately.

## Related

- Behavior SPEC: `docs/specs/orchestrator-scale-to-zero/`
- PR-queue canary: `runbooks/PR-QUEUE-ENABLE.md`
- VHDX maintenance: `runbooks/RUNBOOK-HOST-VHDX-MAINTENANCE.md`
- Go host port: **superseded** — `docs/specs/orchestrator-go-port/STATUS.md`
- C# rewrite: sibling `civm-host` ROADMAP F3/F4
