# Built-in plugins

## Safety model

`wininit` includes a small internal plugin pipeline for built-in datasources and
provisioners. These are ordinary packages compiled into `wininit.exe`; they are
not external executables, do not use gRPC, and are not a public plugin SDK.

Every plugin is independently opt-in. Both built-ins are disabled by default,
including for configuration files created before these fields existed. The
pipeline executes enabled plugins only. Disabling a plugin in the current
configuration takes priority over any pending plugin or reboot checkpoint in
persistent state.

The pipeline runs once when the service or foreground process starts. It
rediscovers metadata and reconciles every enabled provisioner on that start. It
does not automatically retry or poll; a failed operation receives at most one
attempt on each later process start.

## Configuration examples

The complete default plugin configuration is inert:

```yaml
version: 1
plugins:
  directory: 'C:\ProgramData\WinInit\plugins'
  datasources:
    nocloud:
      enabled: false
      seed_directory: ''
      discover_volumes: true
  provisioners:
    hostname:
      enabled: false
```

The `plugins.directory` field is reserved for the deferred external plugin
design. Setting that path does not enable or load an executable.

### Use an explicit seed directory and hostname provisioning

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: true
      seed_directory: 'D:\nocloud'
      discover_volumes: false
  provisioners:
    hostname:
      enabled: true
```

When `seed_directory` is non-empty, it is the selected source. Set
`discover_volumes: false` to make that intent explicit and prevent fallback to
volume enumeration. The directory must be an absolute local path; UNC paths and
URLs are rejected.

### Discover an already-mounted CIDATA volume

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: true
      seed_directory: ''
      discover_volumes: true
  provisioners:
    hostname:
      enabled: true
```

Discovery considers mounted Windows volumes labelled `CIDATA` whose filesystem
is ISO9660 or FAT. Exactly one eligible volume must exist. No match is an
unavailable-datasource error; multiple matches are an ambiguity error. The
agent never chooses the first enumerated candidate.

### Disable all provisioning behavior

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: false
  provisioners:
    hostname:
      enabled: false
```

After validating the file, restart the service so the next run uses it. A
disabled plugin performs no discovery, metadata read, Windows rename, or
checkpoint reconciliation. Disabling does not necessarily cancel a rename
which Windows already accepted and has queued for the next restart.

## NoCloud datasource

The first NoCloud implementation has a deliberately small contract:

- `instance-id` is required and identifies the metadata instance;
- `local-hostname` is optional and is the only provisioning value consumed;
- an explicit seed directory or one eligible discovered volume is used; and
- `user-data` is never opened, parsed, logged, archived, or executed.

The `meta-data` file must be a regular file no larger than 64 KiB containing
one YAML mapping. YAML aliases, duplicate supported keys, multiple documents,
excessive nesting, control characters, and overlong identifiers are rejected.
Unknown keys are ignored; they do not expand the provisioning contract.

Additional cloud-init keys, network configuration, vendor data, OpenStack
ConfigDrive layouts, and network metadata services are unsupported. Metadata is
untrusted input even when it comes from attached media.

## Hostname provisioner

Hostname provisioning is independently disabled by default. When enabled and a
`local-hostname` is present, the provisioner accepts only a conservative
Windows host label:

- 1 through 15 ASCII characters;
- letters, digits, and internal hyphens only;
- begins and ends with an ASCII letter or digit; and
- is not entirely numeric.

It does not split, truncate, or silently normalize an FQDN into a host label.
An invalid name fails before a Windows mutation. If the host is joined to a
domain, the plugin refuses the rename. Comparison with the current computer
name is case-insensitive, so an already-active name is an idempotent no-op.

## Restart and reconciliation

`reboot.policy` remains `never`. `wininit` does not automatically restart the
guest.

When Windows accepts a rename but requires restart, state schema version 2
records a durable `reboot_required` checkpoint and status reports the pending
condition. An administrator restarts Windows through their normal change
process. The agent does not perform or claim that restart.

State contains only the SHA-256 of normalized plugin input, not the raw target
hostname or metadata. On the next run, if the hostname plugin is still enabled,
the agent must rediscover the NoCloud seed and compare the active and configured
Windows names with the fresh `local-hostname`. The seed must remain accessible
through restart. If metadata changed, the new input is reconciled. If the seed
is gone, provisioning reports `no_datasource`; the old hash cannot be replayed
as a hostname.

If the plugin is disabled before that run, the configuration choice wins and
the agent does not resume or reconcile the checkpoint. Status must show the
plugin as disabled rather than successful.

## Status and diagnostics

Human-readable and JSON status include one descriptor per known plugin with
`name`, `kind`, `enabled`, and `status`. Runtime states are `disabled`,
`pending`, `running`, `completed`, `reboot_required`, `failed`, or `skipped`. A
disabled plugin is not a failure and is not reported as executed. `pending`
means the enabled plugin has no matching saved result, including after graceful
cancellation. A reboot-required hostname result is not reported as completed
provisioning.

Overall provisioning status distinguishes `disabled`, `pending`,
`running`, `no_datasource`, `metadata_available`, `no_actions`, `completed`,
`reboot_required`, and `failed` as applicable. Status does not reuse a durable
runtime result when the current plugin configuration hash differs.

## Concurrency and durable mutation checkpoints

Before running the lifecycle pipeline, both SCM and foreground modes acquire
the machine-wide named lock `Global\WininitProvisioning`. The lock prevents
concurrent mutation even when two processes use different Registry paths. A
second process fails clearly instead of waiting or running concurrently.

Before calling a platform mutation, the lifecycle persists a `running` plugin
checkpoint. The Registry store writes and verifies an inactive checkpoint,
switches the active marker, and calls `RegFlushKey` before returning. This
low-frequency, expensive flush ensures the pre-mutation checkpoint is committed
to disk.

Diagnostics may include sanitized plugin status and non-sensitive metadata such
as the datasource type. They must not include raw metadata files or user-data.

## External plugins

External executable plugins, the versioned protobuf/gRPC API, and the Go SDK
remain deferred. See [ADR 0006](adr/0006-external-plugin-direction.md). No file
placed in `plugins.directory` is executed by this milestone.
