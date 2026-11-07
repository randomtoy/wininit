[CmdletBinding()]
param([Parameter(Mandatory)][string]$DistDirectory)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('WinInit bundle ' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixture | Out-Null
try {
    $tool = Join-Path $fixture 'verify.exe'
    Push-Location $repoRoot
    try {
        & go build -o $tool ./cmd/wininit-release
        if ($LASTEXITCODE -ne 0) { throw 'Inventory test tool build failed.' }
    } finally { Pop-Location }
    $dist = (Resolve-Path -LiteralPath $DistDirectory).Path
    $manifest = Get-Content -LiteralPath (Join-Path $dist 'release-manifest.json') -Raw | ConvertFrom-Json
    foreach ($name in @($manifest.artifacts.name) + @('release-manifest.json', 'SHA256SUMS')) {
        if ($name -notmatch '^[a-zA-Z0-9.-]+$') { throw 'Unexpected fixture filename.' }
        Copy-Item -LiteralPath (Join-Path $dist $name) -Destination (Join-Path $fixture $name)
    }
    & $tool -verify -require-clean -dist $fixture
    if ($LASTEXITCODE -ne 0) { throw 'Valid clean distribution failed verification.' }
    $rejected = $false
    try {
        & (Join-Path $repoRoot 'scripts\sign.ps1') -Paths @((Join-Path $fixture 'wininit-windows-amd64.exe')) `
            -CertificateThumbprint ('a' * 40) -TimestampServer http://invalid.example
    } catch {
        if ($_.Exception.Message -notlike '*HTTPS RFC 3161*') { throw }
        $rejected = $true
    }
    if (-not $rejected) { throw 'Signing accepted a non-HTTPS timestamp service.' }
    foreach ($mutation in @('binary', 'sbom', 'manifest', 'checksums')) {
        $name = switch ($mutation) {
            'binary' { 'wininit-plugin-hostname-windows-amd64.exe' }
            'sbom' { 'wininit-windows-amd64.exe.cdx.json' }
            'manifest' { 'release-manifest.json' }
            'checksums' { 'SHA256SUMS' }
        }
        $path = Join-Path $fixture $name
        $original = [IO.File]::ReadAllBytes($path)
        try {
            if ($mutation -eq 'binary') { [IO.File]::AppendAllText($path, 'tampered') }
            elseif ($mutation -eq 'sbom') { Remove-Item -LiteralPath $path }
            elseif ($mutation -eq 'manifest') {
                $changed = [Text.Encoding]::UTF8.GetString($original).Replace($manifest.commit, ('a' * 40))
                [IO.File]::WriteAllText($path, $changed)
            }
            else { [IO.File]::AppendAllText($path, "extra entry`n") }
            & $tool -verify -dist $fixture 2>&1 | Out-Host
            if ($LASTEXITCODE -eq 0) { throw "Verifier accepted $mutation corruption." }
        } finally { [IO.File]::WriteAllBytes($path, $original) }
    }
    & $tool -verify -dist $fixture
    if ($LASTEXITCODE -ne 0) { throw 'Restored distribution failed verification.' }
    Write-Host 'Release integrity corruption tests passed.'
}
finally {
    if (Test-Path -LiteralPath $fixture) { Remove-Item -LiteralPath $fixture -Recurse }
}
