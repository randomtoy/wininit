[CmdletBinding()]
param([switch]$RequireClean)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repoRoot = Split-Path -Parent $PSScriptRoot
$changes = @(& git -C $repoRoot status --porcelain=v1 --untracked-files=normal)
if ($LASTEXITCODE -ne 0) { throw 'Cannot determine source checkout status.' }
$modified = $changes.Count -ne 0
if ($RequireClean -and $modified) { throw 'RequireClean refuses a modified source checkout, including MSI/configuration inputs.' }
return $modified
