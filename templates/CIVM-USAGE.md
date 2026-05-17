# CIVM-USAGE.md — template portatil para peer repos

> Como adotar: copiar este arquivo para `docs/CIVM.md` no peer repo e
> ajustar apenas o bloco "Gate local do projeto". Nao copiar runbook de
> administracao da VM para o peer; essa fonte permanece em
> `<civm>/runbooks/`.

# civm — CI compartilhado deste repo

Este repo usa `civm` como runner self-hosted do GitHub Actions para postar
o check visivel no PR. A validacao de produto continua sendo o gate local
do proprio repo, rodado antes de push.

## Padrao atual

- Workflow GitHub Actions: `.github/workflows/ci.yml`
- Runner label: `runs-on: [self-hosted, civm]`
- Check required em branch protection: `Gates (typecheck, test, build, invariants)`
- Fonte operacional da VM: checkout local do repo `civm`
- Runbook admin da VM: `<civm>/runbooks/MULTI-PROJECT-RUNNER.md`

## Gate local do projeto

Substituir este bloco pelo comando real do peer antes de commitar:

```bash
# Exemplo; nao copiar literalmente se o repo tiver outro gate.
npm run lint
npm test
npm run build
```

## Segurança

- Self-hosted runner deve executar apenas PR confiavel/same-repo.
- Nao usar `pull_request_target` com jobs self-hosted.
- Nao expor secrets a codigo vindo de fork externo.
- `civm` executa o workflow do peer; ele nao audita codigo sozinho.

## Termos obsoletos em docs ativas

Nao usar estes nomes em documentacao operacional nova:

- `ci-vm`
- `vitae-ci`
- `ci-result`
- `make ci-vm`
- `CI_VM_HOST`, `CI_VM_USER`, `CI_VM_PASSWORD`
- `~/.config/advoq/ci-vm.env`
- `advoq-ci-vm-autoclean.timer`
- hooks legados `/opt/civm/hooks/job-started.sh` e
  `/opt/civm/hooks/job-completed.sh`

Se algum termo acima aparecer, ele deve estar em arquivo historico,
arquivo de migracao ou bloco explicitamente marcado como legado.

## Como verificar

```bash
rg -n "vitae-ci|ci-result|make ci-vm|CI_VM_|advoq-ci-vm|ci-vm" \
  README.md AGENTS.md CLAUDE.md CODEX.md docs .github/workflows
```

Resultado esperado: nenhum match em docs ativas, exceto contexto historico
explicitamente marcado.

## Histórico

- 2026-05-17 — template criado para padronizar a documentacao de VM/civm
  entre os repos que compartilham a VM.
