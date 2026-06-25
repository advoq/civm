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

## 2026-06-18 05:00 -03 — Gate host-aware cego: root cause real do panic-mata-job ✅

**O que:** Os jobs ms-billing + tenant-isolation-smoke do #1181 foram CANCELADOS
pelo panic_compact (V:16). A investigacao disciplinada (Kahneman #18) achou o root
cause real: nao era barreira faltando — a barreira (gate host-aware no hook
job-started: limpa@60%, rejeita@90%/host-crit) EXISTE; ela estava CEGA.

**Dados medidos:**

- A telemetria que alimenta o gate (scheduled task `civm-host-metrics`) estava
  TRAVADA: snapshot 366min (6h) stale, task em Running com result de falha. O gate
  leu `v_free=43` (stale) enquanto o V: real era 16 -> nao rejeitou (stale =
  fail-open, DT-v2-5) -> disco furou -> panic matou os 2 jobs.
- Root cause do hang: `Get-VHD` (le o tamanho do VHDX, sem timeout) pendura no
  lock do VHDX enquanto o Optimize-VHD do orchestrator compacta — dois donos
  host-side disputam o disco. Sem `ExecutionTimeLimit`, a instancia presa
  bloqueou os runs de 10min por 6h.
- Fix (defense in depth, TDD RED->GREEN): (1) `Get-VhdxSizesWithTimeout` (job +
  `Wait-Job -Timeout 20s`) -> VHDX locked => snapshot com vhdx nulo + `v_free`
  critico, sem pendurar; `gap_gb`/`block_size` null-safe. (2) `ExecutionTimeLimit=
  PT5M` + `MultipleInstances=IgnoreNew` na task. 2 testes Go de scan estatico
  (host_metrics_robustness_test.go), full hostdisk suite + build + vet limpos.
- VALIDACAO na box (PowerShell 5.1): task re-registrada (ExecutionTimeLimit=PT5M
  confirmado), roda com `result=0`, snapshot FRESCO (age 1min) reportando `v_free`
  REAL=18 (nao mais o 43 stale). O gate voltou a enxergar a pressao real.

**Veredito:** ✅ o root cause (gate cego por telemetria travada no Get-VHD)
consertado + validado end-to-end na box. O gate host-aware agora rejeita jobs com
base no V: real, ANTES do panic precisar matar. A telemetria nunca mais pendura
(timeout no Get-VHD + ExecutionTimeLimit como backstop). O warn do orchestrator
foi mantido de proposito (alivio mid-job via fstrim; so o builder-prune e
questionavel — follow-up separado, nao e o root cause).

**Proxima acao:** observar que o panic nao re-dispara em PR pesado (o gate agora
bloqueia antes); avaliar o builder-prune do warn em separado; commit do fix.

## 2026-06-18 06:00 -03 — Barreira de admissao 51GB (so roda PR com disco limpo) ✅/🟡

**O que:** O #1182 entrou na fila e o orchestrator startou a VM pros checks com o
V: em 18 (nao 51) — a regra do usuario "so roda o proximo PR com 51GB livres nos 2
lados, apos full clean" nao era enforcada. O gate host-aware do hook (60%/90%) e a
pre-condicao do JOB; faltava a pre-condicao do BATCH no orchestrator.

**Dados medidos:**

- Gap: a decisao do orchestrator era `VM Off + fila -> start`, sem olhar o disco.
  Foi assim que o #1182 comecou a V:18 (log: `tick v_free_gb:21 queued:1 vm:Running`).
- Fix (TDD RED->GREEN): nova acao `reclaim_before_admit` na decisao pura — VM Off +
  fila + (host V<51 OU guest<51) AND tentativas<2 -> compacta ANTES de admitir,
  NAO starta. So `start` com 51 confirmado nos 2 lados. Anti-deadlock: se o compact
  maxar sem chegar em 51, a 2a tentativa admite (nao trava a fila). O handler reusa
  `Invoke-StopAndCompact` (gateado pra VM-ja-off: pula clean+stop, so compacta). O
  state rastreia `admitReclaimAttempts`; `start` zera; `guest_free` vem do snapshot.
- VALIDACAO: decision-table **30/30** (inclui o caso EXATO do #1182: VM Off + fila +
  V:18 -> reclaim_before_admit, nao start) + gate **10/10** + syntax OK, no pwsh e
  na PS 5.1 da box. Deployado == repo, orchestrator re-ativado.

**Veredito:** ✅ a barreira esta implementada, testada (30/30) e deployada — de
agora em diante NENHUM batch comeca abaixo de 51. 🟡 residual: o `reclaim_before_admit`
ainda nao disparou num evento real (VM Off + fila + V<51); vira ✅ pleno no 1o
disparo medido no log (watch em curso). O #1182 atual comecou pre-barreira.

**Proxima acao:** capturar o 1o reclaim_before_admit ao vivo; commit.

## 2026-06-18 19:17 -03 — Deep-clean do guest sobe o piso de disco 47->54 (E2E builda 35GB)

**O que:** A main CI de 2026-06-18 matou E2E + Go CI por `panic_disk`. Raiz: o piso
"limpo" do disco caiu de ~51 pra ~47-49 ao longo das runs — o `Invoke-GuestFullClean`
so removia ~/.cache + docker prune, nunca limpava _diag, _work, journal nem /tmp. A
E2E Tenant Isolation builda ~35GB de imagens num job (sobe o stack inteiro); com piso
47, 47-35=12 < 18 (panic floor) e o panic mata o job. So 2 de 20 runs historicas da
E2E passaram (8 cancelled, 10 failure).

**Dados medidos:**

- Piso ANTES (panic clean parcial): 47-49. Log: `disk_panic v_free_gb:15` na E2E;
  `guest_full_clean free_after:48` num panic — parcial porque base-images e o _work
  do job ativo resistem mid-job.
- Breakdown do guest (SSH via task SYSTEM, mid-job): ~/.cache 7.6G (yarn 4.3 +
  go-build 3.3), _work 5.5G (3 runners), _diag 1.0G, docker ~40G.
- Fix (deployado == repo, branch fix/deep-clean-guest-floor c50072e): o
  `Invoke-GuestFullClean` agora remove _diag, o conteudo de _work exceto _tool
  (hosted node/go, caro de re-baixar), faz vacuum do journal e limpa /tmp, alem do
  docker system + builder prune. Teste de scan (guest_clean_depth_test) trava os alvos.
- Piso DEPOIS (experimento idle controlado controlled-deepclean.ps1, sem job):
  `vbefore=49 -> guest_free_after=50 -> vafter=54`. +5-7GB. O guest "limpo" ainda usa
  ~58GB (OS + _tool preservado + sistema).

**Veredito:** 🟡 o deep-clean FUNCIONA (piso 47->54, medido), mas 54 e MARGINAL pro
E2E: 54-35=19, so 1GB acima do panic floor (18). O `warn_clean` compra margem na
drenagem, entao provavelmente passa — mas no talo. Decisao do usuario: deixar rodar +
monitorar; se o E2E panicar a 54, ir agressivo (remover _tool tambem, ~58).

**Proxima acao:** medir o E2E rerun real a 54 (watch bemw5w9bf); se panic, deep-clean
agressivo. Com ok do usuario, criar/mergear fix/deep-clean-guest-floor +
fix/bump-undici-tls.

**Categoria:** infra / disco
**Como medir:** experimento controlled-deepclean.ps1 (piso idle); `Get-Content
V:\civm-orchestrator.log | Select-String reclaim_done` (V: pos-reclaim ao vivo).

## 2026-06-18 20:35 -03 — Serializacao: box tinha 2 runners (concurrent prune matava jobs)

**O que:** O usuario apontou que 2 PRs rodavam checks ao mesmo tempo na box, violando
a fila. Raiz: a box tinha DOIS runner instances aceitando advoq jobs — civm-advoq
(repo-level, advoq/advoq) + civm-advoq-org (org-level, advoq/advoq + advoq/civm),
ambos com label civm. Um advoq job caia em qualquer um -> 2 jobs concorrentes no
mesmo disco. O 51GB/deep-clean nao bastava; faltava serializar o runner.

**Dados medidos:**

- Sintoma no #1184: ms-billing e ms-core falharam com "The operation was canceled" +
  "docker pull postgres:16-alpine/redis:8-alpine — retry (concurrent prune on shared
  civm runner)". Um runner podava enquanto o outro puxava imagem -> job morto. O
  govulncheck dos dois passou (codigo compila) -> nao era bug de codigo.
