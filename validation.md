# validation.md — civm

> Log vivo de validações **empíricas** do civm — a fonte de verdade para "isso
> está de fato funcionando agora?" no plano de **infra**. Cobre TODA validação
> de infra; a taxonomia está na tabela **Categorias** abaixo. Ancorado em
> **Kahneman #13**: medir, não asseverar — existe ≠ funciona, verde-no-último-run
> ≠ verde-agora. Cada entrada registra DADOS reais medidos no instante, não
> impressões.
>
> Complementa o `vm.md` (inventário do estado da máquina): aqui ficam as
> **medições que provam ou refutam** um comportamento. **Fronteira:** civm é
> infra e **independente do advoq** — validação de app vive no `validation.md`
> do advoq; não logue app aqui.

## Convenção

- **Append-only** como o `MEMORY.md`: nunca delete, reescreva nem reordene
  entradas antigas. Entrada mais recente no **fim**. Leia de baixo para cima;
  pare quando as entradas recentes bastarem.
- Toda entrada carrega DADOS medidos (número real, sem adjetivo antes do número)
  e um veredito explícito.
- Nunca persista secrets, tokens, valores de env ou PII.

## Categorias (tag opcional, escaneável)

Marque `**Categoria:**` com uma das tags abaixo. Lista viva — estenda quando
surgir um tipo novo de "existe ≠ funciona" no domínio de infra:

| Tag | O que valida | Veredito típico |
| --- | --- | --- |
| `orchestrator-decision` | decision-table N/N PASS contra o módulo DEPLOYADO (`Get-OrchestratorDecision`) | 20/20 PASS |
| `deploy-runtime` | orchestrator rodando no PowerShell 5.1 real da box (não cópia), tick sem erro | tick vivo, 0 erro |
| `disk-reclaim` | Optimize-VHD slack/cooldown + VHDX e V: livre antes/depois do compact | VHDX↓, V:↑ GB |
| `watchdog-live` | timer systemd (disk/runner/reverse/cleanup/metrics) ENABLED+ACTIVE+disparou | last run < janela |
| `runner-health` | runner registrado/online, workers, hooks `ACTIONS_RUNNER_HOOK_*` wired | N/N online |
| `parity` | Go/Node/Python/Docker/gh instalados batem os pins (`civmctl parity`) | N/N Compatible |
| `capacity-admit` | `accepting_jobs`, slots heavy (`admit`), docker-lock serializando | accepting / slot ok |
| `civm-ci-gate` | CI do próprio civm (go test -race/coverage, ciguard, validate-templates) | exit 0 / verde |

## Quando registrar

Faça append de uma entrada quando **medir** (não quando assumir):

- rodou a decision-table contra o módulo deployado, ou viu o orchestrator ticando
  vivo no runtime real;
- mediu V:/VHDX antes/depois de um compact, ou um reclaim recuperando disco;
- confirmou um timer systemd ENABLED+ACTIVE que de fato disparou na janela;
- verificou saúde de runner, paridade de tooling ou um gate de CI do civm.

Mnemônico: a pergunta "isso está funcionando agora?" se responde **daqui** — ou
vira uma entrada nova.

## Schema de entrada

