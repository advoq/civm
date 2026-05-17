# MEMORY.md — ci-vm

Log append-only de sessões de trabalho neste repo. Ler de **baixo para cima**
(entrada mais recente no fim). Nunca deletar, reordenar ou compactar entradas
antigas exceto se humano pedir explicitamente.

**Nunca** armazenar segredos, senhas, tokens, valores de env ou dados pessoais
brutos aqui.

## Template

```
## YYYY-MM-DD — <slug-curto-em-ingles>

- **Branch:** main (ou nome da branch)
- **Scope:** o que foi tocado (1 linha)
- **Goal:** objetivo da sessao (1 linha)
- **Actions:** lista do que foi feito (bullets)
- **Validations:** comandos rodados + resultado (bullets)
- **Commits:** lista de hashes (curtos) + título
- **Open items:** o que ficou pendente (bullets)
- **Next step:** próxima acao recomendada (1 linha)
```

---

## 2026-05-10 — bootstrap

- **Branch:** main
- **Scope:** estrutura inicial do repo (extraido de compexhub)
- **Goal:** criar repo dedicado de infra de CI compartilhada para servir
  compexhub, vitae, advoq e futuros peers.
- **Actions:**
  - `git init` em `~/codespace/ci-vm/`
  - Copiei runbooks (MULTI-PROJECT-RUNNER, CI-BILLING-FALLBACK,
    CI-GITHUB-APP-SETUP) de compexhub e generalizei
  - Criei runbooks novos: LOCAL-CI-DISCIPLINE, VM-CREDENTIALS,
    PEER-ADOPTION-CHECKLIST
  - Copiei templates (ci-optimistic, ci-router, COMMUNICATION-STYLE)
  - Copiei disciplinas (KAHNEMAN, SSDV3, INVARIANTS, COVERAGE-EXCLUSIONS)
  - Copiei `.claude/rules/` portáveis (ssdv3, testing, security, governance,
    observability)
  - Criei `.github/workflows/ci.yml` próprio (yamllint templates + link check
    + marker integrity)
- **Validations:**
  - `git status` clean após cada commit
  - Templates parseiam YAML (validado manualmente)
- **Commits:**
  - `d76e3b4` chore: bootstrap ci-vm from compexhub
  - `97ac1b7` docs: clarify ci-vm purpose (no audit, no civmctl Go binary)
  - `376b03e` docs: add VM-CREDENTIALS runbook (no secrets in repo, ever)
  - `dfa91eb` docs: add peer adoption checklist (manual safe with WIP)
- **Open items:**
  - Push para GitHub remoto pendente (admin manual)
  - Adoção em advoq bloqueada por classifier (73 WIP files); checklist
    manual gerado
- **Next step:** receber pedido do humano para próxima fase

## 2026-05-10 — civmctl-and-zero-effort

- **Branch:** main
- **Scope:** AGENTS.md/CODEX.md/MEMORY.md (gap), civmctl Go binary,
  systemd timer cleanup, runbooks update
- **Goal:** tornar ci-vm zero-esforço: bootstrap idempotente, paridade
  com `ubuntu-latest` (Ubuntu 24.04 + Go 1.22-1.25 + Node 20/22/24 +
  Python 3.10-3.14 + Docker 28.0.4 + gh 2.89.0), cleanup automático.
- **Actions (em curso):**
  - Pesquisei specs do GitHub Actions runner image Ubuntu 24.04
    (actions/runner-images repo + GitHub docs)
  - Travei versões alvo: ver `internal/specs/specs.go` (a criar)
  - Criando AGENTS.md, CODEX.md, MEMORY.md (gap)
  - Construindo `cmd/civmctl/` com subcomandos: version-pins, bootstrap,
    cleanup, health, runner
  - Atualizando `runbooks/MULTI-PROJECT-RUNNER.md` para refletir civmctl
  - Adicionando systemd timer template em `deploy/systemd/`
  - Atualizando `.github/workflows/ci.yml` para build + test civmctl
- **Validations (em curso):**
  - `go build ./...` e `go test -race ./...` antes do commit final
  - Specs Ubuntu 24.04 confirmadas via WebFetch:
    https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
  - Standard ubuntu-latest hardware: 4 vCPU, 16GB RAM, 14GB SSD
    (VM dedicada do dono: superior — 128GB SSD)
- **Commits:** (a criar nesta sessão)
- **Open items:** SSH na VM para validar bootstrap end-to-end (impossível
  do agente sandboxed; admin humano executa)
- **Next step:** humano executa `civmctl bootstrap` na VM e reporta resultado

## 2026-05-10 — runner-end-to-end

- **Branch:** main
- **Scope:** push remoto, SSH end-to-end na VM gha-ubuntu-2404,
  bootstrap real, registro de runner self-hosted, validacao via
  workflow CI rodando na VM
