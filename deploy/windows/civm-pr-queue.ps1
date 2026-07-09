# civm-pr-queue.ps1 — decisao PURA da fila FIFO por-PR da box (Kahneman #13: o
# codigo deployado e o MESMO que o teste exercita). Funcao pura -> testavel sem
# GitHub nem Hyper-V.
#
# O problema: o org runner unico ja serializa o advoq em job-FIFO (nunca 2 jobs ao
# mesmo tempo), mas os jobs de PRs diferentes INTERCALAM na fila. O user quer
# PR-grouping estrito: todos os checks do PR-A rodam, a box LIMPA TUDO + COMPACTA
# (V:~67), e so entao o PR-B — pra cada PR iniciar com o SSD no maximo, igual o pago.
#
# Um "contexto" e a unidade da fila: um PR (`pr-1234`) ou um push de branch
# (`branch-main`) — o push da main e tratado como um PR (roda tudo, compacta, proximo).
# Por isso os ids sao STRING, nao int (a main nao tem numero de PR).
#
# Esta funcao e o cerebro: o orquestrador observa quais contextos tem check de box
# ativo (gate esperando OU check real running/queued) e quantos checks REAIS cada um
# tem; daqui sai QUEM detem o slot, quando o contexto atual acabou (-> limpa+compacta +
# avanca) e o grace que tolera o gap entre os workflows do MESMO contexto.

# Resolve-PrSlot decide a acao da fila a partir do estado observado. Retorna um objeto
# com: action (grant|hold|boundary_advance|idle), currentPr (o contexto que passa a
# deter o slot; '' = nenhum), idleSinceUtc (carimbo do grace, '' = limpo) e reason
# (texto para o log). E PURA: nada de I/O.
function Resolve-PrSlot {
    [CmdletBinding()]
    param(
        # Contextos com atividade de box AGORA, ja em ordem FIFO (primeiro a aparecer
        # primeiro — o caller mantem essa ordem no estado da fila). Cada item:
        # [pscustomobject]@{ number = <string id>; realJobs = <int> } onde realJobs =
        # checks reais (nao-gate) running+queued daquele contexto.
        [object[]]$Prs = @(),
        # Contexto que detem o slot agora ('' = nenhum concedido). String (ex.: 'pr-1234').
        [string]$CurrentPr = '',
        # ISO-8601 UTC de quando o currentPr ficou sem check real ('' = nunca/limpo).
        # E o relogio do grace: zera quando volta a ter check, conta quando fica em 0.
        [string]$CurrentIdleSinceUtc = '',
        [Parameter(Mandatory)][string]$NowUtc,
        # Minutos sem check real ate considerar o contexto CONCLUIDO. Cobre o gap entre
        # os workflows do MESMO contexto (go termina, web ainda nem despachou) — sem isso
        # a fila avancaria no primeiro buraco, no meio do PR.
        [int]$DoneGraceMinutes = 3
    )
    # Mapa id -> realJobs, para lookup O(1) do contexto atual.
    $byId = @{}
    foreach ($p in $Prs) { if ($null -ne $p) { $byId["$($p.number)"] = [int]$p.realJobs } }

    # Proximo contexto FIFO diferente do atual (o 1o da lista que nao seja o current).
    # Serve tanto para conceder (current vazio) quanto para avancar (current concluido).
    $nextPr = ''
    foreach ($p in $Prs) {
        if ($null -ne $p -and "$($p.number)" -ne "$CurrentPr") { $nextPr = "$($p.number)"; break }
    }

    if ("$CurrentPr" -ne '') {
        $curJobs = if ($byId.ContainsKey("$CurrentPr")) { $byId["$CurrentPr"] } else { 0 }
        if ($curJobs -gt 0) {
            # O contexto ativo ainda tem check real -> segura o slot e ZERA o grace
            # (qualquer buraco anterior foi so um gap entre workflows, nao o fim).
            return [pscustomobject]@{ action = 'hold'; currentPr = $CurrentPr; idleSinceUtc = ''; reason = "ctx $CurrentPr com check real ($curJobs)" }
        }
        # currentPr sem check real: pode ser o FIM do contexto ou um gap entre os
        # workflows dele. O grace decide.
        if ([string]::IsNullOrWhiteSpace($CurrentIdleSinceUtc)) {
            # 1o tick ocioso -> arma o grace, ainda segura o slot.
            return [pscustomobject]@{ action = 'hold'; currentPr = $CurrentPr; idleSinceUtc = $NowUtc; reason = "ctx $CurrentPr 0 check real -> grace armado" }
        }
        $idleMin = ([datetime]::Parse($NowUtc).ToUniversalTime() - [datetime]::Parse($CurrentIdleSinceUtc).ToUniversalTime()).TotalMinutes
        if ($idleMin -lt $DoneGraceMinutes) {
            return [pscustomobject]@{ action = 'hold'; currentPr = $CurrentPr; idleSinceUtc = $CurrentIdleSinceUtc; reason = "ctx $CurrentPr dentro do grace ($([math]::Round($idleMin,1))<$DoneGraceMinutes min)" }
        }
        # Grace estourou -> o contexto ACABOU. Limpa tudo + compacta no boundary (cross-PR)
        # e avanca pro proximo (nextPr vazio = fila vazia -> libera o slot, box ociosa).
        return [pscustomobject]@{ action = 'boundary_advance'; currentPr = $nextPr; idleSinceUtc = ''; reason = "ctx $CurrentPr concluido (idle $([math]::Round($idleMin,1))>=$DoneGraceMinutes) -> limpa+compacta + avanca para '$nextPr'" }
    }

    # Sem slot concedido: se ha contexto esperando, concede ao primeiro da fila (FIFO).
    if ("$nextPr" -ne '') {
        return [pscustomobject]@{ action = 'grant'; currentPr = $nextPr; idleSinceUtc = ''; reason = "concede o slot ao ctx '$nextPr' (frente da fila)" }
    }
    # Nada na fila e nada concedido -> ocioso.
    return [pscustomobject]@{ action = 'idle'; currentPr = ''; idleSinceUtc = ''; reason = 'fila vazia' }
}

