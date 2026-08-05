$ErrorActionPreference = 'Stop'

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$sources = @(
    (Join-Path $repoRoot 'deploy\windows\civm-gate-runner-provision.ps1'),
    (Join-Path $repoRoot 'deploy\windows\civm-gate-task-setup.ps1')
)
$expectedVersion = '2.336.0'

function Remove-Junction {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0) {
        throw "refusing to remove non-junction fixture: $Path"
    }
    & cmd.exe /d /c "rmdir `"$Path`""
    if ($LASTEXITCODE -ne 0) { throw "rmdir failed for junction fixture: $Path" }
}

function Assert-Rejected {
    param(
        [Parameter(Mandatory)][scriptblock]$Operation,
        [Parameter(Mandatory)][string]$Pattern
    )
    try {
        & $Operation
        throw 'unsafe junction fixture was accepted'
    } catch {
        if ($_.Exception.Message -notlike $Pattern) { throw }
    }
}

foreach ($source in $sources) {
    $tokens = $null
    $parseErrors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile(
        $source, [ref]$tokens, [ref]$parseErrors)
    if ($parseErrors.Count -ne 0) { throw "parse failed: $source" }
    foreach ($name in @('Assert-SafeOfficialRunnerJunction', 'Get-SafeTreeItems')) {
        $functionAst = $ast.Find({
            param($node)
            $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
                $node.Name -eq $name
        }, $true)
        if ($null -eq $functionAst) { throw "missing function $name in $source" }
        Invoke-Expression $functionAst.Extent.Text
    }

    $RunnerVersion = $expectedVersion
    $expectedRunnerVersion = $expectedVersion
    $fixture = Join-Path ([System.IO.Path]::GetTempPath()) `
        ('civm-junction-fixture-' + [guid]::NewGuid().ToString('N'))
    $external = Join-Path ([System.IO.Path]::GetTempPath()) `
        ('civm-junction-external-' + [guid]::NewGuid().ToString('N'))
    $link = Join-Path $fixture 'bin'
    try {
        New-Item -ItemType Directory -Path $fixture, $external | Out-Null

        $target = Join-Path $fixture "bin.$expectedVersion"
        New-Item -ItemType Directory -Path $target | Out-Null
        New-Item -ItemType File -Path (Join-Path $target 'marker') | Out-Null
        New-Item -ItemType Junction -Path $link -Target $target | Out-Null
        $items = @(Get-SafeTreeItems -Path $fixture `
            -AllowedRunnerRoots @($fixture))
        $unique = @($items.FullName | Sort-Object -Unique)
        if ($items.Count -ne 4 -or $unique.Count -ne 4) {
            throw "safe junction was traversed or duplicated in $source"
        }
        Remove-Junction -Path $link
        Remove-Item -LiteralPath $target -Recurse -Force

        New-Item -ItemType Junction -Path $link -Target $external | Out-Null
        Assert-Rejected {
            [void](Get-SafeTreeItems -Path $fixture -AllowedRunnerRoots @($fixture))
        } '*fora do alvo pinado*'
        Remove-Junction -Path $link

        $nested = Join-Path $fixture 'nested'
        $nestedTarget = Join-Path $nested "bin.$expectedVersion"
        New-Item -ItemType Directory -Path $nestedTarget -Force | Out-Null
        $nestedLink = Join-Path $nested 'bin'
        New-Item -ItemType Junction -Path $nestedLink -Target $nestedTarget | Out-Null
        Assert-Rejected {
            [void](Get-SafeTreeItems -Path $fixture -AllowedRunnerRoots @($fixture))
        } '*fora da raiz top-level*'
        Remove-Junction -Path $nestedLink
        Remove-Item -LiteralPath $nested -Recurse -Force

        $externalFile = Join-Path $external 'outside.txt'
        New-Item -ItemType File -Path $externalFile | Out-Null
        $hardLink = Join-Path $fixture 'outside-hardlink.txt'
        New-Item -ItemType HardLink -Path $hardLink -Target $externalFile | Out-Null
        Assert-Rejected {
            [void](Get-SafeTreeItems -Path $fixture -AllowedRunnerRoots @($fixture))
        } '*link de filesystem proibido*'
        Remove-Item -LiteralPath $hardLink -Force

        $wrongNameTarget = Join-Path $fixture "tools.$expectedVersion"
        New-Item -ItemType Directory -Path $wrongNameTarget | Out-Null
        $wrongName = Join-Path $fixture 'tools'
        New-Item -ItemType Junction -Path $wrongName -Target $wrongNameTarget | Out-Null
        Assert-Rejected {
            [void](Get-SafeTreeItems -Path $fixture -AllowedRunnerRoots @($fixture))
        } '*reparse point proibido*'
        Remove-Junction -Path $wrongName
        Remove-Item -LiteralPath $wrongNameTarget -Recurse -Force

        $wrongVersionTarget = Join-Path $fixture 'bin.2.335.0'
        New-Item -ItemType Directory -Path $wrongVersionTarget | Out-Null
        New-Item -ItemType Junction -Path $link -Target $wrongVersionTarget | Out-Null
        Assert-Rejected {
            [void](Get-SafeTreeItems -Path $fixture -AllowedRunnerRoots @($fixture))
        } '*fora do alvo pinado*'
        Remove-Junction -Path $link
        Remove-Item -LiteralPath $wrongVersionTarget -Recurse -Force

        $chainedTarget = Join-Path $fixture "bin.$expectedVersion"
        New-Item -ItemType Junction -Path $chainedTarget -Target $external | Out-Null
        New-Item -ItemType Junction -Path $link -Target $chainedTarget | Out-Null
        Assert-Rejected {
            [void](Get-SafeTreeItems -Path $fixture -AllowedRunnerRoots @($fixture))
        } '*reparse point proibido*'
        Remove-Junction -Path $link
        Remove-Junction -Path $chainedTarget

        $cleanupRoot = Join-Path $fixture 'cleanup-contract'
        $cleanupLink = Join-Path $cleanupRoot 'external'
        $cleanupSentinel = Join-Path $external 'sentinel.txt'
        New-Item -ItemType Directory -Path $cleanupRoot | Out-Null
        New-Item -ItemType File -Path $cleanupSentinel | Out-Null
        New-Item -ItemType Junction -Path $cleanupLink -Target $external | Out-Null
        Remove-Item -LiteralPath $cleanupRoot -Recurse -Force
        if (-not (Test-Path -LiteralPath $cleanupSentinel -PathType Leaf)) {
            throw 'recursive cleanup traversed an external junction target'
        }
    } finally {
        foreach ($path in @(
                $link,
                (Join-Path $fixture 'outside-hardlink.txt'),
                (Join-Path $fixture 'cleanup-contract\external'),
                (Join-Path $fixture 'tools'),
                (Join-Path $fixture 'nested\bin'),
                (Join-Path $fixture "bin.$expectedVersion"))) {
            if (Test-Path -LiteralPath $path) {
                $item = Get-Item -LiteralPath $path -Force
                if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
                    Remove-Junction -Path $path
                }
            }
        }
        if (Test-Path -LiteralPath $fixture) {
            Remove-Item -LiteralPath $fixture -Recurse -Force
        }
        if (Test-Path -LiteralPath $external) {
            Remove-Item -LiteralPath $external -Recurse -Force
        }
    }
    Write-Host "PASS: $(Split-Path $source -Leaf) official junction contract"
}
