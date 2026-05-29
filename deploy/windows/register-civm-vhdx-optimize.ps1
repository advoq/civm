<#
.SYNOPSIS
    Idempotent registrar for the civm-vhdx-optimize Scheduled Task and its
    watchdog.

.DESCRIPTION
    Registers the SYSTEM Scheduled Tasks defined in
    docs/specs/host-volume-reclamation/SPECv2.md (ITEM-15, DT-v2-6/7):

      * civm-vhdx-optimize           — runs civm-vhdx-optimize.ps1. Primary
        trigger is manual (`schtasks /run /tn civm-vhdx-optimize`) in a
        supervised maintenance window. A weekly off-hours trigger is added so
        the reclaim runs unattended when desired; the SPECv2 baseline uses
        `/sc onstart`, but a weekly window is the safer default for a runner
        host that is rarely rebooted. Both are SYSTEM / RL HIGHEST.

      * civm-vhdx-optimize-watchdog  — runs every 5 minutes. If the VM is Off
        AND no civm-vhdx-optimize instance is running, it unconditionally
        Start-VM (DT-v2-7). This guarantees a crashed reclaim that left the VM
        Off is recovered within 5 minutes.

    Idempotent: existing tasks of the same name are deleted (`/f`) and
    recreated, so re-running converges to the declared definition. Reversible
    with `schtasks /delete /tn <name> /f`.

    Privilege audit (DT-v2-6): both tasks run as SYSTEM with RL HIGHEST. They
    require only the local Hyper-V management right (Optimize-VHD / Start-VM /
    Get-VM) plus read/write on V:. No network credential and no secret is
    embedded; the only outbound action is local SSH to the guest using the
    host's existing key (key management lives outside this registrar).

.PARAMETER ScriptDir
    Directory containing civm-vhdx-optimize.ps1. Defaults to this script's own
    directory so a checked-out deploy/windows tree is self-registering.

.PARAMETER VhdxPath
    Absolute path to the VHDX passed to civm-vhdx-optimize.ps1 (-VhdxPath).

.PARAMETER VMName
    Hyper-V VM name passed to both scripts. Default gha-ubuntu-2404.

.PARAMETER MinHeadroomGB
    Headroom floor passed to civm-vhdx-optimize.ps1 (-MinHeadroomGB). Mirrors
    civm.DefaultHostVolumeHeadroomGB (15).

.PARAMETER WeeklyDay
    Day of week for the unattended weekly trigger. Default SUN.

.PARAMETER WeeklyTime
    HH:mm for the unattended weekly trigger (off-hours, low traffic; never
    during Windows Update — DT-v2-16). Default 03:00.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\register-civm-vhdx-optimize.ps1 `
        -VhdxPath 'V:\gha-ubuntu-2404\Virtual Hard Disks\gha-ubuntu-2404.vhdx'

.NOTES
    Run elevated (the schtasks /create for SYSTEM requires admin). Verify with:
        schtasks /query /tn civm-vhdx-optimize /v /fo LIST
        schtasks /query /tn civm-vhdx-optimize-watchdog /v /fo LIST
    Trigger manually with:
        schtasks /run /tn civm-vhdx-optimize
#>
[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter()]
    [ValidateNotNullOrEmpty()]
    [string]$ScriptDir = $PSScriptRoot,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$VhdxPath,

    [Parameter()]
    [ValidateNotNullOrEmpty()]
    [string]$VMName = 'gha-ubuntu-2404',

    [Parameter()]
    [ValidateRange(1, 4096)]
    [int]$MinHeadroomGB = 15,

    [Parameter()]
    [ValidateSet('MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT', 'SUN')]
    [string]$WeeklyDay = 'SUN',

    [Parameter()]
    [ValidatePattern('^\d{2}:\d{2}$')]
    [string]$WeeklyTime = '03:00',

    [Parameter()]
    [ValidateNotNullOrEmpty()]
    [string]$PowerShellPath = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$OptimizeTaskName = 'civm-vhdx-optimize'
$WatchdogTaskName = 'civm-vhdx-optimize-watchdog'

$OptimizeScript = Join-Path $ScriptDir 'civm-vhdx-optimize.ps1'
$WatchdogScript = Join-Path $ScriptDir 'civm-vhdx-optimize-watchdog.ps1'

if (-not (Test-Path -LiteralPath $OptimizeScript)) {
    throw "optimize script not found: $OptimizeScript"
}

# The watchdog body is small and self-contained; materialize it next to the
# optimize script so a single deploy/windows checkout fully registers. It is
# the operational form of DT-v2-7.
$watchdogBody = @"
<#
.SYNOPSIS
    civm-vhdx-optimize-watchdog: religa a VM se ela ficou Off sem reclaim ativo.
.DESCRIPTION
    SPECv2 DT-v2-7. A cada 5 min (SYSTEM): se a VM esta Off E nenhuma instancia
    de civm-vhdx-optimize esta rodando, faz Start-VM incondicional e loga. Isso
    recupera um reclaim que crashou deixando a VM Off (invariante "VM nunca fica
    Off").
#>
[CmdletBinding()]
param(
    [string]`$VMName = '$VMName'
)
`$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
`$LogPath = 'V:\civm-hyperv-maintenance.log'

