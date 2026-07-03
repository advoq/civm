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
        'advoq/advoq', 'advoq/civm',
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
    # Estado da fila FIFO por-PR (Phase 1b, observe-mode): contextos em ordem de chegada
    # + o slot simulado. Por enquanto so LOGA (would_grant/would_advance), nao impoe.
    [string]$PrQueuePath = 'V:\civm-pr-queue.json',
    # Caminho HOST do contexto concedido. O gate job (runner Windows do HOST, label
    # civm-gate) le isto e segura os jobs reais Linux ate ser a vez do PR. Fica no HOST
    # de proposito: sobrevive ao Stop-VM do guest no compact de boundary (um gate dentro
    # do guest seria cancelado pelo compact). So e escrito com -EnforceQueue.
    [string]$CurrentContextPath = 'V:\civm-current-context',
    # Liga o ENFORCE da fila por-PR: publica o currentPr no host + limpa+compacta no
    # boundary do contexto. Default OFF (so observe). Ligar SO depois do canario provar
    # o gate (gate-no-host) num PR throwaway — nunca direto nos 7 workflows.
    [switch]$EnforceQueue,
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
            # Idade pela ATIVIDADE recente (updated_at), nao created_at: um re-run reusa o
            # created_at original (pode ser >12h) e seria descartado, cegando o orchestrator
            # p/ re-runs (queued e in_progress). updated_at e fresco no re-run; fallback p/
            # created_at se ausente. Mantem a guarda de staleness (run parado >12h = filtrado).
            $tsRaw = if ($run.updated_at) { $run.updated_at } else { $run.created_at }
            $ts = [datetime]::Parse([string]$tsRaw).ToUniversalTime()
            if ($ts -lt $cutoff) { continue }
            $total++
        }
    }
    return $total
}

# Get-PrActivity: agrupa os runs de box ATIVOS (queued+in_progress) do advoq/advoq por
# CONTEXTO — um PR (pr-<num>) ou um push de branch (branch-<ref>). Retorna um hashtable
# id -> contagem de runs ativos. E o que a fila FIFO agrupa (todos os checks de um
# contexto antes do proximo). Falha de API -> pula o status (fail-safe; o tick observe so
# loga). per_page=100 + os 2 status; idade pela atividade (updated_at), igual Get-RunCount.
function Get-PrActivity {
    param([int]$MaxAgeHours = 12)
    $cutoff = (Get-Date).ToUniversalTime().AddHours(-$MaxAgeHours)
    $counts = @{}
    $token = Get-GhTokenForOwner -Owner 'advoq'
    $headers = @{ Authorization = "Bearer $token"; 'User-Agent' = 'civm-orchestrator'; Accept = 'application/vnd.github+json' }
    foreach ($status in @('queued', 'in_progress')) {
        $uri = "https://api.github.com/repos/advoq/advoq/actions/runs?status=$status&per_page=100"
        try { $resp = Invoke-RestMethod -Uri $uri -Headers $headers -Method Get -TimeoutSec 20 } catch { continue }
        foreach ($run in $resp.workflow_runs) {
            if ($run.status -ne $status) { continue }
            $tsRaw = if ($run.updated_at) { $run.updated_at } else { $run.created_at }
            if ([datetime]::Parse([string]$tsRaw).ToUniversalTime() -lt $cutoff) { continue }
            $ctx = if ($run.pull_requests -and @($run.pull_requests).Count -gt 0) { 'pr-' + [int]$run.pull_requests[0].number } else { 'branch-' + [string]$run.head_branch }
            if (-not $counts.ContainsKey($ctx)) { $counts[$ctx] = 0 }
            $counts[$ctx]++
        }
    }
    return $counts
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
    # Garante admitReclaimAttempts — a barreira de admissao (host 55 / guest 40) conta
    # compacts que nao chegaram no floor pra evitar deadlock da fila (>=2 admite mesmo assim).
    if (-not ($s.PSObject.Properties.Name -contains 'admitReclaimAttempts')) {
        $s | Add-Member -NotePropertyName admitReclaimAttempts -NotePropertyValue 0 -Force
    }
    # Garante prevRunning — o gate por-evento (transicao running >0->0) le daqui.
    if (-not ($s.PSObject.Properties.Name -contains 'prevRunning')) {
        $s | Add-Member -NotePropertyName prevRunning -NotePropertyValue 0 -Force
    }
    return $s
}

