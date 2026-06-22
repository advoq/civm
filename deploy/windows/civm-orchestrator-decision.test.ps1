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
    # --- GATE DE ADMISSAO WARM: Running==0 + Queued>0 -> boundary_compact se V<51, senao
    # mark_busy. Precede warn (panic <18 ja ganhou). Compacta no GAP sem matar job. ---
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'boundary_compact'; d = 'GAP: Running=0 + Queued>0 + V<51 -> compacta no gap (sem matar job)' },
    @{ vm = 'Running'; q = 3; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; exp = 'boundary_compact'; d = 'GAP + V=45 (<51) -> compacta ate 51 (politica >=51 por batch; era mark_busy@40)' },
    @{ vm = 'Running'; q = 1; r = 2; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'mark_busy'; d = 'V<51 mas Running=2 (job rodando) -> mark_busy inalterado (NAO mata job)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 40; exp = 'boundary_compact'; d = 'GAP + V=40 (<51) -> compacta (piso de admissao agora 51, nao 40)' },
    @{ vm = 'Running'; q = 0; r = 0; idle = 0; stop = 10; job = $F; vfree = 35; exp = 'idle_debounce'; d = 'V<40 mas Queued=0 (nada na fila) -> nao ha gap a compactar, cai no idle' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'mark_busy'; d = 'GAP + V=0 (medida falhou) -> fail-safe, mark_busy (nao compacta por medida ruim, #15)' },
    # Precedencia (SPECv4 §0): panic (V<18) ANTES do gate (preserva o piso critico e o
    # cooldown/lastPanicUtc); o gate ANTES de warn (no gap warm, compacta ate 51, nao so
    # poda online). cp=T+V<18 -> panic ganha (Running==0 nao mata); cp=F -> gate pega.
    # O gate cobre a faixa 18..51 no gap; warn fica para Running>0.
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 15; cp = $T; exp = 'panic_compact'; d = 'GAP + V<18 + cp=T: panic dispara antes do gate (OK, Running==0 nao mata)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 25; exp = 'boundary_compact'; d = 'GAP + V<28: o gate precede warn -> compacta ate 51 (warn so com Running>0)' },
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
    @{ vm = 'Off'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 0; exp = 'start'; d = 'VM off + fila + V=0 (nao medi) -> admite (fail-safe, nao trava fila)' },
    # --- NOVOS (SPECv4): gate de admissao warm @51 ---
    @{ vm = 'Running'; q = 2; r = 0; idle = 0; stop = 10; job = $F; vfree = 55; exp = 'mark_busy'; d = 'WARM + V=55 (>=51) -> admite limpo (mark_busy)' },
    @{ vm = 'Running'; q = 2; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; ra = 2; exp = 'mark_busy'; d = 'WARM + V<51 + 2 tentativas -> admite sujo (anti-deadlock)' },
    @{ vm = 'Running'; q = 2; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; ra = 1; exp = 'boundary_compact'; d = 'WARM + V<51 + 1 tentativa (<2) -> ainda compacta' },
    @{ vm = 'Running'; q = 2; r = 2; idle = 0; stop = 10; job = $F; vfree = 45; exp = 'mark_busy'; d = 'RF-2: V<51 mas Running=2 (job rodando) -> gate NAO dispara, mark_busy' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 15; cp = $F; exp = 'boundary_compact'; d = 'GAP + V<18 + cooldown (cp=F) -> panic pulado, gate compacta' },
    # --- BORDAS (SPECv4): admissao 50/51 e warn so com Running>0 ---
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 50; exp = 'boundary_compact'; d = 'BORDA WARM + V=50 (<51) -> compacta' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 51; exp = 'mark_busy'; d = 'BORDA WARM + V=51 (==floor, nao <) -> admite (mark_busy)' },
    @{ vm = 'Running'; q = 0; r = 2; idle = 0; stop = 10; job = $F; vfree = 27; exp = 'warn_clean'; d = 'BORDA + Running>0 + V<28 -> warn online (gate nao dispara, Running!=0)' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 28; exp = 'boundary_compact'; d = 'BORDA WARM + V=28 (no gap, 28<51 -> gate, nao warn)' },
    # --- GATE POR-EVENTO (per-PR): so compacta na transicao running >0->0 (fim do PR) ---
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; pr = 0; exp = 'mark_busy'; d = 'running preso em 0 (prevRunning=0) + V<51 -> NAO compacta [anti-thrash por-evento]' },
    @{ vm = 'Running'; q = 1; r = 0; idle = 0; stop = 10; job = $F; vfree = 45; pr = 5; exp = 'boundary_compact'; d = 'TRANSICAO >0->0 (prevRunning=5, running=0) + V<51 -> compacta 1x (fim do PR)' }
)
$pass = 0; $fail = 0
foreach ($c in $cases) {
    $probe = if ($c.job) { { $true } } else { { $false } }
    $cp = if ($c.ContainsKey('cp')) { $c.cp } else { $true }
    $gf = if ($c.ContainsKey('gf')) { $c.gf } else { 999 }
    $ra = if ($c.ContainsKey('ra')) { $c.ra } else { 0 }
    $pr = if ($c.ContainsKey('pr')) { $c.pr } else { 1 }
    $got = Get-OrchestratorDecision -VmState $c.vm -Queued $c.q -Running $c.r -IdleMinutes $c.idle -IdleStopMinutes $c.stop -HasActiveJobProbe $probe -VFreeGB $c.vfree -CanPanic $cp -GuestFreeGB $gf -AdmitReclaimAttempts $ra -PrevRunning $pr
    if ($got -eq $c.exp) { $pass++; "PASS  [$($c.exp)]  $($c.d)" } else { $fail++; "FAIL  esperado=$($c.exp) got=$got  ::  $($c.d)" }
}

