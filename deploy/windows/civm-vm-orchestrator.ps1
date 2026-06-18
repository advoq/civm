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

    Fail-safe (Kahneman #15): qualquer erro de API/SSH e tratado como "nao
    posso provar que esta ocioso" -> NUNCA desliga a VM por duvida; so liga
    (lado seguro: na duvida, mantem a capacidade de pegar job). O lock de
    estado expira por tempo, nunca trava pra sempre.

.NOTES
    Requer um PAT actions:read por resource owner em
    C:\ProgramData\civm\gh-token-{advoq,emersonbusson}.txt (o host nao tem gh).
    DEVE rodar com o mesmo principal do civm-vhdx-autoreclaim (SYSTEM, que ja faz
    SSH ao guest com sucesso); como elevated-user a ssh key fica ilegivel.
    Ao ATIVAR (sem -WhatIf), DESABILITE o autoreclaim + o optimize-watchdog: o
    orchestrator subsume o stop+compact deles (um dono so da VM, sem curadores em
    conflito disputando o lock/power-state — fail-safe #15).
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$VMName = 'gha-ubuntu-2404',
    [string]$VhdxPath = 'V:\Hyper-V\gha-ubuntu-2404\Virtual Hard Disks\gha-ubuntu-2404.vhdx',
    # Um PAT fine-grained por resource owner (cada um cobre 1 dono). advoq cobre
    # advoq/advoq; emersonbusson cobre os 5 repos pessoais.
    [hashtable]$TokenPaths = @{
        'advoq'         = 'C:\ProgramData\civm\gh-token-advoq.txt'
        'emersonbusson' = 'C:\ProgramData\civm\gh-token-emersonbusson.txt'
    },
    [string]$GuestSshTarget = 'emdev@gha-ubuntu-2404',
    [string]$SshKeyPath = 'C:\ProgramData\civm\ssh\id_ed25519',
    # Os 7 runners da box -> 6 repos em 2 donos. Cada repo e consultado com o
    # token do seu owner (TokenPaths). O stop-guard via SSH (Get-GuestHasActiveJob)
    # continua a salvaguarda final, independente de token.
    [string[]]$Repos = @(
        'advoq/advoq',
        'emersonbusson/advoqwhatsappapi', 'emersonbusson/chatwoot-realtime',
        'emersonbusson/n8n-engine', 'emersonbusson/typebot-runtime',
        'emersonbusson/vitae'
    ),
    [ValidateRange(1, 120)][int]$IdleStopMinutes = 10,
    # Pisos de seguranca de disco (V: livre em GB). warn = limpa cache online
    # (seguro, sem matar job); panic = compacta offline mesmo ocupado (mata job,
    # mas o disco NUNCA enche). Ver Get-OrchestratorDecision.
    [int]$WarnFloorGB = 28,
    [int]$PanicFloorGB = 18,
    [string]$StatePath = 'V:\civm-orchestrator-state.json',
    [string]$LogPath = 'V:\civm-orchestrator.log',
    # Lock canonico de reclaim (SPECv3 DT-v3-3): exclusao mutua com qualquer outro
    # reclaimer do mesmo VHDX. Mesmo path do civm-vhdx-autoreclaim/optimize.
    [string]$ReclaimLockPath = 'V:\civm-reclaim.lock',
    # Modo observe: loga "would_start"/"would_stop" em vez de agir. Valida a
    # logica contra a box real sem mexer na VM — mais limpo que -WhatIf (que
    # suprime ate o Add-Content do log e os New-Alias do modulo Hyper-V).
    [switch]$Observe
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

$script:TokenCache = @{}
function Get-GhTokenForOwner {
    param([string]$Owner)
    if ($script:TokenCache.ContainsKey($Owner)) { return $script:TokenCache[$Owner] }
    $path = $TokenPaths[$Owner]
    if ([string]::IsNullOrWhiteSpace($path) -or -not (Test-Path -LiteralPath $path)) {
        throw "token ausente para owner '$Owner' (esperado em $path)"
    }
    $tok = (Get-Content -LiteralPath $path -Raw).Trim()
    $script:TokenCache[$Owner] = $tok
    return $tok
}

# Conta runs de workflow num estado (queued|in_progress) somando todos os repos
# vigiados. Falha de API NAO e ocioso: relanca para o caller decidir fail-safe.
#
# NAO confiar no total_count do filtro ?status= : o indice do GitHub fica STALE e
# lista runs JA COMPLETED como "queued" (fantasmas — 2 runs de 3 semanas atras
# travaram o scale-to-zero: o filtro os contava, mas "gh run cancel" respondia
# "Cannot cancel a run that is completed"). Buscamos os runs e contamos so os que
# REALMENTE estao no status pedido (run.status bate) E sao recentes
# (< MaxAgeHours) — um job em fila nao espera horas; um in_progress legitimo nao
# passa de algumas horas. Dupla guarda: status real + idade.
function Get-RunCount {
    param([string]$Status, [int]$MaxAgeHours = 12)
    $total = 0
    $cutoff = (Get-Date).ToUniversalTime().AddHours(-$MaxAgeHours)
    foreach ($repo in $Repos) {
        $owner = $repo.Split('/')[0]
        $token = Get-GhTokenForOwner -Owner $owner
        $headers = @{ Authorization = "Bearer $token"; 'User-Agent' = 'civm-orchestrator'; Accept = 'application/vnd.github+json' }
        $uri = "https://api.github.com/repos/$repo/actions/runs?status=$Status&per_page=30"
        $resp = Invoke-RestMethod -Uri $uri -Headers $headers -Method Get -TimeoutSec 20
        foreach ($run in $resp.workflow_runs) {
            if ($run.status -ne $Status) { continue }
            $created = [datetime]::Parse([string]$run.created_at).ToUniversalTime()
            if ($created -lt $cutoff) { continue }
            $total++
        }
    }
    return $total
}

function Get-State {
    $s = $null
    if (Test-Path -LiteralPath $StatePath) {
        try { $s = (Get-Content -LiteralPath $StatePath -Raw | ConvertFrom-Json) } catch { }
    }
    if ($null -eq $s) { $s = [pscustomobject]@{ lastBusyUtc = (Get-Date).ToUniversalTime().ToString('o') } }
    # Garante lastPanicUtc (states antigos nao tem) — o cooldown do panic le daqui.
    if (-not ($s.PSObject.Properties.Name -contains 'lastPanicUtc')) {
        $s | Add-Member -NotePropertyName lastPanicUtc -NotePropertyValue '' -Force
    }
    return $s
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
    # df --output=avail evita o awk e tr -dc 0-9 evita o arg de espaco: ambos os
    # escapes ($4 do awk e o " " do tr) via PowerShell -> SSH -> bash corrompiam o
    # campo, deixando o ssh sair non-zero e gerando um guest_full_clean_warn
    # cosmetico (a limpeza ja rodava; so o log do free_after falhava).
    $remote = 'rm -rf ~/.cache/* 2>/dev/null; sudo docker system prune -af --volumes >/dev/null 2>&1; df -BG --output=avail / | tail -1 | tr -dc 0-9'
    try { $free = (& ssh @sshArgs $remote 2>&1 | Select-Object -Last 1); Write-OrcLog 'guest_full_clean' @{ free_after = "$free" } }
    catch { Write-OrcLog 'guest_full_clean_warn' @{ error = $_.Exception.Message } 'WARN' }
}

# Stop-guard independente do token: pergunta ao proprio guest se ha algum
# Runner.Worker ativo (qualquer repo, qualquer dono). E a salvaguarda real contra
# desligar a VM com um job rodando que o PAT (escopado a 1 dono) nao ve via API.
# Fail-safe: SSH falhou -> assume "ha job" -> nao desliga (Kahneman #15).
function Get-GuestHasActiveJob {
    $sshArgs = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=20', '-o', 'StrictHostKeyChecking=accept-new')
    if (-not [string]::IsNullOrWhiteSpace($SshKeyPath)) { $sshArgs += @('-o', 'IdentitiesOnly=yes', '-i', $SshKeyPath) }
    $sshArgs += $GuestSshTarget
    try {
        $n = (& ssh @sshArgs 'pgrep -c "[R]unner.Worker" 2>/dev/null || echo 0' 2>&1 | Select-Object -Last 1)
        return ([int]$n -gt 0)
    }
    catch { Write-OrcLog 'guest_active_probe_failed' @{ error = $_.Exception.Message } 'WARN'; return $true }
}

# Mede o V: livre em GB. 0 = medida falhou -> a decisao trata como fail-safe (nao
# entra em panic/warn por uma medida ruim — Kahneman #15).
function Get-VFreeGB {
    try { return [int]((Get-PSDrive V -ErrorAction Stop).Free / 1GB) }
    catch { Write-OrcLog 'vfree_probe_failed' @{ error = $_.Exception.Message } 'WARN'; return 0 }
}

# warn_clean: limpeza SEGURA durante CI ativo. Poda APENAS o cache de build do
# docker (regeneravel; nao toca imagens de runs em andamento -> sem o bug de
# eviction que o age-guard consertou) + fstrim (marca os blocos liberados pra a
# VHDX dinamica reusa-los em vez de crescer). Best-effort.
function Invoke-GuestWarnClean {
    $sshArgs = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=20', '-o', 'StrictHostKeyChecking=accept-new')
    if (-not [string]::IsNullOrWhiteSpace($SshKeyPath)) { $sshArgs += @('-o', 'IdentitiesOnly=yes', '-i', $SshKeyPath) }
    $sshArgs += $GuestSshTarget
    $remote = 'sudo docker builder prune -af >/dev/null 2>&1; sudo fstrim / >/dev/null 2>&1; df -BG --output=avail / | tail -1 | tr -dc 0-9'
    try { $free = (& ssh @sshArgs $remote 2>&1 | Select-Object -Last 1); Write-OrcLog 'disk_warn_clean' @{ free_after = "$free" } }
    catch { Write-OrcLog 'disk_warn_clean_warn' @{ error = $_.Exception.Message } 'WARN' }
}

# Desliga a VM e compacta o VHDX offline com o gate de 2 fases provado do #106
# (reusa civm-reclaim-gate.ps1). VM fica Off ate o proximo job. Usado pelo fluxo
# ocioso E pelo panic (disco critico, mesmo ocupado). Tres salvaguardas pra o
# curador nao matar o recurso que cura (a frase-ancora do fail-safe):
#   1. Lock canonico V:\civm-reclaim.lock -> nunca dois Optimize no mesmo VHDX.
#   2. Gate pos-Off: o Optimize-VHD e ININTERRUPTIVEL e consome scratch (~10GB);
#      o VMRS (~8GB) so libera com a VM Off. So compacta se a folga MEDIDA pos-Off
#      cobre o ScratchBudget — senao pula (nao empurra o V: abaixo do piso).
#   3. Recover-detection: se recuperou < MinRecoverGB, loga ERRO (nao finge ok).
function Invoke-StopAndCompact {
    param([string]$Reason)
    $reclaimLock = $null
    try {
        $reclaimLock = [System.IO.FileStream]::new($ReclaimLockPath,
            [System.IO.FileMode]::OpenOrCreate, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)
    }
    catch {
        Write-OrcLog 'reclaim_skip_locked' @{ reason = $Reason; lock = $ReclaimLockPath } 'WARN'
        return
    }
    try {
        Write-OrcLog 'reclaim_start' @{ reason = $Reason }
        Invoke-GuestFullClean
        Stop-VM -Name $VMName -Force -ErrorAction Stop
        $deadline = (Get-Date).AddSeconds(180)
        while ((Get-VM -Name $VMName).State -ne 'Off' -and (Get-Date) -lt $deadline) { Start-Sleep 2 }
        if ((Get-VM -Name $VMName).State -ne 'Off') {
            # VM nao parou no deadline -> NAO monta um VHDX ainda em uso (Mount-VHD
            # falharia). Aborta seguro; jobs ja foram mortos pelo Stop-VM -Force.
            Write-OrcLog 'reclaim_abort_vm_not_off' @{ reason = $Reason } 'ERROR'
            return
        }
        # Gate AUTORITATIVO pos-Off (#106): re-mede a folga real (VMRS liberado).
        $vBeforeGB = Get-VFreeGB
        Write-OrcLog 'reclaim_post_off_remeasure' @{ reason = $Reason; v_free_after_off_gb = $vBeforeGB; scratch_budget_gb = $ReclaimScratchBudgetGB }
        if (-not (Test-OptimizeSlack -VFreeAfterOffGB $vBeforeGB)) {
            # Folga insuficiente: o Optimize poderia estourar o V:. Pula (a VM fica
            # Off; o disco segue apertado mas NAO piora). Alerta humano.
            Write-OrcLog 'reclaim_skip_insufficient_slack' @{ reason = $Reason; v_free_after_off_gb = $vBeforeGB; hard_floor_gb = $ReclaimHardFloorGB; scratch_budget_gb = $ReclaimScratchBudgetGB } 'ERROR'
            return
        }
        Mount-VHD -Path $VhdxPath -ReadOnly -ErrorAction Stop
        try { Optimize-VHD -Path $VhdxPath -Mode Full -ErrorAction Stop }
        finally { Dismount-VHD -Path $VhdxPath -ErrorAction SilentlyContinue }
        $vhd = Get-VHD -Path $VhdxPath
        $vAfterGB = [int]((Get-PSDrive V).Free / 1GB)
        $recoveredGB = $vAfterGB - $vBeforeGB
        Write-OrcLog 'reclaim_done' @{ reason = $Reason; vhdx_gb = [int]($vhd.FileSize / 1GB); v_free_gb = $vAfterGB; recovered_gb = $recoveredGB }
        if ($recoveredGB -lt $MinRecoverGB) {
            # O Optimize "passou" mas nao recuperou nada util -> disco apertado que
            # o compact nao resolve (precisa de humano), nao um falso-verde.
            Write-OrcLog 'reclaim_no_progress' @{ reason = $Reason; recovered_gb = $recoveredGB; min_recover_gb = $MinRecoverGB } 'ERROR'
        }
    }
    finally {
        $reclaimLock.Close(); $reclaimLock.Dispose()
        Remove-Item -LiteralPath $ReclaimLockPath -Force -ErrorAction SilentlyContinue
    }
}

# Carrega a decisao pura + as primitivas de reclaim (gate de 2 fases, cooldown)
# — os MESMOS modulos que os testes exercitam (Kahneman #13: codigo deployado ==
# codigo testado).
. "$PSScriptRoot\civm-orchestrator-decision.ps1"
. "$PSScriptRoot\civm-reclaim-gate.ps1"

# ---- decisao principal ----
try {
    $vm = Get-VM -Name $VMName -ErrorAction Stop
    $queued = Get-RunCount -Status 'queued'
    $running = Get-RunCount -Status 'in_progress'
    $state = Get-State

    $last = [datetime]::Parse($state.lastBusyUtc).ToUniversalTime()
    $idleMin = ((Get-Date).ToUniversalTime() - $last).TotalMinutes
    $vfree = Get-VFreeGB
    $nowUtc = (Get-Date).ToUniversalTime().ToString('o')
    # Cooldown do panic: fora da janela -> pode panicar; dentro -> a decisao
    # rebaixa para warn_clean (nao re-mata jobs em loop). Medida de tempo VIVA.
    $canPanic = Test-ReclaimCooldown -LastReclaimUtc $state.lastPanicUtc -NowUtc $nowUtc
    Write-OrcLog 'tick' @{ vm = "$($vm.State)"; queued = $queued; running = $running; idle_min = [math]::Round($idleMin, 1); v_free_gb = $vfree; can_panic = $canPanic }

    # Decide no modulo puro testado (civm-orchestrator-decision.test.ps1); o
    # switch abaixo so EXECUTA a acao. A probe SSH e lazy: Get-OrchestratorDecision
    # so a chama no gate de stop. VFreeGB + CanPanic armam a seguranca de disco.
    $decision = Get-OrchestratorDecision -VmState "$($vm.State)" -Queued $queued -Running $running -IdleMinutes $idleMin -IdleStopMinutes $IdleStopMinutes -HasActiveJobProbe { Get-GuestHasActiveJob } -VFreeGB $vfree -WarnFloorGB $WarnFloorGB -PanicFloorGB $PanicFloorGB -CanPanic $canPanic

    switch ($decision) {
        'noop_off' { }
        'start' {
            if ($Observe) { Write-OrcLog 'would_start' @{ queued = $queued; running = $running } }
            else {
                Start-VM -Name $VMName -ErrorAction Stop
                Write-OrcLog 'vm_started' @{ queued = $queued; running = $running }
            }
        }
        'mark_busy' { $state.lastBusyUtc = $nowUtc; Save-State $state }
        'idle_debounce' { Write-OrcLog 'idle_debounce' @{ idle_min = [math]::Round($idleMin, 1); need = $IdleStopMinutes } }
        'stop_aborted_active_job' {
            Write-OrcLog 'stop_aborted_active_job' @{ note = 'Runner.Worker ativo no guest (repo fora do escopo do token?)' }
            $state.lastBusyUtc = $nowUtc; Save-State $state
        }
        'warn_clean' {
            # Disco apertado (V < WarnFloor) mas ainda nao critico: limpeza SEGURA
            # online (cache de build + fstrim), SEM desligar, SEM matar job.
            if ($Observe) { Write-OrcLog 'would_warn_clean' @{ v_free_gb = $vfree; floor = $WarnFloorGB } }
            else { Write-OrcLog 'disk_warn' @{ v_free_gb = $vfree; floor = $WarnFloorGB }; Invoke-GuestWarnClean }
        }
        'panic_compact' {
            # Disco CRITICO (V < PanicFloor): compacta MESMO ocupado. Mata os jobs
            # ativos (re-rodam), mas o disco encher e infinitamente pior (satura o
            # host, derruba ate o interop). A VM volta sozinha pela fila no proximo
            # tick (cold start).
            if ($Observe) { Write-OrcLog 'would_panic_compact' @{ v_free_gb = $vfree; floor = $PanicFloorGB } }
            else {
                Write-OrcLog 'disk_panic' @{ v_free_gb = $vfree; floor = $PanicFloorGB; note = 'disco critico -> compacta mesmo com job ativo' }
                # Marca o panic ANTES do compact: o cooldown conta do disparo, e se
                # o compact pendurar, o proximo tick nao re-mata jobs.
                $state.lastPanicUtc = $nowUtc; Save-State $state
                Invoke-StopAndCompact -Reason 'panic_disk'
            }
        }
        'stop_and_compact' {
            # Fronteira de PR (ocioso): desliga e compacta. VM fica Off ate o
            # proximo job (cold start).
            if ($Observe) { Write-OrcLog 'would_stop_and_compact' @{ idle_min = [math]::Round($idleMin, 1) } }
            else { Invoke-StopAndCompact -Reason 'idle_pr_boundary' }
        }
    }
}
catch {
    # Fail-safe: na duvida NUNCA desliga (so o caminho de Start e seguro). Um
    # erro aqui significa "nao consegui provar ocioso" -> deixa a VM como esta.
    Write-OrcLog 'orchestrator_error' @{ error = $_.Exception.Message } 'ERROR'
    exit 1
}
