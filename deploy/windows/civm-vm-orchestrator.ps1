<#
.SYNOPSIS
    Scale-to-zero orchestrator: roda a VM do runner SOB DEMANDA.

.DESCRIPTION
    Tarefa minuscula sempre-ligada no host Windows (Scheduled Task, ~1min). A VM
    pesada so liga quando ha trabalho. Decisao a cada tick:

      - VM Off + existe workflow run em fila (queued) num dos repos vigiados
        -> Start-VM (os runners sobem no boot e pegam os jobs).
      - VM Running + NENHUM run in_progress + NENHUM queued, ocioso ha
        >= IdleStopMinutes -> limpeza total do guest (caches + imagens de runs
        finalizadas), Stop-VM, Optimize-VHD (compacta). A VM fica Off.

    Ganhos: com a VM Off ociosa, o Hyper-V devolve TODA a RAM ao Windows e o
    VHDX para de crescer + fica compactado; footprint zero entre rajadas. O
    custo e um cold-start de ~1-2min na primeira rajada (boot + runners
    conectando) — aceitavel para CI.

    Fail-safe (Kahneman #16): qualquer erro de API/SSH e tratado como "nao
    posso provar que esta ocioso" -> NUNCA desliga a VM por duvida; so liga
    (lado seguro: na duvida, mantem a capacidade de pegar job). O lock de
    estado expira por tempo, nunca trava pra sempre.

.NOTES
    Requer um PAT com actions:read em C:\ProgramData\civm\gh-token.txt (o host
    nao tem gh). A lista de repos vem de Repos (derivada dos runners da box).
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$VMName = 'gha-ubuntu-2404',
    [string]$VhdxPath = 'V:\Hyper-V\gha-ubuntu-2404\Virtual Hard Disks\gha-ubuntu-2404.vhdx',
    [string]$TokenPath = 'C:\ProgramData\civm\gh-token.txt',
    [string]$GuestSshTarget = 'emdev@gha-ubuntu-2404',
    [string]$SshKeyPath = 'C:\ProgramData\civm\ssh\id_ed25519',
    [string[]]$Repos = @(
        'advoq/advoq', 'advoq/advoq-org', 'advoq/advoqwhatsappapi',
        'advoq/chatwoot-realtime', 'advoq/n8n-engine',
        'advoq/typebot-runtime', 'advoq/vitae'
    ),
    [ValidateRange(1, 120)][int]$IdleStopMinutes = 10,
    [string]$StatePath = 'V:\civm-orchestrator-state.json',
    [string]$LogPath = 'V:\civm-orchestrator.log'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Write-OrcLog {
    param([string]$Event, [hashtable]$Data = @{}, [string]$Level = 'INFO')
    $rec = [ordered]@{ ts = (Get-Date).ToUniversalTime().ToString('o'); level = $Level; event = $Event }
    foreach ($k in $Data.Keys) { $rec[$k] = $Data[$k] }
    $line = ($rec | ConvertTo-Json -Compress -Depth 5)
    try { Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8 } catch { }
    Write-Host $line
}

function Get-GhToken {
    if (-not (Test-Path -LiteralPath $TokenPath)) { throw "token ausente em $TokenPath" }
    return (Get-Content -LiteralPath $TokenPath -Raw).Trim()
}

# Conta runs de workflow num estado (queued|in_progress) somando todos os repos
# vigiados. Falha de API NAO e ocioso: relanca para o caller decidir fail-safe.
function Get-RunCount {
    param([string]$Token, [string]$Status)
    $total = 0
    $headers = @{ Authorization = "Bearer $Token"; 'User-Agent' = 'civm-orchestrator'; Accept = 'application/vnd.github+json' }
    foreach ($repo in $Repos) {
        $uri = "https://api.github.com/repos/$repo/actions/runs?status=$Status&per_page=1"
        $resp = Invoke-RestMethod -Uri $uri -Headers $headers -Method Get -TimeoutSec 20
        $total += [int]$resp.total_count
    }
    return $total
}

function Get-State {
    if (Test-Path -LiteralPath $StatePath) {
        try { return (Get-Content -LiteralPath $StatePath -Raw | ConvertFrom-Json) } catch { }
    }
    return [pscustomobject]@{ lastBusyUtc = (Get-Date).ToUniversalTime().ToString('o') }
}

function Save-State {
    param($State)
    try { ($State | ConvertTo-Json -Compress) | Set-Content -LiteralPath $StatePath -Encoding UTF8 } catch { }
}

# Limpeza total do guest antes de desligar: zera os caches dos 7 repos e as
# imagens de service de runs finalizadas, devolvendo a VM ao estado limpo
# (~51GB livres) para o proximo PR. Best-effort: falha de SSH nao bloqueia o
# stop (o disco ja sera compactado offline de qualquer forma).
function Invoke-GuestFullClean {
    $sshArgs = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=20', '-o', 'StrictHostKeyChecking=accept-new')
    if (-not [string]::IsNullOrWhiteSpace($SshKeyPath)) { $sshArgs += @('-o', 'IdentitiesOnly=yes', '-i', $SshKeyPath) }
    $sshArgs += $GuestSshTarget
    $remote = 'rm -rf ~/.cache/* 2>/dev/null; sudo docker system prune -af --volumes >/dev/null 2>&1; df -BG / | awk "NR==2{print \$4}"'
    try { $free = (& ssh @sshArgs $remote 2>&1 | Select-Object -Last 1); Write-OrcLog 'guest_full_clean' @{ free_after = "$free" } }
    catch { Write-OrcLog 'guest_full_clean_warn' @{ error = $_.Exception.Message } 'WARN' }
}

# ---- decisao principal ----
try {
    $token = Get-GhToken
    $vm = Get-VM -Name $VMName -ErrorAction Stop
    $queued = Get-RunCount -Token $token -Status 'queued'
    $running = Get-RunCount -Token $token -Status 'in_progress'
    $state = Get-State

    Write-OrcLog 'tick' @{ vm = "$($vm.State)"; queued = $queued; running = $running }

    if ($vm.State -eq 'Off') {
        if ($queued -gt 0) {
            if ($PSCmdlet.ShouldProcess($VMName, 'Start-VM (job na fila)')) {
                Start-VM -Name $VMName -ErrorAction Stop
                Write-OrcLog 'vm_started' @{ queued = $queued }
            }
        }
        return
    }

    # VM Running: se ha trabalho, marca busy e segue.
    if ($queued -gt 0 -or $running -gt 0) {
        $state.lastBusyUtc = (Get-Date).ToUniversalTime().ToString('o')
        Save-State $state
        return
    }

    # Ocioso: so desliga depois de IdleStopMinutes sem trabalho (debounce).
    $last = [datetime]::Parse($state.lastBusyUtc).ToUniversalTime()
    $idleMin = ((Get-Date).ToUniversalTime() - $last).TotalMinutes
    if ($idleMin -lt $IdleStopMinutes) {
        Write-OrcLog 'idle_debounce' @{ idle_min = [math]::Round($idleMin, 1); need = $IdleStopMinutes }
        return
    }

    # Fronteira de PR: limpa o guest, desliga, compacta. VM fica Off ate o
    # proximo job na fila (cold start).
    Write-OrcLog 'pr_boundary_reclaim_start' @{ idle_min = [math]::Round($idleMin, 1) }
    Invoke-GuestFullClean
    if ($PSCmdlet.ShouldProcess($VMName, 'Stop-VM + Optimize-VHD')) {
        Stop-VM -Name $VMName -Force -ErrorAction Stop
        $deadline = (Get-Date).AddSeconds(180)
        while ((Get-VM -Name $VMName).State -ne 'Off' -and (Get-Date) -lt $deadline) { Start-Sleep 2 }
        # Optimize-VHD exige o VHDX MONTADO read-only — so detached nao basta (sem
        # o Mount o Optimize vira no-op e o VHDX NAO encolhe; confirmado
        # 2026-06-17). O autoreclaim ja faz assim e compacta de verdade. Dismount
        # no finally, sempre.
        Mount-VHD -Path $VhdxPath -ReadOnly -ErrorAction Stop
        try { Optimize-VHD -Path $VhdxPath -Mode Full -ErrorAction Stop }
        finally { Dismount-VHD -Path $VhdxPath -ErrorAction SilentlyContinue }
        $vhd = Get-VHD -Path $VhdxPath
        Write-OrcLog 'pr_boundary_reclaim_done' @{
            vhdx_gb   = [int]($vhd.FileSize / 1GB)
            v_free_gb = [int]((Get-PSDrive V).Free / 1GB)
        }
    }
}
catch {
    # Fail-safe: na duvida NUNCA desliga (so o caminho de Start e seguro). Um
    # erro aqui significa "nao consegui provar ocioso" -> deixa a VM como esta.
    Write-OrcLog 'orchestrator_error' @{ error = $_.Exception.Message } 'ERROR'
    exit 1
}
