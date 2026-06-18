# SPEC — orchestrator scale-to-zero da VM do runner

Issue: advoq/civm#135 (base) + #18 (validation log) + fix `fix/orchestrator-disk-panic`
Status: implementado e ativo na box (2026-06-17)
Precedente: `docs/specs/host-volume-reclamation/SPEC.md` (gate de 2 fases, issue #106),
`docs/specs/host-volume-reclaim-liveness/SPEC.md`, `docs/specs/civm-disk-watchdog-busy-cleanup/SPEC.md`

> **Por que este SPEC existe.** Uma scheduled task SYSTEM (`civm-vm-orchestrator`,
> a cada 2 min) tem poder de **desligar a VM com `Stop-VM -Force` e compactar o
> VHDX offline MESMO com jobs de CI rodando** (caminho `panic_compact`). É uma
> operação destrutiva-por-design — mata workers que re-rodam. O audit de docs
> apontou como gap CRÍTICO: existia código sem spec nem runbook. Este documento é
> a fonte de verdade do contrato (quando age, quando se recusa, como reverter).

---

## 1. Contexto

A box do CI roda 7 runners self-hosted numa VM Hyper-V pesada (`gha-ubuntu-2404`)
no volume `V:`. O problema medido é footprint contínuo entre rajadas:

- Com a VM ligada e ociosa, o Hyper-V retém toda a RAM e o VHDX dinâmico só
  **cresce** — nunca encolhe sozinho. O VMRS (saved-state, ~8 GB) e o scratch do
  `Optimize-VHD` ocupam `V:` permanentemente.
- O `civm-vhdx-autoreclaim` legado tentava compactar no idle, mas o **idle
  predicate estava documentadamente quebrado** (o próprio header do script
  recomendava desabilitá-lo) e ele era enganado por **runs fantasmas** do índice
  stale do GitHub (ver §10, item ghost-queued). Resultado empírico: `V:` travado
  em 31 GB por 10 min com a box idle, sem compactar (`validation.md`,
  2026-06-17 06:15).

A decisão é mover o housekeeping para a camada certa: **scale-to-zero**. A VM só
existe quando há trabalho.

## 2. Decisão / Design

### Visão geral

Uma tarefa minúscula sempre-ligada no host (Scheduled Task, ~2 min,
`civm-vm-orchestrator.ps1:1-33`) decide a cada tick com base no estado observado
(power-state da VM + fila do GitHub + `V:` livre):

- **VM Off + existe run `queued` ou `in_progress`** num dos repos vigiados →
  `Start-VM`. Os runners sobem no boot e pegam os jobs
  (`civm-orchestrator-decision.ps1:25-27`).
- **VM Running + nada na fila + nada rodando + ocioso ≥ `IdleStopMinutes`** →
  limpeza total do guest, `Stop-VM`, `Optimize-VHD` (compacta). A VM fica Off
  (`civm-orchestrator-decision.ps1:41-47`, `Invoke-StopAndCompact`
  `civm-vm-orchestrator.ps1:206-255`).

**Ganho:** footprint zero entre rajadas (RAM devolvida ao Windows, VHDX
compactado). **Custo:** cold-start de ~1-2 min na primeira rajada (boot + runners
conectando) — aceitável para CI (`civm-vm-orchestrator.ps1:14-18`). Medido:
`V:` 31 → 51 GB no primeiro ciclo idle ativo (`validation.md`, 2026-06-17 09:59).

### Decisão pura, separada do efeito (Kahneman #13)

A lógica de decisão vive em `civm-orchestrator-decision.ps1`
(`Get-OrchestratorDecision`, linhas 6-48): recebe o estado observado e devolve
**uma** ação string; **não toca a VM**. O orchestrator só **executa** a ação no
`switch` (`civm-vm-orchestrator.ps1:284-325`). Isso permite a decision-table
testar o **mesmo módulo deployado** sem Hyper-V (`civm-orchestrator-decision.ps1:1-5`).

A probe de job ativo é um **scriptblock lazy** (`HasActiveJobProbe`,
`civm-orchestrator-decision.ps1:14,46`): só é chamada quando se chega ao gate de
stop, evitando um SSH por tick.

### Ações possíveis (saídas de `Get-OrchestratorDecision`)

| Ação | Condição | Efeito no `switch` |
| --- | --- | --- |
| `start` | VM Off + `(queued+running)>0` | `Start-VM` (linha 286-292) |
| `noop_off` | VM Off + nada | nada (linha 285) |
| `panic_compact` | VM Running + `0<VFreeGB<PanicFloorGB` + `CanPanic` | `Invoke-StopAndCompact` MESMO ocupado (linha 305-318) |
| `warn_clean` | VM Running + `0<VFreeGB<WarnFloorGB` | poda online, sem stop (linha 299-304) |
| `mark_busy` | VM Running + `(queued+running)>0` | grava `lastBusyUtc` (linha 293) |
| `idle_debounce` | VM Running + ocioso + `idle<IdleStopMinutes` | só loga (linha 294) |
| `stop_aborted_active_job` | VM Running + ocioso ≥ debounce + probe SSH vê worker | aborta stop, re-arma busy (linha 295-298) |
| `stop_and_compact` | VM Running + ocioso ≥ debounce + sem worker | `Invoke-StopAndCompact` (linha 319-324) |

**Ordem de precedência** dentro de `Get-OrchestratorDecision` (crítica): VM Off
primeiro → **segurança de disco** (panic, depois warn) **antes** do fluxo normal →
busy → debounce → gate de stop. Disco apertado supera manter jobs vivos
(`civm-orchestrator-decision.ps1:29-39`).

## 3. Requisitos

| ID | Requisito | Âncora no código |
| --- | --- | --- |
| RF-1 | VM Off + qualquer trabalho na fila → liga (lado seguro: `running>0` com VM off é estado stale, mas sobe mesmo assim) | `decision.ps1:25-27`, test casos 1-2 |
| RF-2 | VM Off + nada → permanece Off (a essência do scale-to-zero) | `decision.ps1:26-27`, test caso 3 |
| RF-3 | VM Running com trabalho → `mark_busy`, **nunca** desliga | `decision.ps1:41`, test casos 4-6 |
| RF-4 | Idle < `IdleStopMinutes` (default 10) → `idle_debounce` (não desliga no primeiro tick idle) | `decision.ps1:43`, test casos 7-8 |
| RF-5 | Idle ≥ debounce + **sem worker no guest** → `stop_and_compact` | `decision.ps1:47`, test casos 10-11 |
| RF-6 | Idle ≥ debounce mas a probe SSH vê `Runner.Worker` ativo (repo fora do escopo do token) → `stop_aborted_active_job` | `decision.ps1:44-46`, test caso 9 |
| RF-7 | Contagem da fila ignora runs fantasmas: conta só `run.status` real + `created_at < MaxAgeHours` (12h) | `Get-RunCount` `orchestrator.ps1:107-125` |
| RF-8 | Um PAT `actions:read` por resource owner; o stop-guard via SSH é a salvaguarda final independente de token | `orchestrator.ps1:38-54,166-175` |
| RF-9 | Ownership exclusivo: orchestrator é o único dono do power-state + stop/compact (ver §2 Ownership abaixo) | `orchestrator.ps1:30-32`, `activate-orchestrator.ps1:12` |

### Ownership (CRÍTICO — um dono só, fail-safe #15)

O orchestrator é o **único dono** do power-state da VM e do par
`Stop-VM`/`Optimize-VHD`. Ao ativá-lo (`activate-orchestrator.ps1`), os curadores
legados foram **DESABILITADOS na box** (todos `Disabled`, 2026-06-17):

- `civm-vhdx-autoreclaim` — `Disable-ScheduledTask` em `activate-orchestrator.ps1:12`.
- `civm-vhdx-optimize` — supervisionado, aposentado pelo orchestrator.
- `civm-vhdx-optimize-watchdog` — religava a VM se ela ficasse Off; conflitaria
  com o scale-to-zero (`orchestrator.ps1:30-32`).

**Por quê:** dois reclaimers disputando o mesmo `V:\civm-reclaim.lock` e o
power-state é exatamente "o curador matando o recurso que cura". Um dono só
elimina a corrida (fail-safe #15). O autoreclaim legado mantém sua cópia inline do
gate até a remoção, mas a **fonte ativa** do gate de 2 fases é
`civm-reclaim-gate.ps1` (`civm-reclaim-gate.ps1:1-5`).

## 4. Camada de disk-safety (panic / warn)

O scale-to-zero só compacta no **idle**. Sob CI **sustentado** (PRs back-to-back),
a VM nunca idla → o VHDX cresce monotonicamente → death-spiral. Medido:
`V:` 39 → 16 GB em ~1h (~22 GB/h), saturando o host até derrubar o interop
WSL↔Windows (`validation.md`, 2026-06-17 18:38). A camada de disco fecha esse gap.

| Camada | Gatilho | Ação | Mata job? |
| --- | --- | --- | --- |
| `warn_clean` | `V: < WarnFloorGB` (28) | `docker builder prune -af` + `fstrim` online (`Invoke-GuestWarnClean`, `orchestrator.ps1:188-195`) | **Não** — poda só cache de build regenerável; nunca imagens de run (sem o bug de eviction que o age-guard consertou) |
| `panic_compact` | `V: < PanicFloorGB` (18) + `CanPanic` | `Stop-VM -Force` + `Optimize-VHD` offline **mesmo ocupado** (`orchestrator.ps1:305-318`) | **Sim** — jobs ativos morrem e re-rodam |

**Tradeoff explícito do panic** (`orchestrator.ps1:305-318`,
`decision.ps1:29-38`): matar jobs que re-rodam é ruim, mas **disco encher é pior**
— satura o host e derruba até o interop. O panic usa `Optimize` **offline** (VM
desligada) → sem o bug de eviction de imagem que ocorre online. A VM volta sozinha
pela fila no próximo tick (cold start).

Floors são parametrizáveis (`orchestrator.ps1:59-60`, defaults
`WarnFloorGB=28`/`PanicFloorGB=18`). Os boundaries são `<` estrito: `V==18` →
`warn` (não panic); `V==28` → `mark_busy` (não warn) — provados nos test casos
17-18.

## 5. Gate de segurança de 2 fases (reusado do #106)

`Invoke-StopAndCompact` (`orchestrator.ps1:206-255`) tem **três** salvaguardas
para o curador não matar o `V:` que cura:

1. **Lock canônico** `V:\civm-reclaim.lock` (`FileShare::None`,
   `orchestrator.ps1:209-216`): exclusão mútua com qualquer outro reclaimer do
   mesmo VHDX. Falha ao abrir → `reclaim_skip_locked` e retorna sem agir.
2. **Gate pós-Off** (`Test-OptimizeSlack`, `civm-reclaim-gate.ps1:28-35`): após
   `Stop-VM`, **re-mede `V:`** — o VMRS (~8 GB) só é liberado com a VM **Off**;
   medir antes subestima (foi o que travou a espiral a 6.6 GB no #106,
   `civm-reclaim-gate.ps1:22-27`). O `Optimize-VHD` é **ininterruptível** e
   **não-monotônico** (pode crescer o `V:` no meio), consumindo scratch ~10 GB. Só
   compacta se `(VFreeAfterOff − HardFloor 1) ≥ ScratchBudget 11`
   (`civm-reclaim-gate.ps1:12-13,34`); senão `reclaim_skip_insufficient_slack` e
   pula — **não empurra o `V:` abaixo do piso** (`orchestrator.ps1:232-237`).
3. **Recover-detection** (`orchestrator.ps1:245-249`): se recuperou
   `< MinRecoverGB` (3, `civm-reclaim-gate.ps1:20`), loga `reclaim_no_progress`
   como ERROR — disco apertado que o compact não resolve (precisa de humano), **não
   finge sucesso** (#13: existe ≠ funciona).

**Cooldown do panic** (`Test-ReclaimCooldown`, `civm-reclaim-gate.ps1:42-55`):
15 min (`PanicCooldownMinutes`, linha 17). Fora da janela → pode panicar; dentro →
a decisão **rebaixa** para `warn_clean` (`decision.ps1:18-21,38`), não re-mata jobs
em loop. O `lastPanicUtc` é gravado **antes** do compact (`orchestrator.ps1:315`):
o cooldown conta do disparo, e se o compact pendurar o próximo tick não re-mata.
O `Optimize` recupera ~25 GB e o crescimento medido é ~22 GB/h → ~1h entre panics
naturais; 15 min só barra o **loop apertado** (panic a cada tick quando o guest
reenche rápido), sem atrapalhar o ritmo real (`civm-reclaim-gate.ps1:14-16`).

## 6. Fail-safe (Kahneman #15 — na dúvida, nunca desliga)

Todo caminho de incerteza resolve para o **lado seguro: manter a capacidade de
pegar job**. Só o caminho de `Start` é seguro por si.

| Condição de incerteza | Comportamento | Âncora |
| --- | --- | --- |
| `VFreeGB <= 0` (medida de `V:` falhou) | **não** entra em panic/warn (não age por medida ruim) | `decision.ps1:38-39`, `Get-VFreeGB` `orchestrator.ps1:179-182`, test caso 19 |
| Probe SSH de job ativo falhou | assume "há job" → **não** desliga | `Get-GuestHasActiveJob` `orchestrator.ps1:166-175` |
| Falha de API do GitHub | relança → cai no `catch` do main → **não** desliga | `Get-RunCount` `orchestrator.ps1:98`, `orchestrator.ps1:327-332` |
| `lastPanicUtc` ilegível no cooldown | **não** trava o reclaim (deixa proteger) | `Test-ReclaimCooldown` `civm-reclaim-gate.ps1:48-54`, test caso 24 |
| SSH de limpeza (full/warn) falhou | best-effort, **não** bloqueia o stop | `Invoke-GuestFullClean` `orchestrator.ps1:149-160` |
| Erro qualquer no main | `orchestrator_error` ERROR + `exit 1`, **deixa a VM como está** | `orchestrator.ps1:327-332` |
| VM não chegou a Off no deadline (180s) | `reclaim_abort_vm_not_off` ERROR, **não** monta VHDX em uso | `orchestrator.ps1:223-228` |

O lock de estado expira por tempo (cooldown), **nunca trava pra sempre**
(`orchestrator.ps1:22-23`).

## 7. Rollback trigger (numérico / observável)

Reverter (re-habilitar o `civm-vhdx-optimize` supervisionado e desabilitar o
orchestrator, ou desarmar **só** a camada de disco via floors=0) quando **qualquer
um** dos sinais objetivos no `V:\civm-orchestrator.log` ocorrer:

- **≥ 2 eventos `disk_panic` com `running>0`** em janela de **< 30 min** — o panic
  está matando jobs em flap em vez de só barrar o death-spiral (o cooldown deveria
  impedir; se ocorrer, a calibração de 15 min está errada para o perfil de carga).
- **`reclaim_no_progress` repetido** (≥ 2 ocorrências consecutivas) — o compact
  "passa" mas `recovered_gb < 3`: o `Optimize` não está liberando espaço; problema
  fora do alcance do orchestrator (precisa de humano).
- **`V:` não recupera pós-panic medido** — após um `disk_panic` + `reclaim_done`,
  o `v_free_gb` registrado continua `< PanicFloorGB`: a compactação não está
  recuperando o piso esperado (~25 GB), então o panic só está custando jobs sem
  ganho.

Reversão imediata (não destrutiva): `Disable-ScheduledTask civm-vm-orchestrator` +
re-habilitar o caminho de optimize supervisionado. A VM volta a ser gerida pelo
curador legado conhecido.

## 8. Eventos de log (`V:\civm-orchestrator.log`)

Linha NDJSON por evento (`Write-OrcLog`, `orchestrator.ps1:75-82`): `ts` (UTC ISO),
`level`, `event` + campos.

| Evento | Quando | Campos principais | Âncora |
| --- | --- | --- | --- |
| `tick` | todo tick | `vm`, `queued`, `running`, `idle_min`, `v_free_gb`, `can_panic` | `orchestrator.ps1:277` |
| `vm_started` | ação `start` aplicada | `queued`, `running` | `orchestrator.ps1:290` |
| `idle_debounce` | ocioso antes do debounce | `idle_min`, `need` | `orchestrator.ps1:294` |
| `stop_aborted_active_job` | stop abortado: worker ativo via SSH | `note` | `orchestrator.ps1:296` |
| `disk_warn` | `warn_clean` disparado | `v_free_gb`, `floor` | `orchestrator.ps1:303` |
| `disk_panic` | `panic_compact` disparado (mata jobs) | `v_free_gb`, `floor`, `note` | `orchestrator.ps1:312` |
| `would_start` / `would_stop_and_compact` / `would_warn_clean` / `would_panic_compact` | modo `-Observe` (loga, não age) | conforme a ação | `orchestrator.ps1:287,302,310,322` |
| `reclaim_start` | início de `Invoke-StopAndCompact` | `reason` | `orchestrator.ps1:218` |
| `reclaim_post_off_remeasure` | re-medida de `V:` pós-Off (gate fase 2) | `v_free_after_off_gb`, `scratch_budget_gb` | `orchestrator.ps1:231` |
| `reclaim_skip_insufficient_slack` | folga não cobre o scratch → pula compact | `v_free_after_off_gb`, `hard_floor_gb` | `orchestrator.ps1:235` |
| `reclaim_skip_locked` | lock canônico ocupado | `reason`, `lock` | `orchestrator.ps1:214` |
| `reclaim_abort_vm_not_off` | VM não parou no deadline | `reason` | `orchestrator.ps1:226` |
| `reclaim_done` | compact concluído | `vhdx_gb`, `v_free_gb`, `recovered_gb` | `orchestrator.ps1:244` |
| `reclaim_no_progress` | recuperou `< MinRecoverGB` (ERROR) | `recovered_gb`, `min_recover_gb` | `orchestrator.ps1:248` |
| `orchestrator_error` | erro no main (ERROR, exit 1) | `error` | `orchestrator.ps1:330` |

Auxiliares (WARN, best-effort, não bloqueiam): `guest_full_clean[_warn]`,
`disk_warn_clean[_warn]`, `guest_active_probe_failed`, `vfree_probe_failed`.

## 9. Âncoras Kahneman (CIVM = 16 disciplinas)

- **#13 — existência ≠ função; deployado == testado.** A decisão pura é
  dot-sourced pelo orchestrator E pelos testes (`orchestrator.ps1:257-261`,
  `civm-orchestrator-decision.test.ps1:4`): o código deployado é o **mesmo** que a
  decision-table exercita (23 casos) e `civm-reclaim-gate.test.ps1` (10 casos).
  Cada recusa é pareada com seu positivo (ex.: `V<18` panicar **vs.** `V<18` em
  cooldown rebaixar para warn — casos 20/30). `reclaim_no_progress` materializa o
  princípio: o `Optimize` "passar" não prova que liberou espaço — mede-se
  `recovered_gb`.
- **#14 — retry calibrado, não cego.** O cooldown de 15 min (`Test-ReclaimCooldown`)
  é o retry calibrado: barra o `panic` re-disparando a cada tick quando o disco
  reenche rápido, mas não atrapalha o ritmo natural (~1h entre panics medido).
- **#15 — fail-safe = o gate + um dono só.** O gate de 2 fases (lock + slack
  pós-Off + recover-detection) impede o curador de matar o `V:` que cura; o
  ownership exclusivo (autoreclaim/optimize/watchdog desabilitados) elimina
  curadores em conflito disputando o lock/power. Na dúvida (§6), nunca desliga.

## 10. Notas de implementação / armadilhas conhecidas

- **Ghost-queued (RF-7).** O índice `?status=queued` do GitHub fica STALE e lista
  runs JÁ COMPLETED como queued — 2 runs de 3 semanas atrás travaram o
  scale-to-zero (`gh run cancel` respondia "Cannot cancel a run that is
  completed"). `Get-RunCount` ignora o `total_count` do filtro e conta só
  `run.status` real + `created_at < 12h` (`orchestrator.ps1:98-125`,
  `validation.md` 2026-06-17 09:27).
- **Principal SYSTEM obrigatório.** Deve rodar como SYSTEM (mesmo principal do
  autoreclaim) — como elevated-user a ssh key fica ilegível
  (`orchestrator.ps1:25-29`). A task registra com `New-ScheduledTaskPrincipal
  -UserId 'SYSTEM'` (`activate-orchestrator.ps1:8`).
- **Tokens.** Um PAT fine-grained `actions:read` por resource owner em
  `C:\ProgramData\civm\gh-token-{advoq,emersonbusson}.txt` (o host não tem `gh`,
  `orchestrator.ps1:25-27,38-43`).
- **`MultipleInstances IgnoreNew`** (`activate-orchestrator.ps1:9`): um
  `Optimize-VHD` de ~8 min bloqueia os ticks seguintes; um job que chega durante
  um compact espera ~10 min (cold start pior) — aceitável para CI
  (`validation.md` 2026-06-17 09:59).
- **Modo `-Observe`** (`orchestrator.ps1:66-69`): loga `would_*` em vez de agir —
  valida a lógica contra a box real sem mexer na VM. Preferido a `-WhatIf` (que
  suprimiria até o `Add-Content` do log).

## 11. Validação (evidência empírica)

Ver `validation.md` na raiz do repo (log append-only). Marcos:

- **2026-06-17 09:59** — ✅ ciclo scale-to-zero COMPLETO medido: idle → compacta
  (`V:` 31→51) e fila → liga → 2 jobs reais rodando (sem flap).
- **2026-06-17 18:38** — 🟡 death-spiral REAL medido (`V:` 39→16, interop caiu);
  camada de disco coded + unit-validada (decision-table 20/20 à época, hoje 23).
- **2026-06-17 19:45** — ✅ camada panic/warn DEPLOYADA: decision-table **20/20 no
  PowerShell 5.1 da box**, wiring `v_free_gb` vivo no tick real, sem erro. 🟡
  pendente: um `disk_panic` real (`V:<18` + VM Running) registrado num ciclo.

Testes (rodar localmente, sem Hyper-V):

```powershell
pwsh deploy/windows/civm-orchestrator-decision.test.ps1   # 23 casos
pwsh deploy/windows/civm-reclaim-gate.test.ps1            # 10 casos
```
