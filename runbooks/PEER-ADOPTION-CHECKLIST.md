# Runbook — peer adoption checklist (manual, per-repo)

> Use este checklist quando um peer repo (vitae, advoq, futuro repo X)
> for adotar o padrão ci-vm. Cada peer commita independentemente em
> branch isolada.

## Pré-requisitos

- [ ] ci-vm repo presente em `~/codespace/ci-vm/` (local OK; quando
      migrar pra GitHub, ajustar refs)
- [ ] Working tree do peer está clean OU você sabe o que está
      uncommitted (não vai perder nada)
- [ ] git branch atual é segura (não main de produção)

## Passo 0 — Backup do estado atual

```bash
cd ~/codespace/<peer-repo>
git status --short
# Se WIP: commit em branch atual OU stash com cuidado (risco de
# perder untracked se -u). Avalia caso a caso.
```

## Passo 1 — Criar branch isolada

```bash
git checkout -b chore/adopt-ci-vm
```

## Passo 2 — Decidir destino de MEMORY.md vs conversa.md

```bash
# Se peer tem AMBOS, deletar conversa.md e manter MEMORY.md:
[ -f conversa.md ] && [ -f MEMORY.md ] && git rm conversa.md

# Se peer tem só conversa.md, renomear:
[ -f conversa.md ] && [ ! -f MEMORY.md ] && git mv conversa.md MEMORY.md

# Se peer tem só MEMORY.md, nada a fazer.
# Se não tem nenhum, criar MEMORY.md (Passo 5).
```

## Passo 3 — Criar CODEX.md se não existir

Conteúdo minimal (header + cross-refs + snippet COMMUNICATION-STYLE
do Passo 4 + histórico).

## Passo 4 — Inserir snippet COMMUNICATION-STYLE em CLAUDE/AGENTS/CODEX

Copiar bloco entre marcadores `<!-- COMMUNICATION-STYLE:BEGIN -->`
e `<!-- COMMUNICATION-STYLE:END -->` de
`~/codespace/ci-vm/templates/COMMUNICATION-STYLE.md` para o final
de cada arquivo `CLAUDE.md`, `AGENTS.md`, `CODEX.md`.

Adicionar logo abaixo: `> Source canônico: ~/codespace/ci-vm/templates/COMMUNICATION-STYLE.md`

## Passo 5 — Criar MEMORY.md se não existir

Header com formato append-only canônico (campos branch/scope/goal/
actions/validations/commits/open-items/next-step).

Primeira entrada documenta esta adoção.

## Passo 6 — Adotar workflow CI (escolher tier)

```bash
mkdir -p .github/workflows

# Tier 3 (recomendado para repos novos — zero auth, self-healing):
cp ~/codespace/ci-vm/templates/ci-optimistic.yml.template \
   .github/workflows/ci.yml

# OU Tier 1 (router pattern):
cp ~/codespace/ci-vm/templates/ci-router.yml.template \
   .github/workflows/ci.yml
```

EDITAR: substituir steps "echo PLACEHOLDER" pelos gates reais do peer.

## Passo 7 — Commit

```bash
git add CLAUDE.md AGENTS.md CODEX.md MEMORY.md .github/workflows/ci.yml
git commit -m "chore: adopt ci-vm pattern (style + CODEX/MEMORY + ci workflow)"
```

## Passo 8 — Restaurar WIP (se houve stash)

```bash
git checkout <branch-original>
git stash pop
```

## Passo 9 — Branch protection no GitHub (admin)

Settings → Branches → main → Require status checks:

- [ ] Adicionar `Gates (typecheck, test, build, invariants)` como required
- [ ] Remover jobs individuais antigos (lint, test, etc)

## Verificação

```bash
for f in CLAUDE.md AGENTS.md CODEX.md; do
  grep -qF "<!-- COMMUNICATION-STYLE:BEGIN -->" "$f" && echo "OK: $f" || echo "FAIL: $f"
done
[ -f .github/workflows/ci.yml ] && python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "ci.yml OK"
```

## Histórico

- **2026-05-10** — primeira versão. Criada após advoq adoption ser
  bloqueada por classifier de auto-mode (73 uncommitted files). Este
  checklist permite adoção manual sem risco de perder WIP.