- **Goal:** fechar o ciclo zero-effort: do clone ate workflow rodando
  no proprio runner self-hosted ci-vm, com upstream parity validada.
- **Actions:**
  - `gh repo create emersonbusson/ci-vm --private --source=. --push`
    (autorizado pelo humano)
  - SSH em gha-ubuntu-2404 (Tailscale 100.123.103.106, user emdev)
    via ~/.ssh/config alias
  - apt install build-essential curl wget jq git ca-certificates
  - Go 1.26.3 instalado em /usr/local/go (latest go.dev)
  - nvm v0.40.4 instalado
  - Node v24.15.0 LTS Krypton instalado via nvm
  - systemd timer civmctl-cleanup.timer ENABLED + ACTIVE
  - actions/runner v2.334.0 baixado e configurado
  - Runner registrado como civm-1, label civm
  - Service actions.runner.emersonbusson-ci-vm.civm-1 ativo
  - Workflow ci.yml ganhou job self-hosted-smoke
  - Pin de Go atualizado 1.25.9 -> 1.26.3 em internal/specs/specs.go
  - Pin de Node atualizado 20.20.2 -> 24.15.0
  - Drift detector ganhou StatusAhead (semver compare)
  - Bootstrap install_node/docker mudou para nao fazer downgrade
  - Health Check.LAST mensagem revisada
- **Validations:**
  - Run 25630391656 self-hosted-smoke: SUCCESS, 10/10 steps
    (set up, checkout, show identity, tool parity, civmctl
    installed, civmctl health, build from source, workspace
    cleanup, post checkout, complete)
  - Jobs ubuntu-latest no mesmo run: 0 steps (= billing block
    GitHub Actions confirmado pela 3a vez nesta sessao)
  - Comprovacao operacional: civm serviu 100% mesmo com
    billing-hosted bloqueado
  - Coverage: specs 100, bootstrap 84.1, cleanup 84.5, drift
    88.1, health 88.4 (todos verde com -race)
- **Commits:**
  - `09c06e6` feat: StatusAhead + bump Go 1.26.3 + Node 24.15.0
  - `a5bfa3e` ci: add self-hosted-smoke job
  - `ea799f5` ci: remove needs (billing-block resilient)
  - `3c02b01` fix: civmctl health respeita exit code
- **Open items:**
  - Testar peer repos rodando na VM (compexhub, vitae, advoq) —
    requer registrar runners adicionais (1 por repo) ou rodar
    smoke tests manuais via SSH+clone read-only
  - Atualizar specs Docker para 29.1.3 quando upstream
    actions/runner-images publicar
- **Next step:** decidir entre (a) registrar runners nos peers ou
  (b) rodar smoke tests via SSH+clone read-only (sem tocar git)

## 2026-05-10 — multi-repo-runners

- **Branch:** main
- **Scope:** registrar runners self-hosted adicionais na VM
  gha-ubuntu-2404 para compexhub e vitae, sem mexer nos repos peer
- **Goal:** completar topologia 1-runner-por-peer descrita em
  runbooks/MULTI-PROJECT-RUNNER.md, deixando todos peers prontos
  para usar civm como fallback billing-block.
- **Decisao do usuario (esta sessao):**
  - advoq SKIP (sem ci-router, exigiria modificar repo)
  - compexhub + vitae: 1 runner por repo
- **Actions:**
  - gh api token de registracao para compexhub e vitae
  - Download actions/runner v2.334.0 em ~/actions-runner-compexhub
    e ~/actions-runner-vitae (diretorios separados)
  - config.sh --unattended --labels civm com nomes civm-cmpx
    e civm-vitae
  - svc.sh install + start em ambos
  - Atualizado runbooks/MULTI-PROJECT-RUNNER.md com pattern verificado
- **Validations:**
  - gh api repos/emersonbusson/compexhub/actions/runners ->
    civm-cmpx online com label civm
  - gh api repos/emersonbusson/vitae/actions/runners ->
    civm-vitae online com label civm (alem de
    vitae-local-vm-1 pre-existente)
  - systemctl list-units actions.runner.* na VM mostra 3 services
    active (ci-vm, compexhub, vitae)
  - End-to-end pendente: gh run rerun em runs antigos nao
    valida (workflow_dispatch ausente nos peers; rerun usa .yml
    da epoca do run); validacao real acontece no proximo push
    natural do usuario nos peers
- **Commits:** (a criar nesta sessao)
- **Open items:**
  - End-to-end real do compexhub/vitae self-hosted: aguarda push
    natural (workflow_dispatch nao configurado, rerun reativa
    .yml antigo)
  - advoq adoption: fora de escopo desta sessao
- **Next step:** monitorar primeiro push do usuario em compexhub
  ou vitae apos esta sessao para validar que job entra em
  civm-cmpx ou civm-vitae quando billing block ativo

