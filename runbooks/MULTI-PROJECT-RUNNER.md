# Runbook — vitae-ci como runner self-hosted compartilhado entre projetos

> **Quando usar:** múltiplos repositórios (compexhub, vitae, advoq) precisam <!-- invariant-waive:#11 -- runbook operacional documenta runner self-hosted compartilhado entre projetos paralelos do autor por design (cf. plano usuario 2026-05-10) -->
> rodar CI em paralelo no mesmo runner self-hosted `vitae-ci`, sem conflito
> de portas, volumes Docker, work directories ou crosstalk de dados.
>
> **Modelo conceitual (importante):** vitae-ci e' **mirror visivel no
> GitHub**, nao gate alternativo de validacao. O gate de verdade do
> projeto e' `compexhubctl ci local --clean` que cada dev roda no laptop
> ANTES de push (ver [`LOCAL-CI.md`](./LOCAL-CI.md) §"Modelo conceitual").
> Este runner self-hosted existe pra postar checkmark verde no PR sem
> custo de Actions minutes — a validacao real ja aconteceu antes do
> push, em cada laptop.
>
> Aplica-se identicamente aos 3 repos: dev valida local primeiro,
> depois push, depois vitae-ci posta verde. Mesmo padrao em compexhub,
> vitae e advoq. <!-- invariant-waive:#11 -- runbook operacional lista repos peer com mesmo padrao de validacao -->
>
> **Companion runbooks:**
> - [`CI-BILLING-FALLBACK.md`](./CI-BILLING-FALLBACK.md) — Camada 1+2 do
>   mirror visivel no GitHub (compexhub-specific).
> - [`LOCAL-CI.md`](./LOCAL-CI.md) — `compexhubctl ci local` como gate
>   real do projeto (mandatorio antes de push).

## Topologia alvo

```
+------------------------------------------------------+
| VM "vitae-ci" (Ubuntu 22.04+, 4+ cores, 16GB+ RAM)  |
|                                                      |
|  systemd services:                                   |
|   - actions.runner.<owner>.compexhub-1.service       |
|   - actions.runner.<owner>.compexhub-2.service       |
|   - actions.runner.<owner>.compexhub-3.service       |
|     (or org-level if 3 repos)                        |
|                                                      |
|  Cada runner tem:                                    |
|   - Label: vitae-ci                                  |
|   - Work dir: /home/runner/_work-N                   |
|   - PID separado, processo isolado                   |
|                                                      |
|  Ferramentas no PATH (compartilhadas):               |
|   - go 1.26 (ou actions/setup-go@v5 instala)         |
|   - gh CLI ≥ 2.40 (compartilhado, auth via secret)   |
|   - git ≥ 2.30, curl, jq, bash                       |
|   - docker (opcional; cada compose com project name  |
|     único — ver §"Riscos compartilhados")            |
+------------------------------------------------------+
```

GitHub auto-distribui jobs entre runners disponíveis. Com N=3 runners, até
3 jobs simultâneos (distribuídos entre os 3 repos). Sobrando demanda, jobs
ficam em queue até runner liberar.

**Dimensionamento:** começar com N = número de repos ativos (3 hoje:
compexhub, vitae, advoq). Escalar para 5 ou 6 se o gate `Gates (typecheck, <!-- invariant-waive:#11 -- runbook lista repos compartilhando runner -->
test, build, invariants)` ficar consistentemente em queue >2 minutos.

## Pattern: 1 runner por peer-repo na mesma VM

GitHub Actions roteia jobs do repo X **somente** pro runner registrado
em X. Não existe runner cross-repo em conta personal (só org-runners
em GitHub Teams/Enterprise).

**Topologia validada nesta VM:**

```
gha-ubuntu-2404
├── ~/actions-runner/                 (vitae-ci-1     -> emersonbusson/ci-vm)
├── ~/actions-runner-compexhub/       (vitae-ci-cmpx  -> emersonbusson/compexhub)
├── ~/actions-runner-vitae/           (vitae-ci-vitae -> emersonbusson/vitae)
└── /etc/systemd/system/
    ├── actions.runner.emersonbusson-ci-vm.vitae-ci-1.service
    ├── actions.runner.emersonbusson-compexhub.vitae-ci-cmpx.service
    └── actions.runner.emersonbusson-vitae.vitae-ci-vitae.service
```

