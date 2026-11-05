# ADR 0005: Package with WiX and MSI-native service management

- Status: Accepted
- Date: 2026-11-05

## Context

Windows administrators need unattended install, repair, upgrade, rollback, and
uninstall behavior. Ad-hoc scripts do not give Windows Installer ownership or
transaction semantics.

## Decision

Use WiX Toolset 7.0.1 with exact SDK and extension pins. Record WiX 7's required
EULA acknowledgement as `<AcceptEula>wix7</AcceptEula>` in the project;
builders and redistributors remain responsible for reviewing and complying with
the current WiX Open Source Maintenance Fee (OSMF) terms. Install per-machine
under Program Files, create writable ProgramData locations, and use WiX/MSI
service installation and control elements. Keep component GUIDs and UpgradeCode
stable, reject unsupported architectures, and do not add a machine-wide PATH.

Preserve lifecycle state and administrator-owned configuration during upgrade
and ordinary uninstall. Provide a separate, deliberate purge procedure.

## Consequences

MSI construction and full smoke testing require Windows, the pinned WiX
toolchain, and compliance with its licensing/OSMF terms. Service-start failures
can participate in rollback. Component identity and version mapping become
compatibility contracts.

## Alternatives considered

- Custom install/uninstall scripts: rejected because service and rollback state
  would not be owned reliably by Windows Installer.
- An archive-only distribution: retained as a manual/development option but
  insufficient as the supported installation lifecycle.

## References

- [WiX release notes](https://docs.firegiant.com/wix/whatsnew/releasenotes/)
- [WiX `ServiceInstall`](https://docs.firegiant.com/wix/schema/wxs/serviceinstall/)
- [WiX `ServiceControl`](https://docs.firegiant.com/wix/schema/wxs/servicecontrol/)
- [Windows Installer ProductVersion](https://learn.microsoft.com/windows/win32/msi/productversion)
