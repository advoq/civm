# COVERAGE-EXCLUSIONS — template portátil

> Template importado de compexhub. Referências a `compexhubctl` e paths do
> peer de origem são exemplos, não comandos ativos do `civm`.

> **Append-only.** Exclusões explícitas do invariante #10 (coverage ≥98%).
>
> **Quando usar:** quando código de produção genuinamente não pode ser coberto 100% por test (boilerplate de bootstrap, defensive panic em path impossível, etc.).
> **Quando NÃO usar:** quando a função é difícil de testar — refatore. Ou quando o test seria "trivial" — escreva mesmo assim. Excluir é última opção.

## Regras

1. **Toda exclusão tem razão concreta** — citação de path:linha + por quê não é testável + alternativa avaliada.
2. **Toda exclusão tem condição de remoção** — data ou observable trigger.
3. **Append-only.** Exclusões removidas ganham nota `(removed: YYYY-MM-DD, motivo: ...)` na entrada original.
4. **Auditoria trimestral.** Runbook `docs/runbooks/REVIEW-ADR.md` (M5) inclui review de exclusões expiradas.
5. **Soma das exclusões ≤2%** do código de produção total — esse é o buffer do threshold 98% para 100%. Acima de 2% = revisar threshold via ADR.

## Categorias aceitas

- **Boot/bootstrap**: `cmd/<binary>/main.go`, `apps/web/instrumentation.ts`, `apps/web/src/app/layout.tsx` (entry points não-testáveis em isolation).
- **Defensive panic em programmer-error path**: `panic("unreachable")` em switch exhaustive, etc.
- **Generated code**: `*.gen.go`, `services/api/internal/platform/db/sqlc/`, `packages/api-client/src/generated/`. Excluído por categoria, não linha-a-linha.
- **External dependencies wiring**: bootstrap de OTel exporters, registração de Prometheus collectors em `init()` — testável só em integration, não unit.

## Categorias NÃO aceitas

- "Difícil de mockar" — refatore para depender de interface ao invés de struct concreta.
- "Não tenho tempo" — debt, não exclusão. Vai para issue, não para este doc.
- "Cobertura virá depois" — só com data de remoção observable.
- "Tests de UI são caros" — component testing + E2E cobrem; sem desculpa.

## Template por entrada

