# ci-vm — infraestrutura de CI compartilhada

Repo dedicado para a infraestrutura de CI/CD que serve múltiplos
projetos do mesmo dono (compexhub, vitae, advoq, etc).

**O que ci-vm É:**

- Repo de infra que **hospeda configuração da VM self-hosted**
  registrada como GitHub Actions runner com label `vitae-ci`.
- Quando GitHub Actions atribui um job ao label `vitae-ci`, o runner
  na VM executa **exatamente o .yml do peer repo**, igual ubuntu-latest
  faria.
- **Provisionamento automatico via `civmctl`** (Go binary deste repo):
  paridade com Ubuntu 24.04 LTS, Go 1.22-1.25, Node 20/22/24, Python
  3.10-3.14, Docker 28.0.4, gh 2.89.0. Bootstrap idempotente.
- Distribui templates de workflow + runbooks operacionais + disciplinas
  metodológicas + regras granulares que peers podem **copiar** para
  seus próprios repos.

**O que ci-vm NÃO é:**

- ❌ Não é uma camada de "audit". GitHub Actions não audita código por
  si só — só roda o que está no .yml. Se um peer quer audit de estilo,
  adiciona step `eslint .` ou ferramenta-do-projeto no próprio .yml.
- ❌ Não é uma plataforma de orquestração custom. ci-vm é só onde a
  VM mora; orquestração é GitHub Actions padrão.
- ❌ `civmctl` **não faz audit**. Faz provisioning + maintenance da VM
  (bootstrap idempotente, cleanup automatizado, health check, runner
  registration). Discipline checks ficam no projeto do peer.

## Bootstrap em 1 comando (zero-effort)

Numa VM Ubuntu 24.04 LTS limpa, como root:

```bash
git clone https://github.com/emersonbusson/ci-vm.git /opt/ci-vm
cd /opt/ci-vm
go build -o /usr/local/bin/civmctl ./cmd/civmctl
sudo civmctl bootstrap --execute
sudo cp deploy/systemd/civmctl-cleanup.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-cleanup.timer
civmctl health
```

Detalhes em `runbooks/MULTI-PROJECT-RUNNER.md` §"Setup zero-effort".

## Comandos civmctl

| Comando | Função |
|---|---|
| `civmctl version-pins` | imprime versoes alvo (paridade com `ubuntu-latest`) |
| `civmctl bootstrap [--execute]` | provisiona VM (default: dry-run) |
| `civmctl cleanup [--execute]` | limpa Docker, /tmp, _work, apt cache |
| `civmctl health` | health check (disk, mem, runners, ultimo cleanup) |
| `civmctl runner add` | registra runner GitHub Actions self-hosted |
| `civmctl drift` | compara pins locais vs upstream actions/runner-images (HTTP fetch) |

PRD/SPEC/IMPL: `docs/specs/civmctl/`.

## Estrutura por audiência

### Para quem **mantém ci-vm** (admin do repo)

| Arquivo | Função |
|---|---|
| `README.md` | este arquivo |
| `.github/workflows/ci.yml` | próprio CI: yamllint nos templates, link check |
| `.gitignore` | exclude common artifacts |

### Para quem **administra a VM** (sysadmin do vitae-ci)

| Arquivo | Função |
|---|---|
| `runbooks/MULTI-PROJECT-RUNNER.md` | provisionar VM + N runners + tools (parity ubuntu-latest) + cron de cleanup (128GB SSD) |
| `runbooks/LOCAL-CI-DISCIPLINE.md` | filosofia "local CI é gate de verdade, remoto é mirror" |

### Para **peer repos** — **copiar**

| Arquivo | Como adotar |
|---|---|
| `templates/ci-optimistic.yml.template` | `cp ... .github/workflows/ci.yml` no peer; substituir placeholders |
| `templates/ci-router.yml.template` | idem, versão Tier 1 com router |
| `templates/COMMUNICATION-STYLE.md` | copiar bloco entre marcadores BEGIN/END pra CLAUDE/AGENTS/CODEX do peer |
| `runbooks/CI-BILLING-FALLBACK.md` | leia para entender as 3 camadas de fallback (referência, não copy) |
| `runbooks/CI-GITHUB-APP-SETUP.md` | rota de upgrade futuro (referência) |

### Para **peer repos** — **referenciar (não copiar)**

| Arquivo | Função |
|---|---|
| `disciplines/KAHNEMAN-DISCIPLINES.md` | 12 disciplinas Sistema 1 vs 2 — referência metodológica |
| `disciplines/SSDV3-PROMPTS.md` | Spec-Driven Dev V3 — prompts copiáveis |
| `disciplines/INVARIANTS.md` | catálogo de invariantes (cada peer escolhe quais adotar) |
| `rules/ssdv3.md`, `rules/testing.md`, `rules/security.md`, `rules/governance.md`, `rules/observability.md` | granular rules `.claude/rules/*` portáveis |

## Como o vitae-ci runner funciona

1. **Setup uma vez** seguindo `runbooks/MULTI-PROJECT-RUNNER.md`:
   - Provisionar VM Linux (Ubuntu 22.04+, 4+ cores, 128GB SSD)
   - Instalar toolchains (Go, Node, Docker, gh CLI, etc) — parity ubuntu-latest
   - Registrar N runners GitHub com label `vitae-ci`
   - Configurar cron de cleanup diário (disk hygiene)

2. **Cada peer repo** referencia `runs-on: [self-hosted, vitae-ci]` em
   seu próprio `.github/workflows/ci.yml`.

3. **Quando billing GitHub OK:** workflow roda em `ubuntu-latest`
   (GitHub-hosted, paga minutos). Quando billing bloqueado: roteia
   para `vitae-ci` (sem custo). Detector heurístico documentado em
   `runbooks/CI-BILLING-FALLBACK.md`; templates implementam o roteamento.

4. **PR continua sendo criado** de onde o dev quiser (laptop, gh CLI,
   GitHub UI). vitae-ci só executa CI; não cria PRs.

## Adoção em 1 comando (para peer repos)

Não tem 1-comando-mágico — adoção é manual, peer repo decide o que
faz sentido:

```bash
# 1. Copiar template de workflow (escolher tier)
cp ~/codespace/ci-vm/templates/ci-optimistic.yml.template \
   <peer>/.github/workflows/ci.yml
# Editar para substituir placeholders pelos gates reais do peer

# 2. Copiar snippet COMMUNICATION-STYLE
# (copiar bloco entre marcadores BEGIN/END em
#  ~/codespace/ci-vm/templates/COMMUNICATION-STYLE.md
#  pra CLAUDE.md, AGENTS.md, CODEX.md do peer)

# 3. Configurar branch protection no GitHub
# Settings > Branches > main > require status check:
#   "Gates (typecheck, test, build, invariants)"
```

Audit/discipline-checks ficam no projeto do peer (cada um com sua
própria ferramenta — ex.: compexhub tem `compexhubctl`).

## Versionamento

Tags semver opcionais (v1.0.0, v1.1.0). Peer repos podem travar em
versão se quiserem (ex.: `git checkout v1.2.0` antes de copiar
templates).

## Histórico

- **2026-05-10** — bootstrap inicial. Estrutura: 4 runbooks + 3
  templates + 4 disciplines + 5 rules + próprio CI. Repo extraído de
  compexhub conforme `docs/proposals/CI-VM-EXTRACTION.md`.
