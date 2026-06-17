# validation.md — civm

> Log vivo de validações **empíricas** do civm (box, VHDX, orchestrator,
> compact, runners). Kahneman #13: medir, não asseverar — "código existe" ≠
> "função ativa". Cada entrada registra DADOS reais (números medidos no
> instante), não impressões.
>
> Esta é a fonte de verdade para "isso está de fato funcionando?". O `vm.md`
> inventaria o estado da máquina; aqui registramos as **medições que provam (ou
> refutam) um comportamento**.

## Convenção

- **Append-only** como o `MEMORY.md`: nunca delete, reescreva nem reordene
  entradas antigas. Entrada mais recente no **fim** do arquivo.
- Leia de baixo para cima; pare quando as entradas recentes bastarem.
- Toda entrada carrega DADOS medidos (números reais) e um veredito explícito.
- Nunca persista secrets, tokens, valores de env ou PII.

## Schema de entrada

```
## YYYY-MM-DD HH:MM -03 — <titulo>

**O que:** o que foi validado (1-2 frases).
**Dados medidos:** números reais (V: livre, workers, idle_min, decision-table
PASS/FAIL, etc.). Sem adjetivo antes do número.
**Veredito:** ✅ funciona / 🔴 não funciona / 🟡 parcial.
**Proxima acao:** próximo passo concreto, ou "nenhuma".
```

---

## 2026-06-17 06:15 -03 — Orchestrator scale-to-zero vs. self-clean da box

**O que:** Validar dois mecanismos de housekeeping da box: (1) o orchestrator
scale-to-zero refatorado para dot-source o módulo puro de decisão; (2) o
self-clean de disco que deveria compactar o VHDX quando a box fica idle.

**Dados medidos:**

- **Orchestrator (decisão pura):** refatorado para dot-source
  `civm-orchestrator-decision.ps1`, testado por
  `civm-orchestrator-decision.test.ps1`. Decision-table **11/11 PASS** contra o
  módulo **deployado** (`C:\civm-deploy`). Tick observado end-to-end:
  `{event:tick, queued:2, running:0, idle_min:0.6, vm:Running}` → decisão
  `mark_busy` (correta). Kahneman #13 fechado: o módulo deployado é que foi
  exercido, não uma cópia. Status: deployado em `-Observe` (task SYSTEM
  `civm-vm-orchestrator`, a cada 2 min).
- **Self-clean (autoreclaim) — FALHA:** box idle (`workers=0`) por 10 min
  seguidos (poll 06:03→06:13), mas **V: travado em 31 GB**. O
  `civm-vhdx-autoreclaim` **não compactou**. Root cause: o próprio header do
  script (`C:\civm-deploy\civm-vhdx-autoreclaim.ps1`, bloco `.DESCRIPTION` /
  `Rollback trigger`, ~linhas 26-27) documenta que o **idle predicate está
  quebrado** e recomenda desabilitar a scheduled task, mantendo só o
  `civm-vhdx-optimize` supervisionado "until the idle predicate is fixed".
  Config: `ThresholdGB=50`, `IdleWaitSeconds=600`. `LastRun 06:03 result=0`
  (sem compactar), `NextRun 06:31`.
- **Disco:** V: livre **31 GB** (verificado), VHDX **80 GB**, guest
  used=**67 G** / free=**36 G** / total=**108 G**, docker reclaimable
  **~4.7 GB** (volumes 84%). Estimativa: guest-clean + compact → V: **~44-46 GB**
  (alvo 46-48).
- **Secundário (queue):** `queued=2` na API do GitHub com `workers=0` — jobs não
  pegos (possível stuck/approval). Isso também bloquearia o `stop` do
  orchestrator: `queued>0` → `mark_busy`. Precisa tuning.

**Veredito:** 🟡 orchestrator validado mas em `-Observe` (decisão correta,
ainda não atua); 🔴 self-clean atual (autoreclaim) quebrado — idle predicate
documentadamente furado, V: não recupera.

