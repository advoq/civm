# Decisao pura do orchestrator scale-to-zero — extraida para ser testavel sem
# tocar a VM (Kahneman #13: o codigo deployado e o MESMO que o teste exercita).
# Recebe o estado observado e devolve UMA acao. A probe de job ativo e um
# scriptblock lazy: so e chamada quando se chega ao gate de stop (evita um SSH
# por tick).

# Aritmetica do contador da barreira de admissao — PURA e testavel (Kahneman #13).
# vAfter = V: livre medido APOS o compact. vAfter<=0 (nao medi) -> PRESERVA (nao perde
# progresso anti-deadlock nem da false give-up; #15). <Floor -> +1. >=Floor (limpo,
# incl. ==Floor) -> 0. NOTA: um skip do Invoke-StopAndCompact (lock/slack) deixa
# vAfter<Floor -> conta como tentativa (por design; lock persistente -> admite em ~2).
function Update-AdmitAttempts {
    param([Parameter(Mandatory)]$State, [int]$VAfter, [int]$Floor = 51)
    if (-not ($State.PSObject.Properties.Name -contains 'admitReclaimAttempts')) {
        $State | Add-Member -NotePropertyName admitReclaimAttempts -NotePropertyValue 0 -Force
    }
    if ($VAfter -le 0) { return $State }
    if ($VAfter -lt $Floor) { $State.admitReclaimAttempts = [int]$State.admitReclaimAttempts + 1 }
    else { $State.admitReclaimAttempts = 0 }
    return $State
}

# ORQUESTRACAO PURA decisao -> efeito-no-contador (Kahneman #13: o caller CHAMA esta;
# o teste exercita a MESMA). A regra "panic conta so com Running==0" (admissao que
# comecou por panic) vive AQUI, nao no switch do caller. Retorna o $State mutado.
function Resolve-AdmitTransition {
    param([Parameter(Mandatory)]$State, [Parameter(Mandatory)][string]$Decision,
          [int]$Running, [int]$Queued, [int]$VAfter, [int]$Floor = 51)
    switch ($Decision) {
        'reclaim_before_admit' { return (Update-AdmitAttempts -State $State -VAfter $VAfter -Floor $Floor) }
        'boundary_compact'     { return (Update-AdmitAttempts -State $State -VAfter $VAfter -Floor $Floor) }
        'panic_compact'        { if ($Running -eq 0) { return (Update-AdmitAttempts -State $State -VAfter $VAfter -Floor $Floor) }; return $State }
        'start'                { $State.admitReclaimAttempts = 0; return $State }
        'mark_busy'            { if ($Running -eq 0 -and $Queued -gt 0) { $State.admitReclaimAttempts = 0 }; return $State }
        default                { return $State }
    }
}

