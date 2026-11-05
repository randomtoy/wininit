# wininit

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

`wininit` is an independent, open-source Windows initialization agent written in
Go. It is intended for Windows Server guests running on QEMU/KVM and OpenStack
and is being designed for NoCloud and OpenStack ConfigDrive metadata.

> [!IMPORTANT]
> The project is in an early provisioning milestone. It includes an opt-in,
> built-in NoCloud datasource and an independently opt-in Windows hostname
> provisioner. Both are disabled by default. User-data is never opened or
> executed. A running service must not be interpreted as a successfully
> provisioned instance.

`wininit` is an independent design, not a line-by-line port or derivative
implementation of Cloudbase-Init.

## Current status

| Capability | Status |
| --- | --- |
| Single Windows Service and CLI executable | Foundation |
| Foreground execution and graceful cancellation | Foundation |
| Versioned configuration with validation | Foundation |
| Persistent lifecycle state in the Windows Registry | Foundation |
| Status reporting and sanitized diagnostics | Foundation |
| Windows AMD64 EXE and MSI packaging | Foundation |
| NoCloud seed directory and mounted CIDATA volume datasource | Implemented; opt-in |
| Conservative Windows hostname provisioning | Implemented; opt-in |
| OpenStack ConfigDrive datasource | Planned |
| User, password, and SSH key provisioning | Planned |
| PowerShell user-data | Planned |
| Hostname restart checkpoint/reconciliation | Implemented; no automatic restart |
| External executable plugins and public Go SDK | Planned |

See [MVP scope](docs/mvp.md), [compatibility](docs/compatibility.md), and the
[roadmap](docs/roadmap.md) for the exact boundary between implemented and
planned work.

## Architecture

The executable runs in one of three modes: a Windows Service managed by the
Service Control Manager (SCM), a foreground process for development, or a
short-lived CLI command. Application startup loads validated configuration,
opens a state store, and starts a cancellation-aware lifecycle pipeline. On
Windows, production state is kept under versioned Registry checkpoints; tests
use an in-memory store.

Small internal datasource and provisioner interfaces separate the pipeline
from built-in implementations. The NoCloud datasource and hostname provisioner
are compiled into the executable, independently configured, and never enabled
implicitly. The external process/gRPC protocol remains deferred; no public
plugin API is exposed by this milestone.

On each process start, the pipeline invokes the enabled datasource and
reconciles each enabled provisioner once. Built-in constructors perform no I/O;
all discovery and mutation remain behind the enablement gate. The engine does
not poll or retry in a busy loop; a failure is attempted again only after a
later service/process start. A machine-wide
`WininitPrivate\Provisioning` mutex in a protected Windows private namespace
prevents service and foreground instances from mutating the host concurrently,
even if they run in different sessions or use different Registry state paths.

See [Architecture](docs/architecture.md) and the
[architecture decision records](docs/adr/README.md).

## Supported environments

The product targets Windows Server 2022 and Windows Server 2025 on AMD64.
Windows ARM64 is a future compatibility goal. CI on a GitHub-hosted Windows
runner validates native builds, Registry behavior, service administration when
the runner is elevated, and MSI packaging. That runner is not a substitute for
real Windows Server guest testing.

See [Compatibility](docs/compatibility.md) for verified and unverified
combinations.

## Installation

Release artifacts are expected to contain:

```text
wininit-windows-amd64.exe
wininit-windows-amd64.msi
SHA256SUMS
```

Verify `SHA256SUMS` before installing. MVP artifacts are not code-signed.

For an interactive installation, open the MSI from an elevated Windows
session. For unattended installation with a verbose log:

```powershell
msiexec.exe /i .\wininit-windows-amd64.msi /qn /norestart /l*v .\wininit-install.log
```

The MSI installs the executable under Program Files, installs default
configuration without replacing administrator-owned configuration during an
upgrade, prepares writable data directories under ProgramData, and registers
the service with the SCM. MSI-native service actions are used so that failures
participate in installer rollback.

### Uninstallation

Use Apps & Features or run:

```powershell
msiexec.exe /x .\wininit-windows-amd64.msi /qn /norestart /l*v .\wininit-uninstall.log
```

