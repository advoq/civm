---
name: testing
description: Regras de teste em web (Vitest, Playwright) e api (testify, testcontainers).
paths:
  - "**/*.test.ts"
  - "**/*.test.tsx"
  - "**/*.spec.ts"
  - "**/*_test.go"
  - apps/web/tests/**
---

# Testing rules

## Coverage 100% (mandatório a partir de 2026-04-28)

**100% test coverage em código de produção.** Não-aspiracional, **invariante #10 enforce em CI**.

- **Threshold em CI:** 98% (margem de 2% para boilerplate inevitável). Ramp para 99% em M5 + 100% North Star.
- **Exclusões explícitas** em `docs/COVERAGE-EXCLUSIONS.md` append-only com razão obrigatória + condição de remoção.
- **Marcadores inline:**
   - Go: `// nocov:start ... // nocov:end` para blocos defensivos (errors `panic` paths, `init()` boilerplate). Cada uso vai com comentário explicando.
   - TS/TSX: `/* c8 ignore start */ ... /* c8 ignore end */` (v8 provider).
- **O que conta como produção:**
   - Go: tudo em `services/api/` exceto `cmd/<binary>/main.go` (bootstrap), arquivos `*_test.go`, e gerados (`*.gen.go`, `internal/platform/db/sqlc/`).
   - TS: tudo em `apps/web/src/`, `packages/*/src/`, `tools/` exceto `*.test.ts`, `*.spec.ts`, `__mocks__`, `next-env.d.ts`, generated types (`packages/api-client/src/generated/`).
   - **Migrations SQL** estão fora (testadas em integration tests, não unit).
   - **`infra/` e `.github/`** estão fora.

### Por quê 100%

- 80% threshold (anterior) virava teto, não chão. Coverage cai sem ninguém perceber.
- Bug em código não-coberto é detectado em produção, não em CI. Caro.
- Testar sempre força melhor design — função impossível de testar geralmente é função mal-projetada.
- "Cobrir tudo 100% com os testes" — decisão do usuário em 2026-04-28.

### Como ramp do estado atual

Hoje (M0+M1.parte1+M2) não há quase nada em código de produção (Tier-1 ainda em construção).

- **M3 (auth port):** todo código de auth nasce com test coverage 100%. Sem exceção.
- **M4 (hub shell):** componentes UI cobertos via component test + E2E.
- **M5 (observability):** invariante #10 ativo em CI, threshold 98%.

Código pré-existente que não atinge threshold: cada arquivo entra em `docs/COVERAGE-EXCLUSIONS.md` com prazo (ex.: "ramp para 100% em M3 — owner @user, due 2026-06-30").

## Frontend (apps/web)

### Vitest

- Test globals (`describe`/`it`/`expect`) via `vitest/globals` em `vitest.config.ts` (não Jest).
- jsdom environment para component tests.
- Coverage via v8 provider, **threshold 98% em CI** (target 100%).
- React Testing Library para components — query por role/label, não por testid quando possível.
- Test files ao lado do código: `Foo.tsx` + `Foo.test.tsx`.

### Comandos

```bash
npm run test --workspace @compexhub/web         # Run once
npm run test:watch --workspace @compexhub/web   # Watch mode
npm run test:coverage --workspace @compexhub/web
```

### Playwright (E2E)

- Specs em `apps/web/tests/e2e/`.
- Visual regression separado em `visual-regression.spec.ts`. Snapshots em `apps/web/tests/e2e/visual-regression.spec.ts-snapshots/`.
- PWA E2E em `npm run test:e2e:pwa` — habilita `ENABLE_PWA=true`.
- Sempre rodar em headless local + CI. Headed para debug local apenas.
- Cobertura crítica: login → launcher → entrar em produto → editar → prompter (M3+).

## Backend (services/api)

### Go testing + testify

- Arquivos `*_test.go` ao lado do código.
- `testify/assert` e `testify/require`. Preferir `require` para fail-fast em setup.
- Tabela de cenários para >2 casos — pattern `tests := []struct { name, ... }{...}` + `for range; t.Run(...)`.

### testcontainers-go (integration)

- Schemas-per-tenant testados contra Postgres real via testcontainers.
- Cada teste cria seu próprio workspace + schemas → roda assertions → cleanup.
- Usar build tag `//go:build integration` para isolar de unit tests.

### Comandos

```bash
cd services/api
go test -race -count=1 ./...                              # unit
go test -race -count=1 -tags=integration ./...            # integration (precisa Docker)
go test -race -count=1 -coverprofile=cover.out ./...
go tool cover -html=cover.out                             # ver cobertura
```

## Cross-product E2E (M4+)

`tests/e2e/` na raiz cobre fluxos que tocam múltiplos produtos:

- Login → launcher → Orador Fluido editor → workspace switch → outro produto (quando 2º existir).
- Auth + tenancy isolation: criar 2 workspaces em users diferentes, garantir que dados não vazam.

## Disciplina (Kahneman)

1. **Test cobre positivo E negativo.** "Login com password correta" + "login com password errada returna 401 + audit log".
2. **Tabela-driven para >2 cenários.** Reference class evita bias.
3. **Sem `t.Skip`** sem motivo documentado e issue rastreável.
4. **Sem `Sleep`** para sincronizar — use channels/contextos/eventos.
5. **Schemas-per-tenant: 2 tenants no mesmo teste**. Garante isolamento de cross-tenant.

## Don't

- ❌ Mock dependências internas do projeto (use real Postgres via testcontainers).
- ❌ Test que depende de ordem de execução.
- ❌ Snapshots de visual regression sem revisar diff (gera ruído).
- ❌ `t.Skip("flaky")` sem issue assignada e prazo.
- ❌ **Cobertura <98% em código novo** de produção (invariante #10).
- ❌ Skip de teste em CI via `if (process.env.CI)` — flaky test deve ser fixado, não silenciado.
- ❌ Adicionar exclusão a `docs/COVERAGE-EXCLUSIONS.md` sem razão concreta + condição de remoção.
- ❌ `// nocov` ou `/* c8 ignore */` sem comentário adjacente explicando.
- ❌ Submeter shell scripts ou Makefile ao repo (sem teste viável); use `npm run <script>` ou `go run ./tools/compexhubctl <cmd>`.
