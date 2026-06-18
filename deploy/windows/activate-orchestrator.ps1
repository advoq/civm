$ErrorActionPreference = 'Stop'
$taskName = 'civm-vm-orchestrator'
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
$arg = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File C:\civm-deploy\civm-vm-orchestrator.ps1'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arg
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date)
$trigger.Repetition = (New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 2) -RepetitionDuration (New-TimeSpan -Days 3650)).Repetition
# Boot trigger + StartWhenAvailable: a task religa sozinha apos um restart do
# Windows. Sem isso, o gatilho -Once com StartBoundary no passado nao re-dispara
# de forma confiavel pos-reboot (o orchestrator ficaria morto ate intervencao).
$bootTrigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 30) -MultipleInstances IgnoreNew -StartWhenAvailable
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger @($trigger, $bootTrigger) -Principal $principal -Settings $settings | Out-Null
'orchestrator ATIVO (sem -Observe)'
# Um dono so do power/compact da VM (fail-safe #15): desabilita TODOS os curadores
# legados que disputariam o lock/power-state. O optimize-watchdog chegava a religar
# a VM que o orchestrator desligava no idle (C4, confirmado 2026-06-17).
$legacy = @('civm-vhdx-autoreclaim', 'civm-vhdx-optimize', 'civm-vhdx-optimize-watchdog')
foreach ($t in $legacy) { Disable-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue | Out-Null }
$states = ($legacy | ForEach-Object { "$_=" + (Get-ScheduledTask $_ -ErrorAction SilentlyContinue).State }) -join ' '
'orch_state=' + (Get-ScheduledTask $taskName).State + ' | legacy: ' + $states
