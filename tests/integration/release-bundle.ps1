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
    # Reuse actual clean EXEs while modifying only installer input at the same
    # commit. Embedded Go VCS bits must not hide this SkipBuild source change.
    $checkout = Join-Path $fixture 'source checkout'
    $worktreeAdded = $false
    $manifestPath = Join-Path $fixture 'release-manifest.json'
    $sumsPath = Join-Path $fixture 'SHA256SUMS'
    $originalManifest = [IO.File]::ReadAllBytes($manifestPath)
    $originalSums = [IO.File]::ReadAllBytes($sumsPath)
    try {
        & git -C $repoRoot worktree add --detach $checkout HEAD
        if ($LASTEXITCODE -ne 0) { throw 'Source-status regression worktree creation failed.' }
        $worktreeAdded = $true
        [IO.File]::AppendAllText((Join-Path $checkout 'packaging\wix\wininit.yaml'), "`n# modified installer input`n")
        $modified = & (Join-Path $checkout 'scripts\source-status.ps1')
        if (-not $modified) { throw 'Installer-only source modification was not detected.' }
        $refusedDirtyCheckout = $false
        try {
            & (Join-Path $checkout 'scripts\package.ps1') -Version $manifest.version -SkipBuild -RequireClean -DistDirectory $fixture
        } catch {
            if ($_.Exception.Message -notlike 'RequireClean refuses a modified source checkout*') { throw }
            $refusedDirtyCheckout = $true
        }
        if (-not $refusedDirtyCheckout) { throw 'SkipBuild accepted modified MSI sources with RequireClean.' }
        & $tool -dist $fixture -version $manifest.version -commit $manifest.commit -signature $manifest.authenticode -checkout-modified
        if ($LASTEXITCODE -ne 0) { throw 'Modified-checkout development inventory generation failed.' }
        $modifiedManifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
        if (-not $modifiedManifest.source_modified -or -not $modifiedManifest.checkout_modified) {
            throw 'Development inventory falsely declared clean installer sources.'
        }
        & $tool -verify -dist $fixture
        if ($LASTEXITCODE -ne 0) { throw 'Valid declared-modified development inventory was rejected.' }
        & $tool -verify -require-clean -dist $fixture 2>&1 | Out-Host
        if ($LASTEXITCODE -eq 0) { throw 'Clean verification accepted a modified installer checkout.' }
    }
    finally {
        [IO.File]::WriteAllBytes($manifestPath, $originalManifest)
        [IO.File]::WriteAllBytes($sumsPath, $originalSums)
        if ($worktreeAdded) {
            & git -C $repoRoot worktree remove --force $checkout
            if ($LASTEXITCODE -ne 0) { throw 'Owned source-status worktree cleanup failed.' }
        }
    }
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
