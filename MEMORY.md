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
