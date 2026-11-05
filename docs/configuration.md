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
| `reboot.policy` | No | Must be `never` while reboot orchestration is not implemented. |

The default operational locations are conceptually:

```text
Configuration: %ProgramData%\WinInit\wininit.yaml
Logs:          %ProgramData%\WinInit\logs
Plugins:       %ProgramData%\WinInit\plugins
State:         HKLM\SOFTWARE\Wininit
```

The executable's help and distributed configuration are authoritative if a
release changes a path.

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

The foundation schema has no password, token, private-key, or raw user-data
fields. Future sensitive values must not be written to lifecycle state, normal
logs, status output, or diagnostic archives. Configuration sanitization must
default to removal rather than relying only on field-name pattern matching.
