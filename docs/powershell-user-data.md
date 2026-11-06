# PowerShell user-data

`powershell_user_data` is an experimental, separate executable plugin. It is
independently disabled by default. Its implementation is excluded from the
agent's dependency graph and the MSI installs only the agent. Build/release
artifacts contain `wininit-plugin-powershell-windows-amd64.exe` separately.
The same repository keeps protocol, conformance tests and artifacts coordinated;
a separate repository can follow once the wire contract stabilizes.

## Administrative approval and configuration

Review both the plugin and the exact script before enabling privileged code.
Install the plugin under a protected local directory, for example
`C:\ProgramData\WinInit\plugins`, owned and writable only by SYSTEM and
Administrators. Every ancestor must also prevent ordinary-user replacement.
Verify published artifact checksums before computing configuration pins.
The interpreter is native Windows PowerShell 5.1 at
`System32\WindowsPowerShell\v1.0\powershell.exe`; PATH lookup, x86, PowerShell 7,
custom interpreters and execution-policy bypass are not supported.

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: true
      seed_directory: 'C:\WinInitSeed'
      discover_volumes: false
  provisioners:
    powershell_user_data:
      enabled: true
      allow_privileged: true
      execution_id: initial-setup-v1
      script_sha256: '<64 hexadecimal characters from the exact user-data file>'
      max_output_bytes: 65536
      process:
        executable: 'C:\ProgramData\WinInit\plugins\wininit-plugin-powershell-windows-amd64.exe'
        sha256: '<64 hexadecimal characters from the reviewed plugin EXE>'
        timeout_seconds: 120
```

Replace both placeholders with real hashes. On Windows:

```powershell
(Get-FileHash -Algorithm SHA256 -LiteralPath C:\WinInitSeed\user-data).Hash.ToLowerInvariant()
(Get-FileHash -Algorithm SHA256 -LiteralPath C:\ProgramData\WinInit\plugins\wininit-plugin-powershell-windows-amd64.exe).Hash.ToLowerInvariant()
```

`execution_id` is 1–64 ASCII letters, digits, underscores or hyphens. Timeout is
1–3600 seconds for the entire plugin session, including startup/preparation.
Combined stdout/stderr limit is 1024–1048576 bytes; default 65536. Output is
counted and discarded, never returned or logged. A saturated count of limit+1
records overflow. Only exit/failure codes, timestamps and byte counts survive.
`allow_privileged` must be true independently of `enabled`.

To disable it, set `plugins.provisioners.powershell_user_data.enabled: false`.
Disabled configuration does not inspect an image or seed, consume an execution
ID, or resume old work. Re-enabling does not clear execution history.

## Selected source and accepted input

All enabled datasources are probed first; exactly one must be available. The
PowerShell plugin reads only that source, using its configured directory or
unambiguous mounted volume discovery. It verifies the selected metadata's
instance ID and normalized hostname again before reading payload:

- NoCloud: `user-data` beside `meta-data`.
- ConfigDrive: `openstack/<selected-version>/user_data` beside `meta_data.json`.
  Version selection matches the datasource; malformed newer versions never
  trigger downgrade. Builtin and separate ConfigDrive modes both work.

An absent payload is `skipped` and consumes no execution ID. Missing/changed
metadata, ambiguous media, or an unreadable/invalid payload fails preparation.
No script executes. No network metadata is fetched. Regular local files only:
reject reparse traversal, hard links, streams and unsafe path components.

Accepted input is at most 1 MiB of valid UTF-8, optionally with a UTF-8 BOM,
with exact first line `#ps1` or `#ps1_sysnative` and LF or CRLF. The body must
be nonempty. The SHA-256 covers **all original bytes**, including BOM, header
and line endings. UTF-16, NUL, `#ps1_x86`, cloud-config, MIME and compressed
payloads are rejected. Example:

```powershell
#ps1_sysnative
$ErrorActionPreference = 'Stop'
# Place reviewed initialization here.
exit 0
```

The plugin snapshots the approved body in memory during Prepare. It launches
no interpreter until the agent durably saves and flushes a `started` journal
entry. Execute consumes that prepared lease once. Script bytes go over private
stdin to a constant encoded bootstrap, never command-line arguments or an
agent-created temporary payload file. Owned buffers are cleared without a
promise to erase all runtime/OS copies. PowerShell runs without profiles and
noninteractively. Treat nonterminating errors explicitly in your script;
terminating errors return 1 and native nonzero exit codes are preserved.

## One-shot journal and recovery

The journal key hashes plugin name, selected instance ID and `execution_id`.
It stores the approved script digest separately. It does not depend on general
configuration/image changes, hostname, source path or process restarts.

- Exit 0 records success. Later runs reuse it (`reused: true`) without reading
  script or plugin image, after normal datasource selection.
- Exit 3010 records `reboot_required`. It never triggers an automatic reboot.
  Service restarts leave the request pending. A protected volatile Registry
  boot marker changes only after kernel boot; on a later run with fresh metadata
  the request clears without executing again. Real reboot validation remains
  pending; unit tests simulate that boot transition.
- Nonzero exit, timeout, overflow, cancellation, protocol failure or an unknown
  result after a crash consumes the ID. Restarting the service cannot retry it.
  Inspect partial side effects, then choose a **new execution_id** to authorize
  another attempt. Changing only the script pin under a consumed ID is rejected.
- A crash before Execute may leave `started`; the conservative outcome is
  unknown, requiring a new ID even if no script actually ran. This is at-most-one
  dispatch, not exactly-once completion or transactional rollback.
- Preparation failure before intent is saved can be retried after repair with
  the same ID. An intent-save failure never dispatches Execute.

A different instance ID is new work. History remains across instance switches,
so switching back cannot replay consumed work. Up to 128 entries are retained;
a full journal fails rather than evicting replay protection. State schema 3
migrates schemas 1/2 without inventing script results. Do not downgrade an agent
against schema 3 or delete/restore state merely to retry a script: that removes
replay protection. Deliberate state reset/restore is an administrator action.
An invalid volatile boot marker fails closed; investigate its protected Registry
key rather than treating service restart as reboot evidence.

## Trust boundary and limits

This is trusted privileged code, usually running as LocalSystem. The SHA-256
pin is authorization of those exact bytes, not proof they are safe. It retains
administrative provisioning access. Its job is limited to eight processes and
512 MiB total/per-process, assigned while suspended, with kill-on-close cleanup,
a private desktop and scrubbed environment with fixed OS PATH, PATHEXT and
ComSpec (native System32 cmd.exe). Direct local/remote shutdown
privileges are removed. Scripts must not request reboots directly; use 3010.
Approved administrative code can still use services/schedulers or other
privileged mechanisms outside the job. This is not a hostile-script sandbox.

Windows auditing, ScriptBlockLogging, AMSI, crash artifacts and script-created
files can retain input/output outside wininit. The plugin does not disable
security policy or logging. Review scripts for secrets and side effects; never
embed credentials expecting wininit's log redaction to hide them from Windows.
The OS-serviced interpreter may have TrustedInstaller ownership and servicing
hard links; this exception does not weaken configured plugin EXE checks.

See [ADR 0013](adr/0013-powershell-user-data.md), [security](security.md), and
[external process mechanics](external-plugins.md). The experimental userdata v1
protocol is narrow: Describe, read-only Prepare and lease-bound Execute; it
exposes neither script/output bytes nor Registry/platform handles.

Before Execute, started intent is committed and flushed into both alternating
checkpoint slots. Recovery from one damaged slot therefore retains replay
protection; terminal evidence may conservatively recover as started/unknown.