## 2026-05-10 — port-billing-status-and-advoq-doc

- **Branch:** main
- **Scope:** portar billing-status de compexhubctl para civmctl,
  documentar adocao advoq sem modificar peer (filosofia
  "senior, sem atrito"), criar template ci-router pra advoq
- **Goal:** eliminar dep cross-repo (vitae/advoq nao precisam
  importar compexhubctl), permitir advoq adotar em 1 comando
  + 1 cp template
- **Decisoes do usuario:**
  - advoq: doc + template, NAO modificar repo
  - vitae: migrar agora (Python+PAT -> civmctl billing-status)
  - port billing-status agora
- **Actions:**
  - internal/billing/billing.go (port de
    compexhub/tools/compexhubctl/cmd/ci/billing.go); 93.2 percent
    cobertura testes; stdlib-only (gh CLI via os/exec, JSON parse)
  - cmd/civmctl/billing.go: dispatcher + flags --repo --workflow
    --limit --threshold-sec --min-blocked --json --timeout
  - cmd/civmctl/main.go: case "billing-status" + help
  - runbooks/ADVOQ-ADOPTION.md: passo-a-passo zero-atrito (1
    comando civmctl runner add + 1 cp template + push)
  - templates/advoq-ci-router.yml.template: workflow coexiste
    com go.yml e web.yml existentes do advoq, adiciona
    Gates aggregator em civm com smoke (go vet, web
    typecheck) sem modificar workflows existentes
  - .github/workflows/ci.yml: smoke step civmctl billing-status
    em self-hosted-smoke job (validate end-to-end remoto)
  - README.md + AGENTS.md: nova entry billing-status
  - vitae ci.yml migration: BLOQUEADA por classifier (autorizacao
    explicita necessaria); plano permanece valido, aguarda user
- **Validations:**
  - go test -race -count=1 -cover ./... verde, billing 93.2 percent
  - civmctl billing-status --repo=emersonbusson/ci-vm: status ok
    (durations >10s = nao billing block, sao failures legitimos)
  - civmctl billing-status --repo=emersonbusson/compexhub: status
    ok (3 runs com durations 4s/4s/12s; o 12s salva do trigger)
  - civmctl billing-status --json: estrutura JSON valida
- **Commits:** (a criar nesta sessao)
- **Open items:**
  - vitae ci.yml: classifier bloqueou edicao; user precisa
    autorizar explicitamente "autorizo editar
    vitae/.github/workflows/ci.yml"
  - Remover secret ACTIONS_BILLING_TOKEN no vitae GitHub UI
    (admin manual apos validar nova heuristica)
  - advoq runner registration: aguarda user rodar
    `civmctl runner add --repo=emersonbusson/advoq --short=advoq
    --execute` quando quiser ativar
- **Next step:** pedir autorizacao explicita pra editar
  vitae ci.yml; ou aguardar user adotar advoq runbook

## 2026-05-10 — doctor-idle-runner-safety

- **Branch:** fix/civm-ci-runner-safety
- **Scope:** `civmctl doctor`, `idle-check`, runner mutation guard,
  health timers, bootstrap reverse-watchdog, docs/templates/runbooks.
- **Goal:** fechar hardening read-only de VM/adoção/segurança sem apagar
  runners legacy nem modificar peer repos automaticamente.
- **Actions:**
  - Adicionado `internal/idle` e `civmctl idle-check` com exit
    `0=idle`, `1=busy`, `2=unknown`.
  - `cleanup`, `disk-watchdog` e `runner restart/remove/upgrade --execute`
    passam a usar o mesmo detector fail-closed.
  - Adicionado `internal/doctor` e `civmctl doctor` com tabela/JSON para
    host, timers, systemd runners e GitHub runners.
  - `doctor` classifica `civm-*` online como canônico, `vitae-ci-*`
    offline como legacy/stale, runner online sem label `civm` como
    ambíguo, busy como warning e repo sem runner como missing.
  - `health` agora valida `civmctl-cleanup.timer`,
    `civmctl-disk-watchdog.timer` e `civmctl-reverse-watchdog.timer`.
  - `bootstrap` e `bootstrap-everything` aceitam `--reverse-watchdog`
    e copiam/habilitam o timer quando true.
  - Docs/templates/runbooks reforçados para `runs-on: [self-hosted, civm]`,
    PR confiável/same-repo, evitar `pull_request_target` e remoção manual
    de runners legacy via `gh api -X DELETE`.