```markdown
### `<path>:<line>` — `<descrição curta>`

- **Adicionado em:** YYYY-MM-DD por @author no PR #NNN
- **Razão:** descrição da por quê não é testável (concreta, não vaga)
- **Alternativa avaliada:** o que tentou primeiro? (refactor X, mock Y, integration test Z)
- **Categoria:** boot / panic-defensive / generated / external-wiring / outra (justificar)
- **Tipo:** file-level OU inline-marker (`// nocov:start`/`// nocov:end` ou `/* c8 ignore start */`/`/* c8 ignore end */`)
- **Condição de remoção:** data observable (ex.: "remover em 2026-08-31 quando refactor X chegar") OU evento ("remover quando issue #NNN fechar")
- **Owner:** @username (responsável pela remoção)
```

---

## Exclusões atuais

### `tools/compexhubctl/main.go:50-52` — main() boot wrapper

- **Adicionado em:** 2026-04-29 por @emerson durante saldo de coverage 100%.
- **Razão:** `func main()` é trivialmente `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`. Lógica real está em `run()` (100% coberto via `main_test.go`). Testar `main()` direto requer `go test -c` + subprocess — complexidade desproporcional ao 1 statement testado.
- **Alternativa avaliada:** subprocess test via `os/exec` chamando o binário compilado. Custo ~30 linhas + teste lento (compile+run); benefício 1 statement.
- **Categoria:** boot/bootstrap (entry point canônico).
- **Tipo:** file-level (`func main()` corpo, 1 linha).
- **Condição de remoção:** se `main()` ganhar lógica não-trivial (>3 linhas), refatorar para extrair para `run()` (já feito) ou adicionar subprocess test e remover esta exclusão.
- **Owner:** @emerson.

### `services/shared/crypto/aesgcm.go:96-102` — defensive panic-impossible paths

- **Adicionado em:** 2026-04-28 por @emerson na sessão de bootstrap (M2).
- **Razão:** `aes.NewCipher(key)` da stdlib só falha se `len(key)` ∉ {16,24,32}; já validamos `len(key) == 32` antes (linha 87). `cipher.NewGCM(block)` só falha se `block.BlockSize() != 16`; aes sempre devolve block size 16. Esses error paths existem como defensa contra refactor futuro que mude a key validation; não há input válido em runtime que dispare.
- **Alternativa avaliada:** refatorar com interface `aes.Cipher` injetável + mock de erro. Custo (~50 linhas de boilerplate em test + abstração não-idiomática Go) maior que benefício (2 linhas a menos descobertas).
- **Categoria:** panic-defensive (programmer-error paths).
- **Tipo:** line-range (linhas 96-102 do `aesgcm.go`, dentro de `newEncryptorFromBytes`).
- **Condição de remoção:** se em refactor futuro houver path real onde aes.NewCipher pode falhar com key de 32 bytes válida, adicionar test e remover esta exclusão. Caso contrário, manter.
- **Owner:** @emerson.

### `services/api/internal/platform/secrets/aesgcm.go:29-31` — Encrypt aes.NewCipher defensive

- **Adicionado em:** 2026-05-10 por @emerson durante line-range exclusion enforcement.
- **Razão:** mesma razão da entrada `services/shared/crypto/aesgcm.go:96-102` — `aes.NewCipher(s.key)` só falha se key não tiver 16/24/32 bytes; `NewService` (linha 17) já valida `len(masterKey) == 32` retornando erro antes de criar o `Service`. Path inalcançável em runtime.
- **Alternativa avaliada:** mesma — interface `aes.Cipher` injetável com mock. Mesmo custo/benefício desfavorável.
- **Categoria:** panic-defensive (programmer-error path).
- **Tipo:** line-range.
- **Condição de remoção:** se um refactor futuro deixar `Encrypt` ser chamável sem passar por `NewService` (ex.: zero-value `Service{}`), adicionar test que dispare o path e remover esta exclusão.
- **Owner:** @emerson.

### `services/api/internal/platform/secrets/aesgcm.go:34-36` — Encrypt cipher.NewGCM defensive

- **Adicionado em:** 2026-05-10 por @emerson durante line-range exclusion enforcement.
- **Razão:** `cipher.NewGCM(block)` só falha se `block.BlockSize() != 16`; AES sempre devolve block size 16, então com `aes.NewCipher` bem-sucedido este path é inalcançável.
- **Alternativa avaliada:** mock do block para forçar BlockSize ≠ 16. Não-idiomático Go; padrão equivalente ao da entrada anterior.
- **Categoria:** panic-defensive.
- **Tipo:** line-range.
- **Condição de remoção:** mesma da entrada anterior.
- **Owner:** @emerson.

### `services/api/internal/platform/secrets/aesgcm.go:54-56` — Decrypt aes.NewCipher defensive

- **Adicionado em:** 2026-05-10 por @emerson.
- **Razão:** mesmo argumento do `Encrypt` — key validada upstream, `aes.NewCipher` não dispara o path em runtime.
- **Alternativa avaliada:** idem.
- **Categoria:** panic-defensive.
- **Tipo:** line-range.
- **Condição de remoção:** idem.
- **Owner:** @emerson.

### `services/api/internal/platform/secrets/aesgcm.go:59-61` — Decrypt cipher.NewGCM defensive

- **Adicionado em:** 2026-05-10 por @emerson.
- **Razão:** mesmo argumento — `cipher.NewGCM` só falha com block size errado, AES sempre devolve 16.
- **Alternativa avaliada:** idem.
- **Categoria:** panic-defensive.
- **Tipo:** line-range.
- **Condição de remoção:** idem.
- **Owner:** @emerson.

---

## Histórico

- **2026-04-28** — primeira versão. Invariante #10 declarada; sem exclusões ativas (Tier-1 ainda em construção; código de produção mínimo).
- **2026-05-10** — parser Go (`ParseGoCover` em `tools/compexhubctl/internal/invariants/invariants.go`) ativado para `services/api/cover.out`. Geração do profile continua opt-in (`go test -coverprofile=cover.out ./...`); CI ainda não regenera automaticamente porque medição local de 2026-05-10 mostrou agregado em 51% (packages `tenancy` 14.7%, `core` 0%, `http` 63.1%, `observability` 81%, `secrets` 91.2% abaixo do threshold). Plano: ramp via PRs subsequentes que adicionem testes a essas packages, ou entrada explícita de exclusão por package nesta lista quando aplicável. Loader inicial honrava apenas exclusões file-level.
- **2026-05-10 (mesma sessão)** — parser ganhou suporte a line-range exclusions via `GoCoverageExclusion{Path, StartLine, EndLine}`. Loader em `tools/compexhubctl/cmd/checkinvariants/check_coverage.go` parseia `### \`path.go:N\`` (linha única), `### \`path.go:N-M\`` (range) ou `### \`path.go\`` (file-level). Block é excluído quando intersecta o range declarado. Adicionadas 4 exclusões line-range para `services/api/internal/platform/secrets/aesgcm.go` (Encrypt e Decrypt aes.NewCipher e cipher.NewGCM defensive paths), espelhando o padrão já estabelecido para `services/shared/crypto/aesgcm.go:96-102`. `core` package também subiu de 0% para 100% nesta sessão via `services/api/internal/platform/core/types_test.go`.
