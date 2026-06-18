# Decision-table test do orchestrator: TODOS os cenarios, cada recusa pareada
# com seu positivo (Kahneman #13). Sem Pester (sem dependencia). Dot-source o
# MESMO modulo que o orchestrator usa em producao — testa o codigo real.
. "$PSScriptRoot\civm-orchestrator-decision.ps1"
$F = $false; $T = $true
# vfree=999 nos casos antigos = disco folgado, a camada de seguranca fica inerte.
$cases = @(
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'start'; d = 'VM off + queued -> liga' },
    @{ vm = 'Off'; q = 0; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'start'; d = 'VM off + running stale -> liga (defensivo)' },
    @{ vm = 'Off'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'noop_off'; d = 'VM off + nada -> fica off (scale-to-zero)' },
    @{ vm = 'Running'; q = 2; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + trabalho -> busy' },
    @{ vm = 'Running'; q = 0; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + 1 running -> busy' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + 1 queued -> busy' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 3; stop = 10; job = $F; vfree = 999; exp = 'idle_debounce'; d = 'VM on + idle 3<10 -> debounce' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 9.9; stop = 10; job = $F; vfree = 999; exp = 'idle_debounce'; d = 'VM on + idle 9.9<10 -> debounce (boundary)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $T; vfree = 999; exp = 'stop_aborted_active_job'; d = 'VM on + idle 10 + worker ativo -> ABORTA stop (safety)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $F; vfree = 999; exp = 'stop_and_compact'; d = 'VM on + idle 10 + sem worker -> desliga+compacta' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 999; exp = 'stop_and_compact'; d = 'VM on + idle 30 + sem worker -> desliga' },
    # --- camada de seguranca de disco (panic < 18, warn < 28) ---
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; exp = 'panic_compact'; d = 'PANIC: V<18 mesmo OCUPADO -> compacta (disco manda sobre o job)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 0; stop = 10; job = $T; vfree = 12; exp = 'panic_compact'; d = 'PANIC: V<18 supera ate o stop-guard (disco encher e pior)' },
    @{ vm = 'Running'; q = 5; r = 1; idle = 0; stop = 10; job = $F; vfree = 25; exp = 'warn_clean'; d = 'WARN: V<28 ocupado -> limpa cache (seguro, sem matar job)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 25; exp = 'warn_clean'; d = 'WARN: V<28 ocioso -> warn vem antes do compact normal' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 30; exp = 'mark_busy'; d = 'V=30 (>warn) ocupado -> busy normal (camada inerte)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 18; exp = 'warn_clean'; d = 'V=18 (==panic, nao <) -> warn, nao panic (boundary)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 28; exp = 'mark_busy'; d = 'V=28 (==warn, nao <) busy -> mark_busy, sem warn (boundary)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'mark_busy'; d = 'V=0 (medida falhou) -> fail-safe, NAO entra em panic (#16)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 5; exp = 'start'; d = 'VM off + V baixo -> start (seguranca de disco so vale Running)' },
    # --- cooldown do panic (cp = CanPanic) ---
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; cp = $F; exp = 'warn_clean'; d = 'COOLDOWN: V<18 mas panic em cooldown -> rebaixa pra warn (nao re-mata job)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 0; stop = 10; job = $T; vfree = 12; cp = $F; exp = 'warn_clean'; d = 'COOLDOWN: V<18 + worker ativo em cooldown -> warn, nao panic' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; cp = $T; exp = 'panic_compact'; d = 'V<18 fora do cooldown (cp=T) -> panic normal' }
)
$pass = 0; $fail = 0
foreach ($c in $cases) {
    $probe = if ($c.job) { { $true } } else { { $false } }
    $cp = if ($c.ContainsKey('cp')) { $c.cp } else { $true }
    $got = Get-OrchestratorDecision -VmState $c.vm -Queued $c.q -Running $c.r -IdleMinutes $c.idle -IdleStopMinutes $c.stop -HasActiveJobProbe $probe -VFreeGB $c.vfree -CanPanic $cp
    if ($got -eq $c.exp) { $pass++; "PASS  [$($c.exp)]  $($c.d)" } else { $fail++; "FAIL  esperado=$($c.exp) got=$got  ::  $($c.d)" }
}
''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