Cada runner usa o mesmo label `vitae-ci`. Workflows com
`runs-on: [self-hosted, vitae-ci]` no peer X só serão executados pelo
runner de X. Sem crosstalk entre repos.

### Adicionar runner pra novo peer (sequencia replicada)

```bash
# 1. Token de registracao (efemero ~1h, GH escopo "repo")
TOKEN=$(gh api -X POST /repos/emersonbusson/<REPO>/actions/runners/registration-token --jq .token)

# 2. Diretorio dedicado por peer
ssh gha-ubuntu-2404 "mkdir -p ~/actions-runner-<REPO> && cd ~/actions-runner-<REPO> &&
  curl -fsSL -o runner.tar.gz https://github.com/actions/runner/releases/download/v2.334.0/actions-runner-linux-x64-2.334.0.tar.gz &&
  tar xzf runner.tar.gz && rm runner.tar.gz &&
  ./config.sh --unattended --url https://github.com/emersonbusson/<REPO> \
    --token '$TOKEN' --labels vitae-ci --name vitae-ci-<SHORT> \
    --work _work --replace &&
  sudo ./svc.sh install emdev &&
  sudo ./svc.sh start"

# 3. Verificar online
gh api /repos/emersonbusson/<REPO>/actions/runners --jq '.runners[]|"\(.name) \(.status)"'
```

### "Do zero sempre" — clean-slate per job

Sem precisar `--ephemeral` (que requer JIT tokens via webhook). O default
do GitHub Actions ja garante isolamento:

- `GITHUB_WORKSPACE` unico per-job (delete + recreate entre jobs)
- `actions/checkout@v4` faz fresh git clone (sem state preservado)
- `civmctl-cleanup.timer` (cron 04:00 UTC) limpa Docker/tmp/_work diariamente
- Multiplos runners da mesma VM tem `--work _work` separado por runner

Resultado: cada job comeca do zero, sem crosstalk.

### Rollback de runner peer

Se runner quebrar workflow do peer:

```bash
# Stop + uninstall systemd
ssh gha-ubuntu-2404 "cd ~/actions-runner-<REPO> &&
  sudo ./svc.sh stop && sudo ./svc.sh uninstall"

# Remove from GitHub
TOKEN=$(gh api -X POST /repos/emersonbusson/<REPO>/actions/runners/remove-token --jq .token)
ssh gha-ubuntu-2404 "cd ~/actions-runner-<REPO> &&
  ./config.sh remove --token '$TOKEN' &&
  rm -rf ~/actions-runner-<REPO>"
```

Workflow do peer volta a rodar 100% em `ubuntu-latest` (com risco de
billing-block continuar derrubando jobs em <10s).

## Setup zero-effort (recomendado): civmctl bootstrap

A partir de 2026-05-10, **provisionamento e cleanup são automatizados** via
`civmctl` (Go binary do proprio repo ci-vm). Specs ficam travadas em
`internal/specs/specs.go` e seguem `actions/runner-images` Ubuntu2404-Readme.md.

```bash
# Numa VM Ubuntu 24.04 LTS limpa, como root:

# 1. Build civmctl (uma vez; precisa Go ≥ 1.26 instalado manualmente OU
#    rodar bootstrap do compexhub que ja tem Go).
git clone https://github.com/emersonbusson/ci-vm.git /opt/ci-vm
cd /opt/ci-vm && go build -o /usr/local/bin/civmctl ./cmd/civmctl

# 2. Confere versoes alvo (paridade com ubuntu-latest)
civmctl version-pins

# 3. Dry-run primeiro (default; nao modifica nada)
sudo civmctl bootstrap

# 4. Aplicar
sudo civmctl bootstrap --execute

# 5. Instalar systemd timer de cleanup automatico
sudo cp /opt/ci-vm/deploy/systemd/civmctl-cleanup.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-cleanup.timer

# 6. Health check (deve retornar exit 0)
civmctl health
```

Steps idempotentes do bootstrap (todos check-then-apply):

