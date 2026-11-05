# Architecture

## Scope

The agent provides a Windows Service/CLI process, configuration, lifecycle
state, diagnostics, and an internal opt-in plugin pipeline. The current pipeline
contains only a narrow NoCloud datasource and Windows hostname provisioner. The
architecture separates those responsibilities without committing to an
external or public plugin interface.

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
    /       \        /      \
NoCloud   hostname  Registry  memory
 datasource provisioner       tests
```

NoCloud and hostname are built-in implementation packages, not external
processes. The external executable manager and protobuf/gRPC protocol remain
future components and must not be inferred from the diagram.

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

The service and foreground command acquire the machine-wide named lifecycle
lock `Global\WininitProvisioning` before running the engine. This prevents two
processes from mutating the machine concurrently even when their configurations
select different Registry paths. A competing process fails visibly rather than
waiting or running a hidden retry loop.

The datasource stage runs before provisioners. The built-in NoCloud datasource
returns a small internal metadata model containing required `instance-id` and
optional `local-hostname`. If enabled, the hostname provisioner consumes that
model, validates the target, compares the active computer name, and either does
nothing idempotently, refuses the mutation, or requests the Windows rename.
This reconciliation runs once per process start. A failure is retried only on a
later service/process start; the lifecycle engine does not automatically retry
or poll.

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

An in-memory implementation supports deterministic unit tests and is not used
as silent production fallback.

## Built-in plugin pipeline

Small internal `Datasource` and `Provisioner` contracts are defined for the
application's needs. Implementations live separately under
`plugins/datasource/nocloud` and `plugins/provisioner/hostname`. They are
compiled into `wininit.exe`; `plugins.directory` is not scanned or executed.
The pipeline itself lives in `internal/lifecycle`, while `internal/plugin`
contains contracts and catalog types.

NoCloud reads either an explicitly configured seed directory or exactly one
eligible mounted `CIDATA` ISO9660/FAT volume. More than one candidate is an
error, not a precedence choice. Only `instance-id` and `local-hostname` are in
scope. `user-data` is never opened or run.

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
