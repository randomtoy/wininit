[CmdletBinding()]
param(
    [string]$Version = $env:VERSION,
    [string]$Commit = $env:COMMIT,
    [string]$BuildDate = $env:BUILD_DATE,
    [string]$DistDirectory = $env:DIST_DIR
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $DistDirectory) {
    $DistDirectory = Join-Path $repoRoot 'dist'
}
if (-not $Version) {
    $Version = (& git -C $repoRoot describe --tags --always --dirty 2>$null)
    if (-not $Version) { $Version = 'dev' }
}
if (-not $Commit) {
    $Commit = (& git -C $repoRoot rev-parse --verify HEAD 2>$null)
    if (-not $Commit) { $Commit = 'unknown' }
}
if (-not $BuildDate) {
    $BuildDate = (& git -C $repoRoot show -s --format=%cI HEAD 2>$null)
    if (-not $BuildDate) { $BuildDate = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ') }
}

New-Item -ItemType Directory -Force -Path $DistDirectory | Out-Null
$output = Join-Path $DistDirectory 'wininit-windows-amd64.exe'
$ldflags = "-s -w -buildid= -X=github.com/randomtoy/wininit/internal/version.Version=$Version -X=github.com/randomtoy/wininit/internal/version.Commit=$Commit -X=github.com/randomtoy/wininit/internal/version.BuildDate=$BuildDate"

$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & go build -trimpath -buildvcs=true -ldflags $ldflags -o $output ./cmd/wininit
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    $pluginOutput = Join-Path $DistDirectory 'wininit-plugin-configdrive-windows-amd64.exe'
    & go build -trimpath -buildvcs=true -ldflags $ldflags -o $pluginOutput ./cmd/wininit-plugin-configdrive
    if ($LASTEXITCODE -ne 0) { throw "ConfigDrive plugin build failed with exit code $LASTEXITCODE" }
    $powerShellOutput = Join-Path $DistDirectory 'wininit-plugin-powershell-windows-amd64.exe'
    & go build -trimpath -buildvcs=true -ldflags $ldflags -o $powerShellOutput ./cmd/wininit-plugin-powershell
    if ($LASTEXITCODE -ne 0) { throw "PowerShell plugin build failed with exit code $LASTEXITCODE" }
    $nocloudOutput = Join-Path $DistDirectory 'wininit-plugin-nocloud-windows-amd64.exe'
    & go build -trimpath -buildvcs=true -ldflags $ldflags -o $nocloudOutput ./cmd/wininit-plugin-nocloud
    if ($LASTEXITCODE -ne 0) { throw "nocloud plugin build failed with exit code $LASTEXITCODE" }
    $hostnameOutput = Join-Path $DistDirectory 'wininit-plugin-hostname-windows-amd64.exe'
    & go build -trimpath -buildvcs=true -ldflags $ldflags -o $hostnameOutput ./cmd/wininit-plugin-hostname
    if ($LASTEXITCODE -ne 0) { throw "hostname plugin build failed with exit code $LASTEXITCODE" }
}
finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    $env:CGO_ENABLED = $previousCGO
}

Write-Host "Built $output"