| Step | O que faz | Skip se |
|---|---|---|
| `verify_os` | confere `/etc/os-release` ID=ubuntu VERSION_ID=24.04 | OS errado → erro |
| `verify_uid` | confere UID=0 | UID≠0 → erro |
| `apt_base_packages` | instala build-essential, curl, wget, jq, git, ca-certificates | dpkg-query reporta ja instalados |
| `install_go` | baixa tarball go1.25.9 para /usr/local/go | `go version` reporta versao alvo |
| `install_node` | NodeSource setup_20.x + apt install nodejs | `node --version` reporta v20.20.2 |
| `install_docker` | apt repo Docker CE + plugin compose | `docker --version` reporta 28.0.4 |
| `install_gh` | apt repo cli.github.com + apt install gh | `gh --version` reporta 2.89.0 |
| `install_systemd_timer` | enable --now civmctl-cleanup.timer | systemctl is-enabled retorna enabled |

Cleanup automatico (cron diario 04:00 UTC via systemd timer):

| Action | O que limpa | Threshold |
|---|---|---|
| `tmp_old` | `/tmp` antigos | mtime >7 dias E >2h |
| `work_old` | `/home/runner/_work/**/_actions` antigos | mtime >14 dias E >2h |
| `docker_prune` | `docker system prune -af --volumes` | (sem threshold; remove tudo nao usado) |
| `apt_cache` | `apt-get clean && apt-get autoremove -y` | (libera /var/cache/apt) |

`civmctl cleanup --dry-run` (default) lista o que seria liberado sem
deletar. `--execute` aplica. Anti-jobs-em-curso: nunca toca arquivos com
mtime <2h.

### Quando usar setup manual em vez de civmctl

- VM que **nao e** Ubuntu 24.04 LTS (Debian, RHEL, Arch, etc).
- Quer customizar versoes que divergem do `internal/specs/Ubuntu2404`.
- Bootstrap falhou e quer debugar passo-a-passo.

Nesses casos, ver "## Setup operacional (manual)" abaixo.

## Setup operacional (manual, alternativa)

### 1. Provisionar a VM

```bash
# Como root ou sudo
adduser --system --group --home /home/runner runner
apt update
apt install -y git curl jq build-essential

# Go 1.26
GO_VERSION=1.26.0  # ajustar
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ln -s /usr/local/go/bin/go /usr/local/bin/go

# gh CLI
type -p curl >/dev/null
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list
apt update && apt install -y gh
```

### 2. Registrar N runners (1 por repo, escalável)

Cada repo gera um token de registro próprio em GitHub Settings > Actions >
Runners > New self-hosted runner. Repetir para cada N:

```bash
# Como user 'runner'
sudo -u runner -i

cd /home/runner
mkdir actions-runner-N && cd actions-runner-N
RUNNER_VERSION=2.331.0  # latest at provisioning time
curl -fsSL -o runner.tar.gz \
  "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
tar xzf runner.tar.gz
rm runner.tar.gz

# Substituir <TOKEN> pelo token de registro do repo
./config.sh \
  --url "https://github.com/<owner>/<repo>" \
  --token "<TOKEN>" \
  --labels "vitae-ci" \
  --name "vitae-ci-N" \
  --work "_work-N" \
  --unattended \
  --replace

# Instalar como systemd service (require root depois)
sudo ./svc.sh install runner
sudo ./svc.sh start
sudo ./svc.sh status
```

Repetir mudando `actions-runner-N` para `actions-runner-2`, `actions-runner-3`,
etc. Cada um aponta para um repo diferente OU todos para o mesmo repo (se
quiser N runners para 1 repo só).

**Alternativa org-level:** se os 3 repos estão sob a mesma org, registrar runner
no nível da org (Settings > Actions > Runners da organização). 1 runner serve
os 3 repos sem precisar registrar 3 vezes. Recomendado se for o caso.

### 3. Verificar online

```bash
# Por repo
gh api "repos/<owner>/<repo>/actions/runners" --jq '.runners[] | "\(.name) status=\(.status)"'

# Esperado:
# vitae-ci-1 status=online
# vitae-ci-2 status=online
# ...
```

## Isolamento por job (built-in GitHub Actions)

