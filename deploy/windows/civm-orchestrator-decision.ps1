# Decisao pura do orchestrator scale-to-zero — extraida para ser testavel sem
# tocar a VM (Kahneman #13: o codigo deployado e o MESMO que o teste exercita).
# Recebe o estado observado e devolve UMA acao. A probe de job ativo e um
# scriptblock lazy: so e chamada quando se chega ao gate de stop (evita um SSH
# por tick).
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
        [bool]$CanPanic = $true
    )
    # VM Off: liga se ha QUALQUER trabalho (queued ou running). running>0 com VM
    # off e estado transiente/stale da API, mas o lado seguro e subir a VM.
    if ($VmState -eq 'Off') {
        if (($Queued + $Running) -gt 0) { return 'start' }
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