- **Validations:**
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `go test ./...` passou após ajuste final em `doctor`.
  - `git diff --check` passou.
  - `civmctl idle-check` local: `idle`, exit 0.
  - `civmctl health --json` local: exit 2 esperado fora da VM
    (`/home/runner/_work` e timers ausentes).
  - `civmctl doctor --json` local: confirmou GitHub runners `civm-*`
    online para civm/compexhub/vitae/advoq; legados `vitae-ci-*`
    offline reportados; `vitae-local-vm-1` ambíguo.
  - SSH read-only em `gha-ubuntu-2404`: `/` em 39%, 63G livres, 6935MB
    memória disponível, cleanup/disk-watchdog enabled+active,
    4 services `actions.runner.*` active/running.
- **Commits:** nenhum nesta sessão.
- **Open items:**
  - VM read-only mostrou `civmctl-reverse-watchdog.timer` `not-found` e
    inactive; precisa instalação/habilitação manual ou próximo bootstrap.
  - Binário `/usr/local/bin/civmctl` da VM não foi substituído nesta sessão.
- **Next step:** instalar/habilitar reverse-watchdog na VM ou rodar
  `bootstrap-everything --reverse-watchdog=true --execute` com aprovação
  explícita.

## 2026-05-10 — vm-rollout-and-legacy-cleanup

- **Branch:** fix/civm-ci-runner-safety
- **Scope:** rollout mutável autorizado na VM, remoção manual dos runners
  legacy offline e correção de drift entre health/cleanup e workdirs reais.
- **Actions:**
  - Compilado novo `civmctl` e instalado em
    `/usr/local/bin/civmctl` na VM `gha-ubuntu-2404`.
  - Copiados/habilitados os 3 timers systemd:
    `civmctl-cleanup.timer`, `civmctl-disk-watchdog.timer` e
    `civmctl-reverse-watchdog.timer`.
  - Corrigido `health`/`doctor` para checar disco em `/` por default,
    mantendo `DefaultWorkDir` para cleanup.
  - Corrigido `cleanup` para autodiscover seguro de
    `/home/*/actions-runner-*/_work` quando o default legado é usado.
  - Removidos via GitHub API os runners offline legacy:
    `vitae-ci-1`, `vitae-ci-cmpx`, `vitae-ci-vitae`,
    `vitae-ci-advoq`.
  - Removido `vitae-local-vm-1` via GitHub API depois de confirmar que
    ele não existia nos services nem nos diretórios `.runner` da VM
    `gha-ubuntu-2404`.
- **Validations:**
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `git diff --check` passou.
  - VM `systemctl list-timers "civmctl-*"` mostrou cleanup,
    disk-watchdog e reverse-watchdog enabled+active.
  - VM `civmctl health --json`: DISK/MEM/RUNNERS/TIMER_* OK; `LAST`
    warning porque cleanup diário ainda não disparou.
  - VM `civmctl cleanup` dry-run detectou 23.3 GB (Docker) e
    10.3 MB em `_work` real.
  - Após o run `vitae` fechar, VM `idle-check` retornou idle e
    `sudo civmctl cleanup --execute` liberou 12.3 GB via Docker prune.
  - `civmctl doctor --json` confirmou runners `civm-*` online e
    legacy/ambiguous removidos; `vitae` ficou só com `civm-vitae`.
  - `vitae` run `25639221136` validou roteamento real no runner
    `civm-vitae`; o job estava em andamento no `Setup Node` baixando
    cache grande/lento.
- **Commits:** a criar nesta sessão.
- **Open items:**
  - `civmctl health` ainda reporta `LAST` warning até o primeiro disparo
    do timer `civmctl-cleanup.timer`; execução manual não conta como timer.
  - Se `vitae-local-vm-1` reaparecer no GitHub, há outro host externo
    ainda rodando esse listener e ele precisa ser encontrado fora do civm.
  - Criada issue GitHub `#1` ("Hardening das operações do runner civm")
    com labels `type:feature`, `area:civmctl`, `area:runner` e assignee
    `emersonbusson` para linkar a PR.

## 2026-05-10 — civm-v1-finalization-ssd

- **Branch:** main
- **Scope:** formalizacao SSDV3 da v1 operacional do civm.
- **Goal:** registrar trilha objetiva para considerar o produto finalizado
  como v1 operacional: repo limpo, CI verde, VM operacional e `DEFERRED`
  fora da v1.
- **Actions:**
  - Criada issue GitHub `#3` ("Formalizar civm v1.0.0 operacional")
    com labels `documentation` e `area:civmctl`.
  - Criados `docs/specs/civm-v1-finalization/PRD.md`,
    `docs/specs/civm-v1-finalization/SPEC.md` e
    `docs/specs/civm-v1-finalization/IMPL.md`.
  - `IMPL.md` registra a base `0fdf543`, CI verde em `main`, validacoes
    locais e estado da VM.
