# External process plugins

This page details the original ConfigDrive role. Named datasource registration,
separate NoCloud/hostname EXEs and provisioner v1 are described in the
[metadata SDK](external-sdk.md). PowerShell retains its separate userdata API.

The first process plugin is ConfigDrive, built as
`wininit-plugin-configdrive-windows-amd64.exe`. It uses HashiCorp `go-plugin`
with a versioned protobuf/gRPC service and local mTLS. It is independently
distributed and replaceable without rebuilding the agent when protocol v1
remains compatible. It is a wininit plugin, not a Terraform provider.

## Repository decision

Keep the first plugin and experimental SDK in this repository. Shared review,
fixtures, native CI, and protocol changes can be checked atomically while the
contract matures. The executable is a separate build/release artifact; it is
not loaded as a DLL or Go shared object.

The existing built-in ConfigDrive remains the default execution mode for
version 1 configuration compatibility. Selecting `execution: external` makes
the agent use the process adapter exclusively, with no fallback to built-in
code after an external failure. The logical plugin retains its own
`enabled: false` default and one status descriptor.

After protocol stabilization, independent maintainers/release cadence or
different dependencies can justify a separate repository/module. That move
should retain protocol conformance tests and pinned checksums. A plugin
registry, automatic downloads, separate repository creation, and automatic
updates are not implemented.

## Build and configuration

`make build` and `scripts/build.ps1` build the agent and all separate plugins. The MSI
installs the agent; the plugin is a separate release EXE with an entry in
`SHA256SUMS`. Install a reviewed copy in a dedicated administrator-controlled
directory, for example `C:\Program Files\WinInit\plugins`. Do not use a writable
user directory. `plugins.directory` is still reserved and is never scanned.

```yaml
version: 1
plugins:
  datasources:
    configdrive:
      enabled: true
      execution: external
      seed_directory: 'D:\'
      discover_volumes: false
      process:
        executable: 'C:\Program Files\WinInit\plugins\wininit-plugin-configdrive.exe'
        sha256: 'REPLACE_WITH_THE_REVIEWED_BINARY_SHA256'
        timeout_seconds: 15
```

Replace the placeholder with the exact lowercase or uppercase 64-digit SHA-256
of the trusted binary, independently checking the published checksum:

```powershell
Get-FileHash -Algorithm SHA256 -LiteralPath 'C:\Program Files\WinInit\plugins\wininit-plugin-configdrive.exe'
```

An image and containing directory must have a SYSTEM/Administrators owner and
no other write/delete/ACL/ownership grants. The host pins the verified image
and every path ancestor against replacement or rename until process cleanup,
so the subsequent Win32 launch uses that verified path. The restricted child needs ordinary
Users read/execute access to the executable and read access to the selected
seed. Protected password files must remain unreadable to Users. Use SID-based
ACLs, including `/setowner '*S-1-5-32-544'` for manually copied images; inherited
administrator membership alone does not satisfy the explicit owner check.
For a new dedicated plugin directory and reviewed image:

```powershell
icacls.exe 'C:\Program Files\WinInit\plugins' /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)' '*S-1-5-32-545:(OI)(CI)(RX)' /setowner '*S-1-5-32-544'
icacls.exe 'C:\Program Files\WinInit\plugins\wininit-plugin-configdrive.exe' /inheritance:r /grant:r '*S-1-5-18:(F)' '*S-1-5-32-544:(F)' '*S-1-5-32-545:(RX)' /setowner '*S-1-5-32-544'
```

Remove any pre-existing explicit write grants to other principals. DOS-device,
stream, relative, and trailing dot/space aliases are rejected to keep native
image verification and Win32 process creation on the same file.

The parent opens the image without reparse traversal, rejects hard links,
validates ACLs and SHA-256, and holds the verified image against write/delete
until process cleanup. An absent image, mismatch, unsupported protocol,
incompatible identity, malformed response, crash, or timeout is a hard
datasource failure. Only a valid unavailable response permits fallback.
Disabled ConfigDrive never inspects, hashes, or launches an executable.

## Protocol and lifecycle

