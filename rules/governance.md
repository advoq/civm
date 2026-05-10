---
name: governance
description: Regras para issues, PRs, labels, descrições em PT-BR e publicação segura.
paths:
  - ".github/**"
  - "CLAUDE.md"
  - "AGENTS.md"
  - "CODEX.md"
  - ".claude/rules/**"
---

# Governance rules

## Jules / agentes GitHub externos

- Jules deve usar `AGENTS.md` na raiz como arquivo de instruções do repo.
- Antes de publicar PR, Jules deve ler `.github/pull_request_template.md` e esta regra.
- Respostas ao humano, issues, PR descriptions, comentários user-facing e evidências de validação ficam em PT-BR.
- Identifiers, comentários de código, nomes de branch e títulos de commit/PR ficam em inglês; PR title usa Conventional Commits.
- Se só a metadata do PR falhar, corrigir título, corpo, labels ou assignee no GitHub e aguardar apenas `PR Governance / PR metadata guard`; não empurrar commit de código para corrigir descrição.
- Remover texto automático em inglês do corpo do PR quando conflitar com o template do repo.
- Nunca fazer auto-merge, auto-push direto para `origin/main`, `--no-verify`, deploy, rollback ou correção automática fora do escopo aprovado.

## Pull Requests

- Título do PR em inglês, formato Conventional Commits.
- Descrição do PR em Português (BR), clara para revisor fora da conversa.
- Usar `.github/pull_request_template.md` como base.
- Seções obrigatórias: `## Resumo`, `## Commits`, `## Issue`, `## Responsavel`, `## Labels`, `## Validacao`, `## Rollback trigger`.
- `## Commits` lista cada commit em tabela e usa `<details>` clicável para explicar contexto, impacto, arquivos principais e validação.
- O guard bloqueia `## Commits` vazio ou sem tabela, hash de commit e `<details>`.
- Linkar issue com keyword do GitHub: `Closes #NNN`, `Fixes #NNN` ou `Resolves #NNN`.
- Aplicar pelo menos uma label `type:*` e uma label `area:*`.
- Atribuir o PR e a issue linkada ao responsável pela entrega.
- PR e issue linkada devem compartilhar pelo menos um assignee. Quem fecha o problema assina a entrega.
- Não deixar placeholders do template (`#NNN`, marcador pendente, `<descreva>`) no PR final.

## Proteção automática

`.github/workflows/pr-governance.yml` roda em `pull_request_target` e faz
checkout do repositório base. A workflow não executa código vindo da branch do
PR; ela chama apenas:

```bash
go run ./tools/compexhubctl pr-guard --event "$GITHUB_EVENT_PATH"
```

O guard valida descrição, tabela de commits com detalhes clicáveis, link de
issue, assignee compartilhado e labels. Se a regra precisar mudar, altere
primeiro o comando em
`tools/compexhubctl/cmd/prguard` com teste focado.

## Issues

- Criar issue antes ou junto do PR quando a implementação ainda não tem issue.
- Usar o link no PR (`Closes #NNN`, `Fixes #NNN`, `Resolves #NNN`) para manter
  rastreabilidade e fechamento automático correto.
- Issues também devem ser compreensíveis em PT-BR, receber labels coerentes e
  estar atribuídas a quem assume a entrega.
