[CmdletBinding()]
param(
    [string]$DistDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not $DistDirectory) {
    $DistDirectory = Join-Path $repoRoot 'dist'
}
$manifestPath = Join-Path $DistDirectory 'SHA256SUMS'
$bytes = [IO.File]::ReadAllBytes($manifestPath)
if ($bytes -contains 13) {
    throw 'SHA256SUMS contains CR bytes; the manifest must use portable LF line endings.'
}
$content = [Text.Encoding]::UTF8.GetString($bytes)
$lines = $content.TrimEnd("`n").Split("`n")
$expectedNames = @('wininit-windows-amd64.exe', 'wininit-windows-amd64.msi')
if ($lines.Count -ne $expectedNames.Count) {
    throw "SHA256SUMS contains $($lines.Count) entries; expected $($expectedNames.Count)."
}
foreach ($index in 0..($expectedNames.Count - 1)) {
    $match = [regex]::Match($lines[$index], '^(?<hash>[0-9a-f]{64})  (?<name>[^/\\]+)$')
    if (-not $match.Success) {
        throw "Invalid SHA256SUMS line: '$($lines[$index])'."
    }
    $name = $match.Groups['name'].Value
    if ($name -ne $expectedNames[$index]) {
        throw "Unexpected checksum filename '$name'; expected '$($expectedNames[$index])'."
    }
    $path = Join-Path $DistDirectory $name
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    if ($actual -ne $match.Groups['hash'].Value) {
        throw "SHA-256 mismatch for '$name'."
    }
}

Write-Host 'Portable SHA256SUMS verification passed.'