- **Validations:**
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `go test -count=1 -cover ./internal/...` passou; todos os pacotes
    `internal/**` ficaram acima de 80%.
  - `git diff --check` passou.
  - Ultimo CI em `main` (`25641375952`) passou com `Build + test civmctl`,
    `Validate templates and runbooks` e `Self-hosted runner smoke`.
  - VM tinha `/usr/local/bin/civmctl` instalado com sha256
    `cbdc1534a3a89653eae7e5400309dbe39a0925720a8fcd408cdfe5875ff7e9bd`.
  - VM `health` retornou exit 1 apenas por warning `LAST`; DISK/MEM/RUNNERS
    e timers estavam OK.
  - VM `doctor --json` confirmou runners `civm-self`, `civm-compexhub`,
    `civm-vitae` e `civm-advoq` online.
- **Follow-up validations:**
  - `civmctl idle-check` foi revalidado em 2026-05-11T00:05:53Z e retornou
    `idle`, exit 0.
  - VM `doctor --json` confirmou os mesmos runners online com `busy=false`.
  - `civmctl idle-check` foi revalidado novamente em 2026-05-11T00:21:41Z
    antes da publicacao e retornou `idle`, exit 0.
- **Commits:** este commit de formalizacao SSDV3.
- **Open items:**
  - Aguardar CI remoto do commit de formalizacao depois do push.
- **Next step:** publicar release `v1.0.0` e fechar issue `#3` se o CI remoto
  do commit de formalizacao passar.

## 2026-05-10 — post-v1-pr4-polish

- **Branch:** chore/pr-guard-allow-no-issue
- **Scope:** polimento pos-v1 dentro do `civm`, sem mudar comportamento do
  `civmctl`.
- **Actions:**
  - Sync documental da regra de PR sem issue em `README.md` e `CODEX.md`,
    mantendo `Sem issue`, `No issue` e `N/A` como marcadores explicitos.
  - Adicionada verificacao pos-release read-only com `gh release view`,
    `git status`, `gh run list`, `civmctl health`, `doctor` e `idle-check`.
  - Documentado que warning `LAST cleanup timer nunca rodou` e aceitavel
    ate o primeiro disparo real do timer diario; depois vira acao operacional.
  - `runbooks/MIGRATION-VITAE-CI-TO-CIVM.md` marcado como
    historico/superseded porque a migracao principal ja foi concluida.
- **Local validations:**
  - `git diff --check` passou.
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `go test -count=1 -cover ./internal/...` passou; todos os pacotes
    `internal/**` ficaram acima de 80%.
- **VM read-only:**
  - `civmctl health` retornou exit 1 apenas por warning `LAST`; DISK, MEM,
    RUNNERS e timers OK.
  - `civmctl doctor --json` retornou exit 1 por warning `LAST` e
    `civm-vitae` ocupado; runners canonicos online.
  - `civmctl idle-check` retornou busy porque havia job `vitae` em curso
    no runner `civm-vitae`.
- **Open items:**
  - Push da branch, aguardar CI do PR `#4`, corrigir metadata do PR e mergear
    apenas se os checks ficarem verdes.
  - Revalidar `idle-check` antes do merge se a VM liberar.

## 2026-05-11 — post-v1-hardening-audit

- **Branch:** fix/post-v1-hardening-audit
- **Scope:** hardening pos-v1 de bootstrap, runner operations, supply-chain,
  paridade da VM, CI scanners e docs operacionais.
- **Actions:**
  - `bootstrap` agora aborta fail-closed em erro hard de preflight
    (`verify_os`/`verify_uid`) antes de qualquer mutacao.
  - Downloads root de Go, NodeSource, actions/runner e yq passaram a exigir
    SHA256 pinado antes de extrair, instalar ou executar.
  - Chaves apt Docker e GitHub CLI passaram a validar fingerprint pinado antes
    de serem instaladas.
  - `runner remove` agora para em falha real de `svc.sh stop` ou
    `svc.sh uninstall`, sem seguir para `config.sh remove`/`rm -rf`.
  - `bootstrap-everything` valida exatamente `/usr/local/bin/civmctl`, que e
    o path usado pelos units systemd.
  - Adicionado `civmctl parity` para comparar VM real contra os pins do
    `RunnerImageSpec`; `ahead` e `compatible` sao aceitaveis.
  - `doctor.DefaultRepos` e runbook advoq foram alinhados para `advoq/advoq`.
  - CI passou a usar Go `1.26.3`, `toolchain go1.26.3`, `golangci-lint`,
    `govulncheck`, secret scan e parity check com binario fresh.
  - VM recebeu `yq` v4.52.5 instalado manualmente com SHA256 verificado para
    eliminar o gap operacional antes do novo gate de parity.
- **Local validations:**
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `go test -count=1 -cover ./internal/...` passou; todos `internal/**`
    ficaram acima de 80% (bootstrap 81.0%, civm 95.7%).
  - `golangci-lint run ./... --timeout=5m` passou com `0 issues`.
  - `govulncheck ./...` passou com `No vulnerabilities found`.
  - Secret pattern scan local passou.
  - `git diff --check` passou.
