[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$MsiPath,
    [Parameter(Mandatory)]
    [string]$ExpectedMsiVersion,
    [string]$ExpectedExecutableVersion,
    [string]$ArtifactsDirectory,
    [string]$UpgradeMsiPath,
    [string]$ExpectedUpgradeMsiVersion,
    [string]$ExpectedUpgradeExecutableVersion,
    [string]$PluginDistDirectory
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

    # Start-Process joins its argument array without quoting. MSI and log paths
    # routinely contain spaces, so quote every argument explicitly.
    foreach ($argument in $Arguments) {
        if ($argument.Contains('"') -or $argument.EndsWith('\')) { throw 'Unsafe MSI argument.' }
    }
    $quoted = ($Arguments | ForEach-Object { '"' + $_ + '"' }) -join ' '
    $process = Start-Process -FilePath "$env:SystemRoot\System32\msiexec.exe" `
        -ArgumentList $quoted -Wait -PassThru
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
$uninstallMsi = $MsiPath
if ($UpgradeMsiPath) {
    $UpgradeMsiPath = (Resolve-Path -LiteralPath $UpgradeMsiPath).Path
    if ((Get-MsiProperty $UpgradeMsiPath 'UpgradeCode') -ne $expectedUpgradeCode -or
        (Get-MsiProperty $UpgradeMsiPath 'ProductVersion') -ne $ExpectedUpgradeMsiVersion -or
        [version]$ExpectedUpgradeMsiVersion -le [version]$ExpectedMsiVersion -or
        (Get-MsiProperty $UpgradeMsiPath 'ProductCode') -eq (Get-MsiProperty $MsiPath 'ProductCode')) {
        throw 'Upgrade requires a newer version, the same UpgradeCode, and a distinct ProductCode.'
    }
}

$existingService = Get-Service -Name 'wininit' -ErrorAction SilentlyContinue
if ($null -ne $existingService) {
    throw 'A wininit service already exists. Refusing to alter a pre-existing installation.'
}
if (Test-Path -LiteralPath 'HKLM:\SOFTWARE\Wininit\Installation') {
    throw 'An MSI ownership marker already exists. Refusing to alter a pre-existing product.'
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
    $installed = $true
    Invoke-MsiExec -Arguments @('/i', $MsiPath, '/qn', '/norestart', 'REBOOT=ReallySuppress', '/l*v', $installLog)

    $installDirectory = Join-Path $env:ProgramFiles 'WinInit'
    $executable = Join-Path $installDirectory 'wininit.exe'
    $configuration = Join-Path $env:ProgramData 'WinInit\wininit.yaml'
    $logsDirectory = Join-Path $env:ProgramData 'WinInit\logs'
    $pluginsDirectory = Join-Path $env:ProgramData 'WinInit\plugins'
    foreach ($path in @($executable, $configuration, $logsDirectory, $pluginsDirectory)) {
        if (-not (Test-Path -LiteralPath $path)) { throw "Expected installed path is missing: $path" }
    }
    if (@(Get-ChildItem -LiteralPath $installDirectory -File -Recurse).Count -ne 1 -or
        @(Get-ChildItem -LiteralPath $pluginsDirectory -Filter '*.exe' -File).Count -ne 0) {
        throw 'Agent-only MSI unexpectedly installed additional payloads/plugins.'
    }
    foreach ($path in @((Join-Path $env:ProgramData 'WinInit'), $configuration, $logsDirectory, $pluginsDirectory)) {
        $acl = Get-Acl -LiteralPath $path
        if (-not $acl.AreAccessRulesProtected) { throw "Installed ACL is not protected: $path" }
        $owner = $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
        if ($owner -notin @('S-1-5-18', 'S-1-5-32-544')) { throw "Unexpected installed owner '$owner': $path" }
        $rules = $acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])
        foreach ($rule in $rules) {
            if ($rule.AccessControlType -ne [Security.AccessControl.AccessControlType]::Allow -or
                $rule.IdentityReference.Value -notin @('S-1-5-18', 'S-1-5-32-544')) {
                throw "Installed ACL grants unexpected access: $path"
            }
        }
        foreach ($sid in @('S-1-5-18', 'S-1-5-32-544')) {
            if (@($rules | Where-Object { $_.IdentityReference.Value -eq $sid -and
                ($_.FileSystemRights -band [Security.AccessControl.FileSystemRights]::FullControl) -eq [Security.AccessControl.FileSystemRights]::FullControl }).Count -eq 0) {
                throw "Installed ACL is missing full control for '$sid': $path"
            }
        }
    }
    $managedBy = Get-ItemPropertyValue -LiteralPath $installationKey -Name 'ManagedBy' -ErrorAction Stop
    if ($managedBy -ne 'MSI') { throw "Unexpected MSI management marker '$managedBy'." }
    # The MSI marker already guarantees this key exists. New-Item -Force would
    # replace the Registry key and erase both checkpoints and Installation.
    New-ItemProperty -LiteralPath $stateKey -Name $sentinelName -Value $sentinelValue -PropertyType String -Force | Out-Null
    if ((Get-ItemPropertyValue -LiteralPath $installationKey -Name 'ManagedBy') -ne 'MSI') {
        throw 'Adding the smoke-test sentinel changed the MSI ownership marker.'
    }

    $service = Get-Service -Name 'wininit' -ErrorAction Stop
    $service.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))
    $serviceRecord = Get-CimInstance Win32_Service -Filter "Name='wininit'"
    if ($serviceRecord.StartName -ne 'LocalSystem') { throw 'MSI service is not running as LocalSystem.' }
    if ($serviceRecord.StartMode -ne 'Auto') {
        throw "wininit service start mode is '$($serviceRecord.StartMode)', expected 'Auto'."
    }
    if ($serviceRecord.PathName -notlike "*$executable*") {
        throw "wininit service path '$($serviceRecord.PathName)' does not reference '$executable'."
    }
    $service.Close()
    $service = $null

    $versionOutput = (& $executable version --json | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0) { throw 'wininit version failed.' }
    if ($ExpectedExecutableVersion -and $versionOutput.version -cne $ExpectedExecutableVersion) {
        throw 'Installed agent version differs from the expected executable version.'
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
    if ($plugins.Count -ne 7) {
        throw "Status reported $($plugins.Count) plugins, expected seven independently disabled plugins."
    }
    $noCloud = @($plugins | Where-Object { $_.name -eq 'nocloud' -and $_.kind -eq 'datasource' })
    $hostname = @($plugins | Where-Object { $_.name -eq 'hostname' -and $_.kind -eq 'provisioner' })
    $configDrive = @($plugins | Where-Object { $_.name -eq 'configdrive' -and $_.kind -eq 'datasource' })
    foreach ($name in @('password_policy', 'local_users', 'authorized_keys', 'powershell_user_data')) {
        $entry = @($plugins | Where-Object { $_.name -eq $name -and $_.kind -eq 'provisioner' })
        if ($entry.Count -ne 1 -or $entry[0].enabled -ne $false -or $entry[0].status -ne 'disabled') {
            throw "Identity plugin '$name' was not reported exactly once as disabled."
        }
    }
    if ($noCloud.Count -ne 1 -or $noCloud[0].enabled -ne $false) {
        throw 'NoCloud datasource was not reported exactly once as disabled.'
    }
    if ($hostname.Count -ne 1 -or $hostname[0].enabled -ne $false) {
        throw 'Hostname provisioner was not reported exactly once as disabled.'
    }
    if ($configDrive.Count -ne 1 -or $configDrive[0].enabled -ne $false -or $configDrive[0].status -ne 'disabled') {
        throw 'ConfigDrive datasource was not reported exactly once as disabled.'
    }
    if (@($plugins | Where-Object { $_.enabled }).Count -ne 0) {
        throw 'The installer default configuration unexpectedly enabled a plugin.'
    }
    Assert-HostNameUnchanged -Expected $originalHostName -Phase 'install and service startup'

    if ($UpgradeMsiPath) {
        # Administrator-owned contents must survive a major upgrade byte for
        # byte. The sentinel shares the actual lifecycle checkpoint parent.
        Stop-Service -Name wininit -ErrorAction Stop
        [IO.File]::AppendAllText($configuration, "`n# upgrade preservation $sentinelValue`n", [Text.UTF8Encoding]::new($false))
        $configurationHash = (Get-FileHash -LiteralPath $configuration -Algorithm SHA256).Hash
        Invoke-MsiExec -Arguments @('/i', $UpgradeMsiPath, '/qn', '/norestart', 'REBOOT=ReallySuppress', '/l*v', (Join-Path $ArtifactsDirectory 'msi-upgrade.log'))
        $uninstallMsi = $UpgradeMsiPath
        if ((Get-FileHash -LiteralPath $configuration -Algorithm SHA256).Hash -ne $configurationHash -or
            (Get-ItemPropertyValue -LiteralPath $stateKey -Name $sentinelName) -ne $sentinelValue) {
            throw 'Upgrade replaced administrator configuration or lifecycle state.'
        }
        $upgradedVersion = (& $executable version --json | ConvertFrom-Json)
        if ($LASTEXITCODE -ne 0 -or $upgradedVersion.version -ne $ExpectedUpgradeExecutableVersion) {
            throw 'Upgrade did not install the expected executable version.'
        }
        $upgradedService = Get-Service wininit
        try { $upgradedService.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30)) }
        finally { $upgradedService.Dispose() }
        Assert-HostNameUnchanged -Expected $originalHostName -Phase 'upgrade'
    }

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

    if ($PluginDistDirectory) {
        & (Join-Path $PSScriptRoot '..\e2e\guest-scenarios.ps1') -Agent $executable `
            -DistDirectory $PluginDistDirectory -ArtifactsDirectory $ArtifactsDirectory
    }

    $preservedConfigurationHash = (Get-FileHash -LiteralPath $configuration -Algorithm SHA256).Hash
    Invoke-MsiExec -Arguments @('/x', $uninstallMsi, '/qn', '/norestart', 'REBOOT=ReallySuppress', '/l*v', $uninstallLog)
    $installed = $false
    Wait-ServiceAbsent -Name 'wininit'
    if (Test-Path -LiteralPath $executable) {
        throw "Executable remains after uninstall: $executable"
    }
    if (-not (Test-Path -LiteralPath $configuration -PathType Leaf)) {
        throw 'The preserved configuration was removed by uninstall.'
    }
    if ((Get-FileHash -LiteralPath $configuration -Algorithm SHA256).Hash -ne $preservedConfigurationHash) {
        throw 'Uninstall modified the preserved configuration.'
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
            Invoke-MsiExec -Arguments @('/x', $uninstallMsi, '/qn', '/norestart', 'REBOOT=ReallySuppress', '/l*v', $uninstallLog)
        }
        catch {
            Write-Warning "Cleanup uninstall failed: $_"
        }
    }
    if (Test-Path -LiteralPath $stateKey) {
        Remove-ItemProperty -LiteralPath $stateKey -Name $sentinelName -ErrorAction SilentlyContinue
    }
}
