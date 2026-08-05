# civm-gate-runner-provision.ps1 — cleanly reprovision a Windows gate runner.
#
# The rollout first quarantines the existing GitHub registration, then performs
# a side-by-side install under an admin-only DACL. The prior install becomes
# exactly one .rollback until least-privilege setup verifies the new listener.
param(
    [Parameter(Mandatory)][System.Security.SecureString]$GitHubToken,
    [int]$Index = 1,
    [string]$Url = 'https://github.com/advoq',
    [string]$RunnerVersion = '2.336.0',
    [string]$RunnerSHA256 = 'd59123a43003e357b0805b5d0f611d0bd2f65ab67d51bd070dd4e7a0f685c162',
    [string]$Root = 'C:\civm-gate'
)
$ErrorActionPreference = 'Stop'
if ($Index -lt 1 -or $Index -gt 99) { throw 'Index fora de 1..99' }
$Root = [System.IO.Path]::GetFullPath($Root).TrimEnd('\')
if (-not $Root.Equals('C:\civm-gate', [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Root precisa ser exatamente C:\civm-gate'
}
if ($RunnerVersion -notmatch '^[0-9]+\.[0-9]+\.[0-9]+$' -or
    $RunnerSHA256 -notmatch '^[0-9a-fA-F]{64}$') {
    throw 'pin do actions/runner invalido'
}
$uri = [System.Uri]$Url
$owner = $uri.AbsolutePath.Trim('/')
if ($uri.Scheme -ne 'https' -or $uri.Host -ne 'github.com' -or
    $uri.Port -ne 443 -or $uri.UserInfo -ne '' -or $uri.Query -ne '' -or
    $uri.Fragment -ne '' -or
    $owner -notmatch '^[A-Za-z0-9][A-Za-z0-9-]{0,38}$') {
    throw 'Url precisa ter formato https://github.com/owner'
}
$name = "civm-$($owner.ToLowerInvariant())-gate-$Index"
$dir = Join-Path $Root "runner-$Index"
$stage = "$dir.new"
$rollback = "$dir.rollback"
$task = "civm-gate-runner-$Index"
$publisherTask = 'civm-host-orchestrator'
$systemSid = [System.Security.Principal.SecurityIdentifier]'S-1-5-18'
$administratorsSid = [System.Security.Principal.SecurityIdentifier]'S-1-5-32-544'
$networkServiceSid = [System.Security.Principal.SecurityIdentifier]'S-1-5-20'

function Add-FileSystemRule {
    param(
        [Parameter(Mandatory)]$Acl,
        [Parameter(Mandatory)][System.Security.Principal.SecurityIdentifier]$Sid,
        [Parameter(Mandatory)][System.Security.AccessControl.FileSystemRights]$Rights,
        [System.Security.AccessControl.InheritanceFlags]$Inheritance =
            [System.Security.AccessControl.InheritanceFlags]::None
    )
    $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
        $Sid, $Rights, $Inheritance,
        [System.Security.AccessControl.PropagationFlags]::None,
        [System.Security.AccessControl.AccessControlType]::Allow)
    $Acl.AddAccessRule($rule) | Out-Null
}

function Resolve-AccountSid {
    param([Parameter(Mandatory)][string]$Account)
    try {
        return ([System.Security.Principal.SecurityIdentifier]$Account).Value
    } catch {
        return (([System.Security.Principal.NTAccount]$Account).Translate(
            [System.Security.Principal.SecurityIdentifier])).Value
    }
}

function Set-ProtectedAcl {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][bool]$Directory,
        [Parameter(Mandatory)][bool]$NetworkRead,
        [Parameter(Mandatory)][bool]$InheritToChildren
    )
    $acl = if ($Directory) {
        New-Object System.Security.AccessControl.DirectorySecurity
    } else { New-Object System.Security.AccessControl.FileSecurity }
    $acl.SetOwner($systemSid)
    $acl.SetAccessRuleProtection($true, $false)
    $inheritance = if ($Directory -and $InheritToChildren) {
        [System.Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit'
    } else { [System.Security.AccessControl.InheritanceFlags]::None }
    Add-FileSystemRule -Acl $acl -Sid $systemSid `
        -Rights ([System.Security.AccessControl.FileSystemRights]::FullControl) `
        -Inheritance $inheritance
    Add-FileSystemRule -Acl $acl -Sid $administratorsSid `
        -Rights ([System.Security.AccessControl.FileSystemRights]::FullControl) `
        -Inheritance $inheritance
    if ($NetworkRead) {
        Add-FileSystemRule -Acl $acl -Sid $networkServiceSid `
            -Rights ([System.Security.AccessControl.FileSystemRights]::ReadAndExecute) `
            -Inheritance $inheritance
    }
    Set-Acl -LiteralPath $Path -AclObject $acl

    $actual = Get-Acl -LiteralPath $Path
    $rules = @($actual.GetAccessRules(
        $true, $true, [System.Security.Principal.SecurityIdentifier]))
    $expectedCount = if ($NetworkRead) { 3 } else { 2 }
    if (-not $actual.AreAccessRulesProtected -or
        (Resolve-AccountSid -Account $actual.Owner) -ne $systemSid.Value -or
        $rules.Count -ne $expectedCount) {
        throw "DACL de provisionamento divergente: $Path"
    }
    $normalizedFull = [System.Security.AccessControl.FileSystemAccessRule]::new(
        $systemSid, [System.Security.AccessControl.FileSystemRights]::FullControl,
        [System.Security.AccessControl.AccessControlType]::Allow).FileSystemRights
    $normalizedRead = [System.Security.AccessControl.FileSystemAccessRule]::new(
        $networkServiceSid, [System.Security.AccessControl.FileSystemRights]::ReadAndExecute,
        [System.Security.AccessControl.AccessControlType]::Allow).FileSystemRights
    $expected = @{}
    $expected[$systemSid.Value] = [int64]$normalizedFull
    $expected[$administratorsSid.Value] = [int64]$normalizedFull
    if ($NetworkRead) { $expected[$networkServiceSid.Value] = [int64]$normalizedRead }
    $seen = @{}
    foreach ($rule in $rules) {
        $sid = $rule.IdentityReference.Value
        if ($rule.IsInherited -or
            $rule.AccessControlType -ne [System.Security.AccessControl.AccessControlType]::Allow -or
            $rule.InheritanceFlags -ne $inheritance -or
            $rule.PropagationFlags -ne [System.Security.AccessControl.PropagationFlags]::None -or
            -not $expected.ContainsKey($sid) -or $seen.ContainsKey($sid) -or
            [int64]$rule.FileSystemRights -ne $expected[$sid]) {
            throw "ACE de provisionamento inesperada: $Path"
        }
        $seen[$sid] = $true
    }
}

function Get-SafeTreeItems {
    param([Parameter(Mandatory)][string]$Path)
    $pending = [System.Collections.Stack]::new()
    $pending.Push((Get-Item -LiteralPath $Path -Force))
    $items = [System.Collections.Generic.List[object]]::new()
    while ($pending.Count -ne 0) {
        $item = $pending.Pop()
        if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "reparse point proibido no escopo do runner: $($item.FullName)"
        }
        $items.Add($item)
        if ($item.PSIsContainer) {
            foreach ($child in (Get-ChildItem -LiteralPath $item.FullName -Force)) {
                $pending.Push($child)
            }
        }
    }
    return $items.ToArray()
}

