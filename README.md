# civm — infraestrutura de CI compartilhada

Repo dedicado para a infraestrutura de CI/CD que serve múltiplos
projetos do mesmo dono (compexhub, vitae, advoq, etc).

**O que civm É:**

- Repo de infra que **hospeda configuração da VM self-hosted**
  registrada como GitHub Actions runner com label `civm`.
- Quando GitHub Actions atribui um job ao label `civm`, o runner
  na VM executa **exatamente o .yml do peer repo**, igual ubuntu-latest
  faria.
- **Provisionamento automatico via `civmctl`** (Go binary deste repo):
  paridade com Ubuntu 24.04 LTS, Go 1.22-1.26, Node 20/22/24, Python
  3.10-3.14, Docker 28.0.4, gh 2.89.0. Bootstrap idempotente.
- Distribui templates de workflow + runbooks operacionais + disciplinas
  metodológicas + regras granulares que peers podem **copiar** para
  seus próprios repos.

**O que civm NÃO é:**

- ❌ Não é uma camada de "audit". GitHub Actions não audita código por
  si só — só roda o que está no .yml. Se um peer quer audit de estilo,
  adiciona step `eslint .` ou ferramenta-do-projeto no próprio .yml.
- ❌ Não é uma plataforma de orquestração custom. civm é só onde a
  VM mora; orquestração é GitHub Actions padrão.
- ❌ `civmctl` **não faz audit**. Faz provisioning + maintenance da VM
  (bootstrap idempotente, cleanup automatizado, health check, runner
  registration). Discipline checks ficam no projeto do peer.

## Bootstrap em 1 comando (zero-effort)

Numa VM Ubuntu 24.04 LTS limpa, como root:

```bash
git clone https://github.com/advoq/civm.git /opt/civm
cd /opt/civm
go build -o /usr/local/bin/civmctl ./cmd/civmctl
sudo civmctl bootstrap --execute
sudo cp deploy/systemd/civmctl-*.service deploy/systemd/civmctl-*.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-cleanup.timer civmctl-disk-watchdog.timer civmctl-reverse-watchdog.timer
civmctl parity
civmctl health
```

Detalhes em `runbooks/MULTI-PROJECT-RUNNER.md` §"Setup zero-effort".

## Comandos civmctl

| Comando | Função |
|---|---|
| `civmctl version-pins` | imprime versoes alvo (paridade com `ubuntu-latest`) |
| `civmctl parity [--json]` | valida ferramentas instaladas na VM contra os pins autoritativos |
| `civmctl bootstrap [--execute]` | provisiona VM (default: dry-run) |
| `civmctl cleanup [--execute]` | limpa Docker, /tmp, artefatos antigos de _work e apt cache; preserva `_work/_tool` e `_work/_actions`; em `--execute` aborta se detectar job/build ativo |
| `civmctl health` | health check (disk, mem, runners, ultimo cleanup) |
| `civmctl doctor [--json]` | visão read-only consolidada: host, timers, systemd runners e GitHub runners |
| `civmctl idle-check [--json]` | detector read-only de ociosidade: exit `0=idle`, `1=busy`, `2=unknown` |
| `civmctl runner add` | registra runner GitHub Actions self-hosted (mkdir + curl + tar + config.sh + svc.sh install + start) |
| `civmctl runner remove` | desregistra runner; aborta antes de `config.sh remove`/`rm -rf` se stop/uninstall falhar |
| `civmctl drift` | compara pins locais vs upstream actions/runner-images (HTTP fetch) |
| `civmctl billing-status` | detector heuristico de billing-block (zero-PAT, GITHUB_TOKEN suficiente) |
| `civmctl peer-status` | status read-only de adoção/saúde por peer ou fleet: billing, runners online e último run; `--repos=owner/a,owner/b` retorna exit `0=ok`, `1=warn`, `2=critical` |
| `civmctl runner list` | lista runners systemd na VM (parsed; suporta `--json`) |
| `civmctl runner restart` | systemctl restart por --short ou --unit; verifica is-active após delay |
| `civmctl runner upgrade` | upgrade in-place de versão (preserva .runner/.credentials/_work) |
| `civmctl reverse-watchdog` | alerta se disk-watchdog timer parou de disparar (>2h default) |
| `civmctl bootstrap-everything` | wrapper: cp systemd units + daemon-reload + bootstrap |
| `civmctl disk-watchdog` | dispara cleanup agressivo se disk >threshold (default 80%); fail-closed se a VM não estiver ociosa |
| `civmctl ci local-report` | posta commit status via gh api (cross-peer manual reporter) |

