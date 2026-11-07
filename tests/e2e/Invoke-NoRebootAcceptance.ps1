[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$DistDirectory,
    [Parameter(Mandatory)][string]$BaselineMsiPath,
    [Parameter(Mandatory)][string]$BaselineVersion,
    [string]$ArtifactsDirectory = (Join-Path (Get-Location) 'artifacts'),
    [ValidateSet('unclassified', 'github-hosted', 'qemu', 'openstack')][string]$Provider = 'unclassified',
    [ValidateSet('2022', '2025')][string]$ExpectedServer
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not [Environment]::Is64BitProcess -or $env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
    throw 'Use native 64-bit PowerShell on Windows AMD64; Registry and ProgramFiles views must match the MSI.'
}
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Acceptance testing requires an elevated administrator token on a disposable guest.'
}
$dist = (Resolve-Path -LiteralPath $DistDirectory).Path
$baseline = (Resolve-Path -LiteralPath $BaselineMsiPath).Path
$manifest = Get-Content -LiteralPath (Join-Path $dist 'release-manifest.json') -Raw | ConvertFrom-Json
$version = $manifest.version
$msiVersion = $version -replace '^v', ''
$os = Get-CimInstance Win32_OperatingSystem
$computer = Get-CimInstance Win32_ComputerSystem
if ($computer.PartOfDomain) { throw 'Refusing a domain-joined host.' }
if ($ExpectedServer -and $os.Caption -notlike "*Windows Server $ExpectedServer*") { throw 'Host does not match requested Server matrix cell.' }
$configRoot = Join-Path $env:ProgramData 'WinInit'
$installRoot = Join-Path $env:ProgramFiles 'WinInit'
$state = 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Wininit'
foreach ($path in @($configRoot, $installRoot, $state)) {
    if (Test-Path -LiteralPath $path) { throw "Refusing pre-existing installation/configuration/state: $path" }
}
if (Get-Service wininit -ErrorAction SilentlyContinue) { throw 'Refusing a pre-existing wininit service.' }
& (Join-Path $PSScriptRoot '..\integration\assert-no-msi-product.ps1')
$namePath = 'Registry::HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\ComputerName'
$active = (Get-ItemProperty -LiteralPath "$namePath\ActiveComputerName").ComputerName
$pending = (Get-ItemProperty -LiteralPath "$namePath\ComputerName").ComputerName
if ($active -ine $pending) { throw 'Refusing a host with a pending hostname change.' }
New-Item -ItemType Directory -Path $ArtifactsDirectory -Force | Out-Null
$artifacts = (Resolve-Path -LiteralPath $ArtifactsDirectory).Path
$report = [ordered]@{
    schema_version = 1; result = 'failed'; source_commit = $manifest.commit; source_modified = $manifest.source_modified
    version = $version; baseline_version = $BaselineVersion; baseline_kind = 'operator-supplied MSI; CI uses same-source lower-version fixture'
    provider_declared = $Provider; os = $os.Caption; os_version = $os.Version; architecture = $os.OSArchitecture
    manufacturer = $computer.Manufacturer; model = $computer.Model
    powershell = $PSVersionTable.PSVersion.ToString(); started_utc = [DateTime]::UtcNow.ToString('o')
    boot_before = $os.LastBootUpTime.ToUniversalTime().ToString('o'); hostname_before = $active
    candidate_msi_sha256 = (Get-FileHash -LiteralPath (Join-Path $dist 'wininit-windows-amd64.msi') -Algorithm SHA256).Hash.ToLowerInvariant()
    baseline_msi_sha256 = (Get-FileHash -LiteralPath $baseline -Algorithm SHA256).Hash.ToLowerInvariant()
    cases = @(); cleanup = 'not_run'; boot_after = $null; hostname_after = $null; error = $null
    skipped = @('real reboot/resume', 'hostname mutation', 'attached CIDATA/config-2 media', 'identity/password/sshd changes', 'clean-image/cloud creation', 'production certificate signing')
}
try {
    # Hash verification uses a fixed subject list before any installer runs.
    & (Join-Path $PSScriptRoot '..\integration\checksums.ps1') -DistDirectory $dist
    & (Join-Path $PSScriptRoot '..\integration\msi-smoke.ps1') -MsiPath $baseline -ExpectedMsiVersion $BaselineVersion `
        -ExpectedExecutableVersion $BaselineVersion -UpgradeMsiPath (Join-Path $dist 'wininit-windows-amd64.msi') `
        -ExpectedUpgradeMsiVersion $msiVersion -ExpectedUpgradeExecutableVersion $version `
        -PluginDistDirectory $dist -ArtifactsDirectory $artifacts
    $report.cases = @('MSI install, default disablement and protected ACLs', 'major upgrade preserves configuration and Registry sentinel',
        'machine-wide LocalSystem lifecycle lock', 'MSI uninstall preserves operational state')
    $report.result = 'passed'
}
catch { $report.error = $_.Exception.Message; throw }
finally {
    # This harness proved all three roots absent before installing anything.
    # Never erase preserved data if the owned service could still be running.
    try {
        if (Get-Service wininit -ErrorAction SilentlyContinue) { throw 'Owned service remains; preserve data for diagnosis.' }
        & (Join-Path $PSScriptRoot '..\integration\assert-no-msi-product.ps1')
        if (Test-Path -LiteralPath (Join-Path $state 'Installation')) { throw 'MSI ownership marker remains; preserve data for diagnosis.' }
        foreach ($path in @($configRoot, $installRoot)) {
            if (Test-Path -LiteralPath $path) {
                $items = @((Get-Item -LiteralPath $path -Force)) + @(Get-ChildItem -LiteralPath $path -Force -Recurse)
                if (@($items | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint }).Count -ne 0) { throw 'Refusing cleanup through a reparse point.' }
                Remove-Item -LiteralPath $path -Recurse
            }
        }
        if (Test-Path -LiteralPath $state) { Remove-Item -LiteralPath $state -Recurse }
        $report.cleanup = 'passed'
    }
    catch { $report.result = 'failed'; $report.cleanup = 'failed'; throw }
    finally {
        $report.boot_after = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToUniversalTime().ToString('o')
        $report.hostname_after = (Get-ItemProperty -LiteralPath "$namePath\ActiveComputerName").ComputerName
        $afterPending = (Get-ItemProperty -LiteralPath "$namePath\ComputerName").ComputerName
        $unchanged = $report.boot_before -eq $report.boot_after -and $active -ieq $report.hostname_after -and $pending -ieq $afterPending
        if (-not $unchanged) { $report.result = 'failed' }
        $report | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath (Join-Path $artifacts 'acceptance.json') -Encoding UTF8
        if (-not $unchanged) { throw 'Boot timestamp or active/configured hostname changed during no-reboot acceptance.' }
    }
}
Write-Host 'No-reboot acceptance passed; see acceptance.json and plugin-cases.json for exact scope.'