function Protect-AdminTree {
    param([Parameter(Mandatory)][string]$Path)
    foreach ($item in (Get-SafeTreeItems -Path $Path)) {
        Set-ProtectedAcl -Path $item.FullName -Directory $item.PSIsContainer `
            -NetworkRead $false -InheritToChildren $item.PSIsContainer
    }
}

function Get-RunnerProcesses {
    param([Parameter(Mandatory)][string]$ProcessName)
    return @(Get-CimInstance Win32_Process -Filter "Name = '$ProcessName'" |
        Where-Object {
            $_.ExecutablePath -and $_.ExecutablePath.StartsWith(
                $dir + '\', [System.StringComparison]::OrdinalIgnoreCase)
        })
}

function Invoke-GitHubApi {
    param(
        [Parameter(Mandatory)][ValidateSet('GET', 'POST', 'DELETE')][string]$Method,
        [Parameter(Mandatory)][string]$Path
    )
    $plainToken = [System.Net.NetworkCredential]::new('', $GitHubToken).Password
    try {
        return Invoke-RestMethod -Method $Method -Uri "https://api.github.com/$Path" `
            -Headers @{
                Accept = 'application/vnd.github+json'
                Authorization = "Bearer $plainToken"
                'X-GitHub-Api-Version' = '2022-11-28'
                'User-Agent' = 'civm-gate-provision'
            } -ErrorAction Stop
    } finally {
        $plainToken = $null
    }
}