### Adicionar runner pra novo peer (1 comando)

```bash
TOKEN=$(gh api -X POST /repos/<owner>/<repo>/actions/runners/registration-token --jq .token)

# Dry-run primeiro:
civmctl runner add --repo=<owner>/<repo> --token=$TOKEN --short=<short>

# Aplicar:
civmctl runner add --repo=<owner>/<repo> --token=$TOKEN --short=<short> --execute
```

Substitui sequencia manual de mkdir + curl + tar + config.sh + svc.sh.
Token mascarado nos logs (mostrado como `***`). Detalhes em
`runbooks/MULTI-PROJECT-RUNNER.md` §"Pattern: 1 runner por peer-repo".
Downloads executados como root têm SHA256 pinado no código antes de
extração, instalação ou execução de script.

PRD/SPEC/IMPL: `docs/specs/civmctl/`.

## Estrutura por audiência

### Para quem **mantém civm** (admin do repo)

| Arquivo | Função |
|---|---|
| `README.md` | este arquivo |
| `.github/workflows/ci.yml` | próprio CI: yamllint nos templates, link check |
| `.gitignore` | exclude common artifacts |

### Para quem **administra a VM** (sysadmin do civm)

| Arquivo | Função |
|---|---|
| `runbooks/MULTI-PROJECT-RUNNER.md` | provisionar VM + N runners + tools (parity ubuntu-latest) + cron de cleanup (128GB SSD) |
| `runbooks/LOCAL-CI-DISCIPLINE.md` | filosofia "local CI é gate de verdade, remoto é mirror" |

### Para **peer repos** — **copiar**

| Arquivo | Como adotar |
|---|---|
| `templates/ci-optimistic.yml.template` | `cp ... .github/workflows/ci.yml` no peer; substituir placeholders |
| `templates/ci-router.yml.template` | idem, versão Tier 1 com router |
| `templates/CIVM-USAGE.md` | copiar para `docs/CIVM.md` no peer; ajustar gate local do projeto |
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

## Como o civm runner funciona

1. **Setup uma vez** seguindo `runbooks/MULTI-PROJECT-RUNNER.md`:
   - Provisionar VM Linux (Ubuntu 22.04+, 4+ cores, 128GB SSD)
   - Instalar toolchains (Go, Node, Docker, gh CLI, etc) — parity ubuntu-latest
   - Registrar N runners GitHub com label `civm`
   - Configurar cron de cleanup diário (disk hygiene)

2. **Cada peer repo** referencia `runs-on: [self-hosted, civm]` em
   seu próprio `.github/workflows/ci.yml`.

   Regra de seguranca: jobs self-hosted devem rodar apenas PR confiavel
   ou same-repo. Evitar `pull_request_target` e nao expor secrets a codigo
   de fork em runner self-hosted.

3. **Quando billing GitHub OK:** workflow roda em `ubuntu-latest`
   (GitHub-hosted, paga minutos). Quando billing bloqueado: roteia
   para `civm` (sem custo). Detector heurístico documentado em
   `runbooks/CI-BILLING-FALLBACK.md`; templates implementam o roteamento.

4. **PR continua sendo criado** de onde o dev quiser (laptop, gh CLI,
   GitHub UI). civm só executa CI; não cria PRs.

## Adoção em 1 comando (para peer repos)

Não tem 1-comando-mágico — adoção é manual, peer repo decide o que
faz sentido:

```bash
# 1. Copiar a doc operacional curta do peer
mkdir -p <peer>/docs
cp ~/codespace/civm/templates/CIVM-USAGE.md <peer>/docs/CIVM.md
# Editar o bloco "Gate local do projeto" com o comando real do peer

# 2. Copiar template de workflow (escolher tier)
cp ~/codespace/civm/templates/ci-optimistic.yml.template \
   <peer>/.github/workflows/ci.yml
# Editar para substituir placeholders pelos gates reais do peer

# 3. Copiar snippet COMMUNICATION-STYLE
# (copiar bloco entre marcadores BEGIN/END em
#  ~/codespace/civm/templates/COMMUNICATION-STYLE.md
#  pra CLAUDE.md, AGENTS.md, CODEX.md do peer)

# 4. Configurar branch protection no GitHub
# Settings > Branches > main > require status check:
#   "Gates (typecheck, test, build, invariants)"

# 5. Verificar adoção/saúde dos peers antes de publicar ou investigar CI
civmctl peer-status --repos=advoq/civm,emersonbusson/compexhub --workflow=ci.yml
```

Audit/discipline-checks ficam no projeto do peer (cada um com sua
própria ferramenta — ex.: compexhub tem `compexhubctl`).
`peer-status` é observabilidade read-only: consolida sinais para decisão
humana, mas não corrige workspace ou configuração de peer automaticamente.

## Versionamento

`civm` segue SemVer (MAJOR.MINOR.PATCH). Tags + GitHub Releases são
geradas automaticamente por `release-please` a partir de Conventional
Commits em `main`:

- `fix:` → bump patch (`v1.0.0` → `v1.0.1`).
- `feat:` → bump minor (`v1.0.0` → `v1.1.0`).
- `feat!:` ou `BREAKING CHANGE:` no footer → bump major (`v1.0.0` → `v2.0.0`).
- `docs:`, `chore:`, `test:`, `build:`, `style:` não bumpam versão.
- `ci:`, `refactor:`, `perf:` entram no CHANGELOG sem bump (configurável).

Workflow `.github/workflows/release.yml` mantém um PR de release aberto
com título `chore: release civm v<X.Y.Z>`,
`.release-please-manifest.json` bumpado e `CHANGELOG.md` regerado.
Mergear esse PR cria a tag e publica o release. Detalhes operacionais em
`runbooks/RELEASE-AUTOMATION.md` (config, token PAT vs GITHUB_TOKEN,
override `release-as`, rollback).
Internamente `civm` no título é texto cosmético, não `package-name` do
release-please. Em PR agrupado a branch é `release-please--branches--main`
sem componente; configurar `package-name: civm` faz o release-please esperar
componente na branch e abortar antes de criar a tag.

Peer repos podem travar em versão se quiserem (ex.: `git checkout v1.2.0`
antes de copiar templates).

## Governança de PR

PRs devem linkar issue com `Closes #NNN`, `Fixes #NNN` ou
`Resolves #NNN` quando o trabalho merece rastreio. Para PR puramente
operacional, CI ou documentação sem issue real, usar marcador explícito
na seção `## Issue`: `Sem issue`, `No issue` ou `N/A`. Não criar issue
artificial nem referência falsa só para satisfazer o template.

Toda PR também precisa de pelo menos uma label `type:*`, uma label
`area:*` e responsável coerente com a issue quando houver issue linkada.

## Verificação pós-release

Depois de publicar tag/release, confirmar o estado sem executar mutação:

```bash
gh release view v1.0.0
git status --short --branch
gh run list --workflow=ci.yml --branch=main --limit 5
ssh gha-ubuntu-2404 'civmctl parity'
ssh gha-ubuntu-2404 'civmctl health'
ssh gha-ubuntu-2404 'civmctl doctor --json'
ssh gha-ubuntu-2404 'civmctl idle-check'
```

O warning `LAST cleanup timer nunca rodou` em `health`/`doctor` é
aceitável até o primeiro disparo real do `civmctl-cleanup.timer`
(janela diária 04:00 UTC). Se persistir após a próxima janela diária
esperada, vira ação operacional: verificar timer, journal e execução do
cleanup na VM.

## Histórico

- **2026-05-10** — bootstrap inicial. Estrutura: 4 runbooks + 3
  templates + 4 disciplines + 5 rules + próprio CI. Repo extraído de
  compexhub conforme `docs/proposals/CI-VM-EXTRACTION.md`.