GitHub Actions garante automaticamente:

- **`GITHUB_WORKSPACE` único por job.** Cada job recebe um diretório
  efêmero `<work-dir>/<repo>/<repo>` que é deletado/recriado entre jobs.
  Sem crosstalk de filesystem entre repos.
- **Variáveis de ambiente isoladas.** `GITHUB_TOKEN`, `RUNNER_NAME`, etc
  são per-job; um job não vê env vars de outro.
- **Cache de actions/setup-go, actions/setup-node** é compartilhado entre
  runs DO MESMO REPO (chave inclui repo+os+lock-file hash). Cross-repo, as
  chaves divergem, sem colisão.

## Riscos compartilhados (NÃO automatizados pelo GitHub Actions)

### 1. Docker daemon shared

Se algum job spina `docker compose`, o daemon docker é único na VM.
Containers não conflitam por isolamento Linux, **mas networks e named
volumes podem colidir** se múltiplos repos usarem o mesmo `--project-name`.

**Regra:** sempre passar `--project-name <repo>-<run-id>` em qualquer
`docker compose` invocado em CI:

```bash
docker compose --project-name "${GITHUB_REPOSITORY##*/}-${GITHUB_RUN_ID}" up -d
```

Em compexhub o ci.yml atual **não roda docker compose** (integration usa
NEON serverless externo), então o risco é zero. Para vitae/advoq <!-- invariant-waive:#11 -- runbook lista projetos peer no runner -->, **se
precisarem docker**, exigir essa convenção em cada step que invoca
compose.

### 2. Ports fixos

Jobs nunca devem bind portas fixas (`-p 5432:5432`, `-p 6379:6379`).
Múltiplos jobs paralelos = port collision instantânea.

**Regra:** bind portas via `0` (ephemeral) ou via env var:

```yaml
- run: docker run -p ${{ env.PG_PORT }}:5432 postgres
  env:
    PG_PORT: 0  # docker escolhe porta livre
```

OU usar testcontainers-go que automaticamente usa portas ephemeral.

### 3. Filesystem fora do workspace

Job NUNCA escreve em `/tmp/<known-name>`, `/opt/<known>`, `~/.cache/<known>`
ou outro path absoluto que outro job possa também tocar.

**Regra:** todo arquivo temporário via `mktemp -d` (que respeita `TMPDIR` e
gera path único) ou dentro de `${{ runner.temp }}`.

### 4. Disk pressure (CRITICO em VM 128GB)

VM tipica: 128GB SSD. Com 3+ repos peer rodando CI continuamente, sem
limpeza automatica o disco enche em semanas. Budget pratico:

| Item | Tamanho tipico | Notas |
|---|---|---|
| Sistema base (Ubuntu) | 10-15 GB | inevitavel |
| Runner installations (N) | 1-2 GB | ~500MB cada |
| Workspace por job ativo | 1-5 GB | cada repo + dependencias |
| Cache actions/cache | 5-20 GB | acumula ao longo do tempo |
| Docker images + volumes | 10-50 GB | sem cleanup, cresce indefinidamente |
| Go build cache | 2-10 GB | $HOME/.cache/go-build |
| npm/pnpm cache | 1-5 GB | $HOME/.npm |
| Logs + tmp | 1-3 GB | /var/log, /tmp |
| **Buffer disponivel** | **>30 GB** | precisa pra picos |

Setup minimo: **manter sempre >30GB livres**. Abaixo disso, jobs falham
imprevisivelmente.

Ver §"Disk hygiene" abaixo para automacao.

## Runner parity com ubuntu-latest

Para que peer repos rodem na vitae-ci **identicamente** ao GitHub-hosted
ubuntu-latest, instalar:

### Toolchains de linguagem

```bash
# Go (multi-version via go env GOTOOLCHAIN auto)
GO_VERSION=1.26.0
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | sudo tar -C /usr/local -xz
sudo ln -sf /usr/local/go/bin/go /usr/local/bin/go

# Node via fnm (multi-version sem hassle)
curl -fsSL https://fnm.vercel.app/install | bash
fnm install 24
fnm install 22
fnm install 20
fnm default 24

# Python (default Ubuntu suficiente; pip/venv)
sudo apt install -y python3 python3-pip python3-venv

# Ruby (se necessario)
# sudo apt install -y ruby-full
```

