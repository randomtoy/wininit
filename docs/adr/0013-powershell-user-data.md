# ADR 0013: Separate hash-approved PowerShell process and one-shot journal

- Status: Accepted
- Date: 2026-11-06
- Extends: ADRs 0002, 0006, 0009, 0012

## Context

The next roadmap stage needs PowerShell user-data as an independently disabled
plugin, shipped separately from the main package. Arbitrary metadata must not
silently become LocalSystem code. The administrator explicitly approved exact
script SHA-256 pins and a new execution ID to authorize retry after failure or
interruption. The available Windows guest must not be rebooted during testing.

## Decision

Keep the first PowerShell plugin in this repository as its own command/EXE and
checksum artifact, excluded from the agent dependency graph and MSI payload.
Use a separate experimental protobuf/gRPC userdata v1 contract with local
mTLS and pinned-image launch. Do not expand the read-only datasource protocol
or introduce a stable arbitrary-provisioner SDK.

Require enabled, allow_privileged, an execution ID, exact script/image hashes,
bounded timeout/output and protected executable ancestry. After exactly one
datasource is selected, revalidate matching metadata in the child and read only
that source's bounded UTF-8 #ps1/#ps1_sysnative payload. Prepare snapshots it
without execution and returns a lease/hash/size. The host flushes started intent
before Execute; the wire never carries script/output bytes or Registry handles.

Use native OS Windows PowerShell 5.1 only, with a constant encoded bootstrap and
private stdin, no payload spool, profiles or execution-policy bypass. Discard
and count combined output. Preserve numeric native exit codes; terminating
errors fail. Exit 3010 requests administrator reboot only.

Retain elevated provisioning identity, remove shutdown privileges, assign a
suspended process to a kill-on-close job with eight processes and 512 MiB
per-process/total, and use a private desktop and scrubbed environment. This
role differs deliberately from restricted read-only ConfigDrive. It is not a
sandbox for hostile administrative code. Keep Windows auditing/AMSI enabled.

State schema 3 adds a bounded, non-evicting journal keyed by a hash of plugin,
instance ID and execution ID. Store only digests, boot identity, timestamps,
fixed failure codes, numeric exits and byte counts. Migrate schemas 1/2 without
inventing success. Cache success, reject changed script hash under a consumed
ID, and block failed/interrupted/unknown work until a new ID. Preserve history
across instance changes. The contract is at most one dispatch per identity,
not exactly-once completion. A volatile protected Registry boot marker prevents
service restart from clearing 3010 without a kernel boot.

## Consequences

Review and distribute both EXE and payload deliberately. Existing configs stay
disabled; no automatic plugin scanning/download/fallback is introduced. The
journal makes replay conservative after ambiguous crashes, may fill at 128
entries, and is protected only as long as administrators preserve state.
Approved scripts can leave partial side effects, use administrative mechanisms
outside the job, and be retained by Windows auditing. These limits are explicit
in the [operator policy](../powershell-user-data.md).

Portable tests cover parsing, source binding, redaction, wire validation and
journal decisions. Native Windows tests cover the actual interpreter, job,
token, cancellation, output and boot-marker boundaries. Guest testing excludes
reboot; actual kernel-boot/resume validation stays pending.

## Alternatives considered

- Compile an executor into the agent: conflicts with independent distribution.
- Separate repository immediately: premature while the protocol changes together
  with host state and tests; preserve a clean executable/wire boundary instead.
- Accept arbitrary user-data or retry on restart: unsafe implicit authorization
  and replay after partial privileged mutations.
- Restricted ConfigDrive token: prevents the provisioning operations being
  explicitly authorized; a separate privileged role makes the boundary clear.
- Put payload in argv or temporary files: unnecessary disclosure and cleanup
  surfaces. Stdin plus a constant bootstrap keeps the agent's artifacts narrow.

## References

- [Cloudbase user-data envelope conventions](https://cloudbase-init.readthedocs.io/en/latest/userdata.html)
- [Windows PowerShell command-line behavior](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe?view=powershell-5.1)
- [Registry key creation and volatile keys](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-regcreatekeyexw)
