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
JSON por evento** (campos `ts`/`timestamp`, `level`, `event`, `vm`, + dados).
ERROR/CRITICAL também vão pra stderr.

- **Orchestrator scale-to-zero (dono vivo do power-state, desde 2026-06-17):**
  `civm-vm-orchestrator.ps1` escreve em **`V:\civm-orchestrator.log`** (campos
  `ts`, `level`, `event`, + dados). Catálogo de eventos: `tick` (cada decisão:
  `vm`, `queued`, `running`, `idle_min`, `v_free_gb`, `can_panic`),
  `vm_started`, `idle_debounce`, `stop_aborted_active_job`,
  `disk_warn`/`disk_warn_clean` (piso warn 28 GB: limpeza online segura),
  `disk_panic` (piso panic 18 GB: compacta mesmo com job ativo), `reclaim_start`,
  `reclaim_post_off_remeasure`, `reclaim_skip_insufficient_slack`,
  `reclaim_skip_locked`, `reclaim_abort_vm_not_off`, `reclaim_done`,
  `reclaim_no_progress`, `guest_full_clean`, mais os `*_warn`/`*_probe_failed`
  best-effort (`vfree_probe_failed`, `guest_active_probe_failed`,
  `guest_full_clean_warn`, `disk_warn_clean_warn`) e `orchestrator_error`. Fonte
  de verdade: `docs/specs/orchestrator-scale-to-zero/SPEC.md`.
- **Mecanismo de reclaim antigo (SUPERSEDED 2026-06-17, tasks `Disabled`):** o
  `civm-vhdx-autoreclaim`/`optimize`/`optimize-watchdog` escreviam em
  `V:\civm-hyperv-maintenance.log` com eventos `autoreclaim_*`, `optimize_*`,
  `emergency_reclaim_*`, `watchdog_*`. Catálogo preservado para leitura
  histórica; esses eventos não saem mais em operação normal — o orchestrator é
  o emissor vivo.

**Hooks de job:** registram em `hooks.jsonl` (uma linha por job-started/finished,
com `WorkRoot`, disco, cleanup aplicado).

## Métricas

`civmctl metrics dump --stdout` e o **Prometheus textfile collector** (via
`civmctl-metrics.timer`) expõem contadores de capacidade/disco/cleanup para
scrape local. `host-metrics.json` (no host, `V:\`) carrega `v_free_gb` e o gap do
VHDX, consumido pelo guard de headroom do reclaim.

## Log de validação empírica (`validation.md`)

`validation.md` na raiz é o log append-only de **toda validação empírica de
infra** — a fonte de verdade para "isso está de fato funcionando agora?". A
definição, a taxonomia de categorias e o framing Kahneman #13 vivem no **header
do `validation.md`**; complementa o `vm.md` (inventário da máquina). Validação de
app vive no `validation.md` do **acme** (independente); não logue app aqui.

Regras de uso:

- Append-only: entrada mais recente no fim; nunca delete, reescreva nem reordene.
  Leia de baixo para cima.
- Toda entrada carrega DADOS medidos (número real, sem adjetivo antes do número)
  e um veredito explícito.
- Schema: `## YYYY-MM-DD HH:MM -03 — <titulo>`, depois `**O que:**`,
  `**Dados medidos:**`, `**Veredito:**` (✅/🔴/🟡) e `**Proxima acao:**`.
  Opcionais: `**Categoria:**` (tag da taxonomia) e `**Como medir:**` (comando de repro).
- Nunca persista secret/token/PAT/chave, valor de env ou PII.

## Não logar segredo

Nunca logar token/PAT/chave raw (GitHub App key, SSH key, `gh` token). Mascarar
ou omitir. civm é infra: não há PII de usuário final no caminho.

## Don't

- ❌ `fmt.Println` / `log.Printf` em produção (use `slog`).
- ❌ Engolir erro sem log de contexto (`%w` + `slog`).
- ❌ Logar token/chave/secret raw.
- ❌ Métrica/evento órfão sem consumidor (`civmctl health`, runbook, scrape).
- ❌ Task host que muta sem emitir evento no log estruturado da sua camada
  (`V:\civm-orchestrator.log` para o orchestrator vivo;
  `V:\civm-hyperv-maintenance.log` para o mecanismo de reclaim antigo).