### Ferramentas de build

```bash
sudo apt install -y \
  build-essential \
  pkg-config \
  libssl-dev \
  zlib1g-dev \
  postgresql-client \
  redis-tools \
  jq \
  curl \
  wget \
  unzip \
  imagemagick

# Docker
sudo apt install -y docker.io
sudo usermod -aG docker runner
sudo systemctl enable --now docker

# Docker compose plugin
sudo apt install -y docker-compose-plugin
```

### CLIs essenciais

```bash
# gh CLI (autenticado via GITHUB_TOKEN no workflow)
type -p curl >/dev/null
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list
sudo apt update && sudo apt install -y gh

# actionlint (validar workflows)
go install github.com/rhysd/actionlint/cmd/actionlint@latest

# yq (yaml processor)
sudo snap install yq
```

### Verificar parity

Script de smoke test (rodar como user `runner`):

```bash
# Confirmar que tudo essencial esta presente
for cmd in go node npm python3 docker gh git curl jq; do
  if command -v "$cmd" >/dev/null 2>&1; then
    echo "OK: $cmd ($(command -v $cmd))"
  else
    echo "MISSING: $cmd"
  fi
done

# Versoes minimas
go version           # esperado go1.26+
node --version       # esperado v24+
docker --version     # esperado 24+
gh --version         # esperado 2.40+
```

Se faltar algo, runner reporta erro confuso quando workflow tentar usar.

## Disk hygiene (automacao obrigatoria em 128GB)

Sem automacao, disco enche em ~30 dias com 3 repos ativos. Setup:

### Cron de limpeza diaria

Criar `/opt/vitae-ci/cleanup.sh` (NA VM, fora de qualquer repo — nao
viola invariante #14 do compexhub porque nao esta em tools/ scripts/
infra/ de repo). Conteudo:

```bash
#!/usr/bin/env bash
# /opt/vitae-ci/cleanup.sh — cron diario para evitar disco encher
# em vitae-ci compartilhado (128GB SSD).
#
# Crontab: 0 3 * * * /opt/vitae-ci/cleanup.sh >> /var/log/vitae-ci-cleanup.log 2>&1

set -euo pipefail
echo "=== cleanup $(date -Iseconds) ==="
df -h / | tail -1

# 1. Workspaces de jobs antigos (runner deleta, mas tmp persiste as vezes)
find /home/runner/_work-*/_temp -mindepth 1 -maxdepth 2 -mtime +3 -exec rm -rf {} + 2>/dev/null || true

# 2. Docker images orfas + build cache
docker image prune -af --filter "until=168h" || true   # >7 dias
docker container prune -f --filter "until=24h" || true
docker volume prune -f || true
docker builder prune -af --filter "until=72h" || true  # >3 dias

# 3. Go build cache (mantem 7 dias)
go clean -cache -modcache 2>/dev/null || true
# OU mais granular:
# find ~/.cache/go-build -type f -atime +7 -delete

# 4. npm/pnpm cache
npm cache verify 2>/dev/null || true
[ -d "$HOME/.pnpm-store" ] && pnpm store prune 2>/dev/null || true

# 5. APT cache (libera 1-2GB facil)
sudo apt clean
sudo apt autoremove -y

# 6. journal logs (manter 7 dias)
sudo journalctl --vacuum-time=7d

# 7. /tmp (manter 1 dia)
sudo find /tmp -mindepth 1 -maxdepth 1 -mtime +1 -exec rm -rf {} + 2>/dev/null || true

echo "--- after cleanup ---"
df -h / | tail -1
echo
```

Tornar executavel + agendar:

```bash
sudo mkdir -p /opt/vitae-ci
sudo cp cleanup.sh /opt/vitae-ci/cleanup.sh
sudo chmod +x /opt/vitae-ci/cleanup.sh

# Adicionar ao crontab do root
sudo crontab -l 2>/dev/null | { cat; echo "0 3 * * * /opt/vitae-ci/cleanup.sh >> /var/log/vitae-ci-cleanup.log 2>&1"; } | sudo crontab -
```

