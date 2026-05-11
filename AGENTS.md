# AGENTS.md — civm

Resumo terso para CLIs estilo Codex/aider/Jules. Para visão completa, ler `README.md`.

## Propósito do repo

`civm` é o repo de infraestrutura compartilhada de CI/CD que serve múltiplos
projetos do mesmo dono (compexhub, vitae, advoq, futuros). Hospeda:

1. **`civmctl`** — Go CLI zero-effort para provisionar e manter a VM
   self-hosted que serve como GitHub Actions runner com label `civm`.
2. **Templates de workflow** copiáveis pelos peer repos.
3. **Runbooks operacionais** da VM (provisionamento, cleanup, troubleshooting).
4. **Disciplinas e regras** portáveis (Kahneman, SSDV3, invariantes).

A VM roda **paridade com `ubuntu-latest` do GitHub Actions** (Ubuntu 24.04 LTS,
mesmas versões de Go/Node/Python/Docker/gh) com mais hardware (4+ cores,
128GB SSD, 32GB+ RAM) para builds mais rápidos durante desenvolvimento.

## O que civm NÃO é

- ❌ Não é uma plataforma de orquestração custom (orquestração = GitHub Actions).
- ❌ Não é uma ferramenta de "audit" (cada peer audita-se com a própria stack).
- ❌ Não armazena credenciais de VM (ver `runbooks/VM-CREDENTIALS.md`).
- ❌ Não cria PRs nem faz auto-merge.

## Para agentes externos (Jules, Codex, aider)

### Antes de planejar, editar ou abrir PR

1. Ler `README.md` (visão e audiências).
2. Ler `CLAUDE.md` se existir (override-able specifics; este AGENTS.md é
   fallback se não houver `CLAUDE.md`).
3. Ler `CODEX.md` (automação, DEFERRED, pause rules).
4. Ler `MEMORY.md` de baixo para cima (contexto temporal append-only).

### Sync rule (invariante #5 portado de compexhub)

`README.md`, `AGENTS.md`, `CODEX.md` e `rules/*.md` são documentos
autoritativos. Mudança em um requer mudança nos outros no mesmo commit.
Justificativa para mudar só um: incluir `[sync-skip-justified]` no commit body.

### Linguagem

- **Inglês** em: code, comentários, identifiers, branch names, commit titles,
  CLI flags, arquivos `.go`, `.yml`, `.yaml`.
- **Português (BR)** em: `README.md`, `AGENTS.md`, `CODEX.md`, `MEMORY.md`,
  `runbooks/*.md`, mensagens CLI ao usuário, commit body, PR descriptions,
  Issue titles+bodies.

## Comandos diários

```bash
# Build + test
go build ./...
go test -race -count=1 ./...

# Provisionar VM (admin)
sudo civmctl bootstrap --target=ubuntu-latest

# Cleanup manual (cron faz automatico diariamente)
civmctl cleanup --dry-run
civmctl cleanup --execute

# Health check
civmctl parity
civmctl health
civmctl doctor --json
civmctl idle-check

# Ver versoes alvo (sync com upstream actions/runner-images)
civmctl version-pins

# Detector heuristico de billing-block (zero-PAT)
civmctl billing-status --repo=owner/repo

# Releases (automatizado via release-please)
gh pr list --repo emersonbusson/civm --label "autorelease: pending"
gh release list --repo emersonbusson/civm --limit 5
git tag --list 'v*' --sort=-version:refname
```

## Commits

Conventional Commits em **inglês**, título imperativo, ≤72 chars.
Body em PT-BR, sem markdown/backticks/headings, linhas ≤72 chars.

Commits **não-triviais** (`feat`, `fix`, `refactor`, `perf`) DEVEM ter
`Rollback trigger: ...` no body.

