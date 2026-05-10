# systemd units para civmctl

2 timers systemd disponíveis:
- `civmctl-cleanup.timer` — diário 04:00 UTC, full cleanup (Docker, /tmp,
  _work, apt). Idempotente.
- `civmctl-disk-watchdog.timer` — hourly, dispara cleanup agressivo se
  disk >80%. Reativo a picos de uso entre execuções diárias.

Instalação manual após `civmctl bootstrap` ter colocado o binário em
`/usr/local/bin/civmctl`.

## Instalação

### Cleanup diário

```bash
sudo cp civmctl-cleanup.service /etc/systemd/system/
sudo cp civmctl-cleanup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-cleanup.timer
```

### Disk watchdog hourly (opcional, recomendado para SSD <128GB)

```bash
sudo cp civmctl-disk-watchdog.service /etc/systemd/system/
sudo cp civmctl-disk-watchdog.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-disk-watchdog.timer
```

Por hora, checa disk %; se >80%, roda `civmctl disk-watchdog --execute`
que delega para `civmctl cleanup --execute` com thresholds agressivos
(TmpThreshold=24h, WorkThreshold=7d em vez de 7d/14d default).

(`civmctl bootstrap --execute` faz isso automaticamente quando os arquivos
estão em `/etc/systemd/system/`. O step `install_systemd_timer` só roda
`enable --now` se os unit files já existem.)

## Verificar

```bash
systemctl list-timers civmctl-cleanup.timer
systemctl status civmctl-cleanup.timer
journalctl -u civmctl-cleanup.service --since "7 days ago"
```

## Desabilitar

```bash
sudo systemctl disable --now civmctl-cleanup.timer
sudo rm /etc/systemd/system/civmctl-cleanup.{service,timer}
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

## Como o cleanup é seguro

- `IOSchedulingClass=idle` → I/O do cleanup só roda quando o sistema está
  ocioso; jobs de CI ativos têm prioridade.
- `Nice=15` → CPU baixa prioridade.
- `civmctl cleanup --execute` skip arquivos com mtime <2h (anti-jobs-em-curso).
- `TimeoutStartSec=30min` → se cleanup ficar travado, systemd mata.

## Rollback se quebrar disco

Ver `docs/specs/civmctl/PRD.md` §"Rollback trigger".
