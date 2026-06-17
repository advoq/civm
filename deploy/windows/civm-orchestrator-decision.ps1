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
        [scriptblock]$HasActiveJobProbe = { $false }
    )
    # VM Off: liga se ha QUALQUER trabalho (queued ou running). running>0 com VM
    # off e estado transiente/stale da API, mas o lado seguro e subir a VM.
    if ($VmState -eq 'Off') {
        if (($Queued + $Running) -gt 0) { return 'start' }
        return 'noop_off'
    }
    # VM Running com trabalho: marca busy, nunca desliga.
    if (($Queued + $Running) -gt 0) { return 'mark_busy' }
    # Ocioso mas antes do debounce: espera o IdleStopMinutes.
    if ($IdleMinutes -lt $IdleStopMinutes) { return 'idle_debounce' }
    # Gate de stop: a probe SSH (lazy) ve job ativo de QUALQUER repo (mesmo os
    # que o token nao cobre). Se houver, aborta o stop (Kahneman #16 fail-safe).
    if (& $HasActiveJobProbe) { return 'stop_aborted_active_job' }
    return 'stop_and_compact'
}
