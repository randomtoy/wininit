# ADR 0002: Store lifecycle state in the Windows Registry

- Status: Accepted
- Date: 2026-11-05

## Context

The agent needs durable lifecycle and resume state before services and user
profiles are fully available. Adding an embedded database increases packaging,
recovery, patching, and ACL complexity. Registry writes are not equivalent to a
multi-row database transaction.

## Decision

Use HKLM Registry state behind a small internal `StateStore` interface. Store a
versioned serialized checkpoint in an inactive slot, validate it, then replace
the active-slot marker. On Windows, call `RegFlushKey` after activating the
checkpoint and before returning from `Save`, so a platform mutation never begins
against a checkpoint which exists only in the Registry cache. Retain a prior
valid slot for recovery and let the store assign each monotonically increasing
checkpoint generation. Reject unknown schemas and invalid state. Use an
in-memory implementation for unit tests.

State schema version 2 adds non-sensitive built-in plugin status, configuration
identity, and restart-required checkpoints. A valid schema version 1 checkpoint
is migrated to version 2 with provisioning disabled; migration never invents a
completed plugin result.

Never store passwords, tokens, private keys, or raw metadata/user-data, and
never expose Registry access directly to external plugins.

## Consequences

The installation remains dependency-light and administrators can inspect state
with standard Windows tools. The implementation must test partial writes,
invalid active markers, schema incompatibility, flush failure, and recovery.
`RegFlushKey` consumes system-wide hive resources, so state saves remain
low-frequency lifecycle boundaries rather than a periodic loop. Operational
state is deliberately preserved during ordinary upgrade and uninstall.

## Alternatives considered

- SQLite: rejected by project scope and unnecessary for the small state model.
- One mutable Registry value: rejected because an interrupted replacement
  offers less diagnosable recovery.
- Files under ProgramData: rejected for production lifecycle state because the
  Registry provides a more natural Windows machine-state boundary here.

## References

- [Go Registry package](https://pkg.go.dev/golang.org/x/sys/windows/registry)
- [Registry value limits](https://learn.microsoft.com/windows/win32/sysinfo/registry-element-size-limits)
- [Microsoft `RegFlushKey`](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-regflushkey)
