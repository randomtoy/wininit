# ADR 0009: Independently gated built-in provisioning plugins

- Status: Accepted
- Date: 2026-11-06

## Context

The first provisioning milestone needs to read a narrowly defined NoCloud data
source and apply a Windows hostname. Datasource selection and host mutation are
different trust and failure boundaries. Operators must be able to deploy a new
binary without enabling either behavior, and must be able to disable one while
leaving the other configured.

The external process and protobuf/gRPC plugin design remains intentionally
deferred. Stabilizing that public protocol for two built-in implementations
would create a premature compatibility and privilege boundary.

## Decision

Define small internal `Datasource` and `Provisioner` interfaces. Keep their
built-in implementations in separate packages:

```text
plugins/datasource/nocloud
plugins/provisioner/hostname
```

The application catalog constructs every known built-in through constructors
which perform no I/O. The lifecycle pipeline invokes only plugins explicitly
enabled in configuration. The NoCloud datasource and hostname provisioner have
separate `enabled` flags, both defaulting to `false`. Missing plugin fields in
an existing version 1 configuration mean disabled. Configuration disablement
always takes priority over durable work or reboot checkpoints created by an
older run.

Keep configuration schema version 1 and add backward-compatible optional
fields. Migrate lifecycle state schema version 1 to schema version 2 before
using the new plugin runtime and checkpoint fields.

The built-in NoCloud implementation accepts only `instance-id` (required) and
`local-hostname` (optional). It reads from either one explicit seed directory or
one unambiguous, already-mounted Windows volume labelled `CIDATA` with an
ISO9660 or FAT filesystem. It never opens or executes `user-data`.

The hostname provisioner accepts only a conservative 1–15 character ASCII host
label, rejects an all-numeric name, and refuses to rename a domain-joined host.
It is idempotent against the active Windows computer name. The only supported
reboot policy remains `never`: a successful rename which requires restart is
recorded as a durable `reboot_required` checkpoint. The agent never restarts the
machine. After an administrator restarts it, a later enabled run rediscovers
fresh metadata and reconciles the active and configured Windows names.

Status reports each known plugin's configured state and runtime outcome rather
than treating a disabled plugin as successful provisioning.

Run discovery and reconciliation once per process start. Do not use an
automatic retry loop; a failed operation is attempted again only after a later
process start. Gate saved runtime results with a hash of current plugin
configuration.

Do not persist raw metadata or the target hostname. Persist only a normalized
input SHA-256. Reboot reconciliation therefore requires the seed to remain
available: the next run rediscovers fresh metadata and compares it with active
and configured Windows names. Changed metadata is new input; an unavailable
seed produces `no_datasource` rather than replaying an old target.

Acquire the machine-wide named lock `Global\WininitProvisioning` for both SCM
and foreground lifecycles. The lock prevents concurrent mutations regardless
of the configured Registry state path.

## Consequences

Installing or upgrading the agent cannot silently activate metadata discovery
or a hostname change. Operators can stop future plugin work through
configuration even when state contains an older pending checkpoint. Datasource
and provisioner tests can use their internal interfaces without publishing a
cross-process API.

Volume discovery must fail when more than one eligible `CIDATA` volume exists;
the agent must not guess. An explicit seed directory avoids discovery
ambiguity. A hostname accepted for rename may still be pending inside Windows;
disabling the plugin prevents agent execution and reconciliation but is not a
promise to undo a rename already accepted by the operating system.

State migration and reboot reconciliation add durable compatibility behavior
which requires unit, native Windows, and eventually real-VM tests. Real Windows
Server 2022/2025 reboot/resume validation remains pending.

## Alternatives considered

- Enable the built-ins by default: rejected because upgrade must not create new
  machine mutations.
- One global plugin switch: rejected because datasource access and hostname
  mutation require independent operator control.
- Run a pending checkpoint even after configuration disablement: rejected
  because the current explicit safety choice must win over stale intent.
- Use the future external process protocol now: rejected because its security
  and compatibility design is not stable.
- Pick the first `CIDATA` volume: rejected because enumeration order is not a
  safe datasource-selection rule.
- Persist the raw hostname for replay after restart: rejected because the seed
  must remain the source of truth and state should not retain raw metadata.
- Retry inside the long-running service: rejected because it creates hidden
  mutation timing and risks a busy loop; restart is the explicit retry boundary.

## References

- [cloud-init NoCloud datasource](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html)
- [Microsoft `SetComputerNameExW`](https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-setcomputernameexw)