# Resolve-PushWaveCompact: compact OFFLINE entre PUSHES do MESMO PR (mudanca de
# head_sha), sem thrash no meio do MESMO push.
#
# Por que existe: o boundary_compact cross-PR so roda quando o contexto ACABA
# (grace + avanco). Um PR longo (ex.: pr-1423) com dezenas de synchronize mantem
# o slot por dias; o VHDX incha e o V: fica em ~25GB so com warn_clean online.
# O pago nao tem esse gap (VM efemera por job). Aqui o sinal de "push novo" e a
# mudanca do tip head_sha do contexto — NAO o gap running>0->0 (esse thrashava
# no meio do batch do mesmo push: incidente main-push 699eb1d).
#
# Retorna action:
#   none       — nao faz nada (sem tip, guest ocupado, mesmo tip)
#   seed       — 1a vez vendo o tip (grava sha, sem compact)
#   skip_clean — tip mudou e V: ja >= floor (so atualiza sha)
#   compact    — tip mudou, guest ocioso, V: sujo -> Stop+Optimize
#
# PURA: zero I/O. Caller busca tip/V/guest e executa.
function Resolve-PushWaveCompact {
    [CmdletBinding()]
    param(
        [string]$CurrentPr = '',
        [string]$TipHeadSha = '',
        [string]$LastCompactHeadSha = '',
        [string]$LastCompactContext = '',
        [bool]$GuestHasActiveJob = $false,
        [int]$VFreeGB = 999,
        [int]$AdmitFloorGB = 55
    )
    if ([string]::IsNullOrWhiteSpace($CurrentPr)) {
        return [pscustomobject]@{ action = 'none'; reason = 'sem currentPr' }
    }
    if ([string]::IsNullOrWhiteSpace($TipHeadSha)) {
        return [pscustomobject]@{ action = 'none'; reason = 'tip head_sha ausente' }
    }
    # Nunca compacta com Runner.Worker no guest (mata job mid-wave).
    if ($GuestHasActiveJob) {
        return [pscustomobject]@{ action = 'none'; reason = 'guest com job ativo -> defere push-wave' }
    }

    $tip = "$TipHeadSha".ToLowerInvariant()
    $last = "$LastCompactHeadSha".ToLowerInvariant()
    $sameCtx = ("$LastCompactContext" -eq "$CurrentPr")

    # 1a observacao do tip neste contexto: grava sem compact (cold start ja
    # compactou no boundary anterior ou e o 1o PR do dia).
    if (-not $sameCtx -or [string]::IsNullOrWhiteSpace($last)) {
        return [pscustomobject]@{ action = 'seed'; reason = "seed tip $tip no ctx $CurrentPr" }
    }
    # Mesmo push: NAO compacta (anti-thrash mid-batch do mesmo head_sha).
    if ($tip -eq $last) {
        return [pscustomobject]@{ action = 'none'; reason = "mesmo tip $tip (intra-push)" }
    }

    # Tip mudou = novo push no mesmo PR (ou rebased tip).
    # V<=0 = telemetria ausente: so seed o tip (nao Stop-VM as cegas).
    if ($VFreeGB -le 0) {
        return [pscustomobject]@{ action = 'seed'; reason = "tip mudou $last->$tip mas V nao medido -> seed fail-safe" }
    }
    if ($VFreeGB -ge $AdmitFloorGB) {
        return [pscustomobject]@{ action = 'skip_clean'; reason = "tip mudou $last->$tip V=$VFreeGB>=$AdmitFloorGB -> so atualiza tip" }
    }
    return [pscustomobject]@{ action = 'compact'; reason = "tip mudou $last->$tip V=$VFreeGB<$AdmitFloorGB -> compact push-wave" }
}
