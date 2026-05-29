<#
.SYNOPSIS
    Registers the civm-host-metrics Scheduled Task (RF-3, ITEM-15).

.DESCRIPTION
    Idempotently registers a Scheduled Task that runs civm-host-metrics.ps1 as
    SYSTEM on a fixed interval. The collector is read-only on the host (it only
    queries Get-Volume/Get-VHD/Get-VM and delivers a JSON snapshot to the
    guest), so SYSTEM here needs only the Hyper-V read right plus SSH to the
    guest; no network listener and no secret are introduced.

    Idempotent: unregister-then-register (schtasks /create /f). Reversible with
    `schtasks /delete /tn civm-host-metrics /f`. Honors -WhatIf via
    SupportsShouldProcess.

    SPEC: docs/specs/host-volume-reclamation/SPECv2.md
      - DT-v2-6 / ITEM-15 : schtasks /create /tn civm-host-metrics
                            /tr "...host-metrics.ps1" /sc minute /mo 10
                            /ru SYSTEM /rl HIGHEST /f.
      - DT-v2-9           : task runs every 10 min (hostdisk MaxAge=30).
#>
[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'Medium')]
param(
    [string]$TaskName = 'civm-host-metrics',
    # Defaults next to this registrar so deploy/windows/ stays self-contained.
    [string]$ScriptPath = (Join-Path $PSScriptRoot 'civm-host-metrics.ps1'),
    # Collector cadence in minutes (DT-v2-9: 10 min, hostdisk MaxAge=30).
    [int]$IntervalMinutes = 10,
    [string]$PowerShellPath = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not (Test-Path -LiteralPath $ScriptPath)) {
    throw "collector script not found: $ScriptPath"
}
if ($IntervalMinutes -lt 1) {
    throw "IntervalMinutes must be >= 1, got $IntervalMinutes"
}

# Non-interactive, hardened invocation of the collector.
$resolvedScript = (Resolve-Path -LiteralPath $ScriptPath).Path
$action = '"{0}" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "{1}"' -f $PowerShellPath, $resolvedScript

function Test-TaskExists {
    param([string]$Name)
    & schtasks.exe /query /tn $Name *> $null
    return ($LASTEXITCODE -eq 0)
}

# 1. Unregister an existing task first so re-runs converge (idempotent).
if (Test-TaskExists -Name $TaskName) {
    if ($PSCmdlet.ShouldProcess($TaskName, 'Unregister existing Scheduled Task')) {
        & schtasks.exe /delete /tn $TaskName /f
        if ($LASTEXITCODE -ne 0) { throw "schtasks /delete failed for '$TaskName' (exit $LASTEXITCODE)" }
        Write-Host "Removed existing task '$TaskName'."
    } else {
        Write-Host "WhatIf: would remove existing task '$TaskName'."
    }
}

# 2. Register the task: SYSTEM, highest run level, every $IntervalMinutes.
$target = "$TaskName (every $IntervalMinutes min, SYSTEM, RL HIGHEST)"
if ($PSCmdlet.ShouldProcess($target, 'Register Scheduled Task')) {
    & schtasks.exe /create `
        /tn $TaskName `
        /tr $action `
        /sc minute `
        /mo $IntervalMinutes `
        /ru SYSTEM `
        /rl HIGHEST `
        /f
    if ($LASTEXITCODE -ne 0) { throw "schtasks /create failed for '$TaskName' (exit $LASTEXITCODE)" }
    Write-Host "Registered Scheduled Task '$TaskName' running '$resolvedScript' every $IntervalMinutes min as SYSTEM."
    Write-Host "Verify: schtasks /query /tn $TaskName"
    Write-Host "Run now: schtasks /run /tn $TaskName"
    Write-Host "Remove:  schtasks /delete /tn $TaskName /f"
} else {
    Write-Host "WhatIf: would register '$TaskName' running '$resolvedScript' every $IntervalMinutes min as SYSTEM (RL HIGHEST)."
}
