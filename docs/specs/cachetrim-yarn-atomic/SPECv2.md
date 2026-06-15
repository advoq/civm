# SPECv2 — Cache trim atômico por pacote (yarn)

> SSDV3 PASSO 2.5 (auditoria adversarial) → SPEC ativo. Refina o SPEC.

## Achados da auditoria 2.5

### A0 — Escopo: só o yarn v1? Análise por package manager

Trim por arquivo corrompe qual cache? Só os que guardam um pacote como **diretório
multi-arquivo** lido de forma estrita. Mapa:

| Cache                              | Unidade no disco        | Remoção parcial                                       |
| ---------------------------------- | ----------------------- | ---------------------------------------------------- |
| yarn v1 (`.cache/yarn/v6/npm-*/`)  | diretório multi-arquivo | **corrompe** — `--frozen-lockfile` dá ENOENT, não re-busca |
| yarn berry (`.yarn/cache/*.zip`)   | 1 zip                   | seguro (zip é atômico)                               |
| npm (`_cacache/content-v2/...`)    | 1 blob por hash         | seguro — content-addressed, re-busca o que falta     |
| pnpm (`.pnpm-store/v3/files/...`)  | 1 arquivo por hash      | seguro — content-addressed, re-busca                 |
| go-build / golangci                | par `-a`+`-d` ref cruzada | **corrompe** — `-a` órfão → `go vet` "can't import facts" |

**Dois** caches são vulneráveis, por razões diferentes (não só o yarn — correção do
incidente gates 2026-06-15). **yarn v1**: o pacote é um diretório e o yarn assume
que um diretório presente está completo → atômico por pacote (PackageDepth).
**go-build/golangci**: cada entrada é um par `<actionID>-a` + `<outputID>-d` ligados
por hash de conteúdo, e o `-d` pode viver em OUTRO diretório de prefixo — remover
qualquer um orfana a entrada e o `go vet` (modo "vet only, no build") quebra com
"can't import facts ... no such file or directory". Como as refs cruzam prefixos,
nem por arquivo nem por diretório é seguro → só o wipe do dir inteiro acima do cap é
atômico (WipeWhole). npm/pnpm são content-addressed (cada blob é uma unidade
completa) — uma ausência é detectada por integridade e re-baixada → seguros por
arquivo. (O advoq usa yarn v1 + go-build + golangci; não há `_cacache`/`.pnpm-store`
no guest.)

**Generalização (robustez a outros managers):** três modos, escolhidos por cache —
por arquivo (default, content-addressed); `PackageDepth=N` (pacote = diretório,
agrupa nos N primeiros segmentos; yarn v1 = 2); e `WipeWhole` (refs cruzadas
opacas, wipe do dir inteiro como backstop; go-build/golangci, cap generoso para o
wipe ser raro). Um manager futuro entra escolhendo o modo: profundidade fixa →
PackageDepth; refs cruzadas ou profundidade variável (ex.: grupo Maven) → WipeWhole.

### A1 — `.yarn/cache` (yarn berry) é zip de arquivo único

A família yarn casa `.cache/yarn*` (v1, pacote = diretório) **e** `.yarn/cache`
(berry, pacote = `<pkg>.zip`, arquivo único). Atômico-por-dir num cache berry
seria errado.

**Resolução:** sem caso especial. O glob de pacotes é `<root>/*/*` (profundidade
2). No berry os zips estão em profundidade 1 → o glob acha zero dirs de pacote →
caem no coletor de **arquivos soltos** → trim por arquivo. Um zip é unidade
atômica, então trim por arquivo é seguro nele. `DirAtomic` degrada para modo
arquivo sozinho quando não há dir de pacote. Vale para v1 (dir) e berry (zip).

### A2 — Pass 2 (hard-ceiling) pode remover pacote em escrita

O dois-passes do #124: Pass 1 preserva quentes (<MinProtect), Pass 2 impõe o teto
removendo protegidos se o cap não for atingível. Com cap por-dir patologicamente
apertado (4 dirs / 3GB = 0.75GB/dir < 0.84GB natural, tudo recente), Pass 1 não
alcança o cap → Pass 2 remove pacotes recentes. No `job-started` isso é benigno
(o install re-fetcha). O resíduo é o **disk-watchdog disparando no meio de um
install** e removendo o pacote em escrita.

**Resolução (2 camadas):**

1. **Atômico limita o estrago**: mesmo no resíduo, remove-se um pacote inteiro
   (re-fetchável), nunca um parcial — `ENOENT` em pacote pela metade some.
2. **Companion advoq (RF-5)**: reverter o `yarn-advoq-audit` (4º dir que **eu**
   criei em `security-scans.yml`). Com 3 dirs / 3GB = 1GB/dir > 0.84GB → Pass 1
   atinge o cap → Pass 2 **não dispara** para yarn no caso comum. Some o churn e
   o resíduo de race.

### A3 — Glob `*/*` em `.tmp`

`.cache/yarn-x/.tmp/<file>` casaria `*/*`. São transitórios de install; removê-los
atomicamente é inócuo. Sem ação.

## Decisão final (escopo do IMPL)

- **RF-1** (core, civm): `Cap.PackageDepth` (0 = por arquivo; N = por diretório de
  pacote, agrupando nos N primeiros segmentos) + `collectUnits` agrupando por
  pacote. yarn v1 = `PackageDepth=2`; berry/npm/pnpm = 0; dois-passes reaproveitado.
- **RF-3** (civm): `Cap.WipeWhole` para go-build/golangci (refs cruzadas): acima do
  cap, RemoveAll do dir inteiro (atômico). Cap do go-build subido 5→12GB para o
  wipe ser backstop, não o working-set normal (~2.2GB/dir).
- **RF-5** (companion, advoq): reverter `yarn-advoq-audit` → cache yarn default
  com `--mutex network` (3 dirs em vez de 4).
- **RF-4** (validação): rebuild + deploy civmctl → limpar caches corrompidos →
  re-run `web` do #1155 verde.

## Resíduos aceitos (documentados, #16)

- Disk-watchdog no meio de um install ainda pode remover um pacote (agora
  inteiro, re-fetchável) — não corrompe, no pior caso re-baixa. Aceito.
- Corrupção por escrita concorrente (2 jobs/mesmo dir) — fora de escopo,
  mitigada por dirs per-workflow + `--mutex`.
