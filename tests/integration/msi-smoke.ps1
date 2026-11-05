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

$installed = $false
$service = $null
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

    Write-Host "MSI smoke test passed for ProductVersion $actualVersion."
}
finally {
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
