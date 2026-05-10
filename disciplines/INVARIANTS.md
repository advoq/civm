# Invariantes portáveis

> Template importado de compexhub. Referências a `compexhubctl`, paths de
> `apps/web` e `services/api` são exemplos do peer de origem, não comandos
> ativos do `civm`. No `civm`, o gate operacional atual fica em
> `.github/workflows/ci.yml` e `rules/testing.md`.

As 13 invariantes deste repositório são **testáveis em CI** e **falham bloqueando merge** se violadas. Cada invariante é definida em `tools/compexhubctl/internal/invariants/` e orquestrada por `tools/compexhubctl/cmd/checkinvariants/` (Go).

Toda exceção exige ADR + waiver com expiry (data limite + condição de remoção).

| # | Nome | Rationale | Check (CI) | Onde dispara |
|---|------|-----------|------------|--------------|
| 1 | Sem secrets hardcoded | Vazamento via git push é irreversível. Atacante com diff history extrai keys mesmo após commit-revert. | `gitleaks dir --no-git --redact` em CI + `gitleaks git --pre-commit --staged` em pre-commit | pre-commit, pre-push, CI job `invariants` |
| 2 | Sem `console.log` em produção | Quebra observabilidade (sem nível, sem contexto, polui prod logs). Use telemetria adequada. | `compexhubctl check-invariants 2` — grep `console.log/warn/error/debug` em `apps/web/src/**` excluindo `*.test.*`/`*.spec.*` | pre-commit (lint-staged), CI |
| 3 | PII scrubbing em logs Go | LGPD + GDPR + ataque de exposição via log files. PII raw em logs compromete o usuário sem ele saber. | `compexhubctl check-invariants 3` — grep `slog.String("(cpf\|email\|phone\|password\|token)"\|...)` sem `MaskPII()` wrapper | CI |
| 4 | Conventional Commits | Histórico legível, automated changelog, scope whitelist evita "fix: things". | `commitlint` em commit-msg hook + scope-enum em `commitlint.config.js` | commit-msg hook |
| 5 | Sync rule (CLAUDE ≡ AGENTS ≡ rules) | Documentação de agente fica desincronizada → agentes seguem regras conflitantes → bugs sutis. | `compexhubctl check-sync` — staged-diff (pre-commit) ou HEAD-diff (CI). Skip via `[sync-skip-justified]` no body. | pre-commit, pre-push, CI |
| 6 | Rollback trigger presente em commits não-triviais | Decisão sem condição de reversão é Sistema 1 (Kahneman #2). Skin-in-the-game para PR. | `commit-msg` hook checa `Rollback trigger:` em body para `feat\|fix\|refactor\|perf` | commit-msg |
| 7 | OpenAPI válido | Contracts são source-of-truth. Spec inválida = TS gen falha + Go stubs erram = drift silencioso. | `spectral lint contracts/*.yaml services/api/openapi/*.yaml` | pre-commit (se OpenAPI staged), CI |
| 8 | TODO com owner+date | TODO genérico apodrece. Owner+date força decisão "agora ou em 2026-08-01". | `compexhubctl check-invariants 8` — grep `TODO\|FIXME` sem `(@user, YYYY-MM-DD)` em `apps/web/src/**`, `services/api/**`, `packages/**` (excluindo tests) | CI |
| 9 | Web não bypassa @compexhub/api-client | Drift de tipos entre web e API. Manter `@compexhub/api-client` como única ponte. BFF (`apps/web/src/app/api/**`) está isento. | `compexhubctl check-invariants 9` — grep `fetch('/api/v1/...')` ou `axios` para `/api/v1/` em `apps/web/src/**` (excluindo `app/api/`) | CI |
| 10 | Coverage ≥98% em produção | 80% virava teto, não chão. 100% mandatório (≥98% em CI com 2% buffer para boilerplate). Decisão do usuário 2026-04-28. | `compexhubctl check-invariants 10` — exige `docs/COVERAGE-EXCLUSIONS.md`; se reports existirem, valida `apps/web/coverage/coverage-summary.json` contra 98%. `services/api/cover.out` ainda é presença diagnosticada, sem parser Go ativo. | CI (gate, ainda com stub quando reports não existem). Localmente: `go run ./tools/compexhubctl check-invariants 10`. |
| 11 | Self-containment textual | Documentação não menciona projetos paralelos do autor. Decisão, lista de termos bloqueados e exceções legítimas em `docs/decisions/ADR-007-repo-self-containment.md`. | `compexhubctl check-invariants 11` — regex compilada em `tools/compexhubctl/internal/invariants/external_refs.go` aplicada a `.md`/`.yaml`/`.yml`. Sanitiza linha contra padrões legítimos (nome do autor, email, GitHub handle, module Go) antes da regex bloqueada. Exclui `MEMORY.md` e este arquivo + ADR-007 por meta-referência. | CI (gate). Localmente: `go run ./tools/compexhubctl check-invariants 11`. |
| 12 | Per-product isolation (apps/X ⊥ apps/Y) | Mudança em produto X **NÃO PODE** quebrar produto Y. CLAUDE.md §"Boundary discipline" proíbe `internal/apps/X/` importar `internal/apps/Y/`. Coordenação só via `internal/platform/`. Constraint declarada pelo usuário em 2026-04-30 — base do plano de reorg multi-produto. | `compexhubctl check-invariants 12` — varre arquivos `.go` em `services/api/internal/apps/<X>/` procurando imports `internal/apps/<other>/` onde `other != X`. Zero overhead (string scan, sem `go list`). | CI (gate). Localmente: `go run ./tools/compexhubctl check-invariants 12`. |
| 13 | NEXT_PUBLIC_E2E_AUTH_BYPASS=true proibido em arquivos de produção | Esta env var desabilita o auth gate do middleware Next.js (apps/web/src/middleware.ts) — usado **apenas** em playwright. Se chegar a produção, atacante bypassa signin completamente. Adicionada na Z7 do plano de production readiness (2026-04-30). | `compexhubctl check-invariants 13` — varre `.env*`, `vercel.json`, `Dockerfile*`, `fly.toml` procurando literal `NEXT_PUBLIC_E2E_AUTH_BYPASS=true`. Excecoes: `.env.example`, `.env.test`, `apps/web/tests/e2e/`. | CI (gate). Localmente: `go run ./tools/compexhubctl check-invariants 13`. |

## Como rodar localmente

```bash
go run ./tools/compexhubctl check-invariants        # roda todos
go run ./tools/compexhubctl check-invariants 1 3 6  # roda apenas os listados

go run ./tools/compexhubctl check-sync              # invariante #5 contra HEAD
go run ./tools/compexhubctl check-sync --staged     # invariante #5 contra staged
```

`npm run check:invariants` é o atalho.

## Inline waiver

Para casos pontuais, use comentário inline `// invariant-waive:#N -- <razão>`:

```ts
// invariant-waive:#2 -- inline script para dev-only localhost preview
console.warn("Cache clear failed", err);
```

O script de check ignora a linha. Razão é **obrigatória** após `--`. Auditável via `grep invariant-waive .`.

Casos típicos:

- `#2` em scripts inline injetados no HTML (template literals que viram código browser-side em dev).
- `#3` se PII tem que aparecer em audit log explícito (raríssimo; abrir ADR antes).
- `#8` em comentários históricos de migrations já mergeadas.

## Como adicionar exceção (waiver)

1. Abrir ADR em `docs/decisions/ADR-NNN-waiver-invariant-X.md`.
2. Documentar:
   - Qual invariante e por quê o waiver é necessário (caso real, não conveniência)
   - Escopo (paths específicos, datas)
   - **Expiry** numérico/observável: data limite OU condição de remoção
   - Quem aprovou
3. Adicionar config no checker Go (`tools/compexhubctl/internal/invariants/` ou `cmd/checkinvariants/`).
4. Append em `docs/META-AUDIT.md`.
5. Auditar trimestralmente (`docs/runbooks/REVIEW-ADR.md`).

Se waiver expirar e não for removido: **CI volta a bloquear**. Não há override silencioso.

## Mecanismo de exclusões de coverage (#10)

Quando código de produção genuinamente não pode ser coberto 100%, registra-se exclusão **explícita** em `docs/COVERAGE-EXCLUSIONS.md` (append-only). Categorias aceitas:

- **Boilerplate de bootstrap** (Go `cmd/<binary>/main.go`, Next.js `instrumentation.ts`).
- **Defensive panics em paths impossíveis** (errors `panic` que indicam programmer error, não user input).
- **Generated code** (`*.gen.go`, `packages/api-client/src/generated/`, sqlc output) — fora do escopo, não rebaixa coverage.
- **Migration SQL** — testado em integration tests, não em unit (fora).

Inline markers (granular dentro de função):

- **Go:** `// nocov:start` / `// nocov:end` — comentário adjacente explicando.
- **TS:** `/* c8 ignore start */` / `/* c8 ignore end */` — comentário adjacente.

Cada inline marker é **auditável** via grep CI; deve ter razão visível.

## Adicionando uma 11ª invariante

Toda invariante nova:

1. ADR explicando rationale + check method + falso-positivo esperado.
2. Implementação no `compexhubctl` (`tools/compexhubctl/internal/invariants/` ou novo subpacote).
3. Run em CI por **30 dias em modo warn** (loga mas não bloqueia) antes de promover a `error`.
4. Métricas: contagem de violações capturadas, falsos-positivos.
5. Após 30d, decisão: promover, manter warn, ou remover.

## Exceções de skip em CI

Diferente de invariantes (que são gates de qualidade do código), há
condições operacionais em que um step de CI pode falhar por motivo
**fora do controle do PR** — tipicamente quota/billing exhausted em
serviço externo. Para esses, o repo usa a composite action em
`.github/actions/billing-aware-step/action.yml` que converte falha em
"warning amarelo" (não bloqueia merge) somente quando a saída do step
contém marcadores inequívocos de billing/quota.

### Markers reconhecidos (case-insensitive)

- `payment required` ou `http 402`
- `quota exceeded`, `quota_exceeded`
- `insufficient quota`, `insufficient credits`, `insufficient balance`
- `billing failure`, `billing error`, `billing disabled`, `billing inactive`
- `exceeded usage` ou `exceeded limit` (combinado com `plan|tier|quota`)
- `rate limit` combinado com `plan`
- `out of quota`, `out of credits`

Lista append-only — adições documentadas no PR que adiciona o marker.

### Steps protegidos hoje

- `integration` job, steps "Apply public migrations" e "Run integration
  tests" — Neon serverless pode hit quota em preview branches.

### Steps NÃO protegidos (e por quê)

- `lint`, `test`, `build`, `contracts-check`, `invariants`: deterministic;
  qualquer falha aqui é bug real. Não há vetor billing.
- Auth/AI provider keys não são usadas em CI (somente runtime), então
  jobs de build não tocam APIs pagas.

### Como adicionar marker novo

1. Detectar a ocorrência: ler o log da run que falhou erroneamente.
2. PR amend na regex em `.github/actions/billing-aware-step/action.yml`
   (append-only — não remover marker existente).
3. Atualizar a lista acima neste arquivo no mesmo PR.
4. Testar via `.github/workflows/test-billing-aware.yml`
   (workflow_dispatch — não roda em push/PR).

### Tradeoff documentado

GitHub Actions não expõe `conclusion: skipped` mid-run. O melhor que a
action consegue é "exit 0 + ::warning::" — a check do PR fica amarela
em vez de vermelha. Para ter status "skipped" verdadeiro (cinza), seria
preciso preempar o step via `if:` antes dele rodar — só funciona quando
sabemos a priori que vai falhar (ex.: secret ausente, padrão usado no
job `integration` desde sempre via `steps.db.outputs.skip`).

## Histórico de invariantes promovidas/removidas

(append-only)

- 2026-04-28: Invariantes 1–9 declaradas em ADR-000 (formato) + CHARTER §"Tier-3 Infra". Status: warn em M0 (sem CI ainda); error em M1 (com Husky + CI ativos).
- 2026-05-05: Documento alinhado ao gate atual 1–13 e ao `compexhubctl check-invariants`; #10 segue com parser web ativo e parser Go pendente.
- 2026-04-28 (rev2): Invariante 10 (coverage ≥98%) adicionada após decisão do usuário "cobrir tudo 100% com os testes". Status: declarada em M2; checker Go valida `apps/web/coverage/coverage-summary.json` quando presente e reporta "implementation pending" quando reports não existem. Parser Go para `services/api/cover.out` ainda precisa ser promovido antes de transformar #10 em gate completo.
- 2026-04-29: Invariante 11 (self-containment textual) adicionada via ADR-007. Doc autocontida — sem menção a projetos paralelos do autor. Exceções legítimas (nome, email, GitHub handle, module Go) preservadas por contexto. `MEMORY.md` excluído por convenção (append-only). Status: ativa em CI a partir desta entry.
- 2026-04-30: Invariante 12 (per-product isolation) adicionada como base do plano de reorg multi-produto. Status: ativa em CI a partir desta entry.
- 2026-04-30: Invariante 13 (NEXT_PUBLIC_E2E_AUTH_BYPASS=true proibido em prod files) adicionada na fase Z7 do plano de production readiness. Z6 wireou `.env.production.example` com `BYPASS=false`; Z7 enforce que esse valor nunca derive para `true` em arquivos production-bound. Status: ativa em CI a partir desta entry.
