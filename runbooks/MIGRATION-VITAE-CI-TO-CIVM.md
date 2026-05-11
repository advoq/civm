# Runbook — Migração label `vitae-ci` → `civm` (executar manualmente)

> **Status:** histórico/superseded desde 2026-05-10. A migração principal
> `vitae-ci` → `civm` já foi concluída; manter este arquivo apenas para
> auditoria, contexto temporal e rollback histórico. Para o estado
> operacional atual, usar `README.md` e `runbooks/MULTI-PROJECT-RUNNER.md`.
>
> **Por que esse runbook existe:** sessão 2026-05-10 refatorou todo o
> código do ci-vm pra usar label `civm` (commit `f30387e`). 5 ações
> destrutivas restantes precisam de autorização nominal explícita por
> ação, e o classifier do agente bloqueia execução automática.
>
> Este runbook tem todos comandos copy/paste-able. Tempo total: ~10 min.

## Pré-requisitos

- `gh auth status` autenticado com escopo `repo` + `admin:org` (pra rename)
- SSH funcional pra `gha-ubuntu-2404`
- Working dir `~/codespace/` com clones de: ci-vm, compexhub, vitae, advoq

## Sequência segura (ordem importa — peers ANTES dos runners)

### Passo 1 — Rename GitHub repo `ci-vm` → `civm`

```bash
cd ~/codespace/ci-vm
gh repo rename civm --yes
git remote set-url origin https://github.com/emersonbusson/civm.git
cd .. && mv ci-vm civm  # opcional: renomear pasta local também
cd civm
```

**Rollback:** `gh repo rename ci-vm --yes`

### Passo 2 — Atualizar Go module path

(Apenas se renomeou pasta local. Senão pula.)

```bash
cd ~/codespace/civm
find . -type f \( -name "*.go" -o -name "go.mod" \) \
  -exec sed -i 's|github.com/emersonbusson/ci-vm|github.com/emersonbusson/civm|g' {} +
go test -race -count=1 ./... | tail -5
git add -A && git commit -m "refactor(civm): module path ci-vm -> civm post-rename"
git push origin main
```

**Rollback:** `git revert HEAD && git push`

### Passo 3 — Update workflows dos peers (compexhub, vitae)

advoq não usa label vitae-ci ainda (template ci-router não foi pushado).
Apenas compexhub e vitae:

```bash
# compexhub
sed -i 's/vitae-ci/civm/g' ~/codespace/compexhub/.github/workflows/ci.yml
python3 -c "import yaml; yaml.safe_load(open('$HOME/codespace/compexhub/.github/workflows/ci.yml'))" && echo "YAML OK"
cd ~/codespace/compexhub
git add .github/workflows/ci.yml
git commit -m "ci(compexhub): label vitae-ci -> civm (alinha com civm repo)"
git push origin main

# vitae
sed -i 's/vitae-ci/civm/g' ~/codespace/vitae/.github/workflows/ci.yml
python3 -c "import yaml; yaml.safe_load(open('$HOME/codespace/vitae/.github/workflows/ci.yml'))" && echo "YAML OK"
cd ~/codespace/vitae
git add .github/workflows/ci.yml
git commit -m "ci(vitae): label vitae-ci -> civm (alinha com civm repo)"
git push origin main
```

**Rollback:** `git revert HEAD && git push` em cada peer.

### Passo 4 — Re-register 4 runners na VM com label novo

**Crítico:** fazer DEPOIS do Passo 3. Senão peers ficam sem runner
(label antigo desaparece dos runners enquanto workflows ainda usam).

Pra cada runner: stop service antiga → uninstall → remove do GitHub →
re-register com novo nome + label → install + start.

#### vitae-ci-1 → civm-self (repo civm/ci-vm)

```bash
REPO="emersonbusson/civm"  # ou ci-vm se nao renomeou
SHORT="self"
DIR="/home/emdev/actions-runner"

REM_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/remove-token --jq .token)
REG_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/registration-token --jq .token)
ssh gha-ubuntu-2404 "
  cd $DIR
  sudo ./svc.sh stop 2>&1 | tail -2
  sudo ./svc.sh uninstall 2>&1 | tail -2
  ./config.sh remove --token '$REM_TOKEN' 2>&1 | tail -3
  ./config.sh --unattended --url https://github.com/$REPO --token '$REG_TOKEN' \\
    --labels civm --name civm-$SHORT --work _work --replace 2>&1 | tail -3
  sudo ./svc.sh install emdev 2>&1 | tail -2
  sudo ./svc.sh start 2>&1 | tail -2
  systemctl is-active actions.runner.\$(echo $REPO | tr / -).civm-$SHORT.service
"
```

#### vitae-ci-cmpx → civm-compexhub

