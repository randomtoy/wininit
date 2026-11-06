# Architecture

## Scope

The agent provides a Windows Service/CLI process, configuration, lifecycle
state, diagnostics, and an internal opt-in plugin pipeline. The current pipeline
contains NoCloud/ConfigDrive datasources and hostname, password policy, local
user, and authorized-key provisioners. ConfigDrive may explicitly run in a
separate process using the experimental datasource v1 SDK.

## Component view

```text
Windows SCM / command-line user
              |
        cmd/wininit
              |
        internal/app -------------- internal/diagnostics
              |
      internal/lifecycle ---------- internal/config
         /         \
 internal/plugin    StateStore
   |                 /      \
   +-- NoCloud      Registry  memory tests
   +-- ConfigDrive
   +-- hostname
   +-- password_policy / local_users / authorized_keys
   +-- ConfigDrive process adapter -- mTLS/gRPC -- separate ConfigDrive EXE
```

Internal interfaces remain private to lifecycle composition. The experimental
wire surface lives separately in `sdk/datasource/v1` and does not expose
Registry access or provisioner operations. The first plugin stays in this
repository with a separate executable artifact; [ADR 0012](adr/0012-process-configdrive.md)
records the repository and trusted-process boundary.

## Process modes

The same executable supports:

1. **SCM service mode.** The process reports SCM state changes, starts the
   lifecycle engine, accepts stop and shutdown controls, and propagates
   cancellation.
2. **Foreground mode.** `wininit run` uses the same application lifecycle while
   handling console interruption for development and diagnosis.
3. **One-shot CLI mode.** Status, validation, diagnostics, version, and service
   administration commands perform a bounded operation and return an
   appropriate exit code.

MSI-installed service registration is owned by Windows Installer. Direct CLI
service install/uninstall commands are intended only for development and
manual deployments.

## Application and lifecycle

The application is the composition root. It loads and validates configuration,
constructs the platform-appropriate state store, loads existing lifecycle
state, and runs the lifecycle engine until completion or cancellation.

The lifecycle engine tracks truthful agent state, per-plugin outcomes, and
errors. The catalog constructs descriptors/objects for every known built-in,
but constructors perform no I/O. The pipeline calls only plugins enabled by the
current configuration. Datasource and provisioner enablement are independent.
A disabled plugin is reported as disabled, not as executed or successful, and
current configuration always overrides an old pending checkpoint.

The service and foreground command acquire `WininitPrivate\Provisioning`, a
mutex in a protected Windows private namespace, before running the engine. The
namespace boundary contains the Builtin Administrators SID and deliberately no
session SID, so the same object coordinates elevated callers across sessions.
A protected DACL grants namespace and mutex access only to SYSTEM and Builtin
Administrators. This prevents two processes from mutating the machine
concurrently even when their configurations select different Registry paths.
A competing process fails visibly rather than waiting or running a hidden retry
loop.

Windows mutex ownership belongs to the acquiring thread. A dedicated worker
goroutine pins itself to an OS thread for wait, ownership, and release. An
abandoned mutex is accepted as acquired because durable state reconciliation is
the recovery mechanism. The public release operation is idempotent and may be
called from another goroutine; it signals the owner thread to release and clean
up the mutex handle. The protected namespace handle is retained for the process
lifetime.

The datasource stage probes all enabled sources before provisioners. NoCloud
maps `instance-id` and optional `local-hostname`; ConfigDrive maps `uuid` and
optional `hostname` into the same internal metadata model. Exactly one source
must succeed: multiple available sources fail before mutation, and only
unavailable sources permit fallback. Malformed input never triggers fallback.
If enabled, the hostname provisioner consumes that
model, validates the target, compares the active computer name, and either does
nothing idempotently, refuses the mutation, or requests the Windows rename.
This reconciliation runs once per process start. A failure is retried only on a
later service/process start; the lifecycle engine does not automatically retry
or poll.

Enabled provisioners run in password-policy, local-user, SSH-key, hostname
order. Identity inputs are administrator-owned configuration, not metadata.
Adapters perform native Windows calls and protected handle-relative file
updates. A mutation is preceded by a durable running checkpoint. Input hashes
include the configuration fingerprint as well as instance/metadata identity.
See [identity](identity.md) and [external process lifecycle](external-plugins.md).

## Configuration

Configuration is a versioned operator-facing document. Loading requires an
explicit schema version, applies safe defaults to optional fields, rejects
unknown fields or invalid schema versions, and validates values before the
service starts. The installed default does not enable unimplemented
capabilities. Administrator-owned configuration is preserved through upgrades.

See [Configuration](configuration.md).

## Persistent state

The production `StateStore` is backed by the Windows Registry. State schema
version 2 includes lifecycle phase, instance identifier, plugin runtime state,
attempt/reboot counters, restart/checkpoint information, timestamps, and
sanitized error metadata. It never includes passwords, tokens, private keys, or
raw metadata/user-data. A valid schema version 1 checkpoint is migrated to
schema version 2 without interpreting absent plugin state as success or
enablement.

