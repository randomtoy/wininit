[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Version,
    [Parameter(Mandatory)][string]$DistDirectory,
    [ValidateSet('unsigned', 'authenticode')][string]$Signature = 'unsigned',
    [switch]$RequireClean
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repoRoot = Split-Path -Parent $PSScriptRoot
$dist = (Resolve-Path -LiteralPath $DistDirectory).Path
$commit = & git -C $repoRoot rev-parse --verify HEAD
if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the source commit.' }
Push-Location $repoRoot
try {
    # Version pinned independently of the runtime module. Go's public checksum
    # database authenticates this build-time tool and its module dependencies.
    foreach ($name in @('wininit', 'wininit-plugin-configdrive', 'wininit-plugin-powershell', 'wininit-plugin-nocloud', 'wininit-plugin-hostname')) {
        $binary = Join-Path $dist "$name-windows-amd64.exe"
        & go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.12.0 bin `
            -json -output-version 1.6 -std -short-purls -noserial -notimestamp -version $Version `
            -output "$binary.cdx.json" $binary
        if ($LASTEXITCODE -ne 0) { throw "SBOM generation failed for $name." }
    }
    # Recheck after the build/SBOM steps: SkipBuild may reuse clean EXEs while
    # the MSI's WiX/configuration inputs come from a modified working tree.
    $checkoutModified = & (Join-Path $PSScriptRoot 'source-status.ps1') -RequireClean:$RequireClean
    $arguments = @('run', './cmd/wininit-release', '-dist', $dist, '-version', $Version, '-commit', $commit, '-signature', $Signature)
    if ($RequireClean) { $arguments += '-require-clean' }
    if ($checkoutModified) { $arguments += '-checkout-modified' }
    & go @arguments
    if ($LASTEXITCODE -ne 0) { throw 'Release inventory generation failed.' }
    & go run ./cmd/wininit-release -dist $dist -verify
    if ($LASTEXITCODE -ne 0) { throw 'Release inventory verification failed.' }
}
finally { Pop-Location }
