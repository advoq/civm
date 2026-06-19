# Runbook — serializacao de runner do advoq (1 runner por org)

> **Quando usar:** voce ve dois PRs do advoq rodando checks ao mesmo tempo na
> box, ou jobs falhando com `concurrent prune on shared civm runner` /
> `docker pull ... retry` / `The operation was canceled`. Tambem ao
> (re)provisionar a box, para garantir que o invariante de serializacao continua
> imposto.

## O invariante

**A box deve ter NO MAXIMO 1 runner civm-labeled servindo o advoq.** Esse runner
e o **runner ORG** `civm-advoq-org` (registrado contra
`https://github.com/advoq`), que atende `advoq/advoq` E `advoq/civm` num **unico
processo** — serializando a org inteira em fila FIFO. **Nao** pode coexistir um
runner POR-REPO `civm-advoq` (registrado contra `https://github.com/advoq/advoq`).

Os runners pessoais (`civm-vitae`, `civm-advoqwhatsappapi`,
`civm-chatwoot-realtime`, `civm-n8n-engine`, `civm-typebot-runtime`, `civm-self`)
nao sao afetados — cada um serve um owner diferente do advoq.

## Por que (a falha real)

Tanto o runner org quanto o por-repo carregam o label `civm`. Um job de
`advoq/advoq` com `runs-on: [self-hosted, civm]` cai em **qualquer um dos dois**.
Com os dois ativos, GitHub pode despachar **dois jobs de advoq ao mesmo tempo**
na mesma VM — mesmo disco, mesmo daemon Docker. O hook `job-completed` de um job
roda `docker prune` enquanto o outro job faz `docker pull postgres:16-alpine` ->
o pull e abortado:

```
docker pull postgres:16-alpine — retry (concurrent prune on shared civm runner)
The operation was canceled
```

Foi exatamente o que derrubou `ms-billing` e `ms-core` no PR #1184
(validation.md 2026-06-18 20:35). O `govulncheck` dos dois passou — o codigo
compila; **nao era bug de codigo, era contencao de runner**. O deep-clean de
disco (~51 GB livres) nao resolve: o problema e **concorrencia**, nao espaco.

Mantendo so o runner org, advoq nunca roda 2 jobs simultaneos: a fila daquele
unico runner serializa tudo. Medido: `runner busy peak = 1` durante o re-run do
#1184 apos a serializacao.

## Onde fica o provisionamento real

- **Runner ORG (`civm-advoq-org`):** registrado **MANUALMENTE** no nivel da org
  (GitHub Settings > Actions > Runners da organizacao). O `civmctl runner add`
  **nao** cobre runners org — ele so aceita `--repo=owner/repo` e registra contra
  `https://github.com/<owner>/<repo>` (runner POR-REPO). Por isso o invariante
  nao pode ser garantido so pelo `runner add`.
- **Runner POR-REPO (`civm-advoq`):** era registrado pelo runbook
  `ADVOQ-ADOPTION.md` (Passo 1, `civmctl runner add --repo=advoq/advoq`). Esse
  passo foi corrigido para **nao** registrar o por-repo — ver aquele runbook.

## As 4 camadas que impoem o invariante (defense-in-depth)

| # | Camada | O que faz | Onde |
|---|--------|-----------|------|
| 1 | **Guard (deteccao)** | `civmctl doctor` reporta o check `RUNNER_SERIALIZATION` como **critico** quando um runner org + por-repo da mesma org coexistem. Roda na box e no CI `self-hosted-smoke`. | `internal/runner/serialize.go` (`DetectCollisions`, puro) + `internal/doctor` (`checkRunnerSerialization`) |
| 2 | **Watchdog (nao-ressurreicao)** | O runner-watchdog (tick ~2min) **declina** restartar um runner por-repo redundante (`runner-restart-skipped` reason `redundant-repo-runner`). Sem isto, um runner so `disable`d (loaded, inactive/dead) seria ressuscitado a cada tick. | `internal/runner/watchdog.go` (`restartWatchdogRunners`) |
| 3 | **Enforcement (remocao)** | `deploy/windows/serialize-runner.ps1` — idempotente, dry-run default. Le o doctor, e **remove por completo** o runner por-repo redundante via `civmctl runner remove` (svc.sh stop + uninstall + config.sh remove + rm -rf), mantendo o org. | `deploy/windows/serialize-runner.ps1` |
| 4 | **Provisionamento (origem)** | `ADVOQ-ADOPTION.md` deixou de registrar o runner por-repo; documenta o runner org como o caminho Day-0. | `runbooks/ADVOQ-ADOPTION.md` |

