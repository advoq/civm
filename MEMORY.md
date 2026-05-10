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
  - Runner registrado como vitae-ci-1, label vitae-ci
  - Service actions.runner.emersonbusson-ci-vm.vitae-ci-1 ativo
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
  - Comprovacao operacional: vitae-ci serviu 100% mesmo com
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
  para usar vitae-ci como fallback billing-block.
- **Decisao do usuario (esta sessao):**
  - advoq SKIP (sem ci-router, exigiria modificar repo)
  - compexhub + vitae: 1 runner por repo
- **Actions:**
  - gh api token de registracao para compexhub e vitae
  - Download actions/runner v2.334.0 em ~/actions-runner-compexhub
    e ~/actions-runner-vitae (diretorios separados)
  - config.sh --unattended --labels vitae-ci com nomes vitae-ci-cmpx
    e vitae-ci-vitae
  - svc.sh install + start em ambos
  - Atualizado runbooks/MULTI-PROJECT-RUNNER.md com pattern verificado
- **Validations:**
  - gh api repos/emersonbusson/compexhub/actions/runners ->
    vitae-ci-cmpx online com label vitae-ci
  - gh api repos/emersonbusson/vitae/actions/runners ->
    vitae-ci-vitae online com label vitae-ci (alem de
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
  vitae-ci-cmpx ou vitae-ci-vitae quando billing block ativo
