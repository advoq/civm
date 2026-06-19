# Decision-table test do orchestrator: TODOS os cenarios, cada recusa pareada
# com seu positivo (Kahneman #13). Sem Pester (sem dependencia). Dot-source o
# MESMO modulo que o orchestrator usa em producao — testa o codigo real.
. "$PSScriptRoot\civm-orchestrator-decision.ps1"
$F = $false; $T = $true
# vfree=999 nos casos sem disco = disco folgado, a camada de seguranca fica inerte.
$cases = @(
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'start'; d = 'VM off + queued -> liga' },
    @{ vm = 'Off'; q = 0; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'start'; d = 'VM off + running stale -> liga (defensivo)' },
    @{ vm = 'Off'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'noop_off'; d = 'VM off + nada -> fica off (scale-to-zero)' },
    @{ vm = 'Running'; q = 2; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + trabalho -> busy' },
    @{ vm = 'Running'; q = 0; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + 1 running -> busy' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + 1 queued -> busy' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 3; stop = 10; job = $F; vfree = 999; exp = 'idle_debounce'; d = 'VM on + idle 3<10 -> debounce' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 9.9; stop = 10; job = $F; vfree = 999; exp = 'idle_debounce'; d = 'VM on + idle 9.9<10 -> debounce (boundary)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $T; vfree = 999; exp = 'stop_aborted_active_job'; d = 'VM on + idle 10 + worker ativo -> ABORTA stop' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $F; vfree = 999; exp = 'stop_and_compact'; d = 'VM on + idle 10 + sem worker -> compacta' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 999; exp = 'stop_and_compact'; d = 'VM on + idle 30 + sem worker -> compacta' },
    # --- disco SO quando ha TRABALHO (busy): nao bloqueia o stop+compact ocioso ---
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; exp = 'panic_compact'; d = 'BUSY + V<18 -> panic (disco manda sobre o job)' },
    @{ vm = 'Running'; q = 5; r = 1; idle = 0; stop = 10; job = $F; vfree = 25; exp = 'warn_clean'; d = 'BUSY + V<28 -> warn online (sem matar job)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 30; exp = 'mark_busy'; d = 'BUSY + V=30 (>warn) -> busy normal' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 28; exp = 'mark_busy'; d = 'BUSY + V=28 (==warn, nao <) -> busy (boundary)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 18; exp = 'warn_clean'; d = 'BUSY + V=18 (==panic, nao <) -> warn, nao panic (boundary)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'mark_busy'; d = 'BUSY + V=0 (medida falhou) -> fail-safe, busy (#15)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; cp = $F; exp = 'warn_clean'; d = 'BUSY + V<18 + cooldown -> rebaixa pra warn (nao re-mata)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; cp = $T; exp = 'panic_compact'; d = 'BUSY + V<18 fora do cooldown -> panic' },
    # --- BOUNDARY-COMPACT: gap entre sequencias de PR (Running==0 + Queued>0 + V<40) ---
    # Compacta no GAP sem matar job (nenhum in_progress). O panic (que mata job)
    # continua o fallback pra fila tao colada que Running nunca chega a 0 antes de V<18.
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'boundary_compact'; d = 'GAP: Running=0 + Queued>0 + V<40 -> compacta no gap (sem matar job)' },
    @{ vm = 'Running'; q = 3; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; exp = 'mark_busy'; d = 'GAP mas V=45 (>40, folgado) -> NAO compacta, segue busy (poupa cold-start)' },
    @{ vm = 'Running'; q = 1; r = 2; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'mark_busy'; d = 'V<40 mas Running=2 (job rodando) -> mark_busy inalterado (NAO mata job)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 40; exp = 'mark_busy'; d = 'GAP + V=40 (==floor, nao <) -> mark_busy (boundary estrito)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'idle_debounce'; d = 'V<40 mas Queued=0 (nada na fila) -> nao ha gap a compactar, cai no idle' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'mark_busy'; d = 'GAP + V=0 (medida falhou) -> fail-safe, mark_busy (nao compacta por medida ruim, #15)' },
    # Precedencia: panic (V<18) fica ANTES do boundary na cadeia e usa hasWork=(Q+R)>0,
    # nao Running>0. Com Q=1/R=0/V<18 o panic dispara primeiro — e tudo bem: Running==0,
    # entao o panic NAO mata job (compacta igual ao boundary). A unica diferenca e o
    # cooldown/label, inofensivo aqui. Deixar panic ganhar < 18 mantem o piso critico
    # uniforme (nao re-deriva V<18 entre dois caminhos). Boundary cobre a faixa 18..40.
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 15; cp = $T; exp = 'panic_compact'; d = 'GAP mas V<18: panic dispara antes (hasWork), e OK pois Running==0 nao mata job' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 25; exp = 'warn_clean'; d = 'GAP mas V<28: warn dispara antes do boundary (V na faixa de warn) -> poda online' },
    # --- OCIOSA + V baixo -> stop_and_compact (Optimize offline -> V:~51), NAO warn/panic ---
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 25; exp = 'stop_and_compact'; d = 'IDLE + V<28 + idle>10 -> compacta FULL (o fix: nao warn online)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 15; exp = 'stop_and_compact'; d = 'IDLE + V<18 + idle>10 -> compacta FULL (o fix: nao panic)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 5; stop = 10; job = $F; vfree = 15; exp = 'idle_debounce'; d = 'IDLE + V<18 + idle<10 -> debounce (ocioso nao consome disco)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $T; vfree = 15; exp = 'stop_aborted_active_job'; d = 'IDLE + V<18 + worker no guest -> ABORTA stop (safety)' },
    # --- BARREIRA DE ADMISSAO: VM Off + fila so starta com 51GB nos 2 lados ---
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 5; exp = 'reclaim_before_admit'; d = 'VM off + fila + host V<51 -> reclaim ANTES de admitir (nao starta sujo)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 18; exp = 'reclaim_before_admit'; d = 'VM off + fila + host V=18 (o caso #1182) -> reclaim, NAO start' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 51; exp = 'start'; d = 'VM off + fila + host V=51 (==floor) -> admite (boundary)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 60; exp = 'start'; d = 'VM off + fila + host V>51 -> admite' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 60; gf = 40; exp = 'reclaim_before_admit'; d = 'VM off + fila + host OK mas GUEST<51 -> reclaim (os 2 lados)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 18; ra = 2; exp = 'start'; d = 'VM off + fila + V<51 + 2 tentativas -> admite (anti-deadlock da fila)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'start'; d = 'VM off + fila + V=0 (nao medi) -> admite (fail-safe, nao trava fila)' }
)
$pass = 0; $fail = 0
foreach ($c in $cases) {
    $probe = if ($c.job) { { $true } } else { { $false } }
    $cp = if ($c.ContainsKey('cp')) { $c.cp } else { $true }
    $gf = if ($c.ContainsKey('gf')) { $c.gf } else { 999 }
    $ra = if ($c.ContainsKey('ra')) { $c.ra } else { 0 }
    $got = Get-OrchestratorDecision -VmState $c.vm -Queued $c.q -Running $c.r -IdleMinutes $c.idle -IdleStopMinutes $c.stop -HasActiveJobProbe $probe -VFreeGB $c.vfree -CanPanic $cp -GuestFreeGB $gf -AdmitReclaimAttempts $ra
    if ($got -eq $c.exp) { $pass++; "PASS  [$($c.exp)]  $($c.d)" } else { $fail++; "FAIL  esperado=$($c.exp) got=$got  ::  $($c.d)" }
}
''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