### Por que REMOVER e nao `systemctl disable`

O fix manual original foi `sudo systemctl disable
actions.runner.advoq-advoq.civm-advoq.service`. Isso e **fragil**: a unit fica
*loaded*; `runner.List()` ainda a devolve `inactive/dead`, e o
`restartCandidates()` do watchdog a trata como "runner caido" e da `systemctl
restart` — **ressuscitando a colisao** no proximo tick. A camada 2 fecha esse
buraco (o watchdog passou a declinar), mas o estado **duravel** correto e o
runner por-repo **nao existir como service**. Day-0: o runner org torna o
por-repo pura redundancia, entao remove-se de vez (sem shim "disable agora,
deleta depois").

## Como verificar (1 comando)

Pela API do GitHub (busy <= 1 quando serializado):

```bash
# Quantos runners da org estao ocupados agora (deve ser <= 1):
gh api orgs/advoq/actions/runners --jq '[.runners[] | select(.busy)] | length'

# Os runners org registrados (deve haver civm-advoq-org; NAO deve haver civm-advoq aqui):
gh api orgs/advoq/actions/runners --jq '.runners[] | "\(.name) busy=\(.busy) \(.status)"'

# O runner por-repo NAO deve existir no advoq/advoq:
gh api /repos/advoq/advoq/actions/runners --jq '.runners[] | "\(.name) \(.status)"'
# Esperado: vazio (ou so o org, que NAO aparece em /repos/.../runners)
```

Pelo doctor (na box ou via SSH):

```bash
ssh gha-ubuntu-2404 'civmctl doctor --repos=auto --json' \
  | jq '.hook_checks[] | select(.name=="RUNNER_SERIALIZATION")'
# severity "ok" = serializado. "critical" = colisao (rode o enforcement abaixo).
```

## Como impor (enforcement idempotente)

Do host Windows (elevated PowerShell, mesma maquina do orchestrator):

```powershell
# 1. Dry-run (default): mostra o que removeria, sem agir.
powershell -NoProfile -ExecutionPolicy Bypass -File C:\civm-deploy\serialize-runner.ps1

# 2. Aplicar: remove o runner por-repo redundante, mantem o org.
powershell -NoProfile -ExecutionPolicy Bypass -File C:\civm-deploy\serialize-runner.ps1 -Execute
```

Idempotente: sem colisao -> no-op (sai 0). Re-rodar apos remocao -> no-op. Aborta
fail-closed se houver job/build ativo (idle-gate interno do `civmctl runner
remove`).

Equivalente manual no guest (se preferir agir direto, ainda a REMOCAO, nao
disable):

```bash
TOKEN=$(gh api -X POST /repos/advoq/advoq/actions/runners/remove-token --jq .token)
sudo civmctl runner remove --short=advoq --token="$TOKEN" --execute
```

## Residual conhecido

O runner org da **job-serial FIFO**, nao **strict PR-grouping**: dois PRs de
advoq sao serializados job-a-job (nunca 2 jobs ao mesmo tempo), mas os jobs de um
PR podem intercalar com os do outro na fila. Se algum dia for preciso "todos os
jobs de um PR antes de qualquer job do outro", isso exige um gate de PR
adicional (concurrency group por PR no workflow) — fora do escopo da
serializacao de runner. A serializacao resolve a contencao de disco/Docker, que
era a falha real.

## Rollback trigger

Reverter a serializacao (re-registrar o runner por-repo) **somente** se o runner
org provar ser gargalo:

- fila p95 de advoq > 5 min sustentada por 3 dias (medir
  `gh run list --repo advoq/advoq --status queued`); E
- nenhuma recorrencia de `concurrent prune` esperada com a topologia atual.

Nesse caso, a alternativa correta nao e "2 runners no mesmo disco" (a falha
original), e sim **mais um runner org** ou **isolar advoq numa segunda VM** — ver
`MULTI-PROJECT-RUNNER.md` §"Capacity planning" e §"Rollback trigger".

## Historico

- **2026-06-18** — Runbook criado. Origem: incidente #1184 (ms-billing/ms-core
  mortos por `concurrent prune` com 2 runners advoq ativos). Serializacao
  codificada em 4 camadas (guard no doctor, watchdog nao-ressuscita, script de
  enforcement, fix do runbook de adocao).
