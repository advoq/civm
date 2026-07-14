$ErrorActionPreference = 'Stop'

$scripts = @(
    (Join-Path $PSScriptRoot 'activate-orchestrator.ps1'),
    (Join-Path $PSScriptRoot 'register-orchestrator.ps1')
)

foreach ($path in $scripts) {
    $source = Get-Content -LiteralPath $path -Raw
    $errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile($path, [ref]$null, [ref]$errors) | Out-Null
    if ($errors) { throw "$path possui erro de parse: $($errors -join '; ')" }
    if ($source -match '(?m)^\s*Unregister-ScheduledTask\b') { throw "$path remove a ultima task valida" }
    foreach ($contract in @('-AtStartup', '-StartWhenAvailable', '-AllowStartIfOnBatteries', '-DontStopIfGoingOnBatteries', '-Force')) {
        if (-not $source.Contains($contract)) { throw "$path nao implementa $contract" }
    }
}

$activate = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'activate-orchestrator.ps1') -Raw
if ($activate -notmatch 'dual-owner recusado') { throw 'activate-orchestrator nao recusa dual-owner' }

'PASS: 2/2 scripts preservam task valida, reboot, bateria e substituicao forcada; activate recusa dual-owner'
