# civm-gate-task-setup.ps1 — persistencia do gate runner via SCHEDULED TASK.
#
# Por que task e nao service do Windows: o config.cmd --runasservice instala o service
# mas o start sempre da Win32 1068 nesta box (sem dependencias declaradas, NETWORK
# SERVICE, persiste no retry — nao e quirk de timing). A scheduled task e o MESMO padrao
# que ja sustenta o orquestrador aqui. Sobrevive a reboot (trigger AtStartup) e a crash
# (watchdog: tick a cada 2min com IgnoreNew re-sobe o run.cmd se ele morreu).
#
# Uso (PowerShell ELEVADO no host):
#   .\civm-gate-task-setup.ps1 -Index 2
param([Parameter(Mandatory)][int]$Index, [string]$Root = 'C:\civm-gate')
$ErrorActionPreference = 'Stop'
$svc = "actions.runner.acme.civm-gate-$Index"
$dir = Join-Path $Root "runner-$Index"
$task = "civm-gate-runner-$Index"

# Remove o service quebrado (1068) pra nao competir com a task no boot pelo mesmo .runner.
& sc.exe stop $svc 2>$null | Out-Null
& sc.exe delete $svc 2>$null | Out-Null

# Persistencia via WATCHDOG. O run.cmd sai com codigo 0 mesmo quando o listener morre
# (provado: matei o Runner.Listener e a task ficou Ready / LastTaskResult=0), entao
# "restart on failure" NAO dispara. Em vez disso: dois triggers — AtStartup (sobrevive
# reboot) + a cada 2min (revive em crash) — com MultipleInstances=IgnoreNew: se run.cmd
# esta vivo o tick de 2min e ignorado; se morreu, o tick re-sobe. SYSTEM elevado, sem
# limite de tempo, roda logado ou nao.
$action = New-ScheduledTaskAction -Execute (Join-Path $dir 'run.cmd') -WorkingDirectory $dir
$tBoot = New-ScheduledTaskTrigger -AtStartup
$tWatch = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 2) -RepetitionDuration (New-TimeSpan -Days 3650)
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName $task -Action $action -Trigger $tBoot, $tWatch -Principal $principal -Settings $settings -Force | Out-Null
Start-ScheduledTask -TaskName $task
Write-Host "OK: task '$task' criada (AtStartup + watchdog 2min/IgnoreNew, sem time limit) e iniciada."
