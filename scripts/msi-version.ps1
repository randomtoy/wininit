[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$Version
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$match = [regex]::Match($Version, '^v?(?<major>0|[1-9][0-9]*)\.(?<minor>0|[1-9][0-9]*)\.(?<patch>0|[1-9][0-9]*)$')
if (-not $match.Success) {
    throw "'$Version' is not an MSI-compatible release version. Use vMAJOR.MINOR.PATCH without prerelease or build metadata."
}

$major = [uint64]$match.Groups['major'].Value
$minor = [uint64]$match.Groups['minor'].Value
$patch = [uint64]$match.Groups['patch'].Value
if ($major -gt 255 -or $minor -gt 255 -or $patch -gt 65535) {
    throw "'$Version' exceeds Windows Installer limits (major/minor <= 255, patch <= 65535)."
}

Write-Output "$major.$minor.$patch"
