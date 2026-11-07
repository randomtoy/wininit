# Match the hostname adapter's physical DNS view, not just NetBIOS. A long DNS
# name may have a truncated NetBIOS alias; using that alias as a seed could
# accidentally request a rename in a nominally unchanged-name test.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not ('WinInitE2E.HostSnapshot' -as [type])) {
    Add-Type -TypeDefinition @'
using System.Runtime.InteropServices;
using System.Text;
namespace WinInitE2E {
    public static class HostSnapshot {
        [DefaultDllImportSearchPaths(DllImportSearchPath.System32)]
        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, ExactSpelling = true, SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        public static extern bool GetComputerNameExW(int format, StringBuilder name, ref uint size);
    }
}
'@
}
$buffer = New-Object Text.StringBuilder(256)
$size = [uint32]256
if (-not [WinInitE2E.HostSnapshot]::GetComputerNameExW(5, $buffer, [ref]$size)) {
    throw "Cannot read physical DNS hostname (Windows code $([Runtime.InteropServices.Marshal]::GetLastWin32Error()))."
}
$namePath = 'Registry::HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\ComputerName'
[PSCustomObject]@{
    Active = $buffer.ToString()
    Pending = (Get-ItemProperty -LiteralPath 'Registry::HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters').'NV Hostname'
    NetBiosActive = (Get-ItemProperty -LiteralPath "$namePath\ActiveComputerName").ComputerName
    NetBiosPending = (Get-ItemProperty -LiteralPath "$namePath\ComputerName").ComputerName
    BootUtc = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToUniversalTime().ToString('o')
}
