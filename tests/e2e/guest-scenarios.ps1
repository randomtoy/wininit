[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Agent,
    [Parameter(Mandatory)][string]$DistDirectory,
    [Parameter(Mandatory)][string]$ArtifactsDirectory
)
# Called by msi-smoke only after installing an owned, initially disabled service.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$id = [Guid]::NewGuid().ToString('N')
$root = Join-Path $env:SystemDrive "WinInitE2E-$id"
$state = "Registry::HKEY_LOCAL_MACHINE\SOFTWARE\WininitE2E-$id"
$configuration = Join-Path $env:ProgramData 'WinInit\wininit.yaml'
$originalConfiguration = [IO.File]::ReadAllBytes($configuration)
$utf8 = New-Object Text.UTF8Encoding($false)
$cases = New-Object 'System.Collections.Generic.List[object]'
$seed = Join-Path $root 'seed'
$counter = Join-Path $root 'executions.txt'
$source = 'nocloud'
$scriptPin = ''
$executionID = "success-$id"
$images = @{}
$pins = @{}
$scenarioEnabled = $false
$minimumGeneration = 0
$minimumSourceAttempts = 0

function Protect-Path([string]$Path, [bool]$Readable) {
    if ((Get-Item -LiteralPath $Path -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Reparse fixture path.' }
    $isDirectory = Test-Path -LiteralPath $Path -PathType Container
    if ($isDirectory) { $acl = New-Object Security.AccessControl.DirectorySecurity }
    else { $acl = New-Object Security.AccessControl.FileSecurity }
    $acl.SetOwner((New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')))
    $acl.SetAccessRuleProtection($true, $false)
    $inheritance = [Security.AccessControl.InheritanceFlags]::None
    if ($isDirectory) { $inheritance = [Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit' }
    foreach ($sid in @('S-1-5-18', 'S-1-5-32-544', 'S-1-5-32-545')) {
        if ($sid -eq 'S-1-5-32-545' -and -not $Readable) { continue }
        $rights = [Security.AccessControl.FileSystemRights]::FullControl
        if ($sid -eq 'S-1-5-32-545') { $rights = [Security.AccessControl.FileSystemRights]::ReadAndExecute }
        $rule = New-Object Security.AccessControl.FileSystemAccessRule(
            (New-Object Security.Principal.SecurityIdentifier($sid)), $rights, $inheritance,
            [Security.AccessControl.PropagationFlags]::None, [Security.AccessControl.AccessControlType]::Allow)
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
}
function Stop-Agent {
    $service = Get-Service wininit
    try {
        if ($service.Status -ne 'Stopped') {
            Stop-Service wininit -ErrorAction Stop
            $service.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
        }
    } finally { $service.Dispose() }
}
function Write-Configuration([bool]$Enabled, [bool]$BadPin, [bool]$Missing, [bool]$PowerShell) {
    $script:scenarioEnabled = $Enabled
    $on = $Enabled.ToString().ToLowerInvariant()
    $psOn = $PowerShell.ToString().ToLowerInvariant()
    $image = $images[$source]; $hostImage = $images['hostname']; $pin = $pins[$source]
    if ($BadPin) { $pin = '0' * 64 }
    if ($Missing) { $image = Join-Path $root 'missing.exe'; $hostImage = $image }
    $text = @"
version: 1
log:
  directory: '$root\logs'
state:
  registry_path: 'SOFTWARE\WininitE2E-$id'
plugins:
  datasources:
    ${source}:
      enabled: $on
      execution: external
      seed_directory: '$seed'
      discover_volumes: false
      process:
        executable: '$image'
        sha256: '$pin'
        timeout_seconds: 30
  provisioners:
    hostname:
      enabled: $on
      execution: external
      allow_privileged: true
      process:
        executable: '$hostImage'
        sha256: '$($pins['hostname'])'
        timeout_seconds: 30
    powershell_user_data:
      enabled: $psOn
      allow_privileged: true
      execution_id: '$executionID'
      script_sha256: '$scriptPin'
      process:
        executable: '$($images['powershell'])'
        sha256: '$($pins['powershell'])'
        timeout_seconds: 30
reboot:
  policy: never
"@
    [IO.File]::WriteAllText($configuration, $text, $utf8)
    & $Agent validate | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Fixture configuration validation failed.' }
}
function Wait-Provisioning([string]$Expected, [int]$MinimumAttempts = 0) {
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    do {
        $json = & $Agent status --json
        if ($LASTEXITCODE -ne 0) { throw 'Cannot read agent status.' }
        $status = $json | ConvertFrom-Json
        $lifecycle = $null
        if ($null -ne $status.PSObject.Properties['lifecycle']) { $lifecycle = $status.lifecycle }
        $attempts = 0
        $sourceAttempts = 0
        if ($null -ne $lifecycle -and $null -ne $lifecycle.PSObject.Properties['attempts']) {
            $properties = $lifecycle.attempts.PSObject.Properties
            if ($null -ne $properties['hostname']) { $attempts = [int]$properties['hostname'].Value }
            if ($null -ne $properties[$source]) { $sourceAttempts = [int]$properties[$source].Value }
        }
        if ($null -ne $lifecycle -and $lifecycle.generation -ge $minimumGeneration -and
            $sourceAttempts -ge $minimumSourceAttempts -and $status.provisioning -eq $Expected -and
            $status.lifecycle.provisioning -eq $Expected -and
            ($Expected -eq 'failed' -or $status.lifecycle.phase -eq 'agent_running') -and
            $attempts -ge $MinimumAttempts) { return $status }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Provisioning did not reach $Expected."
}
function Start-Scenario([string]$Expected, [int]$MinimumAttempts = 0) {
    $prior = (& $Agent status --json | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read status before service start.' }
    $lifecycle = $null
    if ($null -ne $prior.PSObject.Properties['lifecycle']) { $lifecycle = $prior.lifecycle }
    $script:minimumGeneration = 1
    $script:minimumSourceAttempts = 0
    if ($null -ne $lifecycle) { $script:minimumGeneration = [long]$lifecycle.generation + 2 }
    if ($scenarioEnabled) {
        $script:minimumSourceAttempts = 1
        if ($null -ne $lifecycle -and $null -ne $lifecycle.PSObject.Properties['attempts'] -and
            $null -ne $lifecycle.attempts.PSObject.Properties[$source]) {
            $script:minimumSourceAttempts = [int]$lifecycle.attempts.PSObject.Properties[$source].Value + 1
        }
    }
    try { Start-Service wininit -ErrorAction Stop }
    catch { if ($Expected -ne 'failed') { throw } }
    return Wait-Provisioning $Expected $MinimumAttempts
}
function Pass([string]$Name) { $cases.Add([PSCustomObject]@{ name = $Name; result = 'passed' }) }

if (Test-Path -LiteralPath $root) { throw 'Fixture directory already exists.' }
if (Test-Path -LiteralPath $state) { throw 'Fixture Registry key already exists.' }
New-Item -ItemType Directory -Path $root | Out-Null
try {
    Protect-Path $root $true
    New-Item -ItemType Directory -Path $seed | Out-Null
    Protect-Path $seed $true
    New-Item -ItemType Directory -Path (Join-Path $root 'logs') | Out-Null
    Protect-Path (Join-Path $root 'logs') $false
    foreach ($name in @('nocloud', 'configdrive', 'hostname', 'powershell')) {
        $images[$name] = Join-Path $root "$name.exe"
        Copy-Item -LiteralPath (Join-Path $DistDirectory "wininit-plugin-$name-windows-amd64.exe") -Destination $images[$name]
        Protect-Path $images[$name] $true
        $pins[$name] = (Get-FileHash -LiteralPath $images[$name] -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    $hostname = [Environment]::MachineName
    [IO.File]::WriteAllText((Join-Path $seed 'meta-data'), "instance-id: e2e-$id`nlocal-hostname: $hostname`n", $utf8)
    $cloudVersion = Join-Path $seed 'openstack\2025-04-04'
    New-Item -ItemType Directory -Path $cloudVersion | Out-Null
    [IO.File]::WriteAllText((Join-Path $cloudVersion 'meta_data.json'), "{`"uuid`":`"e2e-$id`",`"hostname`":`"$hostname.example.test`"}", $utf8)
    $payload = "#ps1`nif (-not [Security.Principal.WindowsIdentity]::GetCurrent().IsSystem) { exit 88 }`n`$path = '$counter'`n`$count = 0`nif ([IO.File]::Exists(`$path)) { `$count = [int][IO.File]::ReadAllText(`$path) }`n[IO.File]::WriteAllText(`$path, [string](`$count + 1))`nWrite-Output 'wininit-e2e-private-marker'`n"
    [IO.File]::WriteAllText((Join-Path $seed 'user-data'), $payload, $utf8)
    $scriptPin = (Get-FileHash -LiteralPath (Join-Path $seed 'user-data') -Algorithm SHA256).Hash.ToLowerInvariant()

    Stop-Agent
    Write-Configuration $true $false $false $false
    $first = Start-Scenario 'completed'
    if ($first.lifecycle.plugins.hostname.reboot_required -or (Test-Path -LiteralPath $counter)) { throw 'Metadata-only run requested reboot or executed user-data.' }
    Pass 'LocalSystem external NoCloud and unchanged hostname; user-data disabled'
    Stop-Agent
    $second = Start-Scenario 'completed' ([int]$first.lifecycle.attempts.hostname + 1)
    if ($second.lifecycle.plugins.hostname.reboot_required) { throw 'Service restart requested reboot.' }
    Pass 'Service restart reconciles declarative metadata'

    Stop-Agent
    $source = 'configdrive'
    Write-Configuration $true $false $false $false
    $null = Start-Scenario 'completed'
    Pass 'LocalSystem external ConfigDrive and unchanged hostname'

    Stop-Agent
    $source = 'nocloud'
    Write-Configuration $true $true $false $false
    $failed = Start-Scenario 'failed'
    if ($failed.lifecycle.plugins.hostname.status -ne 'pending') { throw 'Invalid image pin reached hostname provisioning.' }
    Pass 'Wrong image pin fails before provisioners'

    Stop-Agent
    Write-Configuration $false $false $true $false
    $disabled = Start-Scenario 'disabled'
    if (@($disabled.plugins | Where-Object { $_.enabled -or $_.status -ne 'disabled' }).Count -ne 0) { throw 'Plugin disablement failed.' }
    Pass 'Disabled plugins ignore missing EXEs and previous state'

    Stop-Agent
    Write-Configuration $true $false $false $true
    $succeeded = Start-Scenario 'completed'
    if ([IO.File]::ReadAllText($counter) -ne '1') { throw 'PowerShell did not execute once.' }
    $journal = @($succeeded.lifecycle.executions.PSObject.Properties.Value)
    if ($journal.Count -ne 1 -or $journal[0].status -ne 'succeeded') { throw 'Successful execution journal is missing.' }
    Pass 'Explicitly pinned PowerShell executes under LocalSystem'
    Stop-Agent
    $null = Start-Scenario 'completed' ([int]$succeeded.lifecycle.attempts.hostname + 1)
    if ([IO.File]::ReadAllText($counter) -ne '1') { throw 'Successful PowerShell execution replayed.' }
    Pass 'PowerShell success does not replay across service restart'

    Stop-Agent
    $executionID = "failure-$id"
    [IO.File]::WriteAllText((Join-Path $seed 'user-data'), ($payload + "exit 17`n"), $utf8)
    $scriptPin = (Get-FileHash -LiteralPath (Join-Path $seed 'user-data') -Algorithm SHA256).Hash.ToLowerInvariant()
    Write-Configuration $true $false $false $true
    $null = Start-Scenario 'failed'
    if ([IO.File]::ReadAllText($counter) -ne '2') { throw 'Failed PowerShell fixture did not execute once.' }
    Stop-Agent
    $null = Start-Scenario 'failed'
    if ([IO.File]::ReadAllText($counter) -ne '2') { throw 'Failed PowerShell execution replayed.' }
    Pass 'Failed PowerShell execution requires a new execution ID'

    $raw = (& $Agent status --json) -join "`n"
    if ($LASTEXITCODE -ne 0) { throw 'Final status failed.' }
    $logs = (Get-ChildItem -LiteralPath (Join-Path $root 'logs') -File | Get-Content -Raw) -join "`n"
    if ($raw.Contains('wininit-e2e-private-marker') -or $logs.Contains('wininit-e2e-private-marker')) { throw 'Payload leaked into status/logs.' }
    Pass 'Raw user-data output absent from status and agent logs'
}
finally {
    # Service must stop before deleting its owned state/images. Preserve the
    # original installed configuration, including the MSI upgrade sentinel.
    Stop-Agent
    [IO.File]::WriteAllBytes($configuration, $originalConfiguration)
    if (Test-Path -LiteralPath $state) { Remove-Item -LiteralPath $state -Recurse }
    if (Test-Path -LiteralPath $root) {
        $reparse = @(Get-ChildItem -LiteralPath $root -Force -Recurse | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint })
        if ($reparse.Count -ne 0) { throw 'Refusing recursive cleanup of a reparse fixture.' }
        Remove-Item -LiteralPath $root -Recurse
    }
    $cases | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $ArtifactsDirectory 'plugin-cases.json') -Encoding UTF8
    $script:scenarioEnabled = $false
    $null = Start-Scenario 'disabled'
}
