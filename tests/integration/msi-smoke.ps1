[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$MsiPath,
    [Parameter(Mandatory)]
    [string]$ExpectedMsiVersion,
    [string]$ExpectedExecutableVersion,
    [string]$ArtifactsDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not $ArtifactsDirectory) {
    $ArtifactsDirectory = Join-Path (Get-Location) 'artifacts'
}
$MsiPath = (Resolve-Path -LiteralPath $MsiPath).Path
New-Item -ItemType Directory -Force -Path $ArtifactsDirectory | Out-Null
$installLog = Join-Path $ArtifactsDirectory 'msi-install.log'
$uninstallLog = Join-Path $ArtifactsDirectory 'msi-uninstall.log'

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'MSI smoke testing requires an elevated administrator token; the test was not run.'
}

function Get-MsiProperty {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Name
    )

    $installer = New-Object -ComObject WindowsInstaller.Installer
    $database = $installer.GetType().InvokeMember(
        'OpenDatabase',
        [Reflection.BindingFlags]::InvokeMethod,
        $null,
        $installer,
        @($Path, 0)
    )
    $view = $database.OpenView("SELECT ``Value`` FROM ``Property`` WHERE ``Property``='$Name'")
    $view.Execute()
    $record = $view.Fetch()
    if ($null -eq $record) { throw "MSI property '$Name' is missing." }
    return $record.StringData(1)
}

