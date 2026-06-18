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
    # SEGURANCA DE DISCO (VM Running) — ANTES do fluxo normal, pois o disco encher
    # supera manter jobs vivos (death-spiral chegou a 16GB em 2026-06-17, saturou
    # o host, derrubou ate o interop WSL). So age com medida valida: VFreeGB <= 0
    # significa "nao consegui medir" -> fail-safe, nao age (Kahneman #15).
    # panic_compact compacta offline mesmo ocupado (mata job ativo, mas o disco
    # NUNCA enche); warn_clean limpa cache de build (seguro, sem matar job). O
    # panic so dispara fora do cooldown (CanPanic) — dentro da janela ele rebaixa
    # para warn_clean, evitando re-matar jobs em loop (o Optimize ja recuperou
    # ~25GB; nao precisa re-disparar a cada tick).
    if ($VFreeGB -gt 0 -and $VFreeGB -lt $PanicFloorGB -and $CanPanic) { return 'panic_compact' }
    if ($VFreeGB -gt 0 -and $VFreeGB -lt $WarnFloorGB) { return 'warn_clean' }
    # VM Running com trabalho: marca busy, nunca desliga.
    if (($Queued + $Running) -gt 0) { return 'mark_busy' }
    # Ocioso mas antes do debounce: espera o IdleStopMinutes.
    if ($IdleMinutes -lt $IdleStopMinutes) { return 'idle_debounce' }
    # Gate de stop: a probe SSH (lazy) ve job ativo de QUALQUER repo (mesmo os
    # que o token nao cobre). Se houver, aborta o stop (Kahneman #15 fail-safe).
    if (& $HasActiveJobProbe) { return 'stop_aborted_active_job' }
    return 'stop_and_compact'
}