### Watchdog de espaco em disco

Cron extra que dispara cleanup agressivo se disco passar de 80%:

```bash
#!/usr/bin/env bash
# /opt/vitae-ci/disk-watchdog.sh — roda a cada hora
# Crontab: 0 * * * * /opt/vitae-ci/disk-watchdog.sh

THRESHOLD=80
USAGE=$(df / --output=pcent | tail -1 | tr -dc '0-9')

if [ "$USAGE" -gt "$THRESHOLD" ]; then
  echo "$(date -Iseconds) WARNING: disk at ${USAGE}% — running aggressive cleanup"
  /opt/vitae-ci/cleanup.sh

  # Se ainda alto, limpar TUDO de docker
  USAGE_AFTER=$(df / --output=pcent | tail -1 | tr -dc '0-9')
  if [ "$USAGE_AFTER" -gt "$THRESHOLD" ]; then
    echo "Still at ${USAGE_AFTER}% — nuking docker"
    docker system prune -af --volumes
  fi
fi
```

### Monitoramento

Logs em `/var/log/vitae-ci-cleanup.log`. Verificar semanalmente:

```bash
# Ultimas 5 execucoes
tail -50 /var/log/vitae-ci-cleanup.log

# Tendencia de disco
grep "after cleanup" -A1 /var/log/vitae-ci-cleanup.log | tail -20
```

Se disco continua subindo apesar da automacao, investigar quem esta
escrevendo fora do workspace (job mal-comportado violando regra
"Filesystem fora do workspace").

### Rollback trigger (disk hygiene)

Se em 30 dias o disco passar de 90% mais de 3 vezes, escalar:

1. Reduzir N runners (1 por repo em vez de 1+ por repo)
2. Adicionar 2o disco (ou expandir VM se cloud)
3. Migrar caches grandes para volume separado
4. Considerar rotacao de runners com `--ephemeral` (cada job em
   container fresh, descartado depois)

### Limpar caches antigos manualmente (interativo)

```bash
sudo -u runner find /home/runner/_work-*/_temp -mtime +7 -delete
```

(Adicionar a cron diário se quiser.)

## Como vitae e advoq adotam o padrão router <!-- invariant-waive:#11 -- secao operacional descreve adocao por repos peer -->

O `.github/workflows/ci.yml` do compexhub é o template de referência.
Estrutura mínima a copiar:

1. Job `ci-router` em `runs-on: [self-hosted, vitae-ci]` que classifica
   changes + decide `use_local` via heurística.
2. Demais jobs com `runs-on:` conditional via `fromJSON`.
3. Job aggregador final `Gates (typecheck, test, build, invariants)` em
   vitae-ci como check canônico para branch protection.
4. `permissions: { actions: read, contents: read }` no topo.
5. `concurrency:` block escopado por `github.workflow + github.ref`.

Para o detector heurístico, vitae/advoq podem escolher entre 3 tiers <!-- invariant-waive:#11 -- repos peer -->
(em ordem de preferência operacional):

- **Tier 1 — detector via Go (rota mais determinista):** vendor o binário
  `compexhubctl` da release do compexhub e chamar
  `compexhubctl ci billing-status --workflow=ci.yml`. Mesma lógica
  do ci-router atual do compexhub.
- **Tier 2 — detector via Go remoto (sem vendor):** rodar
  `go run github.com/<owner>/compexhub/tools/compexhubctl@latest ci billing-status`
  no step do workflow. Mesma logica do Tier 1, sem precisar vendor o
  repo inteiro. Exige Go disponivel no runner (actions/setup-go@v5
  instala em segundos).
- **Tier 3 — optimistic-retry pattern (zero-auth, self-healing):** adotar
  `docs/templates/ci-optimistic.yml.template` que **não usa detector**.
  Sempre tenta `ubuntu-latest` primeiro com `continue-on-error: true`;
  se falhar (incluindo billing block que mata o job em <10s sem step
  rodar), automaticamente dispara versão local em `vitae-ci`. Aggregator
  passa se ANY um dos dois roteamentos completou success. Pros: zero
  detection logic, zero auth, self-healing. Cons: ~5-30s de billing
  consumido por run quando ubuntu-latest morre rapido (custo baixo na
  pratica).