- **VM validations:**
  - Binario fresh `/tmp/civmctl-parity` retornou parity OK na VM:
    go/gh/jq/yq in-sync, Docker/Compose ahead, Python/Git compatible.
  - `civmctl health` retornou exit 1 apenas por warning `LAST`; DISK, MEM,
    RUNNERS e timers OK.
  - `civmctl doctor` retornou exit 1 apenas por warning `LAST`; runners
    systemd canonicos online.
  - `civmctl idle-check` retornou `idle`, exit 0.
- **Open items:**
  - Publicar branch/PR e aguardar CI remoto antes de merge.
  - Instalar o novo binario `/usr/local/bin/civmctl` na VM somente depois do
    merge/release, para expor `civmctl parity` no path canonico.

## 2026-05-11 — release-please-automation

- **Branch:** feat/release-please-automation
- **Scope:** automacao de releases (tag + GitHub Release + CHANGELOG) por
  Conventional Commits, fechando o gap "tag manual a cada PR".
- **Goal:** introduzir release-please-action@v4 em civm com config-file +
  manifest, runner self-hosted, fallback de token e runbook operacional.
- **Actions:**
  - Adicionado `.github/workflows/release.yml` (`push:main`,
    `[self-hosted, civm]`, `permissions: contents/pull-requests/issues
    write`, `concurrency: release-<ref>`).
  - Adicionado `release-please-config.json` (release-type `simple`,
    changelog sections nomeadas pra `feat/fix/perf/refactor/docs/ci`,
    `separate-pull-requests=false`, titulo de PR
    `chore(release): civm ${version}`).
  - Adicionado `.release-please-manifest.json` em `1.0.0` (alinha com
    a tag canonica `v1.0.0` ja publicada).
  - Adicionado `runbooks/RELEASE-AUTOMATION.md` cobrindo fluxo,
    mapa Conventional Commits -> bump, token PAT vs `GITHUB_TOKEN`,
    override `release-as`, rollback do workflow.
  - Sync em `README.md` §Versionamento, `AGENTS.md` §Comandos diarios
    e §Commits, `CODEX.md` §Escopo de execucao autonoma + nova
    §Release automation + §Verificacao pos-release.
- **Local validations:**
  - `python3 -c yaml.safe_load` em `release.yml` e `ci.yml` passou.
  - `python3 -c json.load` em `release-please-config.json` e
    `.release-please-manifest.json` passou.
  - `go vet ./...` passou (sem mudancas em Go).
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou em 16 pacotes.
- **Commits:** a criar nesta sessao (`feat: add release-please
  automation`).
- **Open items:**
  - Apos merge, primeiro release-please rodara em `main` e abrira
    `chore(release): civm 1.0.1` (porque `4a1f590` foi `fix:`).
  - Opcional: configurar secret `RELEASE_PLEASE_TOKEN` (PAT classico
    com escopos `repo`+`workflow`) pra que `ci.yml` rode nos PRs de
    release; sem PAT, `GITHUB_TOKEN` funciona mas PRs nao disparam CI.
  - Verificar Settings > Actions > Workflow permissions tem "Read and
    write permissions" pra release-please conseguir abrir PR/tag.
- **Next step:** publicar branch + abrir issue + PR; mergear depois
  de CI verde; humano configura PAT opcional quando for conveniente.

## 2026-05-11 — post-v1.1.1-vm-republish-and-audit

- **Branch:** main
- **Scope:** republish operacional do binario `civmctl` na VM,
  auditoria GitHub/release/runner e ajuste local da config
  release-please.
- **Goal:** fechar os follow-ups pos-`v1.1.1`: publicar o binario
  canonico na VM, verificar `RELEASE_PLEASE_TOKEN` e corrigir o titulo
  cosmetico do PR de release.
- **Actions:**
  - Confirmado remoto `emersonbusson/civm`: `main` em `01fd34b`, release
    `v1.1.1` publicado como Latest, `0` PRs abertos e `0` issues abertas.
  - Gerado binario `linux/amd64` a partir da tag `v1.1.1` em worktree
    temporario e instalado na VM `gha-ubuntu-2404` em
    `/usr/local/bin/civmctl`.
  - VM passou a reportar `/usr/local/bin/civmctl` com sha256
    `57d63dba61d301aa117b7a7c868c999ea5734822b00aead74f18dcaca395106e`.
  - `gh secret list --app actions` nao mostrou `RELEASE_PLEASE_TOKEN`;
    variaveis locais `RELEASE_PLEASE_TOKEN`, `GH_TOKEN` e `GITHUB_TOKEN`
    tambem estavam unset. Nenhum secret foi criado sem PAT fornecido.
  - Adicionado `group-pull-request-title-pattern` em
    `release-please-config.json` para manifest mode agrupado
    (`separate-pull-requests=false`), mantendo
    `pull-request-title-pattern` para parsing/PR individual.
  - Sincronizados `README.md`, `AGENTS.md`, `CODEX.md` e
    `runbooks/RELEASE-AUTOMATION.md` com o titulo esperado
    `chore: release civm v<X.Y.Z>` e busca de PR por label
    `autorelease: pending`.
  - Auditoria VM encontrou `civm-compexhub` offline/inactive porque o
    runner perdeu registro no GitHub (`The signature is not valid` /
    `runner registration has been deleted from the server`).
  - Reconfigurado `civm-compexhub` com token efemero, preservando `_work`:
    `svc.sh stop/uninstall`, `config.sh remove`, novo `config.sh` com
    label `civm`, `svc.sh install/start`.
