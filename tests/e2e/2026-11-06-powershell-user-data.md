# PowerShell user-data guest acceptance — 2026-11-06

## Environment and deployment

A disposable, non-domain-joined Windows Server 2025 Standard Evaluation AMD64
VM was accessed through its existing OpenSSH service. Windows reported
10.0.26100.32230 and Windows PowerShell 5.1.26100.7462, on QEMU
Standard PC (Q35 + ICH9, 2009). KVM acceleration was not established. No Windows image, credentials,
raw user-data, or diagnostic archive is committed here.

The tested implementation is commit
`9d9bfe713b957bbcd515ebbadc83404e9414ac23`. The agent and both separate plugin
EXEs were cross-built with Go 1.27.3. Their hashes were compared on the guest:

| Artifact | SHA-256 |
| --- | --- |
| Agent EXE | `e30451d6e8c40233f4697658ab133abd4e43f6ed527be2881cb97fa7753a19e5` |
| PowerShell plugin EXE | `cd7464a984738ec3bb27e6bdbdb3a2f953fc09d0bf06b31770c8bd11fbed9c05` |
| ConfigDrive plugin EXE | `516d5273d3e728c18005a9807526ffe5c74e04ffa601c46cf77ba928672ab631` |

This run predates PR 5 review hardening of Registry permissions and concurrent
cancellation. Those follow-up changes are covered by native Windows regression
and MSI CI; the original acceptance below describes the earlier guest candidate
precisely. The separate review follow-up is recorded at the end.

No prior WinInit service, configuration, installation directory, or state was
present. The candidate was deployed through the development CLI as an
automatic LocalSystem SCM service. This guest run did **not** install an MSI;
MSI construction/defaults/install/uninstall are separate native CI checks.

Fixtures, logs and images used a test-owned directory. State used the isolated
`HKLM\SOFTWARE\WininitPowerShellReview` key. Images/configuration were owned and
writable only by SYSTEM/Administrators. Benign seed/image fixtures permitted
Users read/execute so the restricted external ConfigDrive process could read
them. Hostname and all identity/password-policy plugins stayed disabled.

## Executed checks

Sixteen checks passed before the administrator reboot:

- Native `cmd.exe` exit 23 reached the journal as `exit_failure`, code 23.
- Approved NoCloud payload executed once under LocalSystem; stdout/stderr were
  discarded.
- Restart reused success with both payload and plugin image absent; the counter
  remained one.
- Wrong script pin failed before execution and consumed no journal entry.
- Missing payload was skipped without consuming an execution ID.
- A terminating failure ran once and stayed blocked across service restart.
- A new execution ID explicitly permitted one further failed attempt.
- Output overflow failed with saturated byte count 1025 for a 1024-byte limit.
- An eight-second session deadline terminated an already-running interpreter.
- SCM stop killed both interpreter and native descendant, retained interrupted
  intent, and blocked replay on restart.
- Selected builtin ConfigDrive supplied its colocated payload.
- Selected separate ConfigDrive supplied its colocated payload.
- Two available enabled sources failed before script execution or journal intent.
- Disabled PowerShell ignored an absent executable and reported disabled.
- Both Registry slots, logs and unpacked diagnostics excluded the controlled
  fixture output marker.
- Exit 3010 requested reboot, ran once, and stayed pending across a service
  restart without automatically rebooting Windows.

The guest also reproduced the missing-PATHEXT native launch problem during
development. Supplying fixed PATHEXT/ComSpec restored native exit accounting.
A later read-only fixture confirmed Windows PowerShell appends `.CPL` during
startup; native environment tests allow that OS addition.

## Actual reboot and resume

The administrator explicitly authorized one reboot for this replacement guest.
After all pre-reboot checks, a single administrator `shutdown /r` was issued
outside the plugin. Boot timestamps changed from
`2026-11-06T13:59:25.5003860Z` to `2026-11-06T14:37:06.1947340Z`.

After boot:

- The same SSH credentials worked.
- The automatic LocalSystem service was running without manual start.
- Provisioning and PowerShell status were `completed`, with `reused: true`.
- The reboot fixture's counter remained exactly one.
- The entire execution journal matched the pre-reboot journal, including its
  original 3010 result and original boot identity.
- The volatile current boot marker differed from the journal's original marker.
- Another service restart reused completion without executing again.
- Hostname and the administrator account's password-last-set timestamp matched
  their pre-test values.

Sanitized raw before/after status and scalar metadata are retained locally in
ignored `dist/powershell-guest-acceptance`. Assertions compare the real boot
transition and journal/counter equality; no BootSession key was deleted to
simulate a reboot.

## Cleanup and limits

The test-owned service was stopped and uninstalled through the development
CLI. Its fixture/image/log directory, default configuration directory created
for this run, and isolated Registry subtree were deleted. A fresh SSH command
verified service/directory/key absence and unchanged hostname/password timestamp.
OpenSSH and its configuration were left intact.

This verifies local-seed PowerShell execution and administrator reboot/resume
on one Windows Server 2025 VM. It does not establish Windows Server 2022,
attached CIDATA/config-2 discovery, real OpenStack metadata, domain behavior,
actual hostname rename, hostile-code isolation, or a complete release matrix.

## PR 5 review follow-up

The same guest subsequently ran the native state tests for review commit
`021f89838a22b292567866768a3dc10940ce4625`, without another reboot or service
installation. The cross-built test EXE SHA-256 was
`48decfeb1465134593f4ee8c36b64bc24ac4d8a8c49175426e0d97e389b7babe`.

The pre-fix regression accepted a tampered, valid-checksum journal from an
ordinary-user-writable state key. The hardened state suite passed: unsafe leaf
and ancestor ACLs were rejected, trusted legacy state migrated, protected child
state remained writable below a read-only ancestor, the system CREATOR OWNER
inheritance template was accepted without accepting real untrusted writers,
and boot-marker/corrupt/future-checkpoint tests passed. The suite used isolated
test Registry subtrees; its files and Registry resources were removed afterward.
The same production code then passed the actual interpreter result table
(success/Unicode/scrubbed environment, terminating error, native cmd/EXE exit 23,
3010 and output overflow), the NoCloud/ConfigDrive Registry journal pipeline
(success reuse, failed-ID blocking and pending 3010 across process restarts),
and native descendant cancellation on this guest. The three tests completed in
about 31 seconds. A temporary build overlay changed only TestMain to locate
cross-built fixture/plugin images, since the guest had no Go toolchain; test
bodies and production code were unchanged. No service or reboot was required.
The portable concurrent-error tests remain separate CI coverage.