# --- UNITARIO: funcoes puras Update-AdmitAttempts / Resolve-AdmitTransition (codigo REAL) ---
function Test-Eq($got, $exp, $d) { if ($got -eq $exp) { $script:pass++; "PASS  [unit]  $d" } else { $script:fail++; "FAIL  [unit] esperado=$exp got=$got  ::  $d" } }
function New-St($a) { [pscustomobject]@{ admitReclaimAttempts = $a } }
Test-Eq (Update-AdmitAttempts -State (New-St 0) -VAfter 48 -Floor 51).admitReclaimAttempts 1 'Update vAfter=48 (<51) -> +1'
Test-Eq (Update-AdmitAttempts -State (New-St 1) -VAfter 55 -Floor 51).admitReclaimAttempts 0 'Update vAfter=55 (>=51) -> reset 0'
Test-Eq (Update-AdmitAttempts -State (New-St 1) -VAfter 51 -Floor 51).admitReclaimAttempts 0 'Update vAfter=51 (==floor) -> reset 0'
Test-Eq (Update-AdmitAttempts -State (New-St 1) -VAfter 0  -Floor 51).admitReclaimAttempts 1 'Update vAfter=0 (nao medi) -> PRESERVA (1, #15)'
Test-Eq (Resolve-AdmitTransition -State (New-St 0) -Decision 'panic_compact' -Running 0 -Queued 1 -VAfter 15 -Floor 51).admitReclaimAttempts 1 'Resolve panic + Running==0 -> conta (DT-9)'
Test-Eq (Resolve-AdmitTransition -State (New-St 0) -Decision 'panic_compact' -Running 2 -Queued 1 -VAfter 15 -Floor 51).admitReclaimAttempts 0 'Resolve panic + Running>0 -> NAO conta (DT-9)'
Test-Eq (Resolve-AdmitTransition -State (New-St 1) -Decision 'boundary_compact' -Running 0 -Queued 1 -VAfter 45 -Floor 51).admitReclaimAttempts 2 'Resolve boundary -> conta (+1)'
Test-Eq (Resolve-AdmitTransition -State (New-St 2) -Decision 'start' -Running 0 -Queued 1 -VAfter 60 -Floor 51).admitReclaimAttempts 0 'Resolve start -> reset'
Test-Eq (Resolve-AdmitTransition -State (New-St 2) -Decision 'mark_busy' -Running 0 -Queued 1 -VAfter 60 -Floor 51).admitReclaimAttempts 0 'Resolve mark_busy + admissao warm -> reset'
Test-Eq (Resolve-AdmitTransition -State (New-St 2) -Decision 'mark_busy' -Running 2 -Queued 1 -VAfter 45 -Floor 51).admitReclaimAttempts 2 'Resolve mark_busy + Running>0 (mid-batch) -> NAO reseta'

# --- STATEFUL: convergencia <=2 compacts/episodio com disco irrecuperavel, funcoes REAIS ---
function Test-Converge($v, $cp, $label) {
    $st = New-St 0; $vm = 'Running'; $compacts = 0; $admit = $false
    for ($i = 0; $i -lt 6; $i++) {
        $dec = Get-OrchestratorDecision -VmState $vm -Queued 1 -Running 0 -IdleMinutes 0 -IdleStopMinutes 10 -HasActiveJobProbe { $false } -VFreeGB $v -CanPanic $cp -AdmitFloorGB 51 -GuestFreeGB 999 -AdmitReclaimAttempts ([int]$st.admitReclaimAttempts) -PrevRunning 1
        if ($dec -in @('boundary_compact', 'reclaim_before_admit', 'panic_compact')) { $compacts++; $vm = 'Off' }
        $st = Resolve-AdmitTransition -State $st -Decision $dec -Running 0 -Queued 1 -VAfter $v -Floor 51
        if ($dec -in @('start', 'mark_busy')) { $admit = $true; break }
    }
    Test-Eq ($admit -and $compacts -le 2) $true "$label -> admite em <=2 compacts (foi $compacts)"
}
Test-Converge 45 $true 'STATEFUL faixa [18,51) irrecuperavel'
Test-Converge 15 $true 'STATEFUL faixa V<18 (DT-9, panic conta)'

# --- STATEFUL: ciclo de vida de 1 PR (running 0->N->0) compacta 1x; running preso em 0 nao re-compacta ---
function Test-PrLifecycle {
    $prev = 0; $compacts = 0
    # ticks: jobs comecam (0->2->2), PR termina (2->0 = transicao), pos-compact preso em 0 (0,0)
    foreach ($run in @(0, 2, 2, 0, 0, 0)) {
        $dec = Get-OrchestratorDecision -VmState 'Running' -Queued 1 -Running $run -IdleMinutes 0 -IdleStopMinutes 10 -HasActiveJobProbe { $false } -VFreeGB 45 -AdmitFloorGB 51 -PrevRunning $prev
        if ($dec -eq 'boundary_compact') { $compacts++ }
        $prev = $run
    }
    Test-Eq $compacts 1 'STATEFUL ciclo 1 PR (0,2,2,0,0,0) -> compacta 1x (so na transicao 2->0), nao nos 0 presos'
}
Test-PrLifecycle

''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