**Proxima acao:** Não executar ainda — registrar como **#18** (causa-raiz na
camada certa): ativar o orchestrator (remover `-Observe`) e desabilitar o
autoreclaim quebrado (orchestrator = raiz do housekeeping; autoreclaim = legado
quebrado a aposentar). Tuning da fila `queued=2`/`workers=0` em paralelo, senão
o `stop` do orchestrator também trava.

## 2026-06-17 09:27 -03 — Root cause da fila travada PROVADO + corrigido

**O que:** Investigar por que `queued=2` com `workers=0` (entrada anterior, item
secundário) bloqueia o scale-to-zero. Resultado: não era stuck/approval — era
pior e mais simples.

**Dados medidos:**

- Os 2 "queued" são FANTASMAS: `gh run cancel` respondeu "Cannot cancel a run
  that is completed". Criados em **25/mai** (3 semanas atrás), branch
  `feature/add-finance-module` (Web CI + Go CI). `action_required=0`, `waiting=0`
  — não é approval-pending. O índice `?status=queued` do GitHub fica STALE e
  lista runs JÁ COMPLETED como queued.
- O `Get-RunCount` do orchestrator confiava no `total_count` desse filtro → lia
  `queued=2` (falso) → `mark_busy` → nunca escala pra zero → nunca compacta → **V:
  travado**. Era o orchestrator (e qualquer lógica GitHub-aware, incl. o
  autoreclaim) sendo enganado pelo próprio GitHub.
- FIX aplicado: `Get-RunCount` agora BUSCA os runs (`per_page=30`) e conta só os
  que `run.status` realmente bate o status pedido E `created_at` < 12h (dupla
  guarda: status real + idade). Fantasma de 3 semanas cai nas duas.
- VALIDAÇÃO EMPÍRICA pós-fix: observe tick passou de `queued:2` para
  **`queued:0, running:0, idle_min:1.9`** → decisão `idle_debounce` (correta,
  precisa 10 min idle). Decision-table segue **11/11**.

**Veredito:** ✅ root cause da fila travada provado e corrigido (`queued` 2→0
medido). O orchestrator agora decide certo; ativado em seguida (ver próxima
entrada).

**Proxima acao:** ativar o orchestrator (remover `-Observe`) + desabilitar o
autoreclaim quebrado (#18); medir o `pr_boundary_reclaim_done` (vhdx_gb,
v_free_gb) no primeiro ciclo idle ativo.

## 2026-06-17 09:59 -03 — Orchestrator ATIVO: ciclo scale-to-zero COMPLETO ✅

**O que:** Validar o ciclo end-to-end do orchestrator ativo: stop+compact quando
idle E start quando chega job na fila (o START path, que faltava ao vivo).

**Dados medidos (log do orchestrator):**

- COMPACT (idle → stop): `09:45` e `09:55` dois `pr_boundary_reclaim_done
  vhdx_gb=68 v_free_gb=51`. **V: 31 → 51 GB.** O 2º foi a execução pendente do
  autoreclaim subindo a VM de novo (LastRun `06:31` host) — transiente único,
  não recorre (disabled).
- START (fila → up): push do #1168 às 09:49 → o `Optimize-VHD` de ~8min bloqueou
  os ticks até 09:55 (`MultipleInstances=IgnoreNew`); no 1º tick livre `09:57
  queued:11 vm:Running` → `09:59 queued:9 running:2 idle_min:2`. **A VM subiu e 2
  jobs já rodam na box**, fila 11→9 sendo consumida.

**Veredito:** ✅ ciclo scale-to-zero COMPLETO e medido: idle→compacta (V:51) e
fila→liga→jobs rodando. Os jobs eram REAIS (`running=2`), não fantasmas nem
approval-pending — **sem flap**. O orchestrator substituiu o autoreclaim
quebrado com sucesso.

**Observações (tuning, não bloqueiam):** (1) o `Optimize-VHD` de ~8min bloqueia
os ticks — um job que chega durante um compact espera ~10min (cold start pior);
aceitável p/ CI. (2) `guest_full_clean_warn` (awk do `free_after` log) é
cosmético: o `docker prune` roda antes do awk, então a limpeza ocorre. Fix
pendente.

**Proxima acao:** corpo do #1168 postado (29 commits); monitorar os checks até
verde.