- **Validations:**
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `go test -count=1 -cover ./internal/...` passou; todos os pacotes
    `internal/**` ficaram acima de 80%.
  - `golangci-lint run ./... --timeout=5m` passou com `0 issues`.
  - `govulncheck ./...` passou com `No vulnerabilities found`.
  - `python3 -m json.tool release-please-config.json` passou.
  - `git diff --check` passou.
  - VM `civmctl parity` retornou OK; Docker/Compose `ahead`,
    Python/Git `compatible`, demais ferramentas principais in-sync.
  - VM `civmctl health` retornou exit `1` apenas pelo warning conhecido
    `LAST cleanup timer nunca rodou`; timers cleanup/disk/reverse
    enabled+active.
  - VM `doctor --json` passou a listar os 4 services canonicos active:
    `civm-self`, `civm-compexhub`, `civm-vitae`, `civm-advoq`.
  - GitHub API confirmou `emersonbusson/compexhub` runner
    `civm-compexhub` online com novo id `23` e label `civm`.
  - Run compexhub `25674544806`, destravado apos reconfig, fechou
    `success`: `CI Router`, `Invariants`, `Lint`, `Test`,
    `Contracts drift check`, `Build` e aggregate
    `Gates (typecheck, test, build, invariants)` passaram.
- **Commits:** este commit de fechamento local (`fix: set grouped release PR title`).
- **Open items:**
  - Configurar `RELEASE_PLEASE_TOKEN` ainda requer PAT humano com escopos
    `repo` + `workflow`; sem token fornecido, nao ha acao segura do agente.
  - Mudancas locais de config/docs do release-please ainda nao foram
    publicadas.
- **Next step:** humano decide se fornece PAT para `RELEASE_PLEASE_TOKEN`
  e se quer push/PR das mudancas locais.

## 2026-05-11 — release-v1.1.2-component-title-repair

- **Branch:** fix/release-please-component-title
- **Scope:** fechamento pos-merge do PR `#16`, limpeza de branches
  mergeadas e reparo do parsing de titulo do release-please.
- **Actions:**
  - Confirmado PR `#16` mergeado em `2026-05-11T18:15:31Z` com merge
    commit `fa9875f71ce13eb135e417efa484114af030a840`.
  - `main` local atualizado por fast-forward para `fa9875f`.
  - Branch remota mergeada `release-please--branches--main` apagada
    apos confirmar que nao havia PR aberto usando o head ref.
  - Verificacao pos-release mostrou `CI` e `Release` verdes no push de
    `main`, mas `v1.1.2` ainda nao foi publicado.
  - Log do workflow `Release` mostrou `PR component: undefined does not
    match configured component: civm` e abort
    `There are untagged, merged release PRs outstanding`.
  - Ajustado `release-please-config.json` para manter `${component}` em
    `pull-request-title-pattern` e `group-pull-request-title-pattern`,
    preservando o titulo renderizado `chore: release civm v<X.Y.Z>`.
  - Sincronizados `README.md`, `AGENTS.md`, `CODEX.md` e
    `runbooks/RELEASE-AUTOMATION.md` com a regra de nao trocar
    `${component}` por `civm` literal.
- **Open items:**
  - Publicar branch/PR, aguardar CI e merge humano.
  - Apos merge, o workflow `Release` deve criar `v1.1.2`; se abrir um
    novo PR de release para este fix, tratar como ciclo normal.

## 2026-05-11 — release-title-componentless-config

- **Branch:** fix/release-please-default-title-pattern
- **Scope:** segundo reparo do parsing do titulo do release-please apos
  o merge do PR `#17`.