```
## YYYY-MM-DD HH:MM -03 — <titulo>

**O que:** o que foi validado (1-2 frases).
**Categoria:** <tag da tabela acima>          (opcional, recomendado)
**Como medir:** comando/check para re-medir.  (opcional — quando re-executável)
**Dados medidos:** números reais (V: livre, VHDX GB, workers, idle_min,
decision-table PASS/FAIL, timer last-run). Sem adjetivo antes do número.
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

## 2026-06-17 18:38 -03 — Death-spiral sob CI sustentado + camada de disco 🟡

**O que:** O agente civm-watch (12 ciclos, 60min) mediu o V: despencando sob CI
sustentado (merge do #1168 + 2 PRs back-to-back). O scale-to-zero só compacta no
idle; CI sustentado nunca idla → a VHDX cresce monotonicamente → death-spiral.
Implementada camada de segurança de disco no orchestrator (panic/warn).

**Dados medidos (civm-watch.log, ciclos de 5min):**

- V: 17:13 **39GB** → 17:39 30 → 17:55 27 → 18:00 **22** (ALERTA) → 18:05 **16**
  (ALERTA) → 18:10 **17** (ALERTA). Queda de ~22GB/h sob CI sustentado.
- Efeito colateral medido: ~18:1x o interop WSL↔Windows caiu (`powershell.exe:
  Input/output error`, `/mnt/c` I/O error) — host saturado. WSL em si saudável
  (load 0.81, 6.3Gi RAM livre) → foi a ponte, não o WSL.
- civm#135 MERGED (orchestrator base); ms-auth #1178 OPEN/UNSTABLE
  (govulncheck/Trivy/ms-core QUEUED, não falha).

**Fix (validado por unit, NÃO live ainda):**

- `Get-OrchestratorDecision` ganhou `VFreeGB`/`WarnFloorGB(28)`/`PanicFloorGB(18)`:
  `warn_clean` (V<28: build-cache prune + fstrim, SEM matar job) e `panic_compact`
  (V<18: Optimize offline MESMO ocupado — mata job, re-roda, mas o disco nunca
  enche). Fail-safe: `VFreeGB<=0` (medida falhou) não age (#16).
- Decision-table **20/20** no pwsh 7.4.6 nativo do WSL (a ponte caída bloqueia o
  `powershell.exe`, mas o pwsh Linux roda o código real). 3 arquivos parseiam OK.
- `panic_compact` usa Optimize OFFLINE → sem o bug de eviction (VM desligada);
  `warn_clean` poda só cache de build (regenerável), nunca imagens de run → não
  reintroduz o "No such image" que o age-guard consertou.

**Veredito:** 🟡 PARCIAL — death-spiral REAL e medido (V:39→16 🔴 confirma o gap
do scale-to-zero sob CI sustentado); fix coded + unit-validado (20/20) mas **ainda
não deployado nem provado na box** (ponte caída). Só vira ✅ quando o
`panic_compact` disparar de fato e recuperar o V: num ciclo medido.

**Proxima acao:** PR do fix (`fix/orchestrator-disk-panic`); deploy + medir um
ciclo panic real quando a ponte voltar (`wsl --shutdown` ou restart do Windows).
Analisar o guest disk pra reduzir a FONTE (age-guard do prune deixa imagens de
run recentes acumularem na rajada).

## 2026-06-17 19:45 -03 — Fix de disco DEPLOYADO + validado na box ✅/🟡

**O que:** Ponte voltou (restart do Windows). Deploy do orchestrator com a camada
panic/warn em `C:\civm-deploy` + validacao no runtime real (PowerShell 5.1).

**Dados medidos:**

- Box AUTO-recuperou antes do deploy: VHDX `100->68GB`, V: `19->52GB`,
  `vhdx_attached=False` (destravou sozinho), `orch lastresult=0`. O old
  orchestrator compactou assim que a rajada drenou e a box idlou — confirma que o
  gap era SO o caso busy (o idle sempre funcionou).
- Deploy seguro: `disable -> cp orchestrator+decision -> re-enable` (nenhum tick
  pega arquivo meio-escrito). Counts no deployado: `panic/warn=10`, `VFreeGB=5`,
  `tr-fix=3`.
- `-Observe` tick na box logou `"v_free_gb":52` (campo NOVO) + `noop_off`, SEM
  erro → o wiring novo (mede V: e passa pra decisao) roda no runtime real.
- Decision-table DEPLOYADO rodou **20/20 no PowerShell 5.1 da box** (nao so no
  meu pwsh Linux) — a logica panic/warn passa no ambiente de producao.

**Veredito:** ✅ para DEPLOY + VALIDACAO (decisao 20/20 na box, wiring vivo,
sem erro); 🟡 para a ACAO `panic_compact`, que ainda nao disparou num evento real
de `V:<18 + VM Running` (so ocorre em rajada de CI sustentada). Vira ✅ pleno
quando o log registrar um `disk_panic` + `reclaim` recuperando o V: num ciclo
real — o watchdog e o log capturam o primeiro.

**Proxima acao:** push do PR civm (durabilidade no repo, await go do user). Fix do
tenant-isolation-smoke (`web.yml` `!cancelled()`) aplicado, pendente push.
Reduzir a FONTE (age-guard do prune) rastreado em advoq/civm#137.

## 2026-06-17 20:45 -03 — Auditoria adversarial + endurecimento do panic ✅/🟡

**O que:** Auditoria adversarial multi-lente (3 ceticos, disciplina #18) + audit
de docs (Opus) ANTES do push. Acharam furos REAIS; remediacao completa aplicada.

**Achados medidos:**

- 🔴 CRITICO (#15 fail-safe): o `panic_compact` fazia `Stop-VM + Optimize` NU, sem
  o gate de 2 fases. O `Optimize-VHD` consome scratch (~10GB p100) e pode estourar
  o V: num pico — o curador mataria o recurso que cura. O autoreclaim (desabilitado)
  ja tinha o gate provado (#106) e meu fix o ignorava (#18 ao contrario).
- 🔴 C4 ATIVO: o `civm-vhdx-optimize-watchdog` (=Ready) RELIGAVA a VM que o
  orchestrator desligava — medido no log (`22:27 vm:Running queued:0 running:0`).
  Dois donos do power-state em conflito.
- 🟡 #14 (tr-fix colado no commit), #10 (TODO-later sem issue), citacoes #16->#15
  (a doc civm tem 16 disciplinas, fail-safe=#15, sem #18).

**Remediacao (toda validada):**

- Gate de 2 fases reusado em `civm-reclaim-gate.ps1` (Test-OptimizeSlack: so
  compacta se folga pos-Off >= ScratchBudget 11) + cooldown 15min (Test-Reclaim-
  Cooldown, mata o loop) + lock canonico + recover-detection. Decision-table
  **23/23** + gate **10/10**, no pwsh E no PowerShell 5.1 da box. `-Observe` tick
  logou `can_panic:true`.
- C4: os 3 curadores legados (autoreclaim/optimize/optimize-watchdog) Disabled +
  institucionalizado no `activate-orchestrator.ps1` (+ boot trigger). Box agora:
  orch 2 triggers, 3 legacy Disabled.
- Docs: SPEC criado (`docs/specs/orchestrator-scale-to-zero/`), drift do RUNBOOK +
  observability + README + vm.md consertado, banners SUPERSEDED na cadeia de
  reclaim. Issue #137 pra reduzir a FONTE.

**Veredito:** ✅ o panic agora nao pode estourar o disco (gate pos-Off), nao loopa
(cooldown), nao colide (lock) e alerta se nao recupera. Deployado == repo na box.
🟡 residual: a ACAO panic_compact com gate ainda nao disparou num evento real de
`V:<18 + Running` (vira ✅ pleno no 1o `disk_panic`+`reclaim_done` medido).

**Proxima acao:** await go do user pro push (3 commits: feat codigo, fix tr, docs).

## 2026-06-18 00:10 -03 — Gate provado VIVO sob carga + bug hasWork corrigido ✅

**O que:** Os PRs #138/#1179 rodaram a CI na box. O monitor capturou o gate de
disco agindo ao vivo sob a MESMA carga que causou o death-spiral original; e o
usuario pegou um bug real (V: nao voltava pra 51 com a box ociosa).

**Dados medidos:**

- WARN provado VIVO: sob a CI pesada (tenant-isolation-smoke + parallel), o V:
  caiu de 33GB e o `warn_clean` disparou ~8x (`V:26->35, 24->30, 22->30, 19->27,
  18->31, 18->35`) — recuperou o V: TODA vez, ONLINE, sem matar um job. A CI
  rodou inteira e os 2 PRs ficaram verdes. O `panic_compact` NUNCA precisou
  disparar. Mesma carga que antes derrubou tudo (V:39->16, host saturado).
- BUG achado (hasWork): a box ociosa+apertada (idle 27min, V:22) ficou PRESA em
  `warn_clean` a cada tick — o disk-safety disparava ANTES do gate idle-stop, a
  VM nunca desligava e o V: nunca voltava pra 51 (o warn libera o guest mas nao
  encolhe a VHDX; so o Optimize offline encolhe). Fix: gatear warn/panic em
  `hasWork` -> ociosa cai no `stop_and_compact`. Tambem: o `-Observe` mutava o
  estado (`stop_aborted` resetava `lastBusyUtc`) -> guardado (nao-mutante).
- VALIDACAO end-to-end do fix: idle + V<28 -> `reclaim_start` ->
  `reclaim_post_off_remeasure` -> `reclaim_done` -> **V: 22 -> 52GB, VM Off**.
  Decision-table **24/24** + gate **10/10**, no pwsh e no PowerShell 5.1 da box.

**Veredito:** ✅ o caminho WARN esta provado VIVO sob carga real (recupera sem
matar job); o ciclo fecha (warn segura durante CI -> idle compacta pra ~52). O
`panic_compact` segue como salvaguarda nao-disparada (o melhor cenario). O bug
hasWork (que prenderia o V:) corrigido + medido end-to-end.

**Proxima acao:** commit do fix hasWork+observe no #138; merge dos 2 PRs no go do
user.
