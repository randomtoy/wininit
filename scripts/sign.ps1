[CmdletBinding()]
param(
    [Parameter(Mandatory)][string[]]$Paths,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$CertificateThumbprint,
    [Parameter(Mandatory)][uri]$TimestampServer,
    [ValidateSet('CurrentUser', 'LocalMachine')][string]$CertificateStore = 'CurrentUser',
    [string]$SignTool
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $TimestampServer.IsAbsoluteUri -or $TimestampServer.Scheme -ne 'https' -or
    $TimestampServer.UserInfo -or $TimestampServer.Query -or $TimestampServer.Fragment) {
    throw 'Signing requires an HTTPS RFC 3161 timestamp server without embedded credentials.'
}
if (-not $SignTool) {
    $command = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($command) { $SignTool = $command.Source }
    else {
        $sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
        $candidates = @(Get-ChildItem -LiteralPath $sdk -Directory -ErrorAction Stop |
            Where-Object { $_.Name -match '^10\.0\.\d+\.0$' } |
            Sort-Object { [version]$_.Name } -Descending |
            ForEach-Object { Join-Path $_.FullName 'x64\signtool.exe' } |
            Where-Object { Test-Path -LiteralPath $_ -PathType Leaf })
        if ($candidates.Count -eq 0) { throw 'Windows SDK signtool.exe was not found.' }
        $SignTool = $candidates[0]
    }
}
# Keys stay in the existing certificate provider/store. Never accept a PFX
# password in argv, import a private key, or modify machine trust stores.
foreach ($path in $Paths) {
    $resolved = (Resolve-Path -LiteralPath $path -ErrorAction Stop).Path
    $arguments = @('sign', '/fd', 'SHA256', '/tr', $TimestampServer.AbsoluteUri,
        '/td', 'SHA256', '/sha1', $CertificateThumbprint, '/s', 'My')
    if ($CertificateStore -eq 'LocalMachine') { $arguments += '/sm' }
    & $SignTool @arguments $resolved
    if ($LASTEXITCODE -ne 0) { throw "Authenticode signing failed for $resolved ($LASTEXITCODE)." }
    & $SignTool verify /pa /all /tw $resolved
    if ($LASTEXITCODE -ne 0) { throw "Authenticode verification failed for $resolved ($LASTEXITCODE)." }
}
