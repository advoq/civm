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
