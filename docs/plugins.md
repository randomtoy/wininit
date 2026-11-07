# Plugins

## Safety model

`wininit` includes a small internal plugin pipeline for built-in datasources and
provisioners. Implementations are compiled packages by default. NoCloud, ConfigDrive and hostname
can explicitly select separate verified EXEs using the experimental v1
protobuf/gRPC SDK; see [external plugins](external-plugins.md).

Every plugin is independently opt-in. All seven logical plugins are disabled by default,
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
    configdrive:
      enabled: false
      seed_directory: ''
      discover_volumes: true
      execution: builtin
      process:
        executable: ''
        sha256: ''
        timeout_seconds: 15
  provisioners:
    hostname:
      enabled: false
    password_policy:
      enabled: false
      minimum_length: 14
      maximum_age_days: 90
      minimum_age_days: 1
      history_length: 24
      lockout_threshold: 10
      lockout_duration_minutes: 15
      lockout_window_minutes: 15
    local_users:
      enabled: false
      users: []
    authorized_keys:
      enabled: false
      users: []
```

The `plugins.directory` field remains reserved and is never scanned. Setting
that path does not enable or load an executable. External ConfigDrive requires
an explicit image path, SHA-256 pin, and execution mode.

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
is ISO9660 or FAT. Exactly one eligible volume must exist. Query failures and
unsupported filesystems on unrelated drives are skipped. No eligible match is
an unavailable-datasource result; multiple eligible matches are an ambiguity
error. The agent never chooses the first enumerated candidate. The discovery
checkpoint remains pending while other enabled datasource candidates have not
yet returned unavailable.

### Disable all provisioning behavior

All identity plugins must also retain `enabled: false`; the example below
uses their disabled defaults by omitting their sections.

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: false
    configdrive:
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

Additional cloud-init keys, network configuration, vendor data, and network
metadata services are unsupported. OpenStack layouts use the separate
ConfigDrive plugin described below. Metadata is
untrusted input even when it comes from attached media.

## OpenStack ConfigDrive datasource

ConfigDrive is a separate plugin named `configdrive`, disabled by default even
for existing NoCloud installations. To enable it with a local ConfigDrive root:

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: false
    configdrive:
      enabled: true
      seed_directory: 'D:\configdrive'
      discover_volumes: false
  provisioners:
    hostname:
      enabled: true
```

The directory must contain `openstack/<version>/meta_data.json`. To discover
mounted media instead, set `seed_directory: ''` and `discover_volumes: true`.
Only FAT/ISO9660 volumes labelled `config-2` (case-insensitively) qualify. Zero
matches return unavailable; multiple matching volumes fail. Direct UNC paths,
URLs, and relative seed paths are rejected by configuration validation. No
network metadata request or automatic mount is performed.

The newest supported dated directory present on the seed is selected, in this
order: `2025-04-04`, `2020-10-14`, `2018-08-27`, `2017-02-22`, `2016-10-06`,
`2016-06-30`, `2015-10-15`, `2013-10-17`, `2013-04-04`, `2012-08-10`.
`openstack/latest` is a compatibility fallback only when none of those dated
directories exists. Unknown dated directories are ignored. A selected
directory without metadata, malformed JSON, or a symlink in the `openstack`,
version, or file component is an error: the agent never silently downgrades to
older metadata after such a failure.

Only these fields are consumed:

| OpenStack field | Internal field | Behavior |
| --- | --- | --- |
| `uuid` | `InstanceID` | Required nonempty string, at most 256 UTF-8 bytes, without surrounding whitespace or control characters. |
| `hostname` | `LocalHostname` | Optional ASCII DNS name/FQDN; lowercase its first label and pass it to the hostname provisioner. |

For example, `Worker-01.example.invalid` becomes `worker-01`. The datasource
does not set a DNS suffix and never truncates a long label to fit Windows.
The hostname provisioner's 15-character and nonnumeric-name checks still
apply. A missing or empty `hostname` skips hostname work; the display `name`
is never used as a fallback.

Metadata must be a regular UTF-8 JSON file no larger than 64 KiB containing a
single object. Duplicate keys, non-string consumed fields, invalid DNS names,
and nesting deeper than 32 levels fail with sanitized errors. Unknown fields
are validated and ignored. The plugin never opens `user_data`, `password`,
`vendor_data*.json`, `network_data.json`, EC2 metadata, or injected file paths.
These files and the raw metadata are never copied into diagnostics.

