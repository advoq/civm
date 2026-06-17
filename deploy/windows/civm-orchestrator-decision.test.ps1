# Decision-table test do orchestrator: TODOS os cenarios, cada recusa pareada
# com seu positivo (Kahneman #13). Sem Pester (sem dependencia). Dot-source o
# MESMO modulo que o orchestrator usa em producao — testa o codigo real.
. "$PSScriptRoot\civm-orchestrator-decision.ps1"
$F = $false; $T = $true
$cases = @(
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; exp = 'start'; d = 'VM off + queued -> liga' },
    @{ vm = 'Off'; q = 0; r = 1; idle = 0; stop = 10; job = $F; exp = 'start'; d = 'VM off + running stale -> liga (defensivo)' },
    @{ vm = 'Off'; q = 0; r = 0; idle = 0; stop = 10; job = $F; exp = 'noop_off'; d = 'VM off + nada -> fica off (scale-to-zero)' },
    @{ vm = 'Running'; q = 2; r = 1; idle = 0; stop = 10; job = $F; exp = 'mark_busy'; d = 'VM on + trabalho -> busy' },
    @{ vm = 'Running'; q = 0; r = 1; idle = 0; stop = 10; job = $F; exp = 'mark_busy'; d = 'VM on + 1 running -> busy' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; exp = 'mark_busy'; d = 'VM on + 1 queued -> busy' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 3; stop = 10; job = $F; exp = 'idle_debounce'; d = 'VM on + idle 3<10 -> debounce' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 9.9; stop = 10; job = $F; exp = 'idle_debounce'; d = 'VM on + idle 9.9<10 -> debounce (boundary)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $T; exp = 'stop_aborted_active_job'; d = 'VM on + idle 10 + worker ativo -> ABORTA stop (safety)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $F; exp = 'stop_and_compact'; d = 'VM on + idle 10 + sem worker -> desliga+compacta' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; exp = 'stop_and_compact'; d = 'VM on + idle 30 + sem worker -> desliga' }
)
$pass = 0; $fail = 0
foreach ($c in $cases) {
    $probe = if ($c.job) { { $true } } else { { $false } }
    $got = Get-OrchestratorDecision -VmState $c.vm -Queued $c.q -Running $c.r -IdleMinutes $c.idle -IdleStopMinutes $c.stop -HasActiveJobProbe $probe
    if ($got -eq $c.exp) { $pass++; "PASS  [$($c.exp)]  $($c.d)" } else { $fail++; "FAIL  esperado=$($c.exp) got=$got  ::  $($c.d)" }
}
''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