function Get-OrchestratorDecision {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][ValidateSet('Off', 'Running')][string]$VmState,
        [Parameter(Mandatory)][int]$Queued,
        [Parameter(Mandatory)][int]$Running,
        [double]$IdleMinutes = 0,
        [int]$IdleStopMinutes = 10,
        [scriptblock]$HasActiveJobProbe = { $false },
        [int]$VFreeGB = 999,
        [int]$WarnFloorGB = 28,
        [int]$PanicFloorGB = 18,
        # CanPanic=false (cooldown ativo) rebaixa o panic para warn_clean: nao
        # re-mata jobs dentro da janela de cooldown; so poda online. Calculado
        # pelo caller via Test-ReclaimCooldown (civm-reclaim-gate.ps1).
        [bool]$CanPanic = $true,
        # BARREIRA DE ADMISSAO: o disco DEVE estar limpo em AdmitFloorGB (51GB)
        # livres nos DOIS lados (host V: + guest) antes de admitir o proximo batch.
        # GuestFreeGB vem do snapshot de host-metrics; 999 = desconhecido (nao
        # bloqueia). AdmitReclaimAttempts conta compacts que nao chegaram em 51
        # (rastreado pelo caller); >=2 admite mesmo assim (anti-deadlock da fila).
        [int]$AdmitFloorGB = 51,
        [int]$GuestFreeGB = 999,
        [int]$AdmitReclaimAttempts = 0,
        # PrevRunning = running do tick ANTERIOR. O gate warm so compacta na TRANSICAO
        # running >0->0 (um PR/onda de runs ACABOU), nao a cada running==0. Se running
        # fica preso em 0 (pos-compact, VM religando), prevRunning=0 -> sem transicao ->
        # sem re-compactar (anti-thrash por-EVENTO; 1 compactacao por PR, sem timer). Um
        # PR cabe folgado em 58 (medido), entao 1 compactacao por PR basta.
        [int]$PrevRunning = 0
    )
    # VM Off com fila: BARREIRA DE ADMISSAO. So admite o proximo batch com o disco
    # LIMPO em AdmitFloorGB (51) nos DOIS lados — host V: e guest. Disco sujo ->
    # reclaim_before_admit (compacta offline primeiro), NAO starta. Incidente
    # 2026-06-18: o orchestrator startava no "tem fila" sem a pre-condicao, e o
    # #1182 rodou os checks a V:18, furando. VFreeGB<=0 / GuestFreeGB<=0 = "nao
    # medi" -> nao bloqueia (fail-safe, nao trava a fila por telemetria ausente).
    # AdmitReclaimAttempts >= 2 = o compact ja maxou sem chegar em 51 -> admite
    # mesmo assim (evita deadlock da fila quando 51 nao e atingivel).
    if ($VmState -eq 'Off') {
        if ((($Queued + $Running) -gt 0)) {
            $hostBelow = ($VFreeGB -gt 0 -and $VFreeGB -lt $AdmitFloorGB)
            $guestBelow = ($GuestFreeGB -gt 0 -and $GuestFreeGB -lt $AdmitFloorGB)
            if (($hostBelow -or $guestBelow) -and $AdmitReclaimAttempts -lt 2) { return 'reclaim_before_admit' }
            return 'start'
        }
        return 'noop_off'
    }
    $hasWork = (($Queued + $Running) -gt 0)
    # SEGURANCA DE DISCO — so quando ha TRABALHO. Ociosa, o fluxo normal abaixo faz
    # stop_and_compact (Optimize OFFLINE -> V: ~51), que e MELHOR que o warn online
    # (este so libera o guest e da fstrim, nao encolhe a VHDX). Sem o gate hasWork,
    # a box ociosa com V<28 ficava presa em warn_clean a cada tick: a VM nunca
    # desligava e o V: nunca voltava pra 51 (bug achado 2026-06-18: idle 27min,
    # V: travado em 22, VM Running). O disco encher DURANTE CI supera manter jobs
    # vivos (death-spiral chegou a 16GB, saturou o host, derrubou o interop). So
    # age com medida valida: VFreeGB <= 0 = "nao medi" -> fail-safe (Kahneman #15).
    # panic_compact compacta offline mesmo ocupado (mata job, disco NUNCA enche);
    # warn_clean poda cache de build (seguro, sem matar job). Fora do cooldown
    # (CanPanic); dentro, o panic rebaixa pra warn_clean (nao re-mata em loop).
    if ($hasWork -and $VFreeGB -gt 0 -and $VFreeGB -lt $PanicFloorGB -and $CanPanic) { return 'panic_compact' }
    # ADMISSAO DE BATCH WARM (precede warn; panic <18 ja ganhou acima). VM Running,
    # nenhum job ativo (Running==0) e batch na fila (Queued>0): o batch (PR/re-run) so
    # inicia com V: >= AdmitFloorGB (51). Sujo -> boundary_compact (Stop+Optimize; NAO
    # mata job pois Running==0); o proximo tick cai no ramo Off e religa via 'start'.
    # Precede warn: warn so poda online e NAO recupera o V: do host -> nao serve pra
    # admitir um batch; fica reservado a job rodando (Running>0). HOST-ONLY (o guest
    # snapshot e de 10min, stale demais pra decidir um compact de ~8min; Kahneman #15).
    # VFreeGB<=0 = "nao medi" -> admite (fail-safe). AdmitReclaimAttempts>=2 -> admite
    # mesmo <51 (anti-deadlock; o caller conta via Resolve-AdmitTransition). Substitui o
    # antigo boundary @40 (politica >=51 por batch; >=51-por-batch supera a otimizacao 40).
    if ($Running -eq 0 -and $Queued -gt 0) {
        if ($PrevRunning -gt 0 -and $VFreeGB -gt 0 -and $VFreeGB -lt $AdmitFloorGB -and $AdmitReclaimAttempts -lt 2) { return 'boundary_compact' }
        return 'mark_busy'
    }
    # Disco baixo com JOB RODANDO (Running>0): poda online, sem stop/kill.
    if ($hasWork -and $VFreeGB -gt 0 -and $VFreeGB -lt $WarnFloorGB) { return 'warn_clean' }
    # VM Running com trabalho: marca busy, nunca desliga.
    if ($hasWork) { return 'mark_busy' }
    # Ocioso mas antes do debounce: espera o IdleStopMinutes.
    if ($IdleMinutes -lt $IdleStopMinutes) { return 'idle_debounce' }
    # Gate de stop: a probe SSH (lazy) ve job ativo de QUALQUER repo (mesmo os
    # que o token nao cobre). Se houver, aborta o stop (Kahneman #15 fail-safe).
    if (& $HasActiveJobProbe) { return 'stop_aborted_active_job' }
    return 'stop_and_compact'
}
