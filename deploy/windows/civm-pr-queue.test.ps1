# Teste da decisao PURA da fila FIFO por-PR (Resolve-PrSlot). Sem GitHub/Hyper-V ->
# roda em qualquer pwsh (Kahneman #13). Ids sao STRING (ctx = 'pr-1234' ou 'branch-main').
. "$PSScriptRoot\civm-pr-queue.ps1"

$pass = 0; $fail = 0
function Test-Slot($label, $got, $expAction, $expCurrent) {
    if ($got.action -eq $expAction -and "$($got.currentPr)" -eq "$expCurrent") {
        $script:pass++; "PASS  [$($got.action) -> $($got.currentPr)]  $label"
    }
    else {
        $script:fail++; "FAIL  esperado=($expAction -> $expCurrent) got=($($got.action) -> $($got.currentPr))  ::  $label"
    }
}
function Test-Eq($label, $got, $exp) {
    if ("$got" -eq "$exp") { $script:pass++; "PASS  [eq] $label" }
    else { $script:fail++; "FAIL  [eq] esperado='$exp' got='$got'  ::  $label" }
}
function Pr($n, $j) { [pscustomobject]@{ number = $n; realJobs = $j } }

# Relogio fixo (a funcao e pura: le os timestamps passados, nunca o relogio real).
$now = '2026-06-25T00:00:00Z'
$ago1 = '2026-06-24T23:59:00Z'  # 1 min antes de $now
$ago4 = '2026-06-24T23:56:00Z'  # 4 min antes de $now
$exact = '2026-06-24T23:57:00Z' # exatamente 3 min antes de $now

# --- IDLE: nada na fila, nada concedido ---
Test-Slot 'fila vazia -> idle' (Resolve-PrSlot -Prs @() -CurrentPr '' -NowUtc $now) 'idle' ''

# --- GRANT: sem slot + ctx esperando -> concede ao 1o da fila (FIFO por chegada) ---
Test-Slot 'grant ao 1o da fila' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0), (Pr 'pr-11' 0)) -CurrentPr '' -NowUtc $now) 'grant' 'pr-10'
Test-Slot 'grant respeita a ORDEM da lista, nao o id' (Resolve-PrSlot -Prs @((Pr 'branch-main' 0), (Pr 'pr-5' 0)) -CurrentPr '' -NowUtc $now) 'grant' 'branch-main'
Test-Slot 'grant mesmo com realJobs=0 (gate esperando)' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0)) -CurrentPr '' -NowUtc $now) 'grant' 'pr-10'

# --- HOLD: ctx atual com check real -> segura o slot e ZERA o grace ---
$h = Resolve-PrSlot -Prs @((Pr 'pr-10' 3)) -CurrentPr 'pr-10' -NowUtc $now
Test-Slot 'hold com check real' $h 'hold' 'pr-10'
Test-Eq 'hold com check real zera o grace' $h.idleSinceUtc ''
Test-Slot 'check real vence o grace antigo' (Resolve-PrSlot -Prs @((Pr 'pr-10' 5)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago4 -NowUtc $now) 'hold' 'pr-10'

# --- GRACE: ctx sem check real, mas pode ser gap entre os workflows DELE ---
$g = Resolve-PrSlot -Prs @((Pr 'pr-10' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc '' -NowUtc $now
Test-Slot '0 check real, 1o tick ocioso -> hold (arma grace)' $g 'hold' 'pr-10'
Test-Eq 'arma o grace com NowUtc' $g.idleSinceUtc $now
Test-Slot 'dentro do grace (1<3 min) -> hold' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago1 -NowUtc $now -DoneGraceMinutes 3) 'hold' 'pr-10'
Test-Eq 'dentro do grace preserva o idleSince' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago1 -NowUtc $now -DoneGraceMinutes 3).idleSinceUtc $ago1

# --- BOUNDARY_ADVANCE: grace estourou -> ctx concluido -> limpa+compacta + avanca ---
Test-Slot 'grace estourou (4>=3) + proximo na fila -> avanca' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0), (Pr 'pr-11' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 'pr-11'
Test-Slot 'grace estourou + fila vazia -> avanca para vazio (libera o slot)' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' ''
Test-Slot 'ctx SUMIU da lista (fechado) + grace estourou -> avanca pro proximo' (Resolve-PrSlot -Prs @((Pr 'pr-11' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 'pr-11'
Test-Eq 'boundary_advance limpa o idleSince' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0), (Pr 'pr-11' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3).idleSinceUtc ''

# --- FIFO no avanco + borda do grace ---
Test-Slot 'avanca para o 1o da lista diferente do atual' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0), (Pr 'pr-11' 0), (Pr 'pr-12' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 'pr-11'
Test-Slot 'grace == limite (3>=3) -> avanca (borda)' (Resolve-PrSlot -Prs @((Pr 'pr-10' 0), (Pr 'pr-11' 0)) -CurrentPr 'pr-10' -CurrentIdleSinceUtc $exact -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 'pr-11'

# --- main (push de branch) tratado como contexto, igual um PR ---
Test-Slot 'branch-main como contexto -> grant' (Resolve-PrSlot -Prs @((Pr 'branch-main' 4)) -CurrentPr '' -NowUtc $now) 'grant' 'branch-main'
Test-Slot 'branch-main com check real -> hold' (Resolve-PrSlot -Prs @((Pr 'branch-main' 4)) -CurrentPr 'branch-main' -NowUtc $now) 'hold' 'branch-main'
Test-Slot 'PR espera a main terminar (FIFO): main na frente -> grant main' (Resolve-PrSlot -Prs @((Pr 'branch-main' 0), (Pr 'pr-20' 0)) -CurrentPr '' -NowUtc $now) 'grant' 'branch-main'

''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
