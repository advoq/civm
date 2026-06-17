---
name: observability
description: Observabilidade do civm — civmctl read-only, slog estruturado, host-metrics, log de manutenção, Prometheus textfile.
paths:
  - "cmd/civmctl/**"
  - "internal/**"
  - "deploy/windows/*.ps1"
---

# Observability rules

civm é infra Go (runner self-hosted + camada host Hyper-V). Observabilidade aqui
é sobre **estado da VM/runner e da limpeza de disco**, não sobre HTTP/tenant/DB.

## Estado da VM/runner (read-only)

`civmctl doctor --repos=auto --json` e `civmctl capacity --json` são a rota
read-only canônica para estado da VM/runner. `capacity` usa 90% de disco como
hard-fail para `accepting_jobs=false`; pressão antes do job começa em 60% via
`disk-watchdog` e hook `job-started` (`civm.DefaultPreCleanupPct`).

`civmctl disk-audit --json` reporta ownership seguro de disco: `_work`,
`_work/_tool`, `_work/_actions`, `$HOME/.cache`, `$HOME/go/pkg`,
`$HOME/codespace`, Docker reclaimable, `/var/log` e `/var/cache`. Clones em
`$HOME/codespace` são observabilidade-only e não são removidos automaticamente.

`civmctl health` agrega o estado dos timers. `civmctl-metrics.timer` deve ficar
habilitado junto com cleanup, disk-watchdog, mem-watchdog, runner-watchdog e
reverse-watchdog. Metrics missing é warning; cleanup e disk-watchdog missing
continuam críticos.

## Logs estruturados

**Go (civmctl):** `slog.JSONHandler` é o handler default. Nunca `fmt.Println` ou
`log.Printf` em produção — sempre `slog` com contexto.

```go
slog.InfoContext(ctx, "hook job-started",
    slog.String("repo", repo),
    slog.String("work_root", workRoot),
    slog.Int("disk_pct", pct),
)
```

**Camada host (PowerShell):** as tasks `deploy/windows/*.ps1` emitem **uma linha
JSON por evento** em `V:\civm-hyperv-maintenance.log` (campos `timestamp`,
`level`, `event`, `vm`, + dados). Eventos: `autoreclaim_*`, `optimize_*`,
`emergency_reclaim_*`, `watchdog_*`. ERROR/CRITICAL também vão pra stderr.

**Hooks de job:** registram em `hooks.jsonl` (uma linha por job-started/finished,
com `WorkRoot`, disco, cleanup aplicado).

## Métricas

`civmctl metrics dump --stdout` e o **Prometheus textfile collector** (via
`civmctl-metrics.timer`) expõem contadores de capacidade/disco/cleanup para
scrape local. `host-metrics.json` (no host, `V:\`) carrega `v_free_gb` e o gap do
VHDX, consumido pelo guard de headroom do reclaim.

## Log de validação empírica (`validation.md`)

`validation.md` na raiz é o log vivo de validações **empíricas** — a fonte de
verdade para "isso está de fato funcionando?" (box, VHDX, orchestrator, compact,
runners). Princípio Kahneman #13: **medir, não asseverar** — "código existe" ≠
"função ativa". Complementa o `vm.md` (que inventaria o estado da máquina): aqui
ficam as **medições que provam ou refutam** um comportamento (decision-table
PASS/FAIL contra o módulo deployado, V: livre antes/depois do compact,
`workers`/`idle_min` no instante).

Regras:

- Append-only como o `MEMORY.md`: entrada mais recente no fim; nunca delete,
  reescreva nem reordene entradas antigas. Leia de baixo para cima.
- Toda entrada registra DADOS medidos (números reais, sem adjetivo antes do
  número) e um veredito explícito.
- Schema por entrada: `## YYYY-MM-DD HH:MM -03 — <titulo>`, depois `**O que:**`,
  `**Dados medidos:**`, `**Veredito:**` (✅ funciona / 🔴 não / 🟡 parcial) e
  `**Proxima acao:**`.
- Nunca persista secret/token/PAT/chave, valor de env ou PII.

## Não logar segredo

Nunca logar token/PAT/chave raw (GitHub App key, SSH key, `gh` token). Mascarar
ou omitir. civm é infra: não há PII de usuário final no caminho.

## Don't

- ❌ `fmt.Println` / `log.Printf` em produção (use `slog`).
- ❌ Engolir erro sem log de contexto (`%w` + `slog`).
- ❌ Logar token/chave/secret raw.
- ❌ Métrica/evento órfão sem consumidor (`civmctl health`, runbook, scrape).
- ❌ Task host que muta sem emitir evento em `civm-hyperv-maintenance.log`.
