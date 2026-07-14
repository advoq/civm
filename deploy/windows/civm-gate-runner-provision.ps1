# civm-gate-runner-provision.ps1 — provisiona um runner GitHub Actions no HOST Windows
# para a fila FIFO por-PR da box. O gate job (wait-for-slot) roda neste runner.
#
# POR QUE NO HOST, NAO NO GUEST: o gate job ESPERA (loop) enquanto o PR atual roda. No
# boundary do contexto, o orquestrador faz Stop-VM do guest para compactar — um gate
# DENTRO do guest seria cancelado pelo Stop-VM (cancelaria os runs dos outros PRs). No
# host, o gate sobrevive ao Stop-VM e le V:\civm-current-context direto (sem SSH).
#
# Label civm-gate + nome com sufixo -gate: o serialize.go ignora runners "-gate" na
# deteccao de colisao (eles nao fazem Docker/disco, nao causam o concurrent-prune do
# #1184), entao nao violam o invariante "1 runner civm por org".
#
# Pre-requisito — token de REGISTRO de runner da ORG acme (precisa de admin na org):
#   $tok = gh api -X POST /orgs/acme/actions/runners/registration-token --jq .token
#
# Uso (PowerShell ELEVADO no host, mesma maquina do orquestrador):
#   .\civm-gate-runner-provision.ps1 -RegToken $tok -Index 1
# Para um pool, rode com -Index 1..4 (4 runners cobrem ate 4 PRs esperando junto).
param(
    [Parameter(Mandatory)][string]$RegToken,
    [int]$Index = 1,
    [string]$Url = 'https://github.com/acme',
    [string]$RunnerVersion = '2.319.1',
    [string]$Root = 'C:\civm-gate'
)
$ErrorActionPreference = 'Stop'
$name = "civm-gate-$Index"
$dir = Join-Path $Root "runner-$Index"
New-Item -ItemType Directory -Path $dir -Force | Out-Null
# Baixa o actions-runner do Windows (uma vez por dir).
if (-not (Test-Path (Join-Path $dir 'config.cmd'))) {
    $zip = Join-Path $dir 'runner.zip'
    $src = "https://github.com/actions/runner/releases/download/v$RunnerVersion/actions-runner-win-x64-$RunnerVersion.zip"
    Write-Host "baixando o runner $RunnerVersion ..."
    Invoke-WebRequest -Uri $src -OutFile $zip
    Expand-Archive -Path $zip -DestinationPath $dir -Force
    Remove-Item $zip -Force
}
Push-Location $dir
try {
    # --runasservice: o runner sobe como service do Windows (resiliente a reboot). O
    # _work fica em C:\civm-gate (host) — o gate nao usa disco do guest.
    & .\config.cmd --unattended --url $Url --token $RegToken --labels 'civm-gate' --name $name --work '_work' --runasservice --replace
    Write-Host "OK: gate runner '$name' provisionado (label civm-gate) no HOST."
}
finally { Pop-Location }
