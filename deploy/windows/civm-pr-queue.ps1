# civm-pr-queue.ps1 — decisao PURA da fila FIFO por-PR da box (Kahneman #13: o
# codigo deployado e o MESMO que o teste exercita). Funcao pura -> testavel sem
# GitHub nem Hyper-V.
#
# O problema: o org runner unico ja serializa o advoq em job-FIFO (nunca 2 jobs ao
# mesmo tempo), mas os jobs de PRs diferentes INTERCALAM na fila. O user quer
# PR-grouping estrito: todos os jobs do PR-A rodam, a box COMPACTA (V:~67), e so
# entao os jobs do PR-B comecam — pra cada PR iniciar com o SSD no maximo.
#
# Esta funcao e o cerebro: o orquestrador observa quais PRs tem job de box ativo
# (gate esperando OU job real running/queued) e quantos jobs REAIS (nao-gate) cada
# um tem; daqui sai QUEM detem o slot, quando o PR atual acabou (-> compacta +
# avanca) e o grace que tolera o gap entre os workflows do MESMO PR (nao avanca no
# primeiro buraco). O gate de cada PR le o slot concedido e so libera os jobs reais
# quando e a vez dele.

# Resolve-PrSlot decide a acao da fila a partir do estado observado. Retorna um
# objeto com: action (grant|hold|boundary_advance|idle), currentPr (o PR que passa
# a deter o slot; 0 = nenhum), idleSinceUtc (carimbo do grace, '' = limpo) e reason
# (texto para o log). E PURA: nada de I/O — o caller observa o GitHub e persiste o
# estado; aqui so ha aritmetica de fila.
function Resolve-PrSlot {
    [CmdletBinding()]
    param(
        # PRs com atividade de box AGORA, ja em ordem FIFO (primeiro a aparecer
        # primeiro — o caller mantem essa ordem no estado da fila). Cada item:
        # [pscustomobject]@{ number = <int>; realJobs = <int> } onde realJobs = jobs
        # reais (nao-gate) running+queued daquele PR.
        [object[]]$Prs = @(),
        # PR que detem o slot agora (0 = nenhum concedido).
        [int]$CurrentPr = 0,
        # ISO-8601 UTC de quando o currentPr ficou sem job real ('' = nunca/limpo).
        # E o relogio do grace: zera quando volta a ter job, conta quando fica em 0.
        [string]$CurrentIdleSinceUtc = '',
        [Parameter(Mandatory)][string]$NowUtc,
        # Minutos sem job real ate considerar o PR CONCLUIDO. Cobre o gap entre os
        # workflows do MESMO PR (go termina, web ainda nem despachou) — sem isso a
        # fila avancaria no primeiro buraco, no meio do PR.
        [int]$DoneGraceMinutes = 3
    )
    # Mapa number -> realJobs, para lookup O(1) do PR atual.
    $byNum = @{}
    foreach ($p in $Prs) { if ($null -ne $p) { $byNum[[int]$p.number] = [int]$p.realJobs } }

    # Proximo PR FIFO diferente do atual (o primeiro da lista que nao seja o current).
    # Serve tanto para conceder (current=0) quanto para avancar (current concluido).
    $nextPr = 0
    foreach ($p in $Prs) {
        if ($null -ne $p -and [int]$p.number -ne $CurrentPr) { $nextPr = [int]$p.number; break }
    }

    if ($CurrentPr -ne 0) {
        $curJobs = if ($byNum.ContainsKey($CurrentPr)) { $byNum[$CurrentPr] } else { 0 }
        if ($curJobs -gt 0) {
            # O PR ativo ainda tem job real -> segura o slot e ZERA o grace (qualquer
            # buraco anterior foi so um gap, nao o fim).
            return [pscustomobject]@{ action = 'hold'; currentPr = $CurrentPr; idleSinceUtc = ''; reason = "PR $CurrentPr com job real ($curJobs)" }
        }
        # currentPr sem job real: pode ser o FIM do PR ou um gap entre os workflows
        # dele. O grace decide.
        if ([string]::IsNullOrWhiteSpace($CurrentIdleSinceUtc)) {
            # 1o tick ocioso -> arma o grace, ainda segura o slot.
            return [pscustomobject]@{ action = 'hold'; currentPr = $CurrentPr; idleSinceUtc = $NowUtc; reason = "PR $CurrentPr 0 job real -> grace armado" }
        }
        $idleMin = ([datetime]::Parse($NowUtc).ToUniversalTime() - [datetime]::Parse($CurrentIdleSinceUtc).ToUniversalTime()).TotalMinutes
        if ($idleMin -lt $DoneGraceMinutes) {
            return [pscustomobject]@{ action = 'hold'; currentPr = $CurrentPr; idleSinceUtc = $CurrentIdleSinceUtc; reason = "PR $CurrentPr dentro do grace ($([math]::Round($idleMin,1))<$DoneGraceMinutes min)" }
        }
        # Grace estourou -> o PR ACABOU. Compacta no boundary (cross-PR) e avanca pro
        # proximo (nextPr=0 = fila vazia -> libera o slot, box ociosa).
        return [pscustomobject]@{ action = 'boundary_advance'; currentPr = $nextPr; idleSinceUtc = ''; reason = "PR $CurrentPr concluido (idle $([math]::Round($idleMin,1))>=$DoneGraceMinutes) -> compacta + avanca para $nextPr" }
    }

    # Sem slot concedido: se ha PR esperando, concede ao primeiro da fila (FIFO).
    if ($nextPr -ne 0) {
        return [pscustomobject]@{ action = 'grant'; currentPr = $nextPr; idleSinceUtc = ''; reason = "concede o slot ao PR $nextPr (frente da fila)" }
    }
    # Nada na fila e nada concedido -> ocioso.
    return [pscustomobject]@{ action = 'idle'; currentPr = 0; idleSinceUtc = ''; reason = 'fila vazia' }
}