function Save-State {
    param($State)
    try { ($State | ConvertTo-Json -Compress) | Set-Content -LiteralPath $StatePath -Encoding UTF8 } catch { }
}

# Monta os args de SSH (batch, timeout, chave) para um alvo. Centralizado: as 3
# funcoes que falam com o guest reusam a mesma config em vez de duplicar.
function Get-GuestSshArgs {
    param([Parameter(Mandatory)][string]$Target)
    $a = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=20', '-o', 'StrictHostKeyChecking=accept-new')
    if (-not [string]::IsNullOrWhiteSpace($SshKeyPath)) { $a += @('-o', 'IdentitiesOnly=yes', '-i', $SshKeyPath) }
    $a += $Target
    return $a
}

# Descobre o IPv4 do guest direto do Hyper-V (sem DNS). Usado como fallback
# quando o NOME gha-ubuntu-2404 nao resolve no boot. Exige integration services
# reportando IP (poucos segundos pos-Start-VM). Falha -> $null (o caller so usa
# se nao for nulo).
function Get-GuestIPAddress {
    try {
        $ips = (Get-VMNetworkAdapter -VMName $VMName -ErrorAction Stop).IPAddresses
        return ($ips | Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' } | Select-Object -First 1)
    }
    catch { return $null }
}

# SSH ao guest com retry/backoff. Pos-reboot (ex.: queda de energia) o nome
# gha-ubuntu-2404 demora a resolver pelo switch Hyper-V -> "Could not resolve
# hostname"/"Connection refused" transitorios faziam o clean+fstrim e o
# stop-guard pularem (a limpeza nao rodava -> o Optimize nao recuperava nada).
# Tenta ate $Retries vezes; se o NOME nao resolve, acrescenta o IP da VM como
# alvo e tenta por IP (remove a dependencia de DNS). $ErrorActionPreference local
# = Continue para o stderr do ssh nao virar throw -> decidimos sucesso pelo
# $LASTEXITCODE. Retorna a ultima linha do stdout; $script:LastGuestSshOk diz se
# algum alvo respondeu com exit 0.
function Invoke-GuestSsh {
    param(
        [Parameter(Mandatory)][string]$Command,
        [int]$Retries = 3,
        [int]$BackoffSeconds = 5
    )
    $ErrorActionPreference = 'Continue'
    $script:LastGuestSshOk = $false
    $user = ($GuestSshTarget -split '@')[0]
    $targets = [System.Collections.Generic.List[string]]::new()
    $targets.Add($GuestSshTarget)
    $lastLine = $null
    for ($attempt = 1; $attempt -le $Retries; $attempt++) {
        for ($i = 0; $i -lt $targets.Count; $i++) {
            $out = (& ssh @(Get-GuestSshArgs $targets[$i]) $Command 2>&1)
            if ($LASTEXITCODE -eq 0) { $script:LastGuestSshOk = $true; return ($out | Select-Object -Last 1) }
            $lastLine = ($out | Select-Object -Last 1)
            if (($out | Out-String) -match 'resolve hostname' -and $targets.Count -eq 1) {
                $ip = Get-GuestIPAddress
                if ($ip) { $targets.Add("$user@$ip") }
            }
        }
        if ($attempt -lt $Retries) { Start-Sleep -Seconds $BackoffSeconds }
    }
    return $lastLine
}

# Limpeza total do guest antes de desligar: zera os caches dos 7 repos e as
# imagens de service de runs finalizadas, devolvendo a VM ao estado limpo
# (~51GB livres) para o proximo PR. Best-effort: falha de SSH nao bloqueia o
# stop (o disco ja sera compactado offline de qualquer forma).
function Invoke-GuestFullClean {
    # Deep clean (#137): alem dos caches dos 7 repos, remove o que so crescia e
    # nunca era limpo — _diag (logs do runner), o conteudo de _work exceto _tool
    # (hosted node/go cache, caro de re-baixar), journal e /tmp. Sem isso o piso
    # "limpo" caia de ~51 pra ~47 ao longo das runs, e a E2E (builda ~35GB de
    # imagens num job) batia no panic floor. df --output=avail evita o awk e o
    # tr -dc 0-9 evita o arg de espaco: os escapes via PowerShell -> SSH -> bash
    # corrompiam o campo, deixando o ssh sair non-zero.
    # fstrim -av no FIM (pos-prune, ainda com a VM Running, antes do Stop-VM): o
    # rm/docker-prune libera dezenas de GB mas, sem o UNMAP/TRIM, o VHDX dinamico
    # nao ve esses blocos como livres -> o Optimize-VHD offline recuperava ~0
    # (reclaim_no_progress) e o piso "limpo" caia abaixo de 58. O trim marca os
    # blocos pra o Optimize compactar de verdade (mesma razao do warn_clean).
    $remote = 'rm -rf ~/.cache/* 2>/dev/null; rm -rf ~/actions-runner*/_diag/* 2>/dev/null; for w in ~/actions-runner*/_work; do find "$w" -maxdepth 1 -mindepth 1 ! -name _tool -exec rm -rf {} + 2>/dev/null; done; sudo journalctl --vacuum-size=50M >/dev/null 2>&1; sudo rm -rf /tmp/* /var/tmp/* 2>/dev/null; sudo docker system prune -af --volumes >/dev/null 2>&1; sudo docker builder prune -af >/dev/null 2>&1; sudo fstrim -av >/dev/null 2>&1; df -BG --output=avail / | tail -1 | tr -dc 0-9'
    $free = Invoke-GuestSsh -Command $remote
    if ($script:LastGuestSshOk) { Write-OrcLog 'guest_full_clean' @{ free_after = "$free" } }
    else { Write-OrcLog 'guest_full_clean_warn' @{ error = "$free" } 'WARN' }
}

# Stop-guard independente do token: pergunta ao proprio guest se ha algum
# Runner.Worker ativo (qualquer repo, qualquer dono). E a salvaguarda real contra
# desligar a VM com um job rodando que o PAT (escopado a 1 dono) nao ve via API.
# Fail-safe: SSH falhou (mesmo apos retries) -> assume "ha job" -> nao desliga
# (Kahneman #15).
function Get-GuestHasActiveJob {
    $n = Invoke-GuestSsh -Command 'pgrep -c "[R]unner.Worker" 2>/dev/null || echo 0'
    if (-not $script:LastGuestSshOk) { Write-OrcLog 'guest_active_probe_failed' @{ error = "$n" } 'WARN'; return $true }
    return ([int]$n -gt 0)
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
    $remote = 'sudo docker builder prune -af >/dev/null 2>&1; sudo fstrim / >/dev/null 2>&1; df -BG --output=avail / | tail -1 | tr -dc 0-9'
    $free = Invoke-GuestSsh -Command $remote
    if ($script:LastGuestSshOk) { Write-OrcLog 'disk_warn_clean' @{ free_after = "$free" } }
    else { Write-OrcLog 'disk_warn_clean_warn' @{ error = "$free" } 'WARN' }
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
        # VM Running -> limpa o guest (full clean via SSH) e desliga antes do
        # Optimize. Ja Off (caso reclaim_before_admit, a barreira de 51GB) -> pula
        # direto pro compact: o guest ja foi limpo pelo hook job-completed, e nao
        # da pra SSH num guest desligado.
        if ((Get-VM -Name $VMName).State -ne 'Off') {
            Invoke-GuestFullClean
            Stop-VM -Name $VMName -Force -ErrorAction Stop
            $deadline = (Get-Date).AddSeconds(180)
            while ((Get-VM -Name $VMName).State -ne 'Off' -and (Get-Date) -lt $deadline) { Start-Sleep 2 }
            if ((Get-VM -Name $VMName).State -ne 'Off') {
                # VM nao parou no deadline -> NAO monta um VHDX ainda em uso
                # (Mount-VHD falharia). Aborta seguro; jobs ja foram mortos.
                Write-OrcLog 'reclaim_abort_vm_not_off' @{ reason = $Reason } 'ERROR'
                return
            }
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
        if (Test-ReclaimStuck -RecoveredGB $recoveredGB -VFreeAfterGB $vAfterGB -AdmitFloorGB $AdmitFloorGB) {
            # Recuperou < min E o V: SEGUE abaixo do piso -> disco apertado que o
            # compact nao resolve (precisa de humano), nao um falso-verde.
            Write-OrcLog 'reclaim_no_progress' @{ reason = $Reason; recovered_gb = $recoveredGB; v_free_gb = $vAfterGB; min_recover_gb = $MinRecoverGB; floor = $AdmitFloorGB } 'ERROR'
        }
        elseif ($recoveredGB -lt $MinRecoverGB) {
            # Recuperou pouco MAS o V: ja esta >= piso: o VHDX ja esta compacto
            # (footprint do guest estavel), nao ha o que devolver. Steady-state
            # saudavel — INFO, nao ERROR (evita o falso-vermelho perpetuo).
            Write-OrcLog 'reclaim_already_compact' @{ reason = $Reason; recovered_gb = $recoveredGB; v_free_gb = $vAfterGB; floor = $AdmitFloorGB } 'INFO'
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
. "$PSScriptRoot\civm-pr-queue.ps1"

# ---- decisao principal ----
try {
    $vm = Get-VM -Name $VMName -ErrorAction Stop
    $queued = Get-RunCount -Status 'queued'
    $running = Get-RunCount -Status 'in_progress'
    $state = Get-State

    $last = [datetime]::Parse($state.lastBusyUtc).ToUniversalTime()
    $idleMin = ((Get-Date).ToUniversalTime() - $last).TotalMinutes
    $vfree = Get-VFreeGB
    # Barreira de admissao (backstop): host V: >= 55GB (alcancavel; o compact chega a
    # ~67 sob CI) E guest >= 40GB (o guest so alcanca ~45-63, nunca 70 -> floor proprio
    # menor). O compact entre PRs e INCONDICIONAL (boundary_compact a cada PR; ver
    # decision) e libera o MAXIMO dos 2 lados; 55 e o piso alcancavel pra admitir logo
    # apos compactar (mirar 70 spiralava com reclaim_no_progress falso). O guest free vem
    # do snapshot de host-metrics; 999 = ausente/ilegivel -> desconhecido, nao bloqueia.
    $AdmitFloorGB = 55
    $GuestFloorGB = 40
    $guestFree = 999
    try {
        $snap = Get-Content -LiteralPath 'V:\civm-host-metrics.json' -Raw -ErrorAction Stop | ConvertFrom-Json
        if ($null -ne $snap.guest_free_gb -and [int]$snap.guest_free_gb -gt 0) { $guestFree = [int]$snap.guest_free_gb }
    } catch { $guestFree = 999 }
    $nowUtc = (Get-Date).ToUniversalTime().ToString('o')
    # Cooldown do panic: fora da janela -> pode panicar; dentro -> a decisao
    # rebaixa para warn_clean (nao re-mata jobs em loop). Medida de tempo VIVA.
    $canPanic = Test-ReclaimCooldown -LastReclaimUtc $state.lastPanicUtc -NowUtc $nowUtc
    # Gate por-EVENTO: prevRunning (running do tick anterior) detecta a transicao >0->0
    # (PR/onda de runs acabou) -> compacta 1x por PR; sem timer.
    $prevRunning = [int]$state.prevRunning
    Write-OrcLog 'tick' @{ vm = "$($vm.State)"; queued = $queued; running = $running; idle_min = [math]::Round($idleMin, 1); v_free_gb = $vfree; can_panic = $canPanic; prev_running = $prevRunning }

    # Decide no modulo puro testado (civm-orchestrator-decision.test.ps1); o
    # switch abaixo so EXECUTA a acao. A probe SSH e lazy: Get-OrchestratorDecision
    # so a chama no gate de stop. VFreeGB + CanPanic armam a seguranca de disco.
    $decision = Get-OrchestratorDecision -VmState "$($vm.State)" -Queued $queued -Running $running -IdleMinutes $idleMin -IdleStopMinutes $IdleStopMinutes -HasActiveJobProbe { Get-GuestHasActiveJob } -VFreeGB $vfree -WarnFloorGB $WarnFloorGB -PanicFloorGB $PanicFloorGB -CanPanic $canPanic -AdmitFloorGB $AdmitFloorGB -GuestFloorGB $GuestFloorGB -GuestFreeGB $guestFree -AdmitReclaimAttempts ([int]$state.admitReclaimAttempts) -PrevRunning $prevRunning

    switch ($decision) {
        'noop_off' { }
        'start' {
            # Admite: se ainda <55 (so ocorre com attempts>=2 -> anti-deadlock), emite o
            # evento rastreavel (rollback/abort dependem dele). O reset do contador e do
            # Resolve-AdmitTransition pos-switch (SPECv4 ITEM-2).
            if ($vfree -gt 0 -and $vfree -lt $AdmitFloorGB) {
                $evt = if ($Observe) { 'would_disk_below_floor_admitted' } else { 'disk_below_floor_admitted' }
                Write-OrcLog $evt @{ v_free_gb = $vfree; guest_free_gb = $guestFree; floor = $AdmitFloorGB; attempts = [int]$state.admitReclaimAttempts; path = 'cold' }
            }
            if ($Observe) { Write-OrcLog 'would_start' @{ queued = $queued; running = $running } }
            else {
                Start-VM -Name $VMName -ErrorAction Stop
                Write-OrcLog 'vm_started' @{ queued = $queued; running = $running }
            }
        }
        'reclaim_before_admit' {
            # BARREIRA DE ADMISSAO: VM Off + fila, mas disco < floor (host 55 ou
            # guest 40). Compacta ANTES de admitir (nao starta sujo, evita o caso
            # #1182 a V:18). Conta a tentativa; se o compact maxar sem chegar no
            # floor, a 2a tentativa admite mesmo assim (anti-deadlock, modulo puro).
            if ($Observe) { Write-OrcLog 'would_reclaim_before_admit' @{ v_free_gb = $vfree; guest_free_gb = $guestFree; floor = $AdmitFloorGB; attempts = [int]$state.admitReclaimAttempts } }
            else {
                Write-OrcLog 'reclaim_before_admit' @{ v_free_gb = $vfree; guest_free_gb = $guestFree; floor = $AdmitFloorGB; attempts = [int]$state.admitReclaimAttempts }
                Invoke-StopAndCompact -Reason 'admit_barrier'
                # O incremento do contador (se vAfter ainda <55) e do Resolve-AdmitTransition
                # pos-switch (SPECv4 ITEM-2; era inline aqui).
            }
        }
        'mark_busy' {
            # Admissao warm (Running==0 + Queued>0), disco >= AdmitFloorGB ou nao medido
            # (vfree<=0, fail-safe): mantem a VM up, sem reclaim (nada pra reclamar). O
            # sub-caso "admite sujo" (vfree>0 e <floor) agora e o proprio decision
            # 'reclaim_online_before_admit' abaixo -- este branch nunca mais o ve
            # (civm#154; o reset do contador continua no Resolve-AdmitTransition pos-switch).
            if (-not $Observe) { $state.lastBusyUtc = $nowUtc; Save-State $state }
        }
        'reclaim_online_before_admit' {
            # civm#154: fila quente (Running==0 + Queued>0) com disco < AdmitFloorGB (55).
            # NUNCA para a VM (evita reintroduzir o thrash de boundary_compact removido em
            # 2026-06-25) -- so tenta a MESMA limpeza online ja provada segura no warn_clean
            # (fstrim + docker builder prune via SSH). Best-effort: sem contador
            # anti-deadlock, o proximo tick tenta de novo se nao foi suficiente. O evento
            # disk_below_floor_admitted preserva o nome/shape ja usado por dashboards
            # existentes; disk_warm_reclaim_online e o evento NOVO desta tentativa.
            $evt = if ($Observe) { 'would_disk_below_floor_admitted' } else { 'disk_below_floor_admitted' }
            Write-OrcLog $evt @{ v_free_gb = $vfree; guest_free_gb = $guestFree; floor = $AdmitFloorGB; attempts = [int]$state.admitReclaimAttempts; path = 'warm' }
            if ($Observe) { Write-OrcLog 'would_warm_reclaim_online' @{ v_free_gb = $vfree; floor = $AdmitFloorGB } }
            else { Write-OrcLog 'disk_warm_reclaim_online' @{ v_free_gb = $vfree; floor = $AdmitFloorGB }; Invoke-GuestWarnClean }
            if (-not $Observe) { $state.lastBusyUtc = $nowUtc; Save-State $state }
        }
        'idle_debounce' { Write-OrcLog 'idle_debounce' @{ idle_min = [math]::Round($idleMin, 1); need = $IdleStopMinutes } }
        'stop_aborted_active_job' {
            Write-OrcLog 'stop_aborted_active_job' @{ note = 'Runner.Worker ativo no guest (repo fora do escopo do token?)' }
            # -Observe e nao-mutante: nao reseta o idle timer (senao um dry-run
            # adia o stop_and_compact real em ate IdleStopMinutes).
            if (-not $Observe) { $state.lastBusyUtc = $nowUtc; Save-State $state }
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
    # TRANSICAO DO CONTADOR DA BARREIRA (uma chamada pos-switch; vAfter medido APOS o
    # efeito). A funcao pura Resolve-AdmitTransition decide quem conta/reseta (incl. DT-9:
    # panic so conta com Running==0); o teste exercita a MESMA funcao (Kahneman #13). Para
    # decisoes sem compact/admissao e no-op no contador. (SPECv4 ITEM-2)
    if (-not $Observe) {
        $vAfter = Get-VFreeGB
        $state = Resolve-AdmitTransition -State $state -Decision $decision -Running $running -Queued $queued -VAfter $vAfter -Floor $AdmitFloorGB
        # Rastreia o running deste tick p/ a transicao >0->0 do proximo (gate por-evento).
        $state.prevRunning = $running
        Save-State $state
    }

    # ---- OBSERVE da fila FIFO por-PR (Phase 1b: so LOGA, nunca impoe) ----
    # Agrupa a atividade de box por contexto (PR/branch), mantem a ordem FIFO em
    # $PrQueuePath, e loga o que a fila FARIA (grant/advance) sem mexer no power/compact.
    # Valida o cerebro (Resolve-PrSlot) contra a box real antes de ligar o enforce.
    try {
        $act = Get-PrActivity
        $pq = $null
        if (Test-Path -LiteralPath $PrQueuePath) { try { $pq = Get-Content -LiteralPath $PrQueuePath -Raw | ConvertFrom-Json } catch {} }
        if ($null -eq $pq) { $pq = [pscustomobject]@{ contexts = @(); currentPr = ''; currentIdleSinceUtc = '' } }
        $seen = @{}; foreach ($c in @($pq.contexts)) { if ($null -ne $c) { $seen["$($c.id)"] = $c.firstSeenUtc } }
        $ordered = @()
        foreach ($c in @($pq.contexts)) { if ($null -ne $c -and $act.ContainsKey("$($c.id)")) { $ordered += [pscustomobject]@{ id = "$($c.id)"; firstSeenUtc = $c.firstSeenUtc } } }
        foreach ($id in ($act.Keys | Sort-Object)) { if (-not $seen.ContainsKey("$id")) { $ordered += [pscustomobject]@{ id = "$id"; firstSeenUtc = $nowUtc } } }
        $prs = @(); foreach ($c in $ordered) { $prs += [pscustomobject]@{ number = $c.id; realJobs = [int]$act["$($c.id)"] } }
        $slot = Resolve-PrSlot -Prs $prs -CurrentPr "$($pq.currentPr)" -CurrentIdleSinceUtc "$($pq.currentIdleSinceUtc)" -NowUtc $nowUtc
        if ($slot.action -ne 'hold' -or "$($pq.currentPr)" -ne "$($slot.currentPr)") {
            $ctxStr = (@($ordered | ForEach-Object { $cid = "$($_.id)"; "${cid}:$([int]$act[$cid])" }) -join ' ')
            Write-OrcLog "would_$($slot.action)" @{ current = "$($slot.currentPr)"; ctxs = $ctxStr; reason = $slot.reason }
        }
        # ENFORCE (so com -EnforceQueue, fora de -Observe): publica o ctx concedido no
        # HOST (o gate Windows le; sobrevive ao Stop-VM) e, no boundary do contexto,
        # limpa+compacta antes de liberar o proximo PR. O probe-gate evita compactar com
        # job real ativo no guest (mesma seguranca do stop ocioso).
        if ($EnforceQueue -and -not $Observe) {
            $prevCtx = "$($pq.currentPr)"
            if ($slot.action -eq 'boundary_advance') {
                # No boundary o proximo PR so e liberado quando a box esta apta. A pergunta
                # "o guest tem job ativo?" e checada SEMPRE primeiro, independente do disco —
                # sao duas perguntas independentes (disco limpo nao prova guest ocioso). Antes
                # desta correcao, o caso "box ja fresca" pulava a probe e avancava direto: se
                # Resolve-PrSlot decidiu boundary_advance por um miss passageiro de
                # Get-PrActivity (a contagem de atividade do GitHub e sujeita a lag/timeout,
                # ver Invoke-RestMethod com try/catch silencioso ali), a box fresca não
                # provava nada sobre o PR anterior de fato ter acabado — so que o compact
                # anterior tinha ido bem. Tres casos, agora nesta ordem:
                # (a) guest com job ativo (via SSH, ground-truth independente da contagem do
                #     GitHub) -> NAO avanca de jeito nenhum, segura no atual e re-tenta no
                #     proximo tick — preserva o grace, nunca orfana um PR com trabalho em voo;
                # (b) guest idle + box JA fresca (V>=floor) -> avanca direto, sem desperdicio
                #     entre PRs leves que nao sujaram a box;
                # (c) guest idle + box suja -> compacta e SO entao libera (box fresca).
                if (Get-GuestHasActiveJob) {
                    Write-OrcLog 'pr_boundary_deferred' @{ done = $prevCtx; reason = 'guest com job ativo -> espera esvaziar antes de avancar (independente do disco)' } 'WARN'
                    "$prevCtx" | Set-Content -LiteralPath $CurrentContextPath -NoNewline -Encoding ascii
                    $slot.currentPr = $prevCtx
                    $slot.idleSinceUtc = "$($pq.currentIdleSinceUtc)"
                }
                else {
                    $vNow = Get-VFreeGB
                    if ($vNow -ge $AdmitFloorGB) {
                        Write-OrcLog 'pr_boundary_skip_clean' @{ done = $prevCtx; next = "$($slot.currentPr)"; v_free_gb = $vNow }
                        "$($slot.currentPr)" | Set-Content -LiteralPath $CurrentContextPath -NoNewline -Encoding ascii
                    }
                    else {
                        Write-OrcLog 'pr_boundary_compact' @{ done = $prevCtx; next = "$($slot.currentPr)"; v_free_gb = $vNow } 'WARN'
                        Invoke-StopAndCompact -Reason 'pr_boundary'
                        "$($slot.currentPr)" | Set-Content -LiteralPath $CurrentContextPath -NoNewline -Encoding ascii
                    }
                }
            }
            else {
                try { "$($slot.currentPr)" | Set-Content -LiteralPath $CurrentContextPath -NoNewline -Encoding ascii }
                catch { Write-OrcLog 'pr_publish_error' @{ error = "$($_.Exception.Message)" } 'WARN' }
            }
        }
        $pq.contexts = $ordered
        $pq.currentPr = "$($slot.currentPr)"
        $pq.currentIdleSinceUtc = "$($slot.idleSinceUtc)"
        try { ($pq | ConvertTo-Json -Depth 5 -Compress) | Set-Content -LiteralPath $PrQueuePath -Encoding UTF8 } catch {}
    }
    catch { Write-OrcLog 'pr_queue_observe_error' @{ error = "$($_.Exception.Message)" } 'WARN' }
}
catch {
    # Fail-safe: na duvida NUNCA desliga (so o caminho de Start e seguro). Um
    # erro aqui significa "nao consegui provar ocioso" -> deixa a VM como esta.
    Write-OrcLog 'orchestrator_error' @{ error = $_.Exception.Message } 'ERROR'
    exit 1
}