`sdk/datasource/v1/datasource.proto` defines `Describe` and `Discover`.
Framework handshake version, service version, plugin name `configdrive`, and
kind `datasource` must match. net/rpc fallback, reattachment, executable
arguments, and arbitrary plugin discovery are disabled.

The RPC carries only source-selection settings and normalized instance ID /
local hostname, never Registry handles, password paths, key lists, user-data,
or provisioning requests. Responses are bounded to 128 KiB and revalidated by
the host. Unknown versions fail closed. Within v1, compatible changes may add
optional fields; changed meanings or required behavior need a new protocol
version. The SDK remains experimental. Named NoCloud datasource and metadata-only
provisioner processes are now supported through explicit host configuration;
see [metadata SDK](external-sdk.md). Arbitrary plugin registration remains future work.

The configured 1–60 second deadline covers startup, identity negotiation,
discovery, and RPCs, including framework calls. Cancellation kills the process.
The host owns this deadline and propagates RPC cancellation rather than a
second rounded remote timeout. The SDK additionally caps provider calls at
60 seconds. A provider's early deadline error remains a discovery failure.
Each discovery starts a fresh process and cleanup runs on success and failure;
the agent does not keep a plugin daemon or retry automatically. Plugin stdout,
stderr, and remote error text are discarded or mapped to fixed host errors,
so they do not enter logs, state, or diagnostics. The handshake cookie prevents
accidental manual execution; it is not authentication. Binary verification and
local mTLS provide the deliberate identity/channel checks.

## Windows restrictions and trust

The child uses a restricted primary token: Administrators is deny-only, and
Users, Authenticated Users, and Everyone form a restricting SID list which
requires a second ACL access check. Caller identity is retained for Windows
initialization, but SYSTEM/Admin-only files fail that second check. Privileges
are disabled except traversal and integrity is low. Each launch also has a
random private restricting SID. Its default object DACL grants that SID access
so runtime-created objects pass both checks without granting ordinary users
control over them. It does not inherit the
host environment or input; only
Windows system-directory settings and protocol bootstrap values are supplied.

It is created suspended, assigned to a Job Object, then resumed. The job kills
members on close, limits active processes to one, limits process memory to
256 MiB, and starts without a window. Job UI limits are omitted because Windows
prohibits nesting such jobs inside a host's existing job. Thus a plugin cannot
create an uncontained child before job assignment. The runtime enforces local
loopback control endpoints, RPC size/deadline limits, and cleanup.

Native `CreateProcessAsUser` explicitly selects a fresh noninteractive,
low-integrity window station and desktop. They are separate from host UI and
are closed with the process. The headless parent serializes and restores a
temporary window-station switch while creating those objects on a dedicated
native thread. That thread exits and is joined before child creation because
creating a desktop attaches its calling thread. Only a null
stdin and stdout/stderr pipe handles are inherited through an explicit handle
allowlist; token, job, image, and other host handles are not inherited.

These restrictions are defense in depth for explicitly trusted executable
code, not an AppContainer or an untrusted-code sandbox. Readable filesystem
data and outbound networking are not completely isolated by the OS token.
The reviewed ConfigDrive implementation reads only bounded local metadata
through its confined reader and performs no network requests. Never pin and
enable an arbitrary unreviewed executable. Administrators who require hostile
plugin isolation need an additional sandbox; this protocol does not promise
one. The credential/network policy is intentionally documented rather than
implied by gRPC or a checksum.

Portable process tests use caller credentials and a separate process group for
development only. They do not claim Windows isolation. Native Windows tests
verify restricting SIDs, deny-only Administrators, low integrity, a separate
window station, privilege removal, blocked host-secret
reads/writes, and denied process creation in the job. Other tests execute a
real plugin for valid metadata, version/identity mismatch, malformed output,
crash, environment isolation, deadlines, cancellation, and error redaction.

See [ADR 0012](adr/0012-process-configdrive.md) and
[go-plugin](https://github.com/hashicorp/go-plugin).

PowerShell uses a separate privileged process role and experimental userdata v1
SDK, not the read-only ConfigDrive role described above. Its separate artifact
shares this repository and release manifest; see [PowerShell policy](powershell-user-data.md)
and ADR 0013 for approval, two-phase execution, journal and audit boundaries.
