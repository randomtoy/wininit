# wininit

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

`wininit` is an independent, open-source Windows initialization agent written in
Go. It is intended for Windows Server guests running on QEMU/KVM and OpenStack
and is being designed for NoCloud and OpenStack ConfigDrive metadata.

> [!IMPORTANT]
> The project is in an early provisioning milestone. It includes opt-in
> NoCloud and ConfigDrive datasources, hostname, local users, password policy,
> SSH keys, and a separate PowerShell user-data executable. All are disabled
> by default. PowerShell requires explicit privileged authorization and script/EXE
> SHA-256 pins. ConfigDrive can optionally run as a separate verified EXE. A running service must not be interpreted as a successfully
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
| OpenStack ConfigDrive local seed and mounted config-2 datasource | Implemented; opt-in |
| Standard local users, initial passwords, SAM policy, SSH keys | Implemented; opt-in |
| PowerShell user-data | Experimental separate EXE; opt-in, hash-approved, one-shot |
| Hostname restart checkpoint/reconciliation | Implemented; no automatic restart |
| External ConfigDrive and versioned Go SDK | Experimental v1; opt-in |

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
from implementations. The seven logical plugins are independently configured
and never enabled implicitly. ConfigDrive can select an external executable
using the experimental protobuf/gRPC SDK and HashiCorp go-plugin; the existing
built-in mode remains compatible with older configuration.

On each process start, the pipeline probes all enabled datasources and
reconciles each enabled provisioner once. Built-in constructors perform no I/O;
all discovery and mutation remain behind the enablement gate. The engine does
not poll or retry in a busy loop; a failure is attempted again only after a
later service/process start. PowerShell instead uses a durable one-shot journal:
failed or interrupted executions require a new execution ID. A machine-wide
`WininitPrivate\Provisioning` mutex in a protected Windows private namespace
prevents service and foreground instances from mutating the host concurrently,
even if they run in different sessions or use different Registry state paths.

Exactly one enabled datasource must be available. Multiple matching sources
fail before provisioning; an unavailable source permits fallback, while
malformed metadata stops the run. ConfigDrive reads bounded OpenStack JSON,
maps `uuid` to instance identity, and explicitly maps `hostname` FQDNs to their
first DNS label. It does not execute user-data or configure networking.

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
wininit-plugin-configdrive-windows-amd64.exe
wininit-plugin-powershell-windows-amd64.exe
SHA256SUMS
```

Verify `SHA256SUMS` before installing. MVP artifacts are not code-signed.
The MSI installs the agent. The optional plugins are separate artifacts; see
[external plugin setup](docs/external-plugins.md). A plugin is not discovered or
launched just because it is present.

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
omit plugin sections keep those plugins disabled. Every datasource and
provisioner has its own `enabled` flag, and current configuration disablement
always takes priority over old pending checkpoints. `reboot.policy` remains
`never`; the agent never restarts Windows automatically.

See [Configuration](docs/configuration.md) for the schema and default paths, and
[Built-in plugins](docs/plugins.md) for exact opt-in and disable examples.
Account creation/password policy/SSH setup is documented in
[Identity and access](docs/identity.md). Process mode and the same-repository
decision are documented in [External ConfigDrive](docs/external-plugins.md).
See [PowerShell user-data](docs/powershell-user-data.md) for its separate
installation, approval, execution journal, and retry policy.

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
The narrower [Windows Server 2025 guest review](tests/e2e/2026-11-06-pr4-review.md)
verified identity, real SSH login/revocation, external ConfigDrive local seeds,
SCM cancellation, and MSI installation/upgrade without restarting the guest.
The [PowerShell guest acceptance](tests/e2e/2026-11-06-powershell-user-data.md)
verified the separate LocalSystem plugin, bounded failures, replay protection,
and one administrator reboot/resume on Windows Server 2025.

## Security

The service may require LocalSystem privileges for provisioning work. NoCloud and ConfigDrive
media, metadata, configuration, and future external plugins must all be treated
as untrusted. NoCloud consumes `instance-id` and optional `local-hostname`;
ConfigDrive consumes `uuid` and optional `hostname`. The datasource implementations
never open user-data; the separately enabled PowerShell process may execute only
a hash-approved script from the selected source. External
ConfigDrive execution requires an explicitly selected reviewed image, pinned
checksum, version/identity negotiation, restricted process launch, deadlines,
cancellation, and output redaction. The [experimental process protocol](docs/external-plugins.md)
supports trusted ConfigDrive binaries; it is not a hostile-code sandbox.

Read [SECURITY.md](SECURITY.md) and [Security model](docs/security.md) before
working on state, diagnostics, metadata, user-data, or plugins.

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md) and
follow [CLAUDE.md](CLAUDE.md), which is the project contract for architecture,
state, packaging, security, and the definition of done.

## License

Licensed under the [Apache License 2.0](LICENSE).