Registry updates do not have database-style transactions. A save therefore
writes a complete serialized state to an inactive versioned checkpoint,
validates that checkpoint, and only then changes the active checkpoint marker.
On Windows it calls `RegFlushKey` on the committed key before returning. This
ensures that the checkpoint saved before a platform mutation reached persistent
Registry storage, rather than only the lazy-flush cache. The call is expensive
and can flush other changes in the hive, so saves occur only at low-frequency
lifecycle/plugin boundaries.
Loading validates the active checkpoint and may recover from the most recent
complete prior checkpoint. The store, rather than its callers, assigns a
monotonically increasing generation to each save. Invalid data results in a
visible error rather than an invented initial or successful state.
An unsupported future schema in any checkpoint slot fails closed, even when an
older understood slot is marked active. Loading does not choose the older
checkpoint, and saving does not overwrite future state as though it were
absent.

An in-memory implementation supports deterministic unit tests and is not used
as silent production fallback.

## Built-in plugin pipeline

Small internal `Datasource` and `Provisioner` contracts are defined for the
application's needs. Implementations live separately under
`plugins/datasource/nocloud`, `plugins/datasource/configdrive`, and
`plugins/provisioner/hostname`. They are
compiled into `wininit.exe`; `plugins.directory` is not scanned or executed.
The pipeline itself lives in `internal/lifecycle`, while `internal/plugin`
contains contracts and catalog types.

NoCloud reads either an explicitly configured seed directory or exactly one
eligible mounted `CIDATA` ISO9660/FAT volume. More than one candidate is an
error, not a precedence choice. Query failures and unsupported filesystems on
unrelated local drives are skipped rather than making them candidates. The
discovery checkpoint remains pending until every enabled datasource candidate
has returned unavailable. Only `instance-id` and `local-hostname` are in scope.
`user-data` is never opened or run.

ConfigDrive reads one `openstack/<version>/meta_data.json` from a local seed or
mounted FAT/ISO9660 `config-2` volume. It prefers the newest supported dated
directory, with `latest` as fallback only if no supported date is present.
The parser accepts bounded UTF-8 JSON, rejects duplicate keys and excessive
nesting, and extracts only instance identity and hostname. An OpenStack FQDN
is explicitly normalized to its lowercase first DNS label; Windows-specific
validation remains in the hostname provisioner. No user-data, password,
network/vendor metadata, EC2 layout, or injected file is consumed.

Both datasources share read-only mounted-volume discovery through
`internal/platform/seedmedia`. Media labels remain separate, and both plugin
constructors perform no I/O. See [ADR 0010](adr/0010-openstack-configdrive.md)
for version selection, normalization, and conflict behavior.

Hostname validation accepts only a conservative 1–15 character ASCII label
which is not all numeric. The provisioner refuses domain-joined hosts. Windows
rename may require a restart, but `reboot.policy` remains `never`: the engine
persists `reboot_required` and waits for an administrator to restart the guest.
A later enabled run must rediscover the seed and compares the active/configured
computer names with fresh metadata. Persistent state stores the normalized
input SHA-256 only, not the raw target hostname. The seed must therefore remain
accessible through restart. Changed metadata is reconciled as new input; an
unavailable seed reports `no_datasource` rather than replaying old input.

See [Built-in plugins](plugins.md) and [ADR 0009](adr/0009-opt-in-built-in-provisioning-plugins.md).

## Windows service behavior

SCM integration reports start-pending, running, stop-pending, and stopped
states as appropriate. Stop and system shutdown controls cancel the root
context. The service waits on cancellation or owned lifecycle work; it does not
use a busy loop. Startup and shutdown failures are logged and propagated to the
SCM where the platform API permits.

An error is treated as a clean shutdown cancellation only when its cause is the
root context cancellation. A plugin, platform, or state failure is not hidden
merely because cancellation happened concurrently.

## Logging and diagnostics

Logs are structured and use an operator-configured level. Sensitive or
untrusted payloads must not be logged. Diagnostics build an archive from an
allowlist of available logs, embedded build information, current service and
lifecycle status, and sanitized configuration. The implementation must protect
against path traversal and must not recursively archive arbitrary data
directories.

## Planned component boundaries

- **External plugin manager:** launches verified, version-compatible processes
  with deadlines, cancellation, controlled logs, and deliberate privileges.
- **Plugin protocol and Go SDK:** versioned protobuf/gRPC contracts exposed only
  after the threat model and compatibility policy are accepted.

These descriptions are design direction, not an implemented compatibility
promise.

## Related decisions

See the [ADR index](adr/README.md) for the decisions behind state,
configuration, service hosting, packaging, plugins, and testing.
