[CmdletBinding()]
param(
    [string]$Version = $env:VERSION,
    [switch]$SkipBuild,
    [string]$DistDirectory = $env:DIST_DIR,
    [string]$SigningCertificateThumbprint,
    [uri]$TimestampServer,
    [ValidateSet('CurrentUser', 'LocalMachine')][string]$CertificateStore = 'CurrentUser',
    [switch]$RequireClean
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
$DistDirectory = (Resolve-Path -LiteralPath $DistDirectory).Path
if ([bool]$SigningCertificateThumbprint -ne [bool]$TimestampServer) {
    throw 'Certificate thumbprint and HTTPS timestamp server must be supplied together.'
}

if (-not $SkipBuild) {
    & (Join-Path $PSScriptRoot 'build.ps1') -Version $Version -DistDirectory $DistDirectory
}

$exePath = Join-Path $DistDirectory 'wininit-windows-amd64.exe'
if (-not (Test-Path -LiteralPath $exePath -PathType Leaf)) {
    throw "Windows executable is missing: $exePath"
}
$metadata = (& $exePath version --json | ConvertFrom-Json)
if ($LASTEXITCODE -ne 0 -or $metadata.version -cne $Version) {
    throw 'Agent executable version does not match the requested package version.'
}
$sourceCommit = & git -C $repoRoot rev-parse --verify HEAD
if ($LASTEXITCODE -ne 0 -or $metadata.commit -cne $sourceCommit) {
    throw 'Agent executable commit metadata does not match the source checkout.'
}
if ($SigningCertificateThumbprint) {
    $images = @('wininit', 'wininit-plugin-configdrive', 'wininit-plugin-powershell', 'wininit-plugin-nocloud', 'wininit-plugin-hostname') |
        ForEach-Object { Join-Path $DistDirectory "$_-windows-amd64.exe" }
    & (Join-Path $PSScriptRoot 'sign.ps1') -Paths $images -CertificateThumbprint $SigningCertificateThumbprint `
        -TimestampServer $TimestampServer -CertificateStore $CertificateStore
}

$wixProject = Join-Path $repoRoot 'packaging\wix\WinInit.wixproj'
$configurationFile = Join-Path $repoRoot 'packaging\wix\wininit.yaml'
$outputPath = [IO.Path]::GetFullPath($DistDirectory) + [IO.Path]::DirectorySeparatorChar
$intermediatePath = ([IO.Path]::GetFullPath((Join-Path $DistDirectory 'obj'))).Replace('\', '/') + '/'
$msbuildArguments = @(
    'build',
    $wixProject,
    '--configuration', 'Release',
    '--nologo',
    "-p:MsiProductVersion=$msiVersion",
    "-p:SourceExe=$exePath",
    "-p:ConfigurationFile=$configurationFile",
    "-p:BaseIntermediateOutputPath=$intermediatePath",
    "-p:IntermediateOutputPath=${intermediatePath}Release/",
    "-p:OutputPath=$outputPath"
)

& dotnet @msbuildArguments
if ($LASTEXITCODE -ne 0) { throw "WiX build failed with exit code $LASTEXITCODE" }

$msiPath = Join-Path $DistDirectory 'wininit-windows-amd64.msi'
if (-not (Test-Path -LiteralPath $msiPath -PathType Leaf)) {
    throw "WiX completed without producing the expected MSI: $msiPath"
}

$signature = 'unsigned'
if ($SigningCertificateThumbprint) {
    & (Join-Path $PSScriptRoot 'sign.ps1') -Paths @($msiPath) -CertificateThumbprint $SigningCertificateThumbprint `
        -TimestampServer $TimestampServer -CertificateStore $CertificateStore
    $signature = 'authenticode'
}
& (Join-Path $PSScriptRoot 'release-metadata.ps1') -Version $Version -DistDirectory $DistDirectory `
    -Signature $signature -RequireClean:$RequireClean
Write-Host "Packaged WinInit $Version (MSI ProductVersion $msiVersion)"
