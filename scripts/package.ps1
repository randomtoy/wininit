[CmdletBinding()]
param(
    [string]$Version = $env:VERSION,
    [switch]$SkipBuild,
    [string]$DistDirectory = $env:DIST_DIR
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $DistDirectory) {
    $DistDirectory = Join-Path $repoRoot 'dist'
}
if (-not $Version) {
    $Version = (& git -C $repoRoot describe --tags --exact-match 2>$null)
    if (-not $Version) { $Version = '0.0.0' }
}

$msiVersion = (& (Join-Path $PSScriptRoot 'msi-version.ps1') -Version $Version).Trim()
New-Item -ItemType Directory -Force -Path $DistDirectory | Out-Null

if (-not $SkipBuild) {
    & (Join-Path $PSScriptRoot 'build.ps1') -Version $Version -DistDirectory $DistDirectory
}

$exePath = Join-Path $DistDirectory 'wininit-windows-amd64.exe'
if (-not (Test-Path -LiteralPath $exePath -PathType Leaf)) {
    throw "Windows executable is missing: $exePath"
}

$wixProject = Join-Path $repoRoot 'packaging\wix\WinInit.wixproj'
$configurationFile = Join-Path $repoRoot 'packaging\wix\wininit.yaml'
$outputPath = [IO.Path]::GetFullPath($DistDirectory) + [IO.Path]::DirectorySeparatorChar
$msbuildArguments = @(
    'build',
    $wixProject,
    '--configuration', 'Release',
    '--nologo',
    "-p:MsiProductVersion=$msiVersion",
    "-p:SourceExe=$exePath",
    "-p:ConfigurationFile=$configurationFile",
    "-p:OutputPath=$outputPath"
)

& dotnet @msbuildArguments
if ($LASTEXITCODE -ne 0) { throw "WiX build failed with exit code $LASTEXITCODE" }

$msiPath = Join-Path $DistDirectory 'wininit-windows-amd64.msi'
if (-not (Test-Path -LiteralPath $msiPath -PathType Leaf)) {
    throw "WiX completed without producing the expected MSI: $msiPath"
}

& (Join-Path $PSScriptRoot 'checksums.ps1') -DistDirectory $DistDirectory
Write-Host "Packaged WinInit $Version (MSI ProductVersion $msiVersion)"
