# Read-only installer inventory. Win32_Product must not be used: querying it
# can trigger consistency checks/repairs of unrelated installed applications.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not ('WinInitE2E.MsiInventory' -as [type])) {
    Add-Type -TypeDefinition @'
using System.Runtime.InteropServices;
using System.Text;
namespace WinInitE2E {
    public static class MsiInventory {
        [DefaultDllImportSearchPaths(DllImportSearchPath.System32)]
        [DllImport("msi.dll", CharSet = CharSet.Unicode, ExactSpelling = true)]
        public static extern uint MsiEnumRelatedProductsW(string upgradeCode,
            uint reserved, uint index, StringBuilder productCode);
    }
}
'@
}
$productCode = New-Object Text.StringBuilder(39)
$code = [WinInitE2E.MsiInventory]::MsiEnumRelatedProductsW(
    '{B6E7B4A3-7C3B-4850-AE0B-3F8D1B8E72D9}', 0, 0, $productCode)
if ($code -eq 0) { throw 'A registered WinInit MSI product remains; preserve its files/state.' }
if ($code -ne 259) { throw "Cannot establish absence of registered WinInit products (MSI code $code)." }
