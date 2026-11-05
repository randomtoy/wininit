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
  provisioners:
    hostname:
      enabled: false
reboot:
  policy: never
```

The complete serialized schema is:

| Field | Required | Purpose |
| --- | --- | --- |
| `version` | Yes | Selects the schema; version 1 is the foundation schema. |
| `log.level` | No | One of `debug`, `info`, `warn`, or `error`; defaults to `info`. |
| `log.directory` | No | Location for service logs under ProgramData. |
| `state.registry_path` | No | Path relative to HKLM; it must remain below `SOFTWARE\`. |
| `plugins.directory` | No | Reserved future plugin path; setting it does not enable plugins. |
| `plugins.datasources.nocloud.enabled` | No | Opts into the built-in NoCloud datasource; defaults to `false`. |
| `plugins.datasources.nocloud.seed_directory` | No | Explicit seed directory; defaults to empty. |
| `plugins.datasources.nocloud.discover_volumes` | No | Permits mounted `CIDATA` ISO9660/FAT discovery; defaults to `true`. |
| `plugins.provisioners.hostname.enabled` | No | Opts into hostname mutation; defaults to `false`. |
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

## Backward compatibility and enablement

The built-in plugin additions do not change the configuration schema version.
A pre-existing version 1 file with no `plugins.datasources` or
`plugins.provisioners` section remains valid and leaves both plugins disabled.
Installing or upgrading a new executable must not turn on metadata discovery or
hostname mutation.

Each `enabled` flag is authoritative for that plugin. Disabling NoCloud prevents
seed discovery and metadata reads. Disabling hostname prevents rename and
checkpoint reconciliation. Current configuration takes precedence over a
pending checkpoint created when a plugin was enabled.

When NoCloud is enabled, configure either a non-empty `seed_directory` or
mounted-volume discovery. An explicit directory is the selected source; volume
discovery accepts exactly one mounted `CIDATA` volume formatted as ISO9660 or
FAT and fails on ambiguity. See [Built-in plugins](plugins.md) for complete
enable and disable examples.

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
NoCloud datasource never opens `user-data`. Future sensitive values must not be
written to lifecycle state, normal logs, status output, or diagnostic archives.
Configuration sanitization must default to removal rather than relying only on
field-name pattern matching.