function Stop-IdleListener {
    if ((Get-RunnerProcesses -ProcessName 'Runner.Worker.exe').Count -ne 0) {
        throw "gate possui Runner.Worker ativo; processo preservado: $dir"
    }
    foreach ($listener in (Get-RunnerProcesses -ProcessName 'Runner.Listener.exe')) {
        if ((Get-RunnerProcesses -ProcessName 'Runner.Worker.exe').Count -ne 0) {
            throw "gate iniciou Runner.Worker durante quiescencia; processo preservado: $dir"
        }
        Stop-Process -Id $listener.ProcessId -Force -ErrorAction Stop
    }
    $deadline = (Get-Date).AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 250
        $workers = Get-RunnerProcesses -ProcessName 'Runner.Worker.exe'
        if ($workers.Count -ne 0) {
            throw "gate iniciou Runner.Worker durante quiescencia; processo preservado: $dir"
        }
        $listeners = Get-RunnerProcesses -ProcessName 'Runner.Listener.exe'
    } while ($listeners.Count -ne 0 -and (Get-Date) -lt $deadline)
    if ($listeners.Count -ne 0) { throw "listener antigo nao encerrou: $dir" }
}

function Get-RemoteRunner {
    $allRunners = [System.Collections.Generic.List[object]]::new()
    $page = 1
    do {
        $response = Invoke-GitHubApi -Method GET `
            -Path "orgs/$owner/actions/runners?per_page=100&page=$page"
        $batch = @($response.runners)
        foreach ($runner in $batch) { $allRunners.Add($runner) }
        $page++
    } while ($batch.Count -eq 100)
    $matches = @($allRunners | Where-Object { $_.name -eq $name })
    if ($matches.Count -gt 1) { throw "runner remoto duplicado: $name" }
    if ($matches.Count -eq 0) { return $null }
    return $matches[0]
}

function Quarantine-RemoteRunner {
    $remote = Get-RemoteRunner
    if ($null -eq $remote) { return }
    if (@($remote.labels | Where-Object { $_.name -eq 'civm-gate' }).Count -ne 0) {
        Invoke-GitHubApi -Method DELETE `
            -Path "orgs/$owner/actions/runners/$($remote.id)/labels/civm-gate" |
            Out-Null
    }
    $stable = 0
    $deadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 500
        $remote = Get-RemoteRunner
        $locallyIdle = (Get-RunnerProcesses -ProcessName 'Runner.Worker.exe').Count -eq 0
        $quarantined = $null -eq $remote -or (
            -not $remote.busy -and
            @($remote.labels | Where-Object { $_.name -eq 'civm-gate' }).Count -eq 0)
        if ($locallyIdle -and $quarantined) { $stable++ } else { $stable = 0 }
    } while ($stable -lt 6 -and (Get-Date) -lt $deadline)
    if ($stable -lt 6) { throw "runner nao drenou apos quarentena: $name" }
}