```bash
REPO="emersonbusson/compexhub"
SHORT="compexhub"
DIR="/home/emdev/actions-runner-cmpx"

REM_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/remove-token --jq .token)
REG_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/registration-token --jq .token)
ssh gha-ubuntu-2404 "
  cd $DIR
  sudo ./svc.sh stop && sudo ./svc.sh uninstall
  ./config.sh remove --token '$REM_TOKEN'
  ./config.sh --unattended --url https://github.com/$REPO --token '$REG_TOKEN' \\
    --labels civm --name civm-$SHORT --work _work --replace
  sudo ./svc.sh install emdev && sudo ./svc.sh start
  systemctl is-active actions.runner.emersonbusson-compexhub.civm-compexhub.service
"
```

#### vitae-ci-vitae → civm-vitae

```bash
REPO="emersonbusson/vitae"
SHORT="vitae"
DIR="/home/emdev/actions-runner-vitae"

REM_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/remove-token --jq .token)
REG_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/registration-token --jq .token)
ssh gha-ubuntu-2404 "
  cd $DIR
  sudo ./svc.sh stop && sudo ./svc.sh uninstall
  ./config.sh remove --token '$REM_TOKEN'
  ./config.sh --unattended --url https://github.com/$REPO --token '$REG_TOKEN' \\
    --labels civm --name civm-$SHORT --work _work --replace
  sudo ./svc.sh install emdev && sudo ./svc.sh start
  systemctl is-active actions.runner.emersonbusson-vitae.civm-vitae.service
"
```

#### vitae-ci-advoq → civm-advoq

```bash
REPO="advoq/advoq"  # ATENCAO: org advoq, nao emersonbusson
SHORT="advoq"
DIR="/home/emdev/actions-runner-advoq"

REM_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/remove-token --jq .token)
REG_TOKEN=$(gh api -X POST /repos/$REPO/actions/runners/registration-token --jq .token)
ssh gha-ubuntu-2404 "
  cd $DIR
  sudo ./svc.sh stop && sudo ./svc.sh uninstall
  ./config.sh remove --token '$REM_TOKEN'
  ./config.sh --unattended --url https://github.com/$REPO --token '$REG_TOKEN' \\
    --labels civm --name civm-$SHORT --work _work --replace
  sudo ./svc.sh install emdev && sudo ./svc.sh start
  systemctl is-active actions.runner.advoq-advoq.civm-advoq.service
"
```

### Passo 5 — Validar end-to-end

```bash
# 1. Runners online com label civm
for repo in emersonbusson/civm emersonbusson/compexhub emersonbusson/vitae advoq/advoq; do
  echo "=== $repo ==="
  gh api /repos/$repo/actions/runners --jq '.runners[]|"\(.name) status=\(.status) labels=\(.labels[].name)"' | grep -E "online|civm"
done

# 2. systemd na VM
ssh gha-ubuntu-2404 "systemctl list-units 'actions.runner.*civm*' --no-pager --no-legend"
# Esperado: 4 services civm-self, civm-compexhub, civm-vitae, civm-advoq active

# 3. civmctl runner list (estruturado)
ssh gha-ubuntu-2404 "civmctl runner list --json | jq '.runners[].name'"

# 4. Trigger workflow em compexhub (push trivial ou gh workflow run se workflow_dispatch)
# Ver run pegou em civm-compexhub:
gh run view <run_id> --repo emersonbusson/compexhub --json jobs \
  --jq '.jobs[] | "\(.name) runner=\(.runnerName)"'
# Esperado: runnerName = "civm-compexhub" (ao invés de vitae-ci-cmpx antigo)
```

## Critérios de sucesso

- [ ] `gh repo view emersonbusson/civm` retorna repo (não mais `ci-vm`)
- [ ] 4 runners online: `civm-self`, `civm-compexhub`, `civm-vitae`, `civm-advoq` (todos com label `civm`)
- [ ] Workflows compexhub e vitae rodam sem mudança aparente (label novo, runner novo)
- [ ] systemd na VM mostra 4 services `actions.runner.*.civm-*`

## Rollback completo

Se quebrar produção:

```bash
# 1. Reverter peers
cd ~/codespace/compexhub && git revert HEAD && git push
cd ~/codespace/vitae && git revert HEAD && git push

# 2. Re-register runners com label antigo
# (repetir Passo 4 trocando --labels civm por --labels vitae-ci e
#  --name civm-X por vitae-ci-X)

# 3. Reverter rename do repo
cd ~/codespace/civm && gh repo rename ci-vm --yes
git remote set-url origin https://github.com/emersonbusson/ci-vm.git
cd .. && mv civm ci-vm
```

## Histórico

- **2026-05-10** — Runbook criado após classifier bloquear execução automática.
  Sessão refatorou código (commit `f30387e` em ci-vm) mas 5 ações
  destrutivas (rename repo, edit peers, re-register runners) ficaram
  pendentes pra execução manual conforme este runbook.