function Invoke-MsiExec {
    param([Parameter(Mandatory)][string[]]$Arguments)

    $process = Start-Process -FilePath "$env:SystemRoot\System32\msiexec.exe" `
        -ArgumentList $Arguments -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "msiexec failed with exit code $($process.ExitCode): $($Arguments -join ' ')"
    }
}

function Wait-ServiceAbsent {
    param([Parameter(Mandatory)][string]$Name)

    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        if ($null -eq (Get-Service -Name $Name -ErrorAction SilentlyContinue)) { return }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Service '$Name' still exists after uninstall."
}

function Get-HostNameState {
    $activePath = 'HKLM:\SYSTEM\CurrentControlSet\Control\ComputerName\ActiveComputerName'
    $pendingPath = 'HKLM:\SYSTEM\CurrentControlSet\Control\ComputerName\ComputerName'
    return [PSCustomObject]@{
        Active  = (Get-ItemPropertyValue -LiteralPath $activePath -Name 'ComputerName' -ErrorAction Stop)
        Pending = (Get-ItemPropertyValue -LiteralPath $pendingPath -Name 'ComputerName' -ErrorAction Stop)
    }
}

function Assert-HostNameUnchanged {
    param(
        [Parameter(Mandatory)]$Expected,
        [Parameter(Mandatory)][string]$Phase
    )

    $actual = Get-HostNameState
    if ($actual.Active -ne $Expected.Active -or $actual.Pending -ne $Expected.Pending) {
        throw "Host name changed during $Phase (active '$($Expected.Active)' -> '$($actual.Active)', pending '$($Expected.Pending)' -> '$($actual.Pending)')."
    }
}

$expectedUpgradeCode = '{B6E7B4A3-7C3B-4850-AE0B-3F8D1B8E72D9}'
$actualUpgradeCode = Get-MsiProperty -Path $MsiPath -Name 'UpgradeCode'
if ($actualUpgradeCode -ne $expectedUpgradeCode) {
    throw "Unexpected UpgradeCode '$actualUpgradeCode'; expected '$expectedUpgradeCode'."
}
$actualVersion = Get-MsiProperty -Path $MsiPath -Name 'ProductVersion'
if ($actualVersion -ne $ExpectedMsiVersion) {
    throw "Unexpected ProductVersion '$actualVersion'; expected '$ExpectedMsiVersion'."
}

$existingService = Get-Service -Name 'wininit' -ErrorAction SilentlyContinue
if ($null -ne $existingService) {
    throw 'A wininit service already exists. Refusing to alter a pre-existing installation.'
}
$originalHostName = Get-HostNameState

$installed = $false
$service = $null
$contendedProcess = $null
$stateKey = 'HKLM:\SOFTWARE\Wininit'
$installationKey = Join-Path $stateKey 'Installation'
$sentinelName = 'MsiSmokePreserve'
$sentinelValue = [Guid]::NewGuid().ToString('D')
try {
    Invoke-MsiExec -Arguments @('/i', $MsiPath, '/qn', '/norestart', '/l*v', $installLog)
    $installed = $true

    $installDirectory = Join-Path $env:ProgramFiles 'WinInit'
    $executable = Join-Path $installDirectory 'wininit.exe'
    $configuration = Join-Path $env:ProgramData 'WinInit\wininit.yaml'
    $logsDirectory = Join-Path $env:ProgramData 'WinInit\logs'
    $pluginsDirectory = Join-Path $env:ProgramData 'WinInit\plugins'
    foreach ($path in @($executable, $configuration, $logsDirectory, $pluginsDirectory)) {
        if (-not (Test-Path -LiteralPath $path)) { throw "Expected installed path is missing: $path" }
    }
    $managedBy = Get-ItemPropertyValue -LiteralPath $installationKey -Name 'ManagedBy' -ErrorAction Stop
    if ($managedBy -ne 'MSI') { throw "Unexpected MSI management marker '$managedBy'." }
    New-Item -Force -Path $stateKey | Out-Null
    New-ItemProperty -LiteralPath $stateKey -Name $sentinelName -Value $sentinelValue -PropertyType String -Force | Out-Null

    $service = Get-Service -Name 'wininit' -ErrorAction Stop
    $service.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))
    $serviceRecord = Get-CimInstance Win32_Service -Filter "Name='wininit'"
    if ($serviceRecord.StartMode -ne 'Auto') {
        throw "wininit service start mode is '$($serviceRecord.StartMode)', expected 'Auto'."
    }
    if ($serviceRecord.PathName -notlike "*$executable*") {
        throw "wininit service path '$($serviceRecord.PathName)' does not reference '$executable'."
    }
    $service.Close()
    $service = $null

    $versionOutput = (& $executable version 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw "wininit version failed: $versionOutput" }
    if ($ExpectedExecutableVersion -and $versionOutput -notlike "*$ExpectedExecutableVersion*") {
        throw "Version output '$versionOutput' does not contain '$ExpectedExecutableVersion'."
    }

    $status = $null
    $statusOutput = ''
    $lifecycleReady = $false
    $statusDeadline = [DateTime]::UtcNow.AddSeconds(10)
    do {
        $statusOutput = (& $executable status --json 2>&1 | Out-String).Trim()
        if ($LASTEXITCODE -ne 0) { throw "wininit status --json failed: $statusOutput" }
        try {
            $status = $statusOutput | ConvertFrom-Json -ErrorAction Stop
        }
        catch {
            throw "wininit status did not return valid JSON: $statusOutput"
        }
        $lifecycleProperty = $status.PSObject.Properties['lifecycle']
        if ($null -ne $lifecycleProperty -and
            $null -ne $lifecycleProperty.Value -and
            $lifecycleProperty.Value.phase -eq 'agent_running') {
            $lifecycleReady = $true
            break
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $statusDeadline)
    if (-not $lifecycleReady) {
        throw "wininit service did not report lifecycle phase 'agent_running' within 10 seconds; last status: $statusOutput"
    }
    if ($status.service -ne 'running') {
        throw "Status reported service '$($status.service)', expected 'running'."
    }
    if ($status.provisioning -ne 'disabled') {
        throw "Status reported provisioning '$($status.provisioning)', expected 'disabled'."
    }
    $plugins = @($status.plugins)
    if ($plugins.Count -ne 2) {
        throw "Status reported $($plugins.Count) plugins, expected the disabled NoCloud datasource and hostname provisioner."
    }
    $noCloud = @($plugins | Where-Object { $_.name -eq 'nocloud' -and $_.kind -eq 'datasource' })
    $hostname = @($plugins | Where-Object { $_.name -eq 'hostname' -and $_.kind -eq 'provisioner' })
    if ($noCloud.Count -ne 1 -or $noCloud[0].enabled -ne $false) {
        throw 'NoCloud datasource was not reported exactly once as disabled.'
    }
    if ($hostname.Count -ne 1 -or $hostname[0].enabled -ne $false) {
        throw 'Hostname provisioner was not reported exactly once as disabled.'
    }
    if (@($plugins | Where-Object { $_.enabled }).Count -ne 0) {
        throw 'The installer default configuration unexpectedly enabled a plugin.'
    }
    Assert-HostNameUnchanged -Expected $originalHostName -Phase 'install and service startup'

    $contendedStdout = Join-Path $ArtifactsDirectory 'contended-run.stdout.log'
    $contendedStderr = Join-Path $ArtifactsDirectory 'contended-run.stderr.log'
    $contendedProcess = Start-Process -FilePath $executable `
        -ArgumentList @('run') `
        -RedirectStandardOutput $contendedStdout `
        -RedirectStandardError $contendedStderr `
        -NoNewWindow `
        -PassThru
    if (-not $contendedProcess.WaitForExit(10000)) {
        $contendedProcess.Kill()
        $contendedProcess.WaitForExit()
        throw 'A contended foreground lifecycle process did not exit within 10 seconds; only that child process was terminated.'
    }
    $contendedProcess.WaitForExit()
    if ($contendedProcess.ExitCode -eq 0) {
        throw 'A second lifecycle process unexpectedly exited successfully while the LocalSystem service held the machine lock.'
    }
    $contendedOutput = @(
        Get-Content -LiteralPath $contendedStdout -Raw -ErrorAction SilentlyContinue
        Get-Content -LiteralPath $contendedStderr -Raw -ErrorAction SilentlyContinue
    ) -join "`n"
    if ($contendedOutput -notlike '*another wininit lifecycle process*') {
        throw "Contended run did not report the expected machine-lock error: $contendedOutput"
    }
    $contendedProcess.Dispose()
    $contendedProcess = $null

    Invoke-MsiExec -Arguments @('/x', $MsiPath, '/qn', '/norestart', '/l*v', $uninstallLog)
    $installed = $false
    Wait-ServiceAbsent -Name 'wininit'
    if (Test-Path -LiteralPath $executable) {
        throw "Executable remains after uninstall: $executable"
    }
    if (-not (Test-Path -LiteralPath $configuration -PathType Leaf)) {
        throw 'The preserved configuration was removed by uninstall.'
    }
    if (Test-Path -LiteralPath $installationKey) {
        throw 'The MSI-owned Installation Registry marker remains after uninstall.'
    }
    $preservedValue = Get-ItemPropertyValue -LiteralPath $stateKey -Name $sentinelName -ErrorAction Stop
    if ($preservedValue -ne $sentinelValue) {
        throw 'A lifecycle-state sentinel was not preserved by uninstall.'
    }
    Assert-HostNameUnchanged -Expected $originalHostName -Phase 'uninstall'

    Write-Host "MSI smoke test passed for ProductVersion $actualVersion."
}
finally {
    if ($null -ne $contendedProcess) {
        if (-not $contendedProcess.HasExited) {
            $contendedProcess.Kill()
            $contendedProcess.WaitForExit()
        }
        $contendedProcess.Dispose()
    }
    if ($null -ne $service) {
        $service.Close()
    }
    if ($installed) {
        try {
            Invoke-MsiExec -Arguments @('/x', $MsiPath, '/qn', '/norestart', '/l*v', $uninstallLog)
        }
        catch {
            Write-Warning "Cleanup uninstall failed: $_"
        }
    }
    if (Test-Path -LiteralPath $stateKey) {
        Remove-ItemProperty -LiteralPath $stateKey -Name $sentinelName -ErrorAction SilentlyContinue
    }
}
