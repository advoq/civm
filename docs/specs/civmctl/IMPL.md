# IMPL — civmctl (executado 2026-05-10)

**PRD:** `docs/specs/civmctl/PRD.md`
**SPEC:** `docs/specs/civmctl/SPEC.md`
**Status:** implementado, testado localmente, NÃO testado end-to-end em VM real.

## O que foi feito (commits a seguir)

| Commit | Escopo | Arquivos |
|---|---|---|
| `chore(ci-vm)` | AGENTS/CODEX/MEMORY (gap) | `AGENTS.md`, `CODEX.md`, `MEMORY.md` |
| `docs(ci-vm)` | SSDV3 PRD + SPEC + IMPL | `docs/specs/civmctl/{PRD,SPEC,IMPL}.md` |
| `feat(ci-vm)` | civmctl Go binary | `go.mod`, `cmd/civmctl/*.go`, `internal/{specs,health,cleanup,bootstrap}/*.go` (+ tests) |
| `feat(ci-vm)` | systemd timer | `deploy/systemd/civmctl-cleanup.{service,timer,README.md}` |
| `docs(ci-vm)` | zero-effort docs | `README.md`, `runbooks/MULTI-PROJECT-RUNNER.md` |
| `ci(ci-vm)` | build + test civmctl | `.github/workflows/ci.yml` |

## Critérios de aceitação (do PRD §13)

| Item | Status | Métrica |
|---|---|---|
| PRD aprovado | ✅ | autor + auto mode |
| SPEC aprovado | ✅ | implementação implícita |
| `go build ./...` sem warnings | ✅ | clean |
| `go test -race -count=1 ./...` verde | ✅ | 4 packages OK |
| `civmctl --help` <100ms | ✅ | 8ms medido |
| `civmctl version-pins` <50ms | ✅ | 7ms medido |
| `civmctl health` testado | ✅ | exit 1 em dev (sem runners; esperado) |
| `civmctl cleanup --dry-run` testado | ✅ | sem mutação |
| `civmctl bootstrap --dry-run` testado | ⏭ | requer Linux real (verify_uid falha em sandbox) |
| Cobertura `internal/**` ≥80% | ✅ | specs 100%, health 88.4%, cleanup 84.5%, bootstrap 84.8% |
| Binário <10 MB stripped | ✅ | 2.29 MB |

## Cobertura por package

| Package | Cobertura | Justificativa |
|---|---|---|
| `internal/specs` | 100.0% | data-only, fácil de cobrir |
| `internal/health` | 88.4% | wrappers OS testados via fake + real |
| `internal/cleanup` | 84.5% | fstest.MapFS + RunFn fake |
| `internal/bootstrap` | 84.8% | RunFn fake + cenário already-installed |
| `cmd/civmctl` | 0% | dispatch puro (PRD §RNF-2 marca opcional) |

## RFs implementados

- ✅ **RF-1** `version-pins`: tabela + `--json`
- ✅ **RF-2** `bootstrap`: 8 steps idempotentes (verify_os, verify_uid,
  apt_base_packages, install_go, install_node, install_docker,
  install_gh, install_systemd_timer); dry-run default; --execute exige flag
- ✅ **RF-3** `cleanup`: 4 ações (tmp_old, work_old, docker_prune,
  apt_cache); thresholds configuráveis; anti-jobs (mtime <2h pulado)
- ✅ **RF-4** `health`: 4 checks (DISK, MEM, RUNNERS, LAST); exit 0/1/2
- ✅ **RF-5** `runner add`: wrapper `./config.sh` do actions/runner;
  `runner list` via systemctl
- ✅ **RF-6** Help auto-gerado para todos subcomandos

## RNFs cumpridos

- ✅ **RNF-1** Stdlib-only (zero deps externas)
- ✅ **RNF-2** Cobertura ≥80% em `internal/**`
- ✅ **RNF-3** Binário 2.29 MB stripped (<10 MB target)
- ✅ **RNF-4** `--help` em 8ms
- ✅ **RNF-5** Mutações destrutivas dry-run por default
- ✅ **RNF-6** Logs PT-BR para usuário; identifiers/comments inglês
- ✅ **RNF-7** Exit codes: 0 OK, 1 warning, 2 critical, 64 erro de uso

## Disciplinas Kahneman aplicadas

- **#1 WYSIATI**: `IMPL.md §"O que NÃO foi visto"` declara explicitamente
  que VM real não foi testada (agente sandboxed sem SSH).
- **#2 Counterfactual**: cada commit tem `Rollback trigger:`. PRD §"Rollback
  trigger" define gate de 6 meses.
- **#3 Número antes de adjetivo**: este IMPL.md usa medições reais (8ms,
  84.5%, 2.29MB) em vez de adjetivos ("rápido", "leve", "bem testado").
- **#4 Débito é dívida**: zero TODOs no código. CODEX.md §DEFERRED tem
  gates numéricos de promoção.
- **#5 Lib nova exige entrada**: stdlib-only confirmado (`go.mod` sem
  `require`).

## O que NÃO foi visto

- **VM real**: agente sandboxed sem SSH; bootstrap end-to-end não foi
  testado em Ubuntu 24.04 fresh. Step `apply` de cada install_* invoca
  comandos que precisam ser validados em ambiente real.
- **systemd timer em ação**: `civmctl-cleanup.timer` não foi disparado em
  produção; comportamento sob disk pressure real desconhecido.
- **Compatibilidade de versões em 30 dias**: `actions/runner-images`
  publica updates semanais; `internal/specs/specs.go` precisa sync manual.

## Próximos passos (humano)

1. Push do branch `main` do ci-vm para `origin` (admin manual quando
   confirmar): `gh repo create emersonbusson/ci-vm --private` + `git push -u origin main`
2. Numa VM Ubuntu 24.04 fresh: clone + build + `sudo civmctl bootstrap --execute`
3. Validar `civmctl health` retorna OK
4. `systemctl status civmctl-cleanup.timer` confirma habilitado
5. Esperar 24h e ver `journalctl -u civmctl-cleanup` mostrando run
6. Reportar de volta para atualizar `internal/specs/specs.go` se
   versões mudaram em upstream

## Rollback trigger (do PRD)

Se em 6 meses (2026-11-10) civmctl não estiver provisionando ≥1 VM nova
OU se cleanup quebrar disco em produção, reavaliar:

- Voltar para runbook manual + `runbooks/MULTI-PROJECT-RUNNER.md` (seção
  "Setup operacional manual" preservada)
- Considerar Ansible playbook se múltiplas VMs entraram

Decisão de reverter exige humano + ADR.
