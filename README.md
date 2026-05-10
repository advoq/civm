# ci-vm — infraestrutura de CI compartilhada

Repo dedicado para infra de CI/CD que é cross-cutting entre os projetos
do mesmo dono. Source-of-truth para:

- **Templates de workflow** (router, optimistic-retry, reusable)
- **Runbooks operacionais** (vitae-ci self-hosted runner, billing fallback)
- **Disciplinas metodológicas** (SSDV3 spec-driven dev, Kahneman, invariantes)
- **Regras granulares** (testing, security, governance, observability,
  ssdv3) que repos podem vendor

Cada repo do dono é peer self-contained — adota o que faz sentido pra
ele via copy/paste OU `go run` cross-repo OU reusable workflow.

## Estrutura

```
ci-vm/
├── README.md                ← este arquivo
├── runbooks/                ← procedimentos operacionais
│   ├── MULTI-PROJECT-RUNNER.md   (setup vitae-ci self-hosted compartilhado)
│   ├── CI-BILLING-FALLBACK.md    (3 tiers de fallback billing GitHub)
│   ├── CI-GITHUB-APP-SETUP.md    (rota upgrade para GitHub App)
│   └── LOCAL-CI-DISCIPLINE.md    (local CI é gate de verdade)
├── templates/               ← arquivos copy-paste em peer repos
│   ├── ci-optimistic.yml.template (Tier 3 zero-auth self-healing)
│   ├── ci-router.yml.template     (Tier 1 detector + roteamento)
│   └── COMMUNICATION-STYLE.md     (snippet pra CLAUDE/AGENTS/CODEX)
├── disciplines/             ← metodologia + filosofia
│   ├── KAHNEMAN-DISCIPLINES.md       (12 disciplinas Sistema 1 vs 2)
│   ├── SSDV3-PROMPTS.md              (Spec-Driven Dev V3 prompts)
│   ├── INVARIANTS.md                  (catálogo dos 14 invariantes)
│   └── COVERAGE-EXCLUSIONS-template.md (formato de exclusões coverage)
├── rules/                   ← regras granulares (.claude/rules/* portáveis)
│   ├── ssdv3.md
│   ├── testing.md
│   ├── security.md
│   ├── governance.md
│   └── observability.md
├── .github/workflows/       ← próprio CI deste repo
│   └── ci.yml
└── .gitignore
```

## Como adotar (em qualquer peer repo)

### Mínimo (estilo + auditoria)

1. **Copiar regra de Communication & report style** pra `CLAUDE.md`,
   `AGENTS.md`, `CODEX.md` do peer repo:

   ```bash
   # Copia bloco entre marcadores BEGIN/END
   awk '/<!-- COMMUNICATION-STYLE:BEGIN -->/,/<!-- COMMUNICATION-STYLE:END -->/' \
     ~/codespace/ci-vm/templates/COMMUNICATION-STYLE.md
   ```

2. **Auditar via Go** (sem dependência local):

   ```bash
   go run github.com/emersonbusson/compexhub/tools/compexhubctl@latest audit comm-style
   ```

   Exit 0 = 3/3 ok; exit 1 = falta seção em algum arquivo.

### CI workflow (escolher 1 dos 3 tiers)

- **Tier 1 (router):** copiar `templates/ci-router.yml.template` pra
  `.github/workflows/ci.yml` do peer. Roda detector heurístico,
  decide entre ubuntu-latest e vitae-ci.
- **Tier 3 (optimistic-retry):** copiar
  `templates/ci-optimistic.yml.template`. Sempre tenta ubuntu-latest;
  se falhar, dispara vitae-ci. Self-healing, zero auth.

Em ambos: aggregator job final `Gates (typecheck, test, build, invariants)`
deve ser configurado como required check em branch protection.

### Disciplinas + invariantes

Ler `disciplines/KAHNEMAN-DISCIPLINES.md` e `disciplines/SSDV3-PROMPTS.md`.
Adotar invariantes listados em `disciplines/INVARIANTS.md` que façam
sentido pro seu repo (alguns são compexhub-specific, ex.: invariantes #2,
#3, #9 que falam de stack Next.js/Go).

Cada peer pode implementar próprio detector dos invariantes que adotar.
Exemplo: invariante #14 (no `.sh` em tools/) é genérico — copiar lógica
do compexhubctl `tools/compexhubctl/cmd/checkinvariants/check_no_shell_scripts.go`.

## Runner self-hosted (vitae-ci)

Se múltiplos repos do dono compartilham o runner self-hosted, ler
`runbooks/MULTI-PROJECT-RUNNER.md` para setup com N runners systemd
isolados, capacity planning e checklist de adoção por peer.

## Filosofia

- **Local é o gate de verdade** (`runbooks/LOCAL-CI-DISCIPLINE.md`).
  CI remoto é mirror informativo. Validação real acontece no laptop
  do dev antes de push.
- **Cada repo é self-contained.** ci-vm é a única exceção (admin
  cross-cutting). Repos peer não conhecem uns aos outros.
- **Disciplina vai em invariantes**, não em manual. Se a regra é
  importante, vira gate de CI que falha automático.

## Quando atualizar

- Mudança em template/runbook: commit em ci-vm; peer repos pegam na
  próxima copy/vendor manual.
- Mudança em disciplina: atualizar `disciplines/` + comunicar peers
  (não há propagação automática — vendor é vendor).

Versionamento: tags semver opcionais. Peer repos podem travar em
`@v1.x` se quiserem.

## Histórico

- **2026-05-10** — Bootstrap inicial. Extraído de compexhub conforme
  proposta `docs/proposals/CI-VM-EXTRACTION.md`. Estrutura inicial:
  4 runbooks + 3 templates + 4 disciplines + 5 rules + próprio CI.