Tiers 1 e 2 funcionam com `GITHUB_TOKEN` padrão do workflow — sem PAT
extra. Tier 3 é o único que funciona mesmo se o token estiver indisponível
(quase nunca acontece em workflow context, mas é uma fallback final).

## Checklist de adoção (por repo)

Para cada repo (compexhub, vitae, advoq) que vai usar vitae-ci: <!-- invariant-waive:#11 -- checklist enumera repos peer -->

- [ ] Runner registrado e online (verificar via `gh api repos/<owner>/<repo>/actions/runners`)
- [ ] Workflow `ci.yml` adota router pattern (template do compexhub)
- [ ] `compexhubctl ci billing-status` chamavel no workflow (via `go run` ou binario vendor-eado)
- [ ] Branch protection rule de `main`:
  - [ ] `Require status checks to pass before merging` habilitado
  - [ ] `Gates (typecheck, test, build, invariants)` adicionado como
        required
  - [ ] Jobs individuais (lint, test, etc) **removidos** da lista de
        required (consolidados pelo Gates)
- [ ] `permissions: { actions: read, contents: read }` no top do workflow
- [ ] `concurrency:` block escopado por workflow+ref
- [ ] Se usa docker compose: `--project-name <repo>-${GITHUB_RUN_ID}` em
      todos os steps
- [ ] Se usa portas: nunca bind fixo, usar ephemeral
- [ ] Disk free na VM ≥50GB confirmado

## Verificação end-to-end

```bash
# 1. Ver runners online (em qualquer repo dos 3)
gh api "repos/<owner>/<repo>/actions/runners" \
  --jq '.runners[] | select(.labels[] | .name == "vitae-ci") | "\(.name) \(.status)"'

# 2. Forçar concorrência: abrir 3 PRs draft simultâneos (1 por repo)
#    com mudança trivial. Verificar que todos rodam em paralelo no
#    vitae-ci sem queue.

# 3. Ver histórico de duracao do `Gates` em cada repo:
gh run list --workflow=ci.yml --limit 5 --json databaseId,status,conclusion,startedAt,updatedAt \
  --jq '.[] | "\(.databaseId) \(.conclusion) \(((.updatedAt | fromdateiso8601) - (.startedAt | fromdateiso8601))/60)min"'

# 4. Se 3 jobs em paralelo, esperar p95 do gate ~ tempo do gate solo (sem
#    contention significativa). Se p95 dobrar, escalar N runners.
```

## Capacity planning

Heurística inicial:

| Repos ativos | Workflows típicos | Runners recomendados |
|---|---|---|
| 1 | 1 PR/dia | 1-2 |
| 3 (compexhub + vitae + advoq) | 3-5 PR/dia, alguns simultâneos | 3-5 | <!-- invariant-waive:#11 -- linha de capacity planning lista repos peer -->
| 5+ | dezenas de PR | 5-10 + monitoramento de queue |

Métrica de stress: `gh run list --status queued --jq 'length'` retornar
>0 consistentemente = adicionar runner.

## Rollback trigger

Se em 30 dias (2026-06-09) qualquer dos seguintes acontecer:

1. **3+ ocorrências de port collision em CI** (job falhando porque outro
   job da mesma VM bindou porta) → revisar discipline de portas + adicionar
   linter
2. **Qualquer crosstalk de dados confirmado** (job de repo A vendo state
   de repo B) → investigar + abrir incidente; possivelmente migrar para
   N VMs separadas (1 por repo) ao invés de N runners 1 VM
3. **Queue p95 >5 minutos sustentado por 3 dias** → adicionar mais 2
   runners ou escalar VM
4. **Disk free <10 GB** → cleanup script + acelerar TTL de cache

Cada caso reabre este runbook + atualiza secão Capacity planning.

## Histórico

- **2026-05-10** — Primeira versão. Criada após pedido de unificar
  CI de compexhub + vitae + advoq no mesmo runner self-hosted. <!-- invariant-waive:#11 -- entrada de historico explicita escopo de adocao -->
  Companion da Camada 1 entregue em ci.yml refactor (commit `7e5835e`).