- Fix: systemctl disable do REPO runner civm-advoq, mantendo o ORG runner
  civm-advoq-org (serve advoq/advoq + advoq/civm num so runner). A 1a tentativa
  desativou o ERRADO (o org, que quebraria a CI da civm); o output do script pegou e
  corrigi com swap. Repos pessoais (vitae etc.) intactos.
- VALIDACAO: watch do runner busy durante o re-run do #1184 -> pico busy=1 (nunca 2).
  Serializado provado na coisa.

**Veredito:** ✅ serializado — 1 runner de advoq (o org), busy peak=1 medido. As falhas
de concurrent-prune (ms-billing/ms-core) nao recorrem. 🟡 residual: 1 runner da
job-serial FIFO, nao strict PR-grouping; se exigir "todos de um PR antes do outro"
estrito, falta um gate de PR.

**Proxima acao:** confirmar #1184 verde pos-serializacao + undici. Avaliar gate de PR
se o FIFO nao bastar.

**Categoria:** infra / runner
**Como medir:** `gh api orgs/advoq/actions/runners --jq '[.runners[]|select(.busy)]|length'`
(deve ser <=1); serialize-runner.ps1 lista/desativa os services.

## 2026-06-18 23:30 -03 — Serializacao CODIFICADA (4 camadas), nao mais ajuste manual

**O que:** O `systemctl disable` manual do runner por-repo civm-advoq nao
sobrevive a re-provisao da box. Codifiquei o invariante "1 runner civm por org"
em 4 camadas durables, na branch fix/serialize-runner-provisioning.

**Dados medidos:**

- Camada 1 (guard): `internal/runner/serialize.go` `DetectCollisions` (puro) +
  `internal/doctor` check `RUNNER_SERIALIZATION` (critico na colisao). Camada 2
  (watchdog): `restartWatchdogRunners` declina restartar runner por-repo
  redundante (sem isto o watchdog ressuscitava a unit disabled-mas-loaded a cada
  tick de 2min — modo de falha real que o `disable` manual nao cobria). Camada 3
  (enforcement): `deploy/windows/serialize-runner.ps1` idempotente, dry-run
  default, REMOVE (nao disable) via `civmctl runner remove`. Camada 4 (origem):
  `runbooks/ADVOQ-ADOPTION.md` Passo 1 deixou de registrar o runner por-repo.
- Testes Go: `go test -race -count=1 ./internal/runner/... ./internal/doctor/...`
  -> ok (runner 3.0s, doctor 1.0s). Cobertura: runner 84.7%, doctor 85.2%
  (ambos > 80%, invariante #6). `go vet` + build limpos. Invariante #17 (PS1
  Int32 clamp): hostdisk test verde; rg do regex no serialize-runner.ps1 -> 0.