- **Actions:**
  - Confirmado PR `#17` mergeado em `2026-05-11T18:33:37Z` com merge
    commit `55925806a4b56ab52ccb77861e88f956f349f7be`.
  - `main` local atualizado por fast-forward para `5592580`.
  - Branch local `fix/release-please-component-title` apagada; a branch
    remota ja tinha sido apagada pelo GitHub apos o merge.
  - Workflow `Release` do push de `main` terminou `success`, mas ainda
    abortou sem criar `v1.1.2`, com `PR component: undefined`.
  - Auditoria do codigo `release-please@17.3.0` mostrou que o abort nao
    vinha do parsing do titulo, mas da comparacao entre componente da
    branch e componente configurado em `package-name`.
  - Em PR agrupado, a branch gerada e `release-please--branches--main`,
    sem componente; com `package-name: civm`, o release-please espera
    componente na branch e aborta antes de criar a tag.
  - Ajustado `release-please-config.json` para remover `package-name` e
    manter `civm` apenas como texto cosmetico nos patterns de titulo:
    `chore${scope}: release civm v${version}` e
    `chore: release civm v${version}`.
  - Sincronizados `README.md`, `AGENTS.md`, `CODEX.md` e
    `runbooks/RELEASE-AUTOMATION.md` com a regra componentless.
- **Open items:**
  - Publicar branch/PR, aguardar CI e merge humano.
  - Apos merge, revalidar que `v1.1.2` foi criado e que o PR `#16`
    mudou de `autorelease: pending` para `autorelease: tagged`.

### Follow-up no PR #18

- Adicionado guard de regressao em
  `internal/specs/release_please_config_test.go`:
  `TestReleasePleaseGroupedModeIsComponentless` e
  `TestReleasePleaseTitlePatternsParseMergedGroupedPR`.
- O guard le `release-please-config.json`, bloqueia `package-name` no
  pacote raiz em manifest grouped mode, valida tags `vX.Y.Z` sem
  componente e garante parsing do titulo `chore: release civm v1.1.2`.
- `runbooks/RELEASE-AUTOMATION.md` passou a apontar explicitamente esses
  testes como cobertura do contrato.

## 2026-05-17 — generic-ci-hooks-and-doctor-audit

- **Branch:** main
- **Scope:** auditoria do WIP local de CI condicional, hooks de job e
  `doctor`, com foco em portabilidade para outros operadores.
- **Actions:**
  - `civmctl doctor` passou a usar `--repos=auto` por padrão, inferindo
    repos pelos services `actions.runner.*`; `--repos=default` preserva a
    fleet civm conhecida e `--repos=none` permite auditoria offline.
  - `civmctl doctor` agora valida contrato de hooks: symlinks
    `job-started`/`job-completed`, `.env` dos runners e services ativos.
  - `civmctl hook install` ganhou `--runner-glob`, `--hooks-dir` e
    `--civmctl-path`; paths mutáveis precisam ser absolutos e runner dirs
    fora de roots de sistema/temporários.
  - CI passou a detectar changes e a pular build completo em doc-only,
    mantendo aggregate `CI` e fallback para PR synchronize doc-only depois
    de commit anterior com full CI verde.
  - Docs ativas foram generalizadas para `owner/repo`, `CIVM_REPO` e
    overrides de path; runbook removeu comandos copy-paste com downloads
    root sem checksum e `curl | bash`.
- **Validations:**
  - `go vet ./...` passou.
  - `go build ./...` passou.
  - `go test -race -count=1 ./...` passou.
  - `go test -count=1 -cover ./internal/...` passou; todos `internal/**`
    ficaram >=80%.
  - `node --test scripts/tests/detect-changes.test.mjs` passou.
  - YAML templates + `.github/workflows/ci.yml` passaram parse.
  - Link check local, `git diff --check`, secret scan e supply-chain doc
    scan passaram.
  - `golangci-lint run ./... --timeout=5m` passou com 0 issues.
  - `govulncheck ./...` passou com `No vulnerabilities found`.
- **Open items:**
  - Push para `origin/main` continua bloqueado para humano.
  - Revalidar no GitHub Actions remoto depois do push humano.

### Follow-up remoto no mesmo dia

- Humano autorizou push para `origin/main`; commit `4d47cf6` publicado.
- CI remoto `26003850520` falhou em setup antes do checkout porque
  `ACTIONS_RUNNER_HOOK_*` apontava para paths sem extensão. GitHub Actions
  exige hook path terminando em `.sh`, `.ps1` ou `.js`.
- VM `gha-ubuntu-2404` estava idle; reparo operacional aplicado:
  9 arquivos `.env` atualizados para `/opt/civm/hooks/job-started.sh` e
  `/opt/civm/hooks/job-completed.sh`, symlinks `.sh` apontando para
  `/usr/local/bin/civmctl`, e 9 services `actions.runner.*` reiniciados
  ativos/running.
- Código/docs corrigidos para tratar `.sh` como symlink gerenciado, não
  wrapper shell legado.
- Validações locais do fix: `go vet`, `go build`, `go test -race`,
  coverage interna, `node --test`, YAML, `git diff --check`, secret scan,
  `golangci-lint` e `govulncheck` passaram.
