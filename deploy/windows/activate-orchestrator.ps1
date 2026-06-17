$ErrorActionPreference = 'Stop'
$taskName = 'civm-vm-orchestrator'
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
$arg = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File C:\civm-deploy\civm-vm-orchestrator.ps1'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arg
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date)
$trigger.Repetition = (New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 2) -RepetitionDuration (New-TimeSpan -Days 3650)).Repetition
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 30) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings | Out-Null
'orchestrator ATIVO (sem -Observe)'
Disable-ScheduledTask -TaskName civm-vhdx-autoreclaim -ErrorAction SilentlyContinue | Out-Null
$ar = Get-ScheduledTask civm-vhdx-autoreclaim -ErrorAction SilentlyContinue
'orch_state=' + (Get-ScheduledTask $taskName).State + ' autoreclaim_state=' + $ar.State
