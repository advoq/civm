# systemd units para civmctl

4 timers systemd disponíveis:
- `civmctl-cleanup.timer` — diário 04:00 UTC, full cleanup (Docker, /tmp,
  _work, apt). Idempotente e fail-closed quando há job/build ativo.
- `civmctl-disk-watchdog.timer` — hourly, dispara cleanup agressivo se
  disk >80%. Reativo a picos de uso entre execuções diárias e usa o mesmo
  guard de ociosidade do cleanup.
- `civmctl-runner-watchdog.timer` — a cada ~2min depois do boot, repara
  hooks e reinicia runners offline/failed se a VM estiver idle. Não faz
  rerun automático por padrão.
- `civmctl-reverse-watchdog.timer` — 4-em-4h, alerta se o disk-watchdog
  parou de disparar.

Instalação manual após `civmctl bootstrap` ter colocado o binário em
`/usr/local/bin/civmctl`.

## Instalação

### Todos os timers operacionais

```bash
sudo cp civmctl-*.service /etc/systemd/system/
sudo cp civmctl-*.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-cleanup.timer civmctl-disk-watchdog.timer civmctl-runner-watchdog.timer civmctl-reverse-watchdog.timer
```

O disk-watchdog checa disk %; se >80%, roda
`civmctl disk-watchdog --execute`, que delega para
`civmctl cleanup --execute` com thresholds agressivos
(TmpThreshold=24h, WorkThreshold=7d em vez de 7d/14d default).

(`civmctl bootstrap --execute --runner-watchdog=true
--reverse-watchdog=true` faz isso automaticamente quando os arquivos estão
em `/etc/systemd/system/`. O step `install_systemd_timers` só roda
`enable --now` se os unit files já existem.)

## Verificar

```bash
systemctl list-timers civmctl-cleanup.timer
systemctl list-timers civmctl-disk-watchdog.timer
systemctl list-timers civmctl-runner-watchdog.timer
systemctl list-timers civmctl-reverse-watchdog.timer
systemctl status civmctl-cleanup.timer
journalctl -u civmctl-runner-watchdog.service --since "2 hours ago"
journalctl -u civmctl-cleanup.service --since "7 days ago"
civmctl health
civmctl doctor --repos=auto --json
```

## Desabilitar

```bash
sudo systemctl disable --now civmctl-cleanup.timer civmctl-disk-watchdog.timer civmctl-runner-watchdog.timer civmctl-reverse-watchdog.timer
sudo rm /etc/systemd/system/civmctl-*.service /etc/systemd/system/civmctl-*.timer
sudo systemctl daemon-reload
```

## Personalizar horário

Editar `civmctl-cleanup.timer`:

```ini
[Timer]
OnCalendar=*-*-* 04:00:00 UTC   # diario 04:00 UTC
# OnCalendar=Mon..Fri 03:00 UTC  # so dias uteis
# OnCalendar=hourly              # a cada hora (excessivo, nao recomendado)
```

`RandomizedDelaySec=300` espalha em 5 minutos para evitar pico
simultâneo se múltiplas VMs convergirem.

## Opt-in para rerun no runner-watchdog

O timer padrão não reroda CI remoto. Depois de validar pelo journal, um
operador pode criar drop-in explícito:

```ini
# /etc/systemd/system/civmctl-runner-watchdog.service.d/rerun.conf
[Service]
ExecStart=
ExecStart=/usr/bin/flock -n /run/civmctl-runner-watchdog.lock /usr/local/bin/civmctl runner watchdog --execute --repos=auto --rerun-network-failures --max-run-age=6h --json
```

Aplicar:

```bash
sudo systemctl daemon-reload
sudo systemctl restart civmctl-runner-watchdog.timer
```

## Como o cleanup é seguro

- `IOSchedulingClass=idle` → I/O do cleanup só roda quando o sistema está
  ocioso; jobs de CI ativos têm prioridade.
- `Nice=15` → CPU baixa prioridade.
- `flock /run/civmctl-cleanup.lock` impede cleanup diário e disk-watchdog
  de rodarem ao mesmo tempo.
- `flock /run/civmctl-runner-watchdog.lock` impede dois watchdogs de runner
  simultâneos. O comando só muta com GitHub acessível e host idle. O timer
  padrão não passa `--rerun-network-failures`; rerun remoto exige execução
  manual ou drop-in opt-in com `--max-run-age=6h`.
- `civmctl cleanup --execute` aborta antes de mutar se detectar
  `Runner.Worker`, processo dentro de `_work`, Docker build/compose/buildctl
  ativo, ou se o detector não conseguir provar host ocioso.
- O guard roda no início e novamente antes de cada mutação (`rm -rf`,
  Docker prune, apt clean/autoremove).
- Arquivos com mtime <2h continuam pulados como segunda camada anti-job.
- `TimeoutStartSec=30min` → se cleanup ficar travado, systemd mata.

## Rollback se quebrar disco

Ver `docs/specs/civmctl/PRD.md` §"Rollback trigger".

## GitHub Actions job hooks

Systemd timers handle periodic and pressure-based cleanup. Job hooks handle
the CI boundary itself.

Install or repair hook wiring with:

```bash
sudo civmctl hook install --execute
```

The command creates two managed shell scripts —
`/opt/civm/hooks/job-started.sh` and `/opt/civm/hooks/job-completed.sh` —
then upserts the corresponding `ACTIONS_RUNNER_HOOK_JOB_STARTED` and
`ACTIONS_RUNNER_HOOK_JOB_COMPLETED` entries in every
`/home/*/actions-runner*/.env`. The runner executes those `.sh` files via
bash; each script delegates to the same Go policy as
`civmctl hook job-started|completed --execute`.

All policy lives in Go under `internal/hook` and can be tested with
`go test ./internal/hook`.
