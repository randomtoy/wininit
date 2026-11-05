# Architecture

## Scope

The foundation establishes a Windows Service/CLI process, configuration,
lifecycle state, and diagnostics. It deliberately does not implement metadata
discovery or provisioning. The architecture keeps those future responsibilities
separate without committing to an unstable public interface.

## Component view

```text
Windows SCM / command-line user
              |
        cmd/wininit
              |
        internal/app -------------- internal/diagnostics
              |
      internal/lifecycle ---------- internal/config
              |
         StateStore
          /      \
Windows Registry  in-memory test store
```

Future datasource, provisioner, and external-plugin managers will sit behind
the application/lifecycle boundary. They are not present in the foundation and
must not be inferred from the diagram.

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

The foundation lifecycle engine tracks truthful agent state and errors. It
does not mark provisioning complete because there are no datasources or
provisioners. Lifecycle transitions are explicit and validated so that future
reboot/resume behavior can extend them without accepting impossible states.

## Configuration

Configuration is a versioned operator-facing document. Loading requires an
explicit schema version, applies safe defaults to optional fields, rejects
unknown fields or invalid schema versions, and validates values before the
service starts. The installed default does not enable unimplemented
capabilities. Administrator-owned configuration is preserved through upgrades.

See [Configuration](configuration.md).

## Persistent state

The production `StateStore` is backed by the Windows Registry. The lifecycle
model includes a schema version, phase, instance identifier when known,
attempt/reboot counters, future resume information, timestamps, and sanitized
error metadata. It never includes passwords, tokens, private keys, or raw
metadata/user-data.

Registry updates do not have database-style transactions. A save therefore
writes a complete serialized state to an inactive versioned checkpoint,
validates that checkpoint, and only then changes the active checkpoint marker.
Loading validates the active checkpoint and may recover from the most recent
complete prior checkpoint. The store, rather than its callers, assigns a
monotonically increasing generation to each save. Invalid data results in a
visible error rather than an invented initial or successful state.

An in-memory implementation supports deterministic unit tests and is not used
as silent production fallback.

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

- **Datasource manager:** discovers candidates, selects one deterministically,
  and returns validated metadata through an internal model.
- **Provisioner manager:** executes idempotent operations with durable
  checkpoints and explicit reboot decisions.
- **External plugin manager:** launches verified, version-compatible processes
  with deadlines, cancellation, controlled logs, and deliberate privileges.
- **Plugin protocol and Go SDK:** versioned protobuf/gRPC contracts exposed only
  after the threat model and compatibility policy are accepted.

These descriptions are design direction, not an implemented compatibility
promise.

## Related decisions

See the [ADR index](adr/README.md) for the decisions behind state,
configuration, service hosting, packaging, plugins, and testing.
