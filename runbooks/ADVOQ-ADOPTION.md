# Runbook — adotar civm no advoq (sem atrito)

> **Audiência**: você (admin) ou qualquer agente futuro que receba pedido
> "rodar advoq na VM". Este runbook deixa adoção em 1 comando + cópia
> de template + push.

## Pré-requisitos

- `gh auth status` autenticado com escopo `repo` na conta dona do advoq
- SSH funcional pra VM `gha-ubuntu-2404` (Tailscale ou rede local)
- `civmctl` >= versão com `runner add --auto` (sessão 2026-05-10+)

## Passo 1 — Registrar runner na VM (1 comando)

```bash
TOKEN=$(gh api -X POST /repos/emersonbusson/advoq/actions/runners/registration-token --jq .token)

civmctl runner add \
  --repo=emersonbusson/advoq \
  --token="$TOKEN" \
  --short=advoq \
  --execute
```

Faz tudo:
- mkdir `~/actions-runner-advoq` na VM
- baixa actions/runner v2.334.0 (paridade com civm-1, civm-cmpx, civm-vitae)
- extrai
- `./config.sh --unattended --labels civm --name civm-advoq --replace`
- `sudo ./svc.sh install emdev`
- `sudo ./svc.sh start`

Token mascarado nos logs. Idempotente (re-rodar substitui).

### Validar online

```bash
gh api /repos/emersonbusson/advoq/actions/runners --jq '.runners[]|"\(.name) \(.status) \(.labels[].name)"'
# Esperado: civm-advoq online (com label civm)
```

E na VM:

```bash
ssh gha-ubuntu-2404 "systemctl is-active actions.runner.emersonbusson-advoq.civm-advoq.service"
# Esperado: active
```

## Passo 2 — Adotar workflow (quando você decidir push em advoq)

Advoq atualmente roda 100% em `ubuntu-latest` (workflows `go.yml`,
`web.yml`). Pra ativar fallback billing-block via civm, adicione um
job router que decide entre `ubuntu-latest` e `[self-hosted, civm]`.
Seguranca: usar o self-hosted apenas para PR confiavel/same-repo; evitar
`pull_request_target` e secrets em jobs que executem codigo de fork.

Template pronto:

```bash
cp ~/codespace/civm/templates/advoq-ci-router.yml.template \
   ~/codespace/advoq/.github/workflows/ci-router.yml
```

O template:
- Roda `ci-router` em `[self-hosted, civm]` (decide via `civmctl billing-status`)
- Output `use_local` (true se billing-blocked)
- Gates aggregator condicional (paralelo aos `go.yml`/`web.yml` existentes)

Push trigger natural (push no advoq) executa o router. Não precisa
modificar `go.yml`/`web.yml` na primeira iteração — coexistem.

### Adoção minimal (sem alterar workflows existentes)

Só adicionar `ci-router.yml` no advoq. Os workflows go.yml e web.yml
continuam rodando em ubuntu-latest (com risco de billing block matar
em <10s). O router vai postar `Gates (civm fallback)` como check
adicional, sempre verde quando billing-block ativa fallback.

### Adoção avançada (Tier 2: rotear go.yml e web.yml)

Editar `go.yml` e `web.yml` pra adicionar `runs-on:` dinâmico igual
compexhub/vitae fazem:

```yaml
runs-on: ${{ needs.ci-router.outputs.use_local == 'true' && fromJSON('["self-hosted","civm"]') || 'ubuntu-latest' }}
```

Nota: jobs `services` matrix de go.yml requer ferramentas Go disponíveis
na VM (já estão: Go 1.26.3 instalado pelo bootstrap). Web job requer
Node 24 (já está: v24.15.0 LTS Krypton via nvm).

## Filosofia "sem atrito"

| Decisão | Razão |
|---|---|
| 1 comando registra runner | civmctl encapsula toda a sequência, dry-run default |
| Template separado em `ci-router.yml` | Não toca workflows existentes; coexiste; rollback trivial via `git rm` |
| Token efêmero gh api | Sem PAT persistente; sem rotação; sem secret novo |
| Label `civm` reuso | Mesmo padrão dos peers já adotados; zero divergência |
| Push trigger natural | Sem precisar `workflow_dispatch:` adicionado |

## Rollback (1 comando)

```bash
TOKEN=$(gh api -X POST /repos/emersonbusson/advoq/actions/runners/remove-token --jq .token)
civmctl runner remove --short=advoq --token="$TOKEN" --execute
```

Faz tudo idempotente (best-effort): svc.sh stop + uninstall + config.sh
remove + rm -rf dir. Token mascarado nos logs.

Se template `ci-router.yml` quebrar workflow advoq:

```bash
cd ~/codespace/advoq
git rm .github/workflows/ci-router.yml
git commit -m "revert: ci-router fallback"
git push
```

Workflows `go.yml`/`web.yml` continuam rodando 100% em ubuntu-latest
como antes (com risco de billing-block continuar matando jobs em <10s).

## Critério de sucesso

- `gh api /repos/emersonbusson/advoq/actions/runners` retorna
  `civm-advoq online`
- Push em advoq dispara `ci-router` que ELE roda em civm-advoq
  (verificar via `gh run view <id> --json jobs --jq '.jobs[] | "\(.name) runner=\(.runnerName)"'`)
- Em billing-block: jobs `gates-civm` rodam normalmente em
  civm-advoq enquanto go.yml/web.yml morrem em ubuntu-latest;
  pelo menos UM job verde permite branch protection passar (após
  configurar required check)

## Histórico

- **2026-05-10** — Runbook criado. Sessão de migração peers para
  padrão civm/civmctl. advoq decidiu adoção minimal (apenas runner +
  template, sem modificar workflows existentes do advoq) por filosofia
  "senior, sem atrito" do usuário.
