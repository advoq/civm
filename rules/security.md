---
name: security
description: Segurança do civm — gitleaks, segredos do runner/host, runner self-hosted, privilégio mínimo, anti-skynet.
paths:
  - "**/*"
---

# Security rules

civm é infra operacional (runner self-hosted + camada host Hyper-V). A superfície
de segurança é **segredo, privilégio do host e código de PR não-confiável em
runner self-hosted** — não há web/HTTP/tenant/DB. Detalhe em `SECURITY.md` e
`docs/INVARIANTS.md`.

## Invariantes (CI gates)

1. **Sem secrets hardcoded** — gitleaks em pre-commit + pre-push + CI.
2. **`govulncheck` limpo** + `go vet` + `go test -race` verdes.
3. **Nunca logar token/chave/secret raw** (ver `rules/observability.md`).

## Segredos do civm

- **Nunca** commitar segredo; `.env` no `.gitignore`.
- Os segredos do civm são: o **GitHub App de release** (`RELEASE_APP_ID` +
  `RELEASE_APP_PRIVATE_KEY`, no Secrets do repo), a **chave SSH host→guest**
  (`C:\ProgramData\civm\ssh`, dona/legível só por SYSTEM) e o **token `gh`**
  (`GH_CONFIG_DIR`/PAT de contingência). Nenhum vive no repo.
- Validar na inicialização que o segredo requerido existe; rotacionar qualquer
  um exposto.

## Runner self-hosted

- Jobs em `runs-on: [self-hosted, civm]` devem rodar apenas PR confiável ou
  same-repo.
- Evitar `pull_request_target` quando qualquer step faz checkout ou executa
  código da branch do PR.
- **Nunca** expor secrets a código vindo de fork em runner self-hosted.
- Runners legacy/offline são removidos **manualmente** via `gh api -X DELETE`
  após revisão humana; `civmctl doctor` apenas reporta.

## Privilégio mínimo (host)

- As tasks `deploy/windows/*.ps1` rodam como **SYSTEM** com o direito Hyper-V
  (Optimize-VHD/Start-VM/Get-VM), acesso a `V:` e SSH ao guest — sem segredo de
  repo embutido. Reversíveis por `schtasks /delete`.
- No guest, o único wrapper NOPASSWD com caminho validado é
  `civm-safedelete` (`deploy/sudoers.d/civm-cleanup`) — preferido a `NOPASSWD`
  em `rm`/`chown` crus.

## Validação de entrada

- Todo `owner/repo` vindo de input externo passa por `ValidateRepo`
  (`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`) antes de virar
  argumento de `gh api`. Nunca interpolar input não-validado em comando.

## Anti-skynet

civm **detecta, nunca corrige automaticamente**. Nunca: auto-commit/revert/push/
merge sem aprovação humana; trigger de deploy/rollback; mutar workspace de peer
sem confirmação; persistir secret em qualquer arquivo do repo; executar comando
de input externo sem validação.

## Reportar vulnerabilidade

Ver `SECURITY.md`: reportar **privadamente ao mantenedor** (canal privado, não
issue pública) antes de divulgar.

## Don't

- ❌ Hardcode de secret em qualquer arquivo do repo.
- ❌ Logar token/chave/secret raw.
- ❌ Rodar fork não-confiável em self-hosted com secrets acessíveis.
- ❌ `pull_request_target` executando código de PR de fork.
- ❌ `NOPASSWD` em comando cru destrutivo (use wrapper validado).
- ❌ Interpolar `owner/repo` não-validado em `gh api`.
- ❌ Pular gitleaks via `--no-verify`.
- ❌ Auto-mutar peer repo / VM sem aprovação humana.
