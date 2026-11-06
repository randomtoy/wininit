# Configuration

## Principles

`wininit` configuration is versioned, validated before use, and safe by
default. The loader begins with safe defaults for optional fields, but the file
must explicitly declare its `version`. Unsupported schema versions, unknown
fields, invalid log levels, unsafe Registry paths, invalid reboot policies, and
inconsistent paths produce actionable validation errors.

The MSI installs a default configuration for first use. It must not overwrite
an administrator-owned configuration during upgrade or ordinary repair.

## Schema

The version 1 configuration installed with the MSI is:

```yaml
version: 1
log:
  level: info
  directory: 'C:\ProgramData\WinInit\logs'
state:
  registry_path: 'SOFTWARE\Wininit'
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
reboot:
  policy: never
```

The complete serialized schema is:

| Field | Required | Purpose |
| --- | --- | --- |
| `version` | Yes | Selects the schema; version 1 is the foundation schema. |
| `log.level` | No | One of `debug`, `info`, `warn`, or `error`; defaults to `info`. |
| `log.directory` | No | Location for service logs under ProgramData. |
| `state.registry_path` | No | Non-empty HKLM subkey below `SOFTWARE\`, dedicated to wininit state. |
| `plugins.directory` | No | Reserved future plugin path; setting it does not enable plugins. |
| `plugins.datasources.nocloud.enabled` | No | Opts into the built-in NoCloud datasource; defaults to `false`. |
| `plugins.datasources.nocloud.seed_directory` | No | Explicit seed directory; defaults to empty. |
| `plugins.datasources.nocloud.discover_volumes` | No | Permits mounted `CIDATA` ISO9660/FAT discovery; defaults to `true`. |
| `plugins.datasources.configdrive.enabled` | No | Opts into OpenStack ConfigDrive; defaults to `false`. |
| `plugins.datasources.configdrive.seed_directory` | No | Local ConfigDrive root containing `openstack/`; defaults to empty. |
| `plugins.datasources.configdrive.discover_volumes` | No | Permits mounted `config-2` ISO9660/FAT discovery; defaults to `true`. |
| `plugins.datasources.configdrive.execution` | No | `builtin` (compatible default) or explicit `external` process. |
| `plugins.datasources.configdrive.process` | External mode | Absolute `executable`, exact `sha256`, and `timeout_seconds` 1–60 (default 15). |
| `plugins.provisioners.hostname.enabled` | No | Opts into hostname mutation; defaults to `false`. |
| `plugins.provisioners.password_policy` | No | Separate `enabled: false` plus bounded SAM password/lockout fields shown above. |
| `plugins.provisioners.local_users` | No | Separate `enabled: false` and `users` entries: `name`, `full_name`, `description`, `password_file`, `password_never_expires`. |
| `plugins.provisioners.authorized_keys` | No | Separate `enabled: false` and `users` entries: `name` and `public_keys`. Empty keys revoke the managed block. |
| `plugins.provisioners.powershell_user_data` | No | Separate `enabled: false`, `allow_privileged: false`; explicit execution ID, script/EXE SHA-256 pins, process timeout and output limit. See [PowerShell policy](powershell-user-data.md). |
| `reboot.policy` | No | Must be `never`; restart is always administrator-controlled. |

The default operational locations are conceptually:

```text
Configuration: %ProgramData%\WinInit\wininit.yaml
Logs:          %ProgramData%\WinInit\logs
Plugins:       %ProgramData%\WinInit\plugins
State:         HKLM\SOFTWARE\Wininit
```

The executable's help and distributed configuration are authoritative if a
release changes a path.

`state.registry_path` must identify a dedicated product/test subkey such as
`SOFTWARE\Wininit`. Validation rejects the `SOFTWARE` root, empty paths, empty
components, surrounding whitespace, slashes, and NUL. Administrators must not
select a shared parent because checkpoint creation and recovery must never
overlap unrelated Registry values.

## Backward compatibility and enablement

The plugin additions do not change the configuration schema version.
A pre-existing version 1 file with no `plugins.datasources` or
`plugins.provisioners` section remains valid and leaves all plugins disabled.
An existing NoCloud configuration without a `configdrive` section keeps
ConfigDrive disabled, without changing the configuration schema version.
Existing enabled ConfigDrive files keep builtin execution. New identity
sections remain disabled when omitted. See [identity inputs and password-file
security](identity.md) and [external process settings](external-plugins.md).
Installing or upgrading a new executable must not turn on metadata discovery or
hostname mutation.

Each `enabled` flag is authoritative for that plugin. Disabling NoCloud prevents
its seed discovery and metadata reads; ConfigDrive has its own independent
gate. Disabling hostname prevents rename and
checkpoint reconciliation. Current configuration takes precedence over a
pending checkpoint created when a plugin was enabled.

When NoCloud is enabled, configure either a non-empty `seed_directory` or
mounted-volume discovery. An explicit directory is the selected source; volume
discovery accepts exactly one mounted `CIDATA` volume formatted as ISO9660 or
FAT and fails on ambiguity. See [Built-in plugins](plugins.md) for complete
enable and disable examples. ConfigDrive has the same source options, but uses
the `config-2` label and OpenStack JSON layout. When multiple datasource
plugins are enabled, exactly one may provide metadata. Availability conflicts
and malformed metadata fail before any provisioner runs.

## Validation

Run validation before restarting a production service:

```powershell
wininit.exe validate
```

Validation reads the selected configuration, requires an explicit schema
version, applies defaults for omitted optional fields, validates the complete
result, and returns a non-zero exit code with field-specific errors. It does
not start provisioning.

## Sensitive values

The schema has no password, token, private-key, or raw user-data fields. The
datasources never open NoCloud `user-data` or ConfigDrive `user_data`. Only the
separate enabled PowerShell process reads the selected source payload, with an
explicit script pin and privileged authorization.
Future sensitive values must not be
written to lifecycle state, normal logs, status output, or diagnostic archives.
Configuration sanitization must default to removal rather than relying only on
field-name pattern matching.
