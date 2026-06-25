# Teste da decisao PURA da fila FIFO por-PR (Resolve-PrSlot). Sem GitHub/Hyper-V ->
# roda em qualquer pwsh (Kahneman #13: o deployado e o testado). Dot-source o MESMO
# modulo que o orquestrador usa.
. "$PSScriptRoot\civm-pr-queue.ps1"

$pass = 0; $fail = 0
function Test-Slot($label, $got, $expAction, $expCurrent) {
    if ($got.action -eq $expAction -and [int]$got.currentPr -eq [int]$expCurrent) {
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

# --- IDLE: nada na fila, nada concedido ---
Test-Slot 'fila vazia -> idle' (Resolve-PrSlot -Prs @() -CurrentPr 0 -NowUtc $now) 'idle' 0

# --- GRANT: sem slot concedido + PR esperando -> concede ao 1o da fila (FIFO) ---
Test-Slot 'grant ao 1o da fila' (Resolve-PrSlot -Prs @((Pr 10 0), (Pr 11 0)) -CurrentPr 0 -NowUtc $now) 'grant' 10
Test-Slot 'grant respeita ORDEM da lista, nao o numero (20 antes de 5)' (Resolve-PrSlot -Prs @((Pr 20 0), (Pr 5 0)) -CurrentPr 0 -NowUtc $now) 'grant' 20
Test-Slot 'grant mesmo com realJobs=0 (gate esperando ainda nao liberou os jobs)' (Resolve-PrSlot -Prs @((Pr 10 0)) -CurrentPr 0 -NowUtc $now) 'grant' 10

# --- HOLD: PR atual com job real -> segura o slot e ZERA o grace ---
$h = Resolve-PrSlot -Prs @((Pr 10 3)) -CurrentPr 10 -NowUtc $now
Test-Slot 'hold com job real' $h 'hold' 10
Test-Eq 'hold com job real zera o grace' $h.idleSinceUtc ''
Test-Slot 'job real vence o grace (idle antigo ignorado quando volta job)' (Resolve-PrSlot -Prs @((Pr 10 5)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now) 'hold' 10
Test-Eq 'job real limpa o idleSince mesmo com grace antigo' (Resolve-PrSlot -Prs @((Pr 10 5)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now).idleSinceUtc ''

# --- GRACE: PR atual sem job real, mas pode ser gap entre os workflows DELE ---
$g = Resolve-PrSlot -Prs @((Pr 10 0)) -CurrentPr 10 -CurrentIdleSinceUtc '' -NowUtc $now
Test-Slot '0 job real, 1o tick ocioso -> hold (arma grace)' $g 'hold' 10
Test-Eq 'arma o grace com NowUtc' $g.idleSinceUtc $now
Test-Slot 'dentro do grace (1<3 min) -> hold' (Resolve-PrSlot -Prs @((Pr 10 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago1 -NowUtc $now -DoneGraceMinutes 3) 'hold' 10
Test-Eq 'dentro do grace preserva o idleSince original' (Resolve-PrSlot -Prs @((Pr 10 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago1 -NowUtc $now -DoneGraceMinutes 3).idleSinceUtc $ago1

# --- BOUNDARY_ADVANCE: grace estourou -> PR concluido -> compacta + avanca ---
Test-Slot 'grace estourou (4>=3) + proximo na fila -> avanca para o proximo' (Resolve-PrSlot -Prs @((Pr 10 0), (Pr 11 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 11
Test-Slot 'grace estourou + fila vazia -> avanca para 0 (libera o slot, box ociosa)' (Resolve-PrSlot -Prs @((Pr 10 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 0
Test-Slot 'PR atual SUMIU da lista (fechado) + grace estourou -> avanca para o proximo' (Resolve-PrSlot -Prs @((Pr 11 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 11
Test-Eq 'boundary_advance limpa o idleSince' (Resolve-PrSlot -Prs @((Pr 10 0), (Pr 11 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3).idleSinceUtc ''

# --- FIFO no avanco: o proximo e o 1o da lista != atual ---
Test-Slot 'avanca para o 1o da lista diferente do atual' (Resolve-PrSlot -Prs @((Pr 10 0), (Pr 11 0), (Pr 12 0)) -CurrentPr 10 -CurrentIdleSinceUtc $ago4 -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 11

# --- Borda: grace exatamente no limite (==3) conta como estourado (>=) ---
$exact = '2026-06-24T23:57:00Z'  # exatamente 3 min antes de $now
Test-Slot 'grace == limite (3>=3) -> avanca (borda)' (Resolve-PrSlot -Prs @((Pr 10 0), (Pr 11 0)) -CurrentPr 10 -CurrentIdleSinceUtc $exact -NowUtc $now -DoneGraceMinutes 3) 'boundary_advance' 11

''; "RESULTADO: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