Ordinary uninstall removes the program and service registration but preserves
operational state and administrator-owned data. This prevents an uninstall,
upgrade, or rollback from erasing lifecycle evidence. Administrators who need
to purge preserved data must first uninstall and then deliberately remove the
documented ProgramData directory and `HKLM\SOFTWARE\wininit` Registry tree.
Back up anything needed for incident analysis before purging.

Do not use `wininit service uninstall` on an MSI-managed installation. Let
Windows Installer own service registration for the lifetime of that product.
The direct service commands exist for development and manually deployed
executables.

## CLI

```text
wininit version
wininit run
wininit status
wininit status --json
wininit validate
wininit collect-logs
wininit service install
wininit service uninstall
wininit service start
wininit service stop
```

Run `wininit help` or `wininit <command> --help` for command-specific options.
Commands return a non-zero exit status when validation or administration fails.
`status` distinguishes SCM service state, lifecycle state, and each plugin's
enabled, runtime, or disabled state. Each plugin descriptor includes `name`,
`kind`, `enabled`, and `status`. Disabled plugins are not reported as
successfully executed, and a pending hostname restart is not reported as
completed provisioning. Saved runtime results are used only when they match the
current plugin configuration hash.

`collect-logs` creates an archive containing available logs, status, build
information, and a sanitized configuration snapshot. Passwords, tokens,
private keys, and raw user-data must never appear in a diagnostic archive.

## Configuration

Configuration has an explicit schema version and is rejected when unsupported
or invalid. Schema version 1 remains backward compatible: configurations which
omit the new built-in plugin sections keep both plugins disabled. NoCloud and
hostname have separate `enabled` flags, and current configuration disablement
always takes priority over old pending checkpoints. `reboot.policy` remains
`never`; the agent never restarts Windows automatically.

See [Configuration](docs/configuration.md) for the schema and default paths, and
[Built-in plugins](docs/plugins.md) for exact opt-in and disable examples.

## Build

Prerequisites:

- Go 1.27.3; `go.mod` is authoritative if this documentation falls behind.
- GNU Make for the convenience targets.
- WiX Toolset 7.0.1 through the repository-pinned .NET SDK project on Windows
  for MSI creation.

```bash
make build       # Windows AMD64 executable
make test        # unit tests
make lint        # formatting and static analysis
make package     # executable, MSI, and checksums on Windows
```

The executable can be cross-compiled from a non-Windows host. MSI construction
and MSI/service smoke tests require Windows. Builds embed version, commit, and
build metadata through Go linker flags. UPX is not used.

The WiX project records the required WiX 7 EULA acknowledgement with
`<AcceptEula>wix7</AcceptEula>`. Building or redistributing the installer does
not transfer compliance responsibility: builders and redistributors must review
and comply with the current WiX Open Source Maintenance Fee (OSMF) terms.

For commands without Make, local testing, and Windows integration setup, see
[Development](docs/development.md).

## Testing

The repository uses:

- platform-independent unit tests for configuration, lifecycle, plugin
  selection, metadata parsing, hostname validation, state migration/recovery,
  and version information;
- Windows tests using dedicated, isolated Registry keys;
- native Windows service and MSI smoke tests where elevation is available; and
- cross-compilation checks for the Windows AMD64 executable.

Tests must clean up resources they create. They must never use the production
Registry path unless an explicitly isolated integration scenario requires it.
Real QEMU/KVM Windows Server 2022 and 2025 NoCloud, rename, restart, and resume
end-to-end validation is still pending.

## Security

The service may require LocalSystem privileges for provisioning work. NoCloud
media, metadata, configuration, and future external plugins must all be treated
as untrusted. The current datasource consumes only required `instance-id` and
optional `local-hostname`; it never opens or executes `user-data`. External
plugin execution is intentionally absent until identity, checksum, protocol
negotiation, least-privilege execution, deadlines, cancellation, and
secret-redaction controls are designed and reviewed.

Read [SECURITY.md](SECURITY.md) and [Security model](docs/security.md) before
working on state, diagnostics, metadata, user-data, or plugins.

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md) and
follow [CLAUDE.md](CLAUDE.md), which is the project contract for architecture,
state, packaging, security, and the definition of done.

## License

Licensed under the [Apache License 2.0](LICENSE).
