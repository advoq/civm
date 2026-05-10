# systemd units para civmctl

Cron diário de cleanup automático na VM ci-vm. Instalação manual após
`civmctl bootstrap` ter colocado o binário em `/usr/local/bin/civmctl`.

## Instalação

```bash
sudo cp civmctl-cleanup.service /etc/systemd/system/
sudo cp civmctl-cleanup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now civmctl-cleanup.timer
```

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
