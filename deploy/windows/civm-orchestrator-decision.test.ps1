# Decision-table test do orchestrator: TODOS os cenarios, cada recusa pareada com seu
# positivo (Kahneman #13). Sem Pester. Dot-source o MESMO modulo que producao usa.
. "$PSScriptRoot\civm-orchestrator-decision.ps1"
$F = $false; $T = $true
# Politica vigente (2026-06-25): NAO se compacta no MEIO de um batch. Com a VM Running
# e job na FILA (Running==0 + Queued>0), a box MANTEM a VM up (mark_busy) pra o runner
# pegar os jobs — o boundary_compact "incondicional" thrashava (compactava em todo gap
# entre as ondas de jobs do mesmo push, parando a VM ~6min com a fila cheia; incidente
# main-push 699eb1d 02:17). Compact so quando a fila ZERA (idle stop_and_compact) ou na
# barreira do Off (reclaim_before_admit antes de admitir). O compact ENTRE PRs de
# verdade vem da fila FIFO por-PR (civm-pr-queue.ps1). Floors: host AdmitFloorGB=55
# (alcancavel; compact chega a ~67), guest GuestFloorGB=40 (Ubuntu ~45-63, nunca 70).
# Chaves por-caso: cp=CanPanic (def T), gf=GuestFreeGB (def 999), gfl=GuestFloorGB (40),
# afl=AdmitFloorGB (55), ra=AdmitReclaimAttempts (0), pr=PrevRunning (1). vfree=999=folgado.
$cases = @(
    # --- BARREIRA DE ADMISSAO: VM Off + fila so starta com host>=55 E guest>=40 ---
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'start'; d = 'VM off + queued + disco folgado -> liga' },
    @{ vm = 'Off'; q = 0; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'start'; d = 'VM off + running stale -> liga (defensivo)' },
    @{ vm = 'Off'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'noop_off'; d = 'VM off + nada -> fica off (scale-to-zero)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 5; exp = 'reclaim_before_admit'; d = 'VM off + fila + host V<55 -> reclaim ANTES de admitir' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 18; exp = 'reclaim_before_admit'; d = 'VM off + fila + host V=18 (#1182) -> reclaim, NAO start' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 54; exp = 'reclaim_before_admit'; d = 'VM off + fila + host V=54 (<55) -> reclaim (borda)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 55; exp = 'start'; d = 'VM off + fila + host V=55 (==floor) -> admite (borda)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 67; exp = 'start'; d = 'VM off + fila + host V=67 (pos-compact) -> admite' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; gf = 39; exp = 'reclaim_before_admit'; d = 'VM off + host OK mas GUEST 39<40 -> reclaim' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; gf = 40; exp = 'start'; d = 'VM off + guest V=40 (==floor) -> admite (borda)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; gf = 45; exp = 'start'; d = 'VM off + guest 45 (real do Ubuntu) -> admite' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 18; ra = 2; exp = 'start'; d = 'VM off + host<55 + 2 tentativas -> admite (anti-deadlock)' },
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'start'; d = 'VM off + V=0 (nao medi) -> admite (fail-safe)' },
    # --- VM Running + JOB ATIVO (Running>0): disk safety online, sem matar sem necessidade ---
    @{ vm = 'Running'; q = 2; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + trabalho + job rodando -> busy' },
    @{ vm = 'Running'; q = 0; r = 1; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'VM on + 1 running -> busy' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; exp = 'panic_compact'; d = 'BUSY + V<18 -> panic (disco manda sobre o job)' },
    @{ vm = 'Running'; q = 5; r = 1; idle = 0; stop = 10; job = $F; vfree = 25; exp = 'warn_clean'; d = 'BUSY + V<28 -> warn online (sem matar job)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 30; exp = 'mark_busy'; d = 'BUSY + V=30 (>warn) -> busy' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 28; exp = 'mark_busy'; d = 'BUSY + V=28 (==warn) -> busy (borda)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 18; exp = 'warn_clean'; d = 'BUSY + V=18 (==panic) -> warn, nao panic (borda)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'mark_busy'; d = 'BUSY + V=0 (medida falhou) -> fail-safe busy (#15)' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; cp = $F; exp = 'warn_clean'; d = 'BUSY + V<18 + cooldown -> rebaixa pra warn' },
    @{ vm = 'Running'; q = 9; r = 2; idle = 0; stop = 10; job = $F; vfree = 15; cp = $T; exp = 'panic_compact'; d = 'BUSY + V<18 fora do cooldown -> panic' },
    # --- OCIOSA (Running==0, Queued==0): debounce -> stop_and_compact (a UNICA via de compact warm) ---
    @{ vm = 'Running'; q = 0; r = 0; idle = 3; stop = 10; job = $F; vfree = 999; exp = 'idle_debounce'; d = 'idle 3<10 -> debounce' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 9.9; stop = 10; job = $F; vfree = 999; exp = 'idle_debounce'; d = 'idle 9.9<10 -> debounce (borda)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $T; vfree = 999; exp = 'stop_aborted_active_job'; d = 'idle 10 + worker ativo -> ABORTA stop' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 10; stop = 10; job = $F; vfree = 999; exp = 'stop_and_compact'; d = 'idle 10 + sem worker -> compacta' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 999; exp = 'stop_and_compact'; d = 'idle 30 + sem worker -> compacta' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 25; exp = 'stop_and_compact'; d = 'idle + V<28 -> compacta FULL (nao warn online)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $F; vfree = 15; exp = 'stop_and_compact'; d = 'idle + V<18 -> compacta FULL (nao panic)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 5; stop = 10; job = $F; vfree = 15; exp = 'idle_debounce'; d = 'idle<10 -> debounce (ocioso nao consome disco)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 30; stop = 10; job = $T; vfree = 15; exp = 'stop_aborted_active_job'; d = 'idle + worker no guest -> ABORTA stop (safety)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'idle_debounce'; d = 'Queued=0 (nada na fila) -> idle, nao compacta' },
    # --- FILA QUENTE (VM Running, Running==0, Queued>0): mark_busy SEMPRE — NAO compacta ---
    # --- no meio do batch (o fix do thrash main-push 699eb1d). Mantem a VM up pros jobs. ---
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 999; exp = 'mark_busy'; d = 'fila quente + disco folgado -> mantem up (sem compact mid-batch)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; exp = 'mark_busy'; d = 'fila quente + V=45 -> mantem up (NAO compacta no gap)' },
    @{ vm = 'Running'; q = 3; r = 0; idle = 0; stop = 10; job = $F; vfree = 67; exp = 'mark_busy'; d = 'fila quente + V=67 -> mantem up' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'mark_busy'; d = 'fila quente + V=0 -> mantem up (fail-safe)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; pr = 5; exp = 'mark_busy'; d = 'transicao >0->0 NAO compacta mais (era boundary_compact) -> mark_busy' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; pr = 0; exp = 'mark_busy'; d = 'sem transicao -> mark_busy' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $T; vfree = 45; exp = 'mark_busy'; d = 'job ativo no guest tb -> mark_busy (sem boundary, probe-gate aposentado aqui)' },
    @{ vm = 'Running'; q = 2; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; ra = 2; exp = 'mark_busy'; d = 'fila quente + 2 tentativas -> mark_busy' },
    # --- PRECEDENCIA: panic (V<18) ainda precede a fila quente ---
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 15; cp = $T; exp = 'panic_compact'; d = 'fila quente + V<18 + cp=T -> panic precede (disco critico manda)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 15; cp = $F; exp = 'mark_busy'; d = 'fila quente + V<18 + cooldown -> panic pulado -> mantem up' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 25; exp = 'mark_busy'; d = 'fila quente + V<28 -> mantem up (warn so com Running>0)' },
    @{ vm = 'Running'; q = 0; r = 2; idle = 0; stop = 10; job = $F; vfree = 27; exp = 'warn_clean'; d = 'Running>0 + V<28 -> warn online' }
)
$pass = 0; $fail = 0
foreach ($c in $cases) {
    $probe = if ($c.job) { { $true } } else { { $false } }
    $cp = if ($c.ContainsKey('cp')) { $c.cp } else { $true }
    $gf = if ($c.ContainsKey('gf')) { $c.gf } else { 999 }
    $gfl = if ($c.ContainsKey('gfl')) { $c.gfl } else { 40 }
    $afl = if ($c.ContainsKey('afl')) { $c.afl } else { 55 }
    $ra = if ($c.ContainsKey('ra')) { $c.ra } else { 0 }
    $pr = if ($c.ContainsKey('pr')) { $c.pr } else { 1 }
    $got = Get-OrchestratorDecision -VmState $c.vm -Queued $c.q -Running $c.r -IdleMinutes $c.idle -IdleStopMinutes $c.stop -HasActiveJobProbe $probe -VFreeGB $c.vfree -CanPanic $cp -AdmitFloorGB $afl -GuestFloorGB $gfl -GuestFreeGB $gf -AdmitReclaimAttempts $ra -PrevRunning $pr
    if ($got -eq $c.exp) { $pass++; "PASS  [$($c.exp)]  $($c.d)" } else { $fail++; "FAIL  esperado=$($c.exp) got=$got  ::  $($c.d)" }
}

# --- UNITARIO: funcoes puras Update-AdmitAttempts / Resolve-AdmitTransition (Off-branch) ---
# Floor host 55 (orchestrator passa -Floor $AdmitFloorGB). So a barreira do Off usa o contador.
function Test-Eq($got, $exp, $d) { if ($got -eq $exp) { $script:pass++; "PASS  [unit]  $d" } else { $script:fail++; "FAIL  [unit] esperado=$exp got=$got  ::  $d" } }
function New-St($a) { [pscustomobject]@{ admitReclaimAttempts = $a } }
Test-Eq (Update-AdmitAttempts -State (New-St 0) -VAfter 48 -Floor 55).admitReclaimAttempts 1 'Update vAfter=48 (<55) -> +1'
Test-Eq (Update-AdmitAttempts -State (New-St 1) -VAfter 67 -Floor 55).admitReclaimAttempts 0 'Update vAfter=67 (>=55) -> reset 0'
Test-Eq (Update-AdmitAttempts -State (New-St 1) -VAfter 55 -Floor 55).admitReclaimAttempts 0 'Update vAfter=55 (==floor) -> reset 0'
Test-Eq (Update-AdmitAttempts -State (New-St 1) -VAfter 0  -Floor 55).admitReclaimAttempts 1 'Update vAfter=0 (nao medi) -> PRESERVA (1, #15)'
Test-Eq (Resolve-AdmitTransition -State (New-St 0) -Decision 'reclaim_before_admit' -Running 0 -Queued 1 -VAfter 45 -Floor 55).admitReclaimAttempts 1 'Resolve reclaim -> conta (+1)'
Test-Eq (Resolve-AdmitTransition -State (New-St 0) -Decision 'panic_compact' -Running 0 -Queued 1 -VAfter 15 -Floor 55).admitReclaimAttempts 1 'Resolve panic + Running==0 -> conta (DT-9)'
Test-Eq (Resolve-AdmitTransition -State (New-St 0) -Decision 'panic_compact' -Running 2 -Queued 1 -VAfter 15 -Floor 55).admitReclaimAttempts 0 'Resolve panic + Running>0 -> NAO conta (DT-9)'
Test-Eq (Resolve-AdmitTransition -State (New-St 2) -Decision 'start' -Running 0 -Queued 1 -VAfter 67 -Floor 55).admitReclaimAttempts 0 'Resolve start -> reset'
Test-Eq (Resolve-AdmitTransition -State (New-St 2) -Decision 'mark_busy' -Running 0 -Queued 1 -VAfter 67 -Floor 55).admitReclaimAttempts 0 'Resolve mark_busy + admissao -> reset'
Test-Eq (Resolve-AdmitTransition -State (New-St 2) -Decision 'mark_busy' -Running 2 -Queued 1 -VAfter 45 -Floor 55).admitReclaimAttempts 2 'Resolve mark_busy + Running>0 (mid-batch) -> NAO reseta'

''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
