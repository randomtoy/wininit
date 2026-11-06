# Windows installer

The AMD64 MSI is built with WiX Toolset 7.0.1 and Windows Installer 5.0. The WiX SDK and extension versions are exact NuGet pins. WiX 7 requires explicit acceptance of its Open Source Maintenance Fee (OSMF) EULA; the project records that acceptance with `<AcceptEula>wix7</AcceptEula>`. Builders and redistributors remain responsible for reviewing and complying with the [WiX OSMF terms](https://docs.firegiant.com/wix/osmf/).

Build on Windows with the .NET 10 SDK, Go, and PowerShell:

```powershell
.\scripts\package.ps1 -Version 0.1.0
```

The script creates `dist\wininit-windows-amd64.exe`, `dist\wininit-windows-amd64.msi`, the separate ConfigDrive/PowerShell plugin EXEs, and `dist\SHA256SUMS`. MSI
installs only the agent; plugins require separate reviewed installation and
explicit enablement. MSI versions must be `MAJOR.MINOR.PATCH`; Windows Installer limits those fields to 255, 255, and 65535 respectively.

Install and uninstall silently while retaining a verbose administration log:

```powershell
msiexec.exe /i .\dist\wininit-windows-amd64.msi /qn /norestart /l*v .\wininit-install.log
msiexec.exe /x .\dist\wininit-windows-amd64.msi /qn /norestart /l*v .\wininit-uninstall.log
```

The MSI installs the executable in `Program Files\WinInit`, installs and starts the `wininit` service through native MSI service tables, and creates `ProgramData\WinInit`. It owns the `HKLM\SOFTWARE\Wininit\Installation` marker used by the CLI to prevent direct SCM commands from conflicting with Windows Installer. Major upgrades retain the stable UpgradeCode and component GUIDs, stop the old service, replace the executable, and restart it. The default configuration is installed only when absent and is a permanent component, so upgrades and ordinary uninstall do not destroy administrator changes. Runtime logs, plugins, and Registry lifecycle state are also preserved on uninstall; only the MSI-owned `Installation` marker subkey is removed.

To intentionally purge data after uninstall, an administrator must remove `C:\ProgramData\WinInit` and `HKLM\SOFTWARE\WinInit`. Review these locations before deletion; the MSI deliberately has no purge custom action.