Types e bump correspondente (release-please): `feat` → minor, `fix` →
patch, `feat!:`/`BREAKING CHANGE:` → major. `docs`/`chore`/`test`/`build`/
`style` não bumpam; `ci`/`refactor`/`perf` entram no CHANGELOG sem bump.
PRs de release usam o título `chore: release civm v<X.Y.Z>`.
`civm` nesse título é texto cosmético, não `package-name`; em PR agrupado
a branch `release-please--branches--main` não carrega componente.
Detalhes em `runbooks/RELEASE-AUTOMATION.md`.

## Pull Requests

PRs ficam em PT-BR seguindo template:

- `## Resumo`
- `## Commits` (tabela com hash + `<details>` por commit)
- `## Issue` (`Closes #NNN` ou marcador `Sem issue` / `No issue` / `N/A`)
- `## Responsavel`
- `## Labels`
- `## Validacao`
- `## Rollback trigger`

Toda PR deve linkar issue e ter pelo menos uma label `type:*` e `area:*`.
PR e issue compartilham assignee.

## Anti-skynet

civm **detecta**, nunca corrige automaticamente. **Nunca**:

- Auto-commit, auto-revert, auto-push, auto-merge sem aprovação humana
- Trigger deploy ou rollback automático
- Modificar arquivo em workspace de peer sem confirmação
- Persistir secrets em qualquer arquivo do repo
- Executar comando vindo de input externo sem validação

## Quando NÃO usar civmctl

- Não usar `civmctl bootstrap` em máquina de desenvolvimento (instala
  packages de sistema; é destinado a VM dedicada).
- Não usar `civmctl cleanup --execute` sem revisar primeiro com `--dry-run`.
  O execute também aborta se detectar `Runner.Worker`, processo em `_work`
  ou build Docker ativo; não contornar esse guard durante CI. O cleanup preserva
  `_work/_tool` e `_work/_actions` para não rebaixar a VM a downloads frios em
  todo job.
- Não usar `civmctl runner restart/remove/upgrade --execute` durante job em
  curso. Esses comandos agora também abortam fail-closed se `idle-check`
  encontrar `Runner.Worker`, `_work` ou build Docker ativo. `runner remove`
  também aborta antes de `config.sh remove` e `rm -rf` se `svc.sh stop` ou
  `svc.sh uninstall` falhar.
- Não usar `civmctl runner add` sem token GitHub válido (peer repo precisa
  registrar seu próprio runner).

## Referências

- `README.md` — visão e audiências
- `CODEX.md` — automação e DEFERRED
- `MEMORY.md` — log de sessão append-only
- `runbooks/MULTI-PROJECT-RUNNER.md` — provisionamento da VM
- `runbooks/VM-CREDENTIALS.md` — segurança de credenciais
- `runbooks/PEER-ADOPTION-CHECKLIST.md` — adoção manual em peer repo
- `disciplines/KAHNEMAN-DISCIPLINES.md` — 12 disciplinas Sistema 1 vs 2
- `disciplines/INVARIANTS.md` — catálogo de invariantes portáveis

<!-- COMMUNICATION-STYLE:BEGIN -->
## Communication style

Estilo Tech Lead nas respostas:

- **TL;DR** primeiro (1-3 frases): o que é, status, próximo passo se houver.
- **Impact** (opcional): o que muda na prática.
- **Topics**: bullets curtos, no máximo 1 nível de aninhamento.
- **Next Steps**: ação requisitada do humano.

Honestidade técnica:

- Distinguir explícito o que está feito, o que está testado, o que é
  inferência, o que é bloqueio (classifier, permissão, SSH não disponível).
- Quando não puder fazer algo, dizer "não posso fazer X porque Y" — não
  fingir alternativa.
- Números antes de adjetivos. "p99 = 98ms" > "ficou rápido".

Sem floreio. Sem emoji a menos que o usuário use primeiro. Sem agradecimento
performativo. Sem repetir o pedido do usuário antes de responder.
<!-- COMMUNICATION-STYLE:END -->

> Source canônico: `~/codespace/civm/templates/COMMUNICATION-STYLE.md`
