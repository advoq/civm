$ErrorActionPreference = 'Stop'
$taskName = 'civm-vm-orchestrator'
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
# DEPLOY ATOMICO (SPECv4 §8 / DT-12): com a task JA des-registrada (Unregister acima),
# nenhum tick NOVO inicia -> a janela de troca esta fechada (um tick em voo ja fez o
# dot-source do par antigo). Copia os 2 .ps1 do repo p/ C:\civm-deploy e valida por AST
# (NAO executa o caller -> sem dependencia de Hyper-V/tokens, sem aborto flaky). So
# entao Register. Aborta se um reclaim estiver em curso.
if (Test-Path 'V:\civm-reclaim.lock') { throw 'reclaim em curso (V:\civm-reclaim.lock); abortar deploy' }
$dst = 'C:\civm-deploy'
if (-not (Test-Path $dst)) { New-Item -ItemType Directory -Path $dst -Force | Out-Null }
# O caller dot-sourceia decision + reclaim-gate via $PSScriptRoot -> os 3 DEVEM
# ser copiados juntos, senao o orchestrator novo chama uma funcao que o gate
# velho em C:\civm-deploy nao tem (ex.: Test-ReclaimStuck) e quebra no tick.
# Caller dot-sourceia decision + reclaim-gate + pr-queue. Host-metrics e task
# separada mas o arquivo DEVE existir em C:\civm-deploy (task falhava 2026-07:
# metrics.json stale desde 2026-06-28).
$toCopy = @(
    'civm-orchestrator-decision.ps1',
    'civm-reclaim-gate.ps1',
    'civm-pr-queue.ps1',
    'civm-vm-orchestrator.ps1',
    'civm-host-metrics.ps1'
)
foreach ($f in $toCopy) {
    $src = Join-Path $PSScriptRoot $f
    if (-not (Test-Path -LiteralPath $src)) { throw "missing source: $src" }
    $destFile = Join-Path $dst $f
    # Skip no-op when ja estamos em C:\civm-deploy (re-run in-place).
    if ((Resolve-Path -LiteralPath $src).Path -eq (Resolve-Path -LiteralPath (Split-Path $destFile -Parent)).Path + "\$f") {
        if ((Test-Path -LiteralPath $destFile) -and ((Get-FileHash $src).Hash -eq (Get-FileHash $destFile).Hash)) { continue }
    }
    if ((Test-Path -LiteralPath $destFile) -and ((Resolve-Path $src).Path -eq (Resolve-Path $destFile).Path)) { continue }
    Copy-Item $src $destFile -Force
}
$perr = $null
[System.Management.Automation.Language.Parser]::ParseFile((Join-Path $dst 'civm-vm-orchestrator.ps1'), [ref]$null, [ref]$perr) | Out-Null
if ($perr) { throw "parse error no caller deployado: $($perr -join '; ')" }
. (Join-Path $dst 'civm-orchestrator-decision.ps1')
. (Join-Path $dst 'civm-reclaim-gate.ps1')
. (Join-Path $dst 'civm-pr-queue.ps1')
"deploy: $($toCopy.Count) .ps1 copiados + validados por AST"
# -EnforceQueue: publica currentPr + push-wave no tip change (sem isto o wave
# nunca roda — so would_* de observe).
$arg = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File C:\civm-deploy\civm-vm-orchestrator.ps1 -EnforceQueue'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arg
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date)
$trigger.Repetition = (New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 2) -RepetitionDuration (New-TimeSpan -Days 3650)).Repetition
# Boot trigger + StartWhenAvailable: a task religa sozinha apos um restart do
# Windows. Sem isso, o gatilho -Once com StartBoundary no passado nao re-dispara
# de forma confiavel pos-reboot (o orchestrator ficaria morto ate intervencao).
$bootTrigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
# PT2H (nao 30min): o Optimize-VHD do gate de admissao e o passo longo; PT2H da margem
# (espelha register-civm-vhdx-autoreclaim). Trade-off: um tick pendurado fica ate 2h, mas
# IgnoreNew so engole ticks (sem efeito) e o CompactVirtualDisk e nativo (VHDX nao
# corrompe). (SPECv4 §8 / M-2)
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Hours 2) -MultipleInstances IgnoreNew -StartWhenAvailable
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger @($trigger, $bootTrigger) -Principal $principal -Settings $settings | Out-Null
'orchestrator ATIVO (sem -Observe)'
# Um dono so do power/compact da VM (fail-safe #15): desabilita TODOS os curadores
# legados que disputariam o lock/power-state. O optimize-watchdog chegava a religar
# a VM que o orchestrator desligava no idle (C4, confirmado 2026-06-17).
$legacy = @('civm-vhdx-autoreclaim', 'civm-vhdx-optimize', 'civm-vhdx-optimize-watchdog')
foreach ($t in $legacy) { Disable-ScheduledTask -TaskName $t -ErrorAction SilentlyContinue | Out-Null }
$states = ($legacy | ForEach-Object { "$_=" + (Get-ScheduledTask $_ -ErrorAction SilentlyContinue).State }) -join ' '
'orch_state=' + (Get-ScheduledTask $taskName).State + ' | legacy: ' + $states