function Write-WatchdogLog {
    param([string]`$Event, [hashtable]`$Data = @{}, [string]`$Level = 'INFO')
    `$record = [ordered]@{
        timestamp = (Get-Date).ToUniversalTime().ToString('o')
        level     = `$Level
        event     = `$Event
        vm        = `$VMName
    }
    foreach (`$k in `$Data.Keys) { `$record[`$k] = `$Data[`$k] }
    `$line = (`$record | ConvertTo-Json -Compress -Depth 6)
    try { Add-Content -LiteralPath `$LogPath -Value `$line -Encoding UTF8 -ErrorAction Stop } catch {}
    Write-Host "`$Event `$line"
}

try {
    `$state = (Get-VM -Name `$VMName -ErrorAction Stop).State
} catch {
    Write-WatchdogLog -Event 'watchdog_get_vm_failed' -Level 'WARN' -Data @{ error = `$_.Exception.Message }
    exit 1
}
if (`$state -eq 'Running') { exit 0 }

# Is a civm-vhdx-optimize instance running? If so the reclaim owns the VM
# state; do not interfere (it has its own guaranteed Start-VM).
`$reclaimRunning = `$false
try {
    `$task = Get-ScheduledTask -TaskName 'civm-vhdx-optimize' -ErrorAction Stop
    if ((`$task | Get-ScheduledTaskInfo).LastTaskResult -eq 267009) { `$reclaimRunning = `$true }  # 0x41301 = currently running
    if (`$task.State -eq 'Running') { `$reclaimRunning = `$true }
} catch {
    # Fall back to a process scan if the scheduled task is not queryable.
    `$reclaimRunning = `$null -ne (Get-Process -Name powershell -ErrorAction SilentlyContinue |
        Where-Object { `$_.CommandLine -like '*civm-vhdx-optimize.ps1*' })
}

if (`$reclaimRunning) {
    Write-WatchdogLog -Event 'watchdog_skip_reclaim_active' -Data @{ vm_state = "`$state" }
    exit 0
}

Write-WatchdogLog -Event 'watchdog_vm_off_starting' -Level 'WARN' -Data @{ vm_state = "`$state" }
try {
    Start-VM -Name `$VMName -ErrorAction Stop
    Write-WatchdogLog -Event 'watchdog_start_vm_issued'
} catch {
    Write-WatchdogLog -Event 'watchdog_start_vm_failed' -Level 'CRITICAL' -Data @{ error = `$_.Exception.Message }
    exit 1
}
exit 0
"@

if ($PSCmdlet.ShouldProcess($WatchdogScript, "Write watchdog script")) {
    Set-Content -LiteralPath $WatchdogScript -Value $watchdogBody -Encoding UTF8 -Force
    Write-Host "wrote watchdog script: $WatchdogScript"
}

# --- schtasks helpers ---------------------------------------------------------
function Remove-TaskIfPresent {
    param([string]$Name)
    & schtasks.exe /query /tn $Name *> $null
    if ($LASTEXITCODE -eq 0) {
        if ($PSCmdlet.ShouldProcess($Name, 'schtasks /delete')) {
            & schtasks.exe /delete /tn $Name /f | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "schtasks /delete failed for $Name (exit $LASTEXITCODE)" }
            Write-Host "deleted existing task: $Name"
        }
    }
}

function New-Task {
    param(
        [string]$Name,
        [string]$Command,
        [string[]]$ScheduleArgs
    )
    $create = @(
        '/create',
        '/tn', $Name,
        '/tr', $Command,
        '/ru', 'SYSTEM',
        '/rl', 'HIGHEST',
        '/f'
    ) + $ScheduleArgs
    if ($PSCmdlet.ShouldProcess($Name, 'schtasks /create /ru SYSTEM /rl HIGHEST')) {
        & schtasks.exe @create
        if ($LASTEXITCODE -ne 0) {
            throw "schtasks /create failed for $Name (exit $LASTEXITCODE)"
        }
        Write-Host "registered task: $Name"
    }
}

# PowerShell command lines for schtasks /tr. -NonInteractive / -NoProfile keep
# the SYSTEM run deterministic; -ExecutionPolicy Bypass avoids policy blocks.
$resolvedOptimize = (Resolve-Path -LiteralPath $OptimizeScript).Path
# Under -WhatIf the watchdog file is not written, so fall back to its declared
# path when Resolve-Path cannot see it yet.
$resolvedWatchdog = if (Test-Path -LiteralPath $WatchdogScript) {
    (Resolve-Path -LiteralPath $WatchdogScript).Path
} else {
    $WatchdogScript
}
$optimizeCmd = '"{0}" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "{1}" -VMName "{2}" -VhdxPath "{3}" -MinHeadroomGB {4}' -f $PowerShellPath, $resolvedOptimize, $VMName, $VhdxPath, $MinHeadroomGB
$watchdogCmd = '"{0}" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "{1}" -VMName "{2}"' -f $PowerShellPath, $resolvedWatchdog, $VMName

# --- Register civm-vhdx-optimize (weekly + manual-trigger friendly) -----------
Remove-TaskIfPresent -Name $OptimizeTaskName
New-Task -Name $OptimizeTaskName -Command $optimizeCmd -ScheduleArgs @(
    '/sc', 'weekly',
    '/d', $WeeklyDay,
    '/st', $WeeklyTime
)
# Manual trigger remains the primary supervised path:
#   schtasks /run /tn civm-vhdx-optimize

# --- Register civm-vhdx-optimize-watchdog (every 5 minutes) -------------------
Remove-TaskIfPresent -Name $WatchdogTaskName
New-Task -Name $WatchdogTaskName -Command $watchdogCmd -ScheduleArgs @(
    '/sc', 'minute',
    '/mo', '5'
)

Write-Host ''
Write-Host 'Done. Verify with:'
Write-Host "  schtasks /query /tn $OptimizeTaskName /v /fo LIST"
Write-Host "  schtasks /query /tn $WatchdogTaskName /v /fo LIST"
Write-Host "Trigger reclaim manually in a maintenance window with:"
Write-Host "  schtasks /run /tn $OptimizeTaskName"
exit 0