## Selecting between datasource plugins

The pipeline probes all enabled datasources before any provisioner runs.
Exactly one must return metadata. An unavailable NoCloud seed can fall back to
ConfigDrive, and an unavailable ConfigDrive can leave NoCloud selected. If
both are available, provisioning fails without changing the host, regardless
of catalog order or whether instance IDs happen to match. Disable the source
you do not intend to use. A malformed source fails immediately; it is not
treated as absent and cannot be bypassed with another source.

This selects sources, not individual metadata fields: metadata from different
datasources is never merged. An enabled but unavailable source is `skipped`;
a disabled source is `disabled` and performs no I/O. All unavailable sources
produce `no_datasource`. A selection conflict produces durable `failed` status.
Enabled provisioners remain `pending` until a source is selected and they run.
Each start clears their prior result, input hash, and reboot flag before
discovery, so an absent, malformed, or conflicting source cannot make an old
provisioner result appear current. Attempt counters retain their history until
a different instance is selected.

## PowerShell user-data provisioner

`powershell_user_data` runs after hostname, only in a separate reviewed EXE. It
is independently disabled by default and requires explicit privileged consent,
script/image SHA-256 pins, and an execution ID. Datasources still read metadata
only. See [PowerShell user-data](powershell-user-data.md) for selected-source
reading, accepted envelopes, journal/retry semantics, and numeric-only results.

## Hostname provisioner

The independently gated `password_policy`, `local_users`, and
`authorized_keys` provisioners run before hostname when enabled. Their complete
configuration, password handling, rerun/revocation rules, and Windows limits
are documented in [Identity and access](identity.md). Defaults are disabled.

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

When Windows accepts a rename but requires restart, state schema version 3
records a durable `reboot_required` checkpoint and status reports the pending
condition. An administrator restarts Windows through their normal change
process. The agent does not perform or claim that restart.

State contains only the SHA-256 of normalized plugin input, not the raw target
hostname or metadata. On the next run, if the hostname plugin is still enabled,
the agent must rediscover the selected NoCloud or ConfigDrive seed and compare
the active and configured Windows names with fresh normalized metadata. The seed must remain accessible
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
cancellation or while fresh datasource selection is blocked. A reboot-required
hostname result is not reported as completed provisioning.

Overall provisioning status distinguishes `disabled`, `pending`,
`running`, `no_datasource`, `metadata_available`, `no_actions`, `completed`,
`reboot_required`, and `failed` as applicable. Status does not reuse a durable
runtime result when the current plugin configuration hash differs.

## Concurrency and durable mutation checkpoints

Before running the lifecycle pipeline, both SCM and foreground modes acquire
the mutex `WininitPrivate\Provisioning` in a Windows private namespace. Its
boundary uses the Builtin Administrators SID without a session SID, and
protected DACLs on the namespace and mutex grant access only to SYSTEM and
Builtin Administrators. The object therefore prevents concurrent mutation
across Windows sessions even when two processes use different Registry paths.
A second process fails clearly instead of waiting or running concurrently.

A worker goroutine pinned to one OS thread waits for, owns, and releases the
thread-affine Windows mutex. Abandoned ownership is accepted so normal durable
checkpoint reconciliation can recover after a crashed owner. Release is
idempotent and may be requested from any goroutine; the pinned owner thread
performs the actual mutex release and handle cleanup. The namespace remains
open for the process lifetime.

Before calling a platform mutation, the lifecycle persists a `running` plugin
checkpoint. The Registry store writes and verifies an inactive checkpoint,
switches the active marker, and calls `RegFlushKey` before returning. This
low-frequency, expensive flush ensures the pre-mutation checkpoint is committed
to disk.

Diagnostics may include sanitized plugin status and non-sensitive metadata such
as the datasource type. They must not include raw metadata files or user-data.

## External plugins

The optional [external ConfigDrive](external-plugins.md) is the first
experimental executable plugin. It stays in this repository as a separate
EXE and versioned SDK surface. Named datasource v1 and metadata-only provisioner v1 now support separate
NoCloud/hostname EXEs; see [metadata SDK](external-sdk.md). Arbitrary third-party
host registrations, registries/downloads and stable Go API promises remain deferred.
PowerShell is a narrow separate-process exception; see ADR 0013.