- 12 unit tests novos cobrindo: org+repo ativo (estado #1184), repo
  disabled-mas-loaded (ainda colide), org-only (no-op), sem-org (no-op),
  org servindo multi-repo, owner diferente (sem falso positivo), e o watchdog
  nao-ressuscita.

**Veredito:** 🟡 codificado e provado em unit test, mas o EFEITO on-box nao foi
re-medido nesta sessao — a box estava OFF (scale-to-zero; ssh timeout). A logica
Go esta verde; a remocao ao vivo (`serialize-runner.ps1 -Execute`) e o doctor
critico contra a box real ainda precisam de uma medicao quando a VM ligar.

**Proxima acao:** quando a box ligar, rodar `ssh gha-ubuntu-2404 'civmctl doctor
--repos=auto --json' | jq '.hook_checks[]|select(.name=="RUNNER_SERIALIZATION")'`
e confirmar severity ok (so o runner org existe). Se aparecer civm-advoq por
heranca, `serialize-runner.ps1 -Execute` e re-medir.

**Categoria:** infra / runner
**Como medir:** `go test -race ./internal/runner/... ./internal/doctor/...`;
on-box: doctor check `RUNNER_SERIALIZATION` == ok.
## 2026-06-19 — Dreno de V: por job instrumentado + desconfusão de escala dos floors 🟡

**O que:** Uma validação adversarial Kahneman achou um HIGH: a margem de disco
que deixa um job E2E sobreviver no floor estava ESTIMADA ("~35GB" de dreno, sem
high-water capturado) e os "floors" estavam confundidos entre DUAS escalas (V:-
livre-GB vs guest-uso-%) — a headline "54−35=19, 1 acima do panic floor 18"
comparava planos diferentes. Dois eixos de fix, sem mexer threshold: (1) mapear
os gates por escala no SPEC §4.1; (2) instrumentar a MEDIÇÃO do dreno real.

**Dados medidos:**

- MAPA (SPEC §4.1): plano Host V:-livre-GB = `AdmitFloorGB=51` (orchestrator.ps1:286,
  decision.ps1:27), `WarnFloorGB=28` (ps1:59), `PanicFloorGB=18` (ps1:60),
  `CritFreeGB=10` (civm.go:105, aplicado em hostdisk.go:193-201 + hook.go:246-249),
  `HeadroomGB=8` (civm.go:110), `HardFloorGB=1` (civm.go:125). Plano Guest-uso-% =
  `HardFailPct=90` (civm.go:38, hook.go:242-245 via diskUsedPct hook.go:725),
  `PreCleanupPct=60` (civm.go:37, hook.go:220), `EmergencyBypassPct=75` (civm.go:47).
  O "18" e o "90%" NÃO são o mesmo floor — escalas físicas distintas.
- GAP de medição achado: o hook lia `host_v_free_gb` só no job-started, e o
  `appendLog` (hook.go:801) DESCARTAVA o campo — nunca chegava ao hooks.jsonl. O
  dreno por job era, portanto, irreconstruível do log; só estimável.
- FIX instrumentado: job-completed também lê host V: (hook.go EventJobCompleted);
  `appendLog` agora emite `host_v_free_gb` + `host_level`; `JobVDrainGB` é a
  definição canônica do high-water (`vfree@started − vfree@completed` por run_id,
  clamp 0 em delta negativo, `ok=false` em extremo <=0). 4 testes novos
  (emissão no log, leitura no completed, tabela JobVDrainGB 6 casos, par
  recusa/positivo Kahneman #13). `go test -race ./internal/hook ./internal/hostdisk`
  = ok; gofmt limpo; golangci-lint = 0 issues.
- FLOOR reformulado (SPEC §4.2): `floor >= CritFreeGB(10) + p95(drain_MEDIDO) +
  safety` — tudo em V:-livre-GB (a escala onde autoreclaim/panic/crit concordam),
  nunca "floor − dreno estimado".

**Veredito:** 🟡 a desconfusão de escala e a instrumentação estão FEITAS e
unit-green; o `AdmitFloorGB=51`/`PanicFloorGB=18` ficam INTOCADOS (não há dado de
dreno que justifique mover). O número de dreno p95 real é PENDENTE: a box está
rodando CI agora — será capturado no próximo job E2E Tenant Isolation (par
job-started/job-completed no hooks.jsonl). Vira ✅ quando o p95 medido confirmar
(ou refutar) o floor de 51.

**Como medir:** com a box ligada, após um E2E Tenant Isolation, parear no
`/var/log/civm/hooks.jsonl` os dois registros do mesmo `run_id` e computar
`host_v_free_gb@job-started − host_v_free_gb@job-completed` (ou rodar `JobVDrainGB`
sobre o par); acumular p95 sobre vários jobs pesados.

**Proxima acao:** capturar o 1o par de dreno real no próximo E2E; com o p95,
derivar `AdmitFloorGB` por §4.2 e fechar o 🟡.
## 2026-06-19 -03 — Reaper de container de CI órfão (job-started) ✅/🟡

**O que:** Validar o novo reaper de órfão não escopado a `_work` root
(`internal/hook/hook.go::reapOrphanCIContainers`) que, na fronteira job-started
(antes de o job subir o stack), derruba qualquer container RODANDO que publique
uma porta fixa de CI conhecida OU tenha `com.docker.compose.project` começando com
`advoq` — fix do incidente 2026-06-19 (órfão de minio em `127.0.0.1:9020` matou
tenant-isolation no #1184/#1186 com "port is already allocated"). Mais a decisão de
manter colisão de porta DETERMINÍSTICA no classificador do advoq (sem retry-sem-reap).

**Categoria:** civm-ci-gate

**Como medir:**
`go test -race ./internal/hook/ ./internal/civm/` (unit);
`go test -tags=integration -run TestIntegrationReapOrphan ./internal/hook/` (efeito real);
`golangci-lint run -c .golangci.yml ./...`.

**Dados medidos:**

- Unit (`-race`): pacote `hook` **90,6%→91,3%** de cobertura; `reapOrphanCIContainers`,
  `isCIOrphan`, `orphanIDsFromInspect` em **100%**. Pacote `civm` **95,8%**. `go test -race
  ./...` da box **ALL GREEN**. golangci-lint `./...` (com e sem `-tags=integration`):
  **0 issues**. gofmt limpo.
- Integration (docker REAL) — EFEITO, não mock:
  - `TestIntegrationReapOrphanRemovesRealLabeledContainer`: **PASS (6,29s)**. Subiu
    um container REAL com label `com.docker.compose.project=advoq-integration-label`
    (`--network none`), rodou o reaper contra o daemon REAL, afirmou que o container
    **sumiu** (`docker ps -aq` vazio). Prova o sinal primário (label).
  - `TestIntegrationReapOrphanFreesRealPort`: **SKIP** nesta box. O bridge/iptables do
    WSL2 atual não consegue publicar porta (`iptables ... raw: Table does not exist`),
    então o setup que prende a porta não roda. O teste self-skip (nunca falha) — vira
    gate REAL no runner self-hosted, onde o bridge funciona, afirmando a porta LIBERADA
    ponta a ponta.
- Classificador advoq (`tools/devctl/internal/ci/failure.go`, repo advoq, commit/PR
  separado): comentário de DECISÃO adicionado — colisão de porta NÃO ganha assinatura
  transitória (#14). Com o reaper, a colisão não chega mais ao classificador; se chegar,
  é bug real → fica determinística (`SigUnknown`), falha rápido. `go test ./internal/ci/`
  do advoq: **verde** (mudança é só comentário).

**Veredito:** ✅ o mecanismo central do reaper (detectar + remover um container REAL
de CI órfão) está implementado, testado por EFEITO contra docker real, e verde com
`-race`/lint. 🟡 residual: a liberação de porta ponta a ponta (`...FreesRealPort`) só
foi exercida como SKIP nesta box (iptables WSL2 quebrado) — vira ✅ pleno no 1º run no
runner self-hosted onde o bridge publica porta. Não houve incidente real pós-deploy
para confirmar in-vivo ainda (o reaper ainda não rodou num job-started que tivesse um
órfão de verdade).

**Proxima acao:** rodar `...FreesRealPort` no runner self-hosted (bridge OK) e capturar
um `docker_reap_orphan_ci` com `Path="reaped N orphan CI container(s)"` no
`/var/log/civm/hooks.jsonl` num job-started real; commit na branch fix/orphan-port-reaper
(sem push — review do usuário).

## 2026-06-18 21:30 -03 — Reap de imagens de run no job-completed reduz a FONTE (#137, unit)

**O que:** O `job-completed` so removia imagens DANGLING; as imagens taggeadas de
service que o run buildou (a E2E builda ~35GB/job) nunca saiam e acumulavam na
rajada ate o panic floor. Adicionado `reapRunImages` (`internal/hook/hook.go`):
`docker image prune -a -f --filter label=com.docker.compose.project=<scope>` por
escopo, com `<scope>` = compose project DESTE runner (`<slot>` + `<slot>-<run_id>`,
lidos de `CIVM_RUNNER_SLOT`/`COMPOSE_PROJECT_NAME`). Escopo box-unico por runner
(multi-project-isolation) → sibling nunca tocado; vendor pull (sem label compose)
nunca matched → o "No such image" race do PR #135 NAO volta.

**Dados medidos:**

- TDD RED→GREEN: 9 testes novos (`run_image_reap_test.go`), todos PASS.
- Suite `internal/hook + hostdisk + civm + cleanup` com `-race -count=1`: 4/4 ok.
- Suite `./internal/...` completa: 0 falhas. `go build ./...`: ok.
- `gofmt -l`: limpo. `golangci-lint -c .golangci.yml ./internal/hook ./internal/civm`: 0 issues.
- Guards de seguranca travados por teste: `image prune -a` SO com filtro de label
  (nunca unscoped); sem slot no env → no-op; job-started NAO reapa por label;
  falha de reap → Warning (job-completed segue exit 0).

**Veredito:** 🟡 PARCIAL — concurrency-safety e fail-safes provados por unit, mas o
EFEITO real (V: cai mais devagar sob a mesma rajada; menos `disk_panic` no log) so
vira ✅ apos deploy + medir um ciclo de CI sustentado na box. O panic floor
permanece como salvaguarda permanente (#15), nao substituido.

**Proxima acao:** deploy do civmctl atualizado nos runners (`hook install` ja injeta
o slot); medir `docker system df` do guest antes/depois de uma rajada e contar
`disk_panic` no orchestrator log num periodo comparavel.

**Categoria:** disk-reclaim
**Como medir:** mid-burst no guest: `docker system df`; e no host:
`Select-String disk_panic` no orchestrator log sobre uma janela de CI sustentado.
## 2026-06-19 04:45 -03 — boundary_compact: compactar no GAP entre PRs (sem matar job) ✅/🟡

**O que:** A decisao do orchestrator (`Get-OrchestratorDecision`) so compactava no
idle TOTAL (`Queued==0 + Running==0`); o `panic`/`warn` so agem com `V:` ja em
perigo (< 28 / < 18). Sob fila CONTINUA (PRs back-to-back) a VM ficava `Running` o
tempo todo, nunca idlava, entao o `stop_and_compact` ocioso NUNCA rodava e o VHDX
crescia job-a-job ate o `panic` matar 1 job. Nova acao `boundary_compact` pega o
GAP entre sequencias de PR (Running==0, nada in_progress, mas Queued>0): compacta
de graca porque nenhum job esta rodando.

**Dados medidos:**

- Gap (root cause, medido 2026-06-19): com fila colada a decisao era `mark_busy`
  enquanto (Queued+Running)>0, e o `stop_and_compact` exigia Queued==0+Running==0
  -> a box Running o tempo todo -> VHDX so cresce -> panic (mata job).
- Fix (TDD RED->GREEN): `boundary_compact` na decisao pura — `Running==0 AND
  Queued>0 AND 0<V<BoundaryCompactFloorGB(40)` -> compacta no gap. Fica ANTES do
  `mark_busy` (senao mark_busy engolia, pois (Q+R)>0) e DEPOIS de panic/warn
  (disco critico com job rodando e mais urgente). Reusa `Invoke-StopAndCompact`
  (mesmo lock V:\civm-reclaim.lock + gate pos-Off Test-OptimizeSlack do panic),
  mas SEM cooldown — nenhum job morre, entao nao ha loop a barrar. O panic
  permanece o fallback (nao removido).
- Piso 40 (Kahneman #3, ancorado em dado): folgado sobre warn(28)/panic(18),
  abaixo do admit(51). 51 limpo - 40 = ~11GB consumidos por um PR ja justifica o
  custo do stop/Optimize (~8min cold-start). Banda efetiva real = 28..40 (panic/
  warn ganham < 28 pois testam hasWork=(Q+R)>0; sub-18 o panic compacta igual,
  Running==0 nao mata job — so muda label/cooldown).
- VALIDACAO: decision-table replicada fielmente em Python (cadeia identica ao .ps1)
  -> 38/38 PASS (30 existentes + 8 novos do boundary, zero regressao). Os 4 casos
  exigidos cobertos: (a) R=0/Q>0/V=35 -> boundary; (b) R=0/Q>0/V=45 -> mark_busy;
  (c) R=2/Q>0/V=35 -> mark_busy (job rodando, NAO mata); (d) panic ainda dispara
  V<18. Go `ps1_safety_test` (Int32 clamp) PASS. Balance de braces/parens OK nos 3
  ps1. pwsh INDISPONIVEL no host -> a decision-table .ps1 (38 casos) e o gate
  (10 casos) rodam na box (PS 5.1), igual ao deploy anterior; sintaxe validada
  estaticamente aqui.

**Veredito:** 🟡 implementado, testado (38/38 na simulacao fiel) e comentado, mas
ainda NAO rodado em pwsh real nem disparado num evento ao vivo na box. Vira ✅
pleno quando: (1) `pwsh civm-orchestrator-decision.test.ps1` der 38/38 na box, e
(2) um `disk_boundary_compact` real for capturado no `V:\civm-orchestrator.log`
(VM Running + Running==0 + Queued>0 + V<40). Limitacao honesta: no caso
back-to-back SEM gap (job novo entra in_progress antes de Running chegar a 0), o
boundary nunca dispara e o `panic_compact` segue sendo o fallback que mata 1 job.

**Categoria:** infra-orchestrator (decisao de power-state / disco)

**Como medir:** `pwsh deploy/windows/civm-orchestrator-decision.test.ps1` na box;
e `Select-String boundary_compact V:\civm-orchestrator.log` pro disparo ao vivo.

**Proxima acao:** rodar a decision-table em pwsh na box (38/38); capturar o 1o
`disk_boundary_compact` ao vivo; nota de composicao com o #137 (libera blocos no
guest; boundary recupera o V: via Optimize) ja registrada no SPEC.

---

## 2026-06-21 — civm-disk-gate-per-batch: gate de admissao warm @51 (SPECv4) DEPLOYADO

> Slice `docs/specs/civm-disk-gate-per-batch/` (PRD -> SPEC -> SPECv2 -> SPECv3 ->
> **SPECv4** = contrato; 5 rodadas de Passo 2.5, blockers 7->1->1->0->0). Substitui o
> boundary @40 pelo gate de admissao warm: `Running==0 + Queued>0 + V<AdmitFloorGB(51)
> + attempts<2 -> boundary_compact`, senao `mark_busy`; **precede warn** (panic <18
> antes). Contador anti-deadlock via funcoes puras `Update-AdmitAttempts` /
> `Resolve-AdmitTransition` (DT-9: panic conta so com Running==0).

**VALIDACAO (medida na box, PS 5.1 via powershell.exe — o "pwsh real" que faltava):**
- `civm-orchestrator-decision.test.ps1` -> **59 PASS / 0 FAIL** (47 decision-table +
  10 unitarios + 2 stateful). O stateful roda `Get-OrchestratorDecision` +
  `Resolve-AdmitTransition` REAIS e prova convergencia em **<=2 compacts/episodio** em
  `[18,51)` E `V<18` (DT-9). Antes do fix do teste: 35/3 (so os casos 45/40/25 mudaram
  p/ boundary), confirmando a mudanca sem regressao nos outros 35.
- **AST OK** nos 4 `.ps1` (decision/caller/activate/register); `grep BoundaryCompactFloorGB`
  = 0 no codigo.
- **Dry-run `-Observe`** do caller novo na box (elevado, contexto SYSTEM real:
  Get-VM/Get-RunCount/tokens) -> exit=0; decidiu `mark_busy` (running=1, RF-2 correto).
- **DEPLOY** (activate elevado; mecanismo novo Unregister->copia 2->AST->Register):
  task `state=Ready execLimit=PT2H`; `decision.ps1` deployado com
  `Resolve-AdmitTransition`/`Update-AdmitAttempts` + 0 refs ao param removido; legados
  Disabled; reclaim.lock livre. 1o tick pos-deploy (09:37, V=35, running=1) = `mark_busy`.

**Veredito:** 🟢 **PLENO — implementado + deployado + provado ponta-a-ponta AO VIVO.**
decision-table 59/0 (PS 5.1 REAL) + dry-run `-Observe` + deploy verificado, E o **ciclo
completo capturado no log** (2026-06-21 10:09-10:19): a box estava em death-spiral (V
travado em 29, queued=5-6, running=1 continuo); ao surgir o 1o gap (`running==0`):
`disk_boundary_compact` (V=29, **floor:51**, "sem matar job") -> `guest_full_clean` (54)
-> `reclaim_post_off_remeasure` (41) -> `reclaim_done` (**V=58, recovered=17 GB**) ->
proximo tick `vm_started` (VM Off + V=58>=51 + queued=6 -> admite **LIMPO**). **Death-
spiral quebrado, ZERO job morto** (em isolamento / carga leve).

**TESTE DE CARGA (re-run dos PRs #1197+#1198, ~30 jobs concorrentes SEM o lock de
serializacao, 10:19-11:11) — achado critico:** a **fila FOI respeitada** (os jobs
esperaram o compact e a VM so religou em >=51 -> confirmado, era a pergunta), MAS a box
**NAO sustentou >=51**: o `Optimize` recuperou **~0 GB** (`recovered_gb=0/1/2` ->
`reclaim_no_progress` ERROR x4+; o V so sobe pelo `Stop-VM` e cai a ~50 em 1 batch) -> o
gate **re-compactou a cada batch** (~8min, VM de pe ~2min/ciclo) -> a fila de 30 **travou**
(= regressao de throughput B4 + o gatilho de rollback `reclaim_no_progress` disparando).
**Causa:** sob carga concorrente o disco do guest enche de blocos docker **IN-USE**
(nao-trimaveis) -> `Optimize` nao tem o que recuperar (pipeline de discard quebrado, ver
`host-volume-reclamation` RF-1/RF-2). **Acao:** **revertido o deploy para HEAD (floor 40)**
-> thrash parou, VM volta a ficar de pe (ticks 11:24/26/28 = mark_busy, sem compact),
`running` 0->1, fila drena.

**Veredito final:** 🟡 **logica CORRETA e provada** (decision-table 59/0 + 1o ciclo 17 GB
+ fila respeitada + zero kill), mas **DEPLOY de floor-51-por-batch GATED** em: (a) **lock
de serializacao** (PAID-CI-PARITY §6.2, nao deployado) p/ a carga nao ser 30-concorrente,
**e/ou** (b) **conserto do discard pipeline** (fstrim->VHDX) p/ o `Optimize` de fato
recuperar. Sem (a)/(b), o floor-51 thrasha sob carga. Box atualmente rodando o HEAD (floor 40).

**Categoria:** infra-orchestrator (decisao de power-state / disco) + disk-reclaim

**Como medir:** `pwsh deploy/windows/civm-orchestrator-decision.test.ps1` (59/0);
`Select-String "disk_boundary_compact|disk_below_floor_admitted" V:\civm-orchestrator.log`
pro disparo ao vivo (quando a fila abrir um gap).

**Proxima acao:** a logica do slice esta pronta, mas o **RE-DEPLOY de floor-51 esta
GATED** em (a) lock de serializacao (PAID-CI-PARITY §6.2) e/ou (b) conserto do discard
pipeline (Optimize precisa de fato recuperar). Avaliar tambem um **cooldown no
boundary_compact** (nao re-compactar apos `reclaim_no_progress`) p/ evitar thrash mesmo
sob carga. Hygiene: ~69 ancoras de linha da doc viva. Commit do slice quando o usuario pedir.

---

## 2026-06-21 — disk-gate PER-PR: cooldown + serializacao → thrash ELIMINADO (medido)

> Correcao do achado do teste anterior (thrash por-gap). Duas pecas: (1) **cooldown**
> no `boundary_compact` (`CanBoundaryCompact` via `Test-ReclaimCooldown` +
> `lastBoundaryCompactUtc`, 20min) → 1 compactacao por janela (~= por PR), nao a cada
> gap; (2) **serializacao** ativada (`CIVM_E2E_RUNNER_AVAILABLE=true` → `civmctl lock
> --scope docker-heavy`, 1 docker-heavy por vez) → carga limitada → boundary limpo →
> `Optimize` recupera. decision-table **61 PASS / 0 FAIL** (PS 5.1; +2 casos de cooldown).

**VALIDACAO AO VIVO (re-run #1197+#1198 DE NOVO, 2026-06-21 ~21:40-22:13, COM cooldown +
serializacao):**
| Metrica | Antes (sem fix) | Agora |
| --- | --- | --- |
| `boundary_compact` / ~33min | ~5 (a cada gap) | **2** (1x/janela de cooldown) |
| `reclaim_no_progress` | 4+ | 1 (benigno — V chegou a 58 pelo Stop) |
| `disk_panic` (mata job) | risco | **0** |
| Fila | **travada em 30** | **drenou (3→1→0)** |
| V | death-spiral → 24 | **oscila 48–58, recupera a 58** |

- **Cooldown provado:** ticks 22:00/22:02 (V=48-49<51, gap) com `can_boundary:false`
  (compactou ha <20min) → **NAO re-compactou** → `disk_below_floor_admitted` (admitiu) →
  sem thrash. 22:04 `can_boundary:true` → compactou **1x** → V=58. 1o ciclo recuperou **22 GB**.
- **Serializacao:** `running` baixo (0-1), nao 30 concorrentes; boundary limpo → `Optimize`
  recupera (22 GB) — raiz do `recovered_gb=0` (concorrencia) resolvida.

**Veredito:** 🟢 **thrash eliminado, fila drena, zero job morto, V estavel 48–58 (recupera
a 58)** — medido. **Ressalva ≥51:** durante o cooldown a box admite a **48–50** (em vez de
re-compactar) — trade-off que mata o thrash; nao e "≥51 ao pe da letra sempre", e "~51 com
recuperacao a 58". ≥51-estrito-sempre = thrash. Encurtar o cooldown (ex.: 10min) aperta
p/ ~50-58 se desejado.

**Nota:** `reclaim_no_progress` (recovered_gb<3) pode false-fire quando o `Stop-VM` ja levou
V a ~56-58 e o `Optimize` so adiciona ~2 — V esta OK (58), o sinal mede so o delta do Optimize.

**Proxima acao:** SPECv5 (revisao per-gap→per-PR + cooldown + serializacao); decidir
cooldown 20→10min se quiser ≥51 mais apertado; commit do slice quando o usuario pedir.

---

## 2026-06-22 — disk-gate POR-EVENTO (transicao running>0->0) substitui o cooldown

> **Correcao de SHAPE (feedback do usuario):** o cooldown era por-TEMPO (20min); o usuario
> quer por-EVENTO — **1 compactacao por PR** (no fim), nunca no meio (o tenant-smoke dura
> >1h e roda inteiro). **Medicao** (`docker system df`): docker ~5 GB (1 img 37MB + 53 vols
> 4.9GB); um build do stack ~10-25 GB → **um PR cabe FOLGADO em 58** (meu "nao cabe 58"
> anterior foi chute errado). O que enche e ACUMULACAO + concorrencia, nao o tamanho do PR.

**Mudanca:** gate warm dispara so na **transicao `prevRunning>0 AND running==0`** (PR/onda
acabou) + `queued>0` + `V<51`. `running` preso em 0 (pos-compact) → `prevRunning==0` → sem
transicao → **sem re-compactar** (anti-thrash por-evento, sem timer). `prevRunning` rastreado
no `state` por tick. `Get-RunCount` conta `workflow_runs in_progress` → `running` fica >0 o
PR inteiro e →0 no fim (boundary natural). decision-table **62 PASS / 0 FAIL** (+ stateful
`Test-PrLifecycle`: ciclo (0,2,2,0,0,0) → compacta EXATAMENTE 1x, so na transicao 2→0).

**VALIDACAO AO VIVO — PARCIAL (honesto, Kahneman #13):**
- ✅ **Anti-thrash provado:** pos-deploy, ticks com `running=0, prevRunning=0, V=41<51, queued>0`
  por 10+min → **ZERO compactacoes** (so `disk_below_floor_admitted`). O gate floor-51 antigo
  teria thrashado aqui.
- ⚠️ **Compactar-na-transicao-real NAO observado ao vivo** — ARTEFATO DO TESTE: re-rodei runs
  com `created_at` de **22.6h atras** (ex.: Web CI `2026-06-21T03:32Z`); `Get-RunCount` filtra
  `created_at > 12h` (guarda de staleness) → re-runs antigos **nao sao contados** → orchestrator
  ve `running=0` mesmo com o job rodando → sem transicao. NAO e bug do gate; em producao runs
  frescos (created_at≈now) sao contados. Re-run reusa o created_at original → invalido p/ este teste.
- ✅ **Lock que ativei (`CIVM_E2E_RUNNER_AVAILABLE=true`) NAO quebrou a CI:** Web CI falhou em
  `yarn format:check` (formatacao do PR #1197), hooks civm OK (`job-started/completed`, disk 58%).

**Veredito:** 🟡 gate por-evento **logicamente provado (62/0) + deployado + anti-thrash ao
vivo**; o "compacta 1x na transicao" e o MESMO caminho ja provado (unit + mecanismo de compact
ao vivo no cooldown-era), mas falta uma observacao ao vivo com **run FRESCO** (<12h) p/ fechar.

**Proxima acao:** validar com 1 run fresco (organico ou dispatch) OU aceitar (unit+anti-thrash)
e seguir p/ SPECv5 + commit. Lock fica ativo (bound de concorrencia). Decisao do usuario.

---

## 2026-06-22 — fix Get-RunCount (updated_at) + gate por-evento CONFIRMADO ao vivo 🟢

> Fecha a lacuna do veredito anterior. **Causa do `running=0`:** `Get-RunCount` filtrava por
> `created_at` (re-run reusa o original, 22-25h → cortado pelo cutoff 12h). **Fix:** filtrar
> por `updated_at` (fresco no re-run; fallback created_at), mantendo a guarda de staleness
> (run parado >12h ainda filtrado). Medido: Web CI `created_at=03:32 (25h)` vs `run_started_at
> /updated_at` ~fresco.

**VALIDACAO AO VIVO (re-run #1197+#1198 COM o fix, 2026-06-22 04:30):**
- ✅ **`MAX running = 1`** (antes do fix: 0) → re-runs agora CONTADOS.
- ✅ **`boundary_compact = 1`** (04:30:17), nota nova `"fim do PR (running >0->0)"`, `queued=30`,
  `V=43<51` → **gate por-evento disparou numa TRANSICAO REAL** (running>0→0 com fila aguardando =
  hand-off warm por-PR). 
- ✅ **`reclaim_done recovered_gb=6 → V=57`** (~58). **`panic=0`** — zero thrash, zero job morto.
- ✅ Pos-compact: `running=0, prev_running=0` → **NAO re-compactou** (anti-thrash por-evento);
  04:50 `running=1` de novo (proximo ciclo).

**Veredito:** 🟢 **gate por-evento funciona end-to-end ao vivo** — 1 compactacao na transicao do
PR, V→57, sem thrash, sem kill. Fix `updated_at` torna re-run um teste valido (e conta re-runs
em producao). **Nota:** os PRs em si falham em `yarn format:check` (formatacao do codigo do PR,
NAO da box) — o gate/box estao OK; os PRs precisam de Prettier (problema separado).

---

## 2026-06-22 — CORRECAO: o "lock" estava errado + gate RE-confirmado ao vivo

> Auditoria. As entradas acima (2026-06-21/22) afirmavam que `CIVM_E2E_RUNNER_AVAILABLE=true`
> ativava um `civmctl lock --scope docker-heavy` que "limita concorrencia pesada". **ERRADO.**

- **A variavel NAO e lock.** Ela troca o runner-alvo dos e2e: `=true` → `[self-hosted, civm,
  civm-e2e]`. O runner `civm-e2e` **nao existe** (o runner advoq so tem label `civm`,
  confirmado no listener log) → seta-la **travaria** os e2e queued. Setei por engano (entendi
  como "lock") → **REVERTIDA** (variavel deletada do repo advoq, volta a `[self-hosted, civm]`).
- **Nao precisa de lock:** o advoq tem **1 runner self-hosted** (`civm-advoq-org`) → jobs
  pesados rodam **1 por vez** por natureza. Nao ha "concorrencia pesada" no advoq pra serializar.
  O death-spiral foi o **thrash do gate antigo** (compactava a cada gap) + acumulacao, NAO
  concorrencia. (Commit 82e2844 e a msg dele tambem citam o "lock" — claim errado; nao re-escrito.)
- **Gate RE-confirmado ao vivo (06:32):** `running=1→0, prev_running=1` → `disk_boundary_compact`
  (nota "fim do PR") → reclaim → V 44→52→~58. 2a observacao de transicao REAL (alem da 04:30).
- **Achado operacional:** ~40 re-runs de teste (meus, falhando em format) entupiram o runner
  unico por horas (drain lento + compactacoes repetidas). Limpaveis via cancelamento.

**Veredito final:** 🟢 gate por-evento correto + deployado + confirmado ao vivo 2x; box saudavel;
advoq revertido (sem variavel, sem PR/codigo meu); paridade com pago = ~58/PR, 1 PR por vez,
pelo runner unico (sem lock). Docs (SPECv5 §1/§3) corrigidos.

## 2026-06-23 01:10 -03 — fstrim no full-clean + reclaim_no_progress era falso alarme

> **Categoria:** `disk-reclaim`. Investigacao do `reclaim_no_progress` recorrente
> apos a queda de energia (box auto-recuperou: task tem `-AtStartup`). Diagnostico
> ao vivo via SSH direto no guest (`ssh gha-ubuntu-2404`) + log do orchestrator.

- **Bug 1 — fstrim ausente no full-clean.** `Invoke-GuestFullClean` (o clean que
  roda no boundary, antes do `Stop-VM`+`Optimize-VHD`) NAO chamava `fstrim`, so o
  `Invoke-GuestWarnClean` (que so dispara em `V<28`). Sem TRIM/UNMAP o VHDX
  dinamico nao via os blocos liberados -> Optimize recuperava ~1GB. Fix: `sudo
  fstrim -av` no fim do full-clean (commit `d5556c1`).
- **Dado medido (fstrim funciona):** `sudo fstrim -av` no guest ao vivo (job
  rodando) trimou **11.3 GiB** em `/dev/sda2`; logo apos um boundary clean ja com
  o fix, `fstrim -av` reportou **`/: 0 B`** (nada sobrou) -> o trim do clean pega
  tudo.
- **Achado: o VHDX ja esta no piso real.** Guest `df`: 108G fs, **52G usados**
  (16G runners+`_tool`, **12G `/home/emdev/codespace` = workspace do usuario com
  trabalho nao-commitado, NAO tocado**, 6G go cache, ~12G OS), VHDX=62G. Logo
  `recovered_gb` baixo e ESPERADO; nao ha bloat grande seguro pra recuperar.
- **Bug 2 — `reclaim_no_progress` era falso-vermelho.** Disparava ERROR todo
  boundary mesmo com `v_free=57` (~alvo 58). Fix: `Test-ReclaimStuck` (gate puro,
  testado) so alerta se `recovered<3` E `v_free<51` (preso de verdade); senao loga
  `reclaim_already_compact` INFO (commit `3a4ed93`).
- **Bug 3 — deploy incompleto.** `activate-orchestrator.ps1` copiava 2 dos 3 .ps1
  dot-sourced; o gate ficava de fora. Fix: copia+valida os 3 (commit `073e88e`).
- **Tambem:** SSH ao guest com retry/backoff + fallback p/ IP da VM (transitoria
  `Could not resolve hostname gha-ubuntu-2404` pos-reboot) — mesmo commit do fstrim.

**Dados medidos (antes/depois, mesmo boundary serial):**

| Instante | evento | recovered_gb | v_free_gb | nivel |
| --- | --- | --- | --- | --- |
| 03:44 UTC (pre-fix3) | reclaim_done -> reclaim_no_progress | 1 | 57 | ERROR (falso) |
| 04:09 UTC (pos-fix3) | reclaim_done -> reclaim_already_compact | 2 | 57 | INFO (correto) |

- Estatica: AST OK (orchestrator+gate); gate tests **15/0** (5 casos novos de
  `Test-ReclaimStuck`); decision tests **62/0** (sem regressao); `go test ./...` do
  modulo verde.
- Deploy ao vivo via `sudo.exe powershell` (UAC off, inline): "3 .ps1 copiados +
  validados por AST", `orch_state=Ready`, gate deployado com `Test-ReclaimStuck`
  confirmado.

**Veredito:** 🟢 fstrim no full-clean correto + deployado (impacto pequeno: VHDX ja
no piso real ~52G usados); `reclaim_no_progress` falso alarme **eliminado**
(confirmado ao vivo: `reclaim_already_compact` INFO substituiu o ERROR nas mesmas
condicoes); SSH resiliente; box saudavel `v_free~57`. **Proxima acao:** se o
`v_free` de fato cair < 51 e persistir, o `reclaim_no_progress` volta a disparar
(agora verdadeiro) -> ai sim investigar bloat novo no guest.

## 2026-06-23 04:31 -03 — Pente-fino do guest + V: 72GB + HARDWARE.md definitivo

**O que:** A entrada anterior concluiu "VHDX ja no piso real, sem bloat seguro". Um pente-fino mais fino
contradisse: `/home/emdev/codespace` tinha ~12 GB de **clones manuais stale** (CI debug de 2026-05-10:
`advoq-vmci-m24-*`, `advoq-ci-main-direct`, etc.) que NENHUMA rotina limpava (fora do escopo do
`Invoke-GuestFullClean`/`civmctl cleanup`). Criados por sessoes de IA antigas acessando a box "da forma
antiga" (clonar repo na VM). Removidos com backup previo (bundles/patches dos dirty -> host `~/civm-attic`,
210 MB; os 3 commits "unpushed" do `advoq-ci-main-direct` confirmados ja em advoq/advoq).

**Dados medidos (guest + host V:):**

| Instante | guest `/` usado | V: livre (off) | recovered_gb | VHDX file |
| --- | --- | --- | --- | --- |
| pre-cleanup | 50 GB | 57 | ~2 | 62 |
| pos rm 12GB + `fstrim` | 38 GB | — | — | — |
| pos `boundary_compact` | 38 GB | **72** | **14** | **47** |

- `fstrim` trimou **33.5 GiB** no `/` (blocos liberados que o `Optimize-VHD` nao enxergava).
- `disk_below_floor_admitted`: **259 (cronico) -> 0** desde o compact pos-cleanup (medido no log).
- Hardware definitivo travado em **`docs/HARDWARE.md`**: host Ryzen 5 3600 / **31.9 GB RAM** / V: SSD
  dedicado **119.2 GB**; VM **8 GB RAM** (= o **VMRS de 8 GB** no V:). O "overhead de 8 GB" do V: e o VMRS
  (estado de RAM), liberado quando a VM desliga (`Stop-VM -Force`) — **NAO e cruft deletavel**.

**Veredito:** 🟢 o below-floor era **bloat real** (clones stale), nao limite do decision-gate. Removido ->
V: 57->72, recovered 2->14, below-floor 259->0. O **RF-10** (compactar sempre que V<51) teria **thrashado** a
box (boot materializa ~8 GB de VMRS, Optimize recuperava ~1, o contador resetava a cada compact -> loop
infinito sem rodar job) — **NAO implementado, de proposito** (Kahneman #13: "a funcao existir" nao prova o
efeito). Sustentavel: passo `codespace_stale` adicionado ao `civmctl cleanup` (mtime>7d, espelha `work_old`;
`go test` verde). **Proxima acao:** deploy do `codespace_stale` no guest; piloto ephemeral advoq-org
(clean-slate por job, simulacao serializada do pago) aguardando o front PR da outra sessao.

## 2026-06-24 18:21 -03 — Floor de admissao 70->55 (alcancavel) + compact incondicional por PR

**O que:** Continuacao da sessao interrompida pela queda de energia (17:11 -03). O gate warm do
orchestrator ja estava (pre-outage) compactando INCONDICIONAL 1x por PR na transicao running >0->0
(anti-thrash por-evento via PrevRunning) + floors separados host/guest. A queda interrompeu o sync do
arquivo de teste `civm-orchestrator-decision.test.ps1`. Terminei o sync, e a observacao do log VIVO da
box revelou que o floor host=70 era fisicamente inalcancavel sob CI -> baixei pra 55 (alcancavel). Sync
de teste + alinhamento de comentarios (Kahneman #13) + floor 55 nos 3 `.ps1` (decision/orchestrator/
reclaim-gate) + os 2 testes.

**Dados medidos (log da box 2026-06-24, com o floor=70 ANTES do fix):**

| Instante UTC | evento | v_free | recovered | resultado |
| --- | --- | --- | --- | --- |
| 20:29 | boundary_compact (fim do PR) | 39->67 | 20 | compacta OK, VHDX piso ~52GB |
| 20:37 | reclaim_before_admit (67<70!) | 67 | 0 | compact EXTRA inutil |
| 20:42 | reclaim_no_progress ERROR | 67 | 0 | FALSO-vermelho (67 e saudavel) |
| 20:43 | disk_below_floor_admitted attempts=2 | 67 | — | admite so apos ~6min de spiral |

- Ceiling real sob CI ativo: V: ~67 (o VHDX tem piso ~52GB = dados genuinos + cache `_tool`
  preservado). 70 e inalcancavel sob carga -> spiral de reclaim + `reclaim_no_progress` falso +
  ~6min de atraso por PR (= a lentidao "tnant demora ~10min pra iniciar" reclamada).
- Fix: `AdmitFloorGB` 70->55 (12GB de margem abaixo do ceiling 67 -> admite logo apos compactar).
  `GuestFloorGB` 40 mantido (guest fica ~45-63). `boundary_compact` incondicional mantido (compacta
  todo PR, libera o maximo dos 2 lados: guest_full_clean no Linux + Optimize-VHD no host).
- Testes (via `pwsh.exe` na box): decision **64/0**, reclaim-gate **15/0**, AST ok nos 3 `.ps1`.
- Deploy: 3 `.ps1` copiados pra `C:\civm-deploy` (deployed==source, diff 0); proximo tick (2min) roda
  floor=55. `reclaim.lock=False` no momento do deploy.

**Veredito:** 🟢 sync de teste concluido (o passo que a queda interrompeu) + bug do floor=70 corrigido
por evidencia VIVA, nao por suposicao (Kahneman #13). Floor 55 alcancavel elimina o spiral + o
`reclaim_no_progress` falso. **Proxima acao:** observar 2-3 ticks pos-deploy pra confirmar admissao
direta (V:~67>=55 -> start sem reclaim extra); depois seguir o #1227 ate verde.

## 2026-06-24 19:21 -03 — boundary_compact matava job em voo (running count laggado) -> probe-gate

**O que:** Observando o #1227 pos-fix-do-floor, achei a causa REAL dos "jobs derrubados antes de
terminar" (a queixa original): o `boundary_compact` parava a VM (Stop-VM) quando o running count do
GitHub era 0, mas esse count LAGGA ~30-60s. Um job recem-despachado (em checkout) ainda aparece como
running=0, e o Stop-VM o matava em voo. O comentario do gate dizia "NAO mata job (Running==0)" — FALSO
(Kahneman #13). Fix: o `boundary_compact` agora consulta a probe SSH `Get-GuestHasActiveJob`
(Runner.Worker no guest, verdade em tempo real, fail-safe: SSH falho -> assume ativo) ANTES de parar a
VM. Job ativo -> novo outcome `boundary_aborted_active_job` (nao para a VM). Espelha o gate ja existente
do stop ocioso (`stop_aborted_active_job`).

**Dados medidos (smoking gun, log da box + GitHub API 2026-06-24):**

| Instante UTC | fonte | evento |
| --- | --- | --- |
| 22:12:43 | GitHub job | `govulncheck (ms-billing)` started (runner civm-advoq-org) |
| 22:13:17 | orch log | tick v=40 r=0 prev=1 -> `disk_boundary_compact` -> `reclaim_start` (Stop-VM) |
| 22:13:31 | GitHub job | `govulncheck (ms-billing)` **cancelled** ("operation was canceled") |
| 22:13:34 | orch log | `reclaim_post_off_remeasure` (VM ja off) |

- O Stop-VM do boundary_compact (22:13:17) matou o job que comecara 34s antes (22:12:43). Mesma
  assinatura dos checkouts cancelados de `Trivy` e `changes` (todos "operation was canceled" no checkout).
  O floor=55 NAO resolvia isto — e race do running count, nao disco.
- Fix validado (via `pwsh.exe` na box): decision **66/0** (2 casos novos de probe-gate:
  job ativo na transicao -> `boundary_aborted_active_job`), reclaim-gate **15/0**, AST ok.
- Deploy: 3 `.ps1` -> `C:\civm-deploy` (deployed==source, diff 0); novo outcome wired em decision +
  orchestrator. Proximo tick usa o probe-gate.

**Veredito:** 🟢 race do job-kill fechada por evidencia VIVA (Kahneman #13: o comentario "nao mata job"
nao batia com o efeito real). A probe SSH e a verdade em tempo real; o running count do GitHub nao.
**Proxima acao:** confirmar no log um `boundary_aborted_active_job` (prova o gate disparando) + ausencia
de novos "operation was canceled"; re-rodar os jobs ja cancelados (Trivy/changes/govulncheck) quando as
runs deles terminarem; seguir o #1227 ate verde.

## 2026-06-24 22:24 -03 — #1227 VERDE + os 2 fixes da box provados ao vivo

**O que:** Fechamento. Apos floor=55 + probe-gate, re-rodei os 3 jobs que tinham sido mortos pre-fix
(`changes`, `Trivy`, `govulncheck ms-billing`) e acompanhei o #1227 ate o fim.

**Dados medidos:**

- **#1227: 45 pass / 0 fail / 6 skip** (paid), `mergeable=MERGEABLE`, `mergeStateStatus=CLEAN`. Os 3
  re-runs passaram (a box nao matou mais nenhum). NAO mergeado (decisao do user).
- **Floor=55 provado:** `vm_started` a `v_free=66` (22:21 UTC) sem `reclaim_before_admit` extra nem
  `reclaim_no_progress` falso — sob floor=70 teria spiralado (66<70).
- **Probe-gate provado AO VIVO:** `boundary_aborted_active_job` disparou 2x (23:49 e 00:03 UTC) — a box
  viu Runner.Worker ativo via SSH e ABORTOU o Stop-VM ("preserva o job em voo") em vez de matar o job.
- Zero `reclaim_no_progress` ERROR e zero job morto desde os deploys.

**Veredito:** 🟢 sessao (pos queda de energia) concluida: o passo interrompido (sync de teste) + 2 bugs
reais da box (floor inalcancavel + race do job-kill), achados por evidencia VIVA e provados em producao.
**Proxima acao:** (a pedido do user) projetar a fila FIFO por-PR real (10+ PRs, compact entre cada) —
hoje a box e job-FIFO, nao PR-grouped (RUNNER-SERIALIZATION.md §Residual). Plano comparando 2
arquiteturas em andamento.

## 2026-06-25 05:01 -03 — Gate de fila por-PR provado AO VIVO end-to-end (canary + 7 workflows)

**O que:** A fila FIFO por-PR planejada na entrada anterior foi construida e
validada AO VIVO, fim-a-fim. Cada workflow box-heavy ganhou um job `wait-for-slot`
num runner HOST (`civm-gate`), com os jobs reais em `needs: wait-for-slot`; o
orchestrator `-EnforceQueue` agrupa a atividade por contexto (`pr-<num>` / push
`branch-<ref>`), mantem FIFO, publica o contexto corrente em
`V:\civm-current-context` e, no boundary, faz clean completo + compact do VHDX
antes de avancar. Isso troca o job-FIFO antigo por **1 PR por vez, compactando
entre cada**.

**Dados medidos:**

- **Canary fim-a-fim:** o `wait-for-slot` do canary ficou **segurado atras de
  `branch-main`** e so liberou **apos o compact no boundary** — `FULL-FLOW DONE`
  no log (job `wait-for-slot` success + `canary-work` success), com o
  `canary-work` rodando numa box **recem-limpa** (fresh). Prova o ciclo completo:
  gate segura -> boundary compacta -> gate libera -> job roda no box limpo.
- **Enforce nao matou job em voo:** **zero** `pr_boundary_compact` enquanto o E2E
  rodou (janela **05:39 -> 06:11 UTC**). O `-EnforceQueue` so compacta quando o
  contexto corrente drena — nunca interrompeu um job ativo (ao contrario do
  job-kill por running-count laggado fechado em 2026-06-24).
- **Rollout:** gate `wait-for-slot` rolado aos **7 workflows box-heavy** no
  **PR #1235**; os gates liberaram nos workflows reais (nao so no canary).

**Veredito:** ✅ a fila por-PR funciona ao vivo: o gate segura o PR no host (sem
contender a box), o boundary compacta o VHDX (V: ~67) e so entao a box atende o
proximo contexto. O residual "job-FIFO, nao PR-grouped" da RUNNER-SERIALIZATION.md
esta RESOLVIDO (Kahneman #13: provado por canary fim-a-fim, nao asseverado).
**Proxima acao:** provisionar pool de gate runners + corrigir service-persistence
(erro 1068); mergear #1235 -> main verde (E2E com timeout 120min).