function Get-LegacyService {
    $servicePath = Join-Path $dir '.service'
    if (-not (Test-Path -LiteralPath $servicePath -PathType Leaf)) { return $null }
    $serviceName = (Get-Content -LiteralPath $servicePath -Raw).Trim()
    if ($serviceName -notmatch '^actions\.runner\.[A-Za-z0-9_.-]+$') {
        throw "nome de service inseguro em $servicePath"
    }
    $cim = Get-CimInstance Win32_Service -Filter "Name = '$serviceName'" `
        -ErrorAction SilentlyContinue
    if ($null -eq $cim) { return $null }
    $match = [regex]::Match($cim.PathName, '^(?:"([^"]+)"|(\S+))')
    if (-not $match.Success) { throw "ImagePath invalido para service $serviceName" }
    $imagePath = if ($match.Groups[1].Success) {
        $match.Groups[1].Value
    } else { $match.Groups[2].Value }
    $expected = Join-Path $dir 'bin\RunnerService.exe'
    if (-not [System.IO.Path]::GetFullPath($imagePath).Equals(
            $expected, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "service fora do diretório do gate: $serviceName"
    }
    return Get-Service -Name $serviceName -ErrorAction Stop
}

foreach ($existingPath in @($Root, $dir, $stage, $rollback)) {
    if (Test-Path -LiteralPath $existingPath) {
        [void](Get-SafeTreeItems -Path $existingPath)
    }
}
if (Test-Path -LiteralPath $stage) {
    throw "staging anterior exige revisao manual: $stage"
}
if (Test-Path -LiteralPath $rollback) {
    throw "rollback anterior exige revisao manual: $rollback"
}
$publisher = Get-ScheduledTask -TaskName $publisherTask -ErrorAction SilentlyContinue
if ($null -ne $publisher -and $publisher.State.ToString() -ne 'Disabled') {
    throw "publisher precisa estar Disabled antes do rollout: $publisherTask"
}

# Removing the base label closes remote admission before any local stop. The
# disabled publisher cannot restore a generation label during the dwell.
Quarantine-RemoteRunner
$oldTask = Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue
$service = Get-LegacyService
$lifecycleErrors = [System.Collections.Generic.List[string]]::new()
if ($null -ne $oldTask) {
    try {
        Disable-ScheduledTask -TaskName $task -ErrorAction Stop | Out-Null
    } catch { $lifecycleErrors.Add($_.Exception.Message) }
}
if ($null -ne $service) {
    try {
        Set-Service -Name $service.Name -StartupType Disabled
    } catch { $lifecycleErrors.Add($_.Exception.Message) }
}
if ($null -ne $service) {
    try {
        Stop-Service -Name $service.Name -Force -ErrorAction Stop
        $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
    } catch { $lifecycleErrors.Add($_.Exception.Message) }
}
Stop-IdleListener
if ($null -ne $service) {
    try {
        & sc.exe delete $service.Name | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "falha removendo service $($service.Name)" }
    } catch { $lifecycleErrors.Add($_.Exception.Message) }
}
if ($null -ne $oldTask) {
    try {
        Unregister-ScheduledTask -TaskName $task -Confirm:$false -ErrorAction Stop
        if ($null -ne (Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue)) {
            throw "task permaneceu registrada: $task"
        }
    } catch { $lifecycleErrors.Add($_.Exception.Message) }
}
Stop-IdleListener
if ($lifecycleErrors.Count -ne 0) {
    throw "cleanup local incompleto apos quarentena: $($lifecycleErrors -join '; ')"
}

New-Item -ItemType Directory -Path $Root -Force | Out-Null
Set-ProtectedAcl -Path $Root -Directory $true -NetworkRead $true `
    -InheritToChildren $false
if (Test-Path -LiteralPath $stage) {
    throw "staging apareceu durante a quarentena: $stage"
}
New-Item -ItemType Directory -Path $stage | Out-Null
Set-ProtectedAcl -Path $stage -Directory $true -NetworkRead $false `
    -InheritToChildren $true
$zip = Join-Path $stage 'runner.zip'
$src = "https://github.com/actions/runner/releases/download/v$RunnerVersion/actions-runner-win-x64-$RunnerVersion.zip"
Write-Host "baixando actions/runner v$RunnerVersion ..."
Invoke-WebRequest -Uri $src -OutFile $zip
$actualSHA256 = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSHA256 -ne $RunnerSHA256.ToLowerInvariant()) {
    throw "SHA256 invalido para actions/runner v$RunnerVersion"
}
Expand-Archive -LiteralPath $zip -DestinationPath $stage -Force
Remove-Item -LiteralPath $zip -Force
foreach ($item in (Get-SafeTreeItems -Path $stage)) {
    if (-not $item.PSIsContainer) { Unblock-File -LiteralPath $item.FullName }
}
Protect-AdminTree -Path $stage

Push-Location $stage
try {
    $registration = Invoke-GitHubApi -Method POST `
        -Path "orgs/$owner/actions/runners/registration-token"
    $RegToken = $registration.token
    if ([string]::IsNullOrWhiteSpace($RegToken)) {
        throw 'API nao retornou registration token'
    }
    & .\config.cmd --unattended --url $Url --token $RegToken `
        --labels 'civm-gate' --name $name --work '_work' `
        --disableupdate --replace
    if ($LASTEXITCODE -ne 0) {
        throw "config.cmd falhou com exit $LASTEXITCODE"
    }
} finally {
    $RegToken = $null
    Pop-Location
}
Protect-AdminTree -Path $stage

$newConfigPath = Join-Path $stage '.runner'
if (-not (Test-Path -LiteralPath $newConfigPath -PathType Leaf)) {
    throw 'config.cmd nao criou .runner'
}
$newConfig = Get-Content -LiteralPath $newConfigPath -Raw |
    ConvertFrom-Json -ErrorAction Stop
if ($newConfig.agentName -ne $name -or $newConfig.DisableUpdate -ne $true) {
    throw 'pos-condicao .runner divergente'
}
$remote = Get-RemoteRunner
if ($null -eq $remote -or $remote.busy -or
    @($remote.labels | Where-Object { $_.name -eq 'civm-gate' }).Count -ne 1 -or
    @($remote.labels | Where-Object { $_.name -like 'civm-generation-*' }).Count -ne 0) {
    throw "pos-condicao remota divergente para $name"
}

if (Test-Path -LiteralPath $dir) {
    Protect-AdminTree -Path $dir
    Move-Item -LiteralPath $dir -Destination $rollback
}
Move-Item -LiteralPath $stage -Destination $dir
Write-Host "OK: '$name' provisionado limpo; execute civm-gate-task-setup.ps1 -Index $Index."
