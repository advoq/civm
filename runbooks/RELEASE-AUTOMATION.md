# Release automation (release-please)

Status: ativo desde 2026-05-11. Mantido em `.github/workflows/release.yml`
+ `release-please-config.json` + `.release-please-manifest.json`.

## Como funciona

1. Cada push em `main` dispara `release.yml` (job `release-please`).
2. `googleapis/release-please-action@v4` lê os commits desde a tag mais
   recente (`v<X.Y.Z>` no manifest) e calcula o proximo bump por
   Conventional Commits:
   - `fix:` -> patch (1.0.0 -> 1.0.1)
   - `feat:` -> minor (1.0.0 -> 1.1.0)
   - `feat!:` / `BREAKING CHANGE:` em footer -> major (1.0.0 -> 2.0.0)
   - `docs:`, `chore:`, `test:`, `style:`, `build:` -> sem bump
   - `ci:`, `refactor:`, `perf:` -> sem bump (entram so no CHANGELOG)
3. Se ha pelo menos 1 commit bumpavel, abre/atualiza um PR
   `chore(release): civm <version>` com:
   - `.release-please-manifest.json` bumpado.
   - `CHANGELOG.md` regerado com as secoes configuradas.
4. Mergear esse PR cria a tag `v<version>` e publica o GitHub Release
   automaticamente. release-please nao escreve em `main` fora desse PR.

## Conventional Commits cheat-sheet

```
<type>(<scope opcional>): <descricao curta em ingles imperativo>

<corpo em PT-BR, linhas <=72 chars, sem markdown>

[BREAKING CHANGE: <descricao>] [opcional, somente major]
Rollback trigger: <gatilho objetivo>   # obrigatorio em feat/fix/refactor/perf
```

Types validos: `feat`, `fix`, `refactor`, `perf`, `docs`, `ci`, `test`,
`chore`, `build`, `style`.

## Token

Por padrao usa `secrets.GITHUB_TOKEN`. Limitacao: PRs e tags criados pelo
`GITHUB_TOKEN` NAO disparam outros workflows (e.g. `ci.yml`). Logo o PR
de release nasce sem CI rodando.

Mitigacoes (em ordem de preferencia):

1. **PAT em secret `RELEASE_PLEASE_TOKEN`** (atual upgrade path)
   - Criar PAT classico em <https://github.com/settings/tokens> com escopos
     `repo` + `workflow`.
   - Adicionar em <https://github.com/emersonbusson/civm/settings/secrets/actions>
     como `RELEASE_PLEASE_TOKEN`.
   - Workflow ja faz fallback `secrets.RELEASE_PLEASE_TOKEN || secrets.GITHUB_TOKEN`.
2. **GitHub App** (ideal long-term, mais setup):
   - Instalar app com permissoes `contents: write` + `pull-requests: write`.
   - Trocar token no workflow pela action `actions/create-github-app-token@v1`.
3. **Re-run manual** (sem upgrade):
   - Quando release-please abrir o PR, `gh pr ready <num>` + close/reopen
     forca CI a rodar.

## Operacao diaria

```bash
# Ver PRs de release pendentes
gh pr list --repo emersonbusson/civm --search "in:title release-please"

# Inspecionar o PR de release antes do merge
gh pr view <num> --repo emersonbusson/civm

# Mergear (squash). Tag + release sao criados imediatamente.
gh pr merge <num> --repo emersonbusson/civm --squash
```

## Override de versao

Caso precise pular um bump sugerido (e.g. forcar major sem `feat!:`):

1. No PR aberto pelo release-please, comentar `release-as: 2.0.0`.
2. release-please reabre o PR com a versao forcada.

Ou ajustar `release-please-config.json` (campo `release-as`) e fazer
commit em `main` direto pela governance normal (issue + branch + PR).

## Validacao apos primeiro merge

```bash
gh release list --repo emersonbusson/civm
git fetch --tags origin
git tag --list 'v*'
```

Confirma que a primeira tag `v1.0.1` (ou superior) foi criada e
`CHANGELOG.md` aparece em `main`.

## Rollback trigger

Se release-please:

- Calcular bump incorreto persistentemente,
- Falhar criando PR/tag por permissao,
- Causar dois releases consecutivos sem changelog significativo,

desabilitar o workflow (`gh workflow disable release.yml`), reverter
o manifest pra ultima tag valida e revisar a config + token antes de
reativar.
