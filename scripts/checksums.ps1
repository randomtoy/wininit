[CmdletBinding()]
param(
    [string]$DistDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $DistDirectory) {
    $DistDirectory = Join-Path $repoRoot 'dist'
}

$artifactNames = @('wininit-windows-amd64.exe', 'wininit-windows-amd64.msi', 'wininit-plugin-configdrive-windows-amd64.exe', 'wininit-plugin-powershell-windows-amd64.exe')
$lines = foreach ($name in $artifactNames) {
    $path = Join-Path $DistDirectory $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required artifact is missing: $path"
    }
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    "$hash  $name"
}

$output = Join-Path $DistDirectory 'SHA256SUMS'
# Checksum manifests are consumed on Windows and Unix. Use canonical LF line
# endings instead of the host platform newline so filenames remain portable.
$content = ($lines -join "`n") + "`n"
[IO.File]::WriteAllText($output, $content, [Text.UTF8Encoding]::new($false))
Write-Host "Wrote $output"
