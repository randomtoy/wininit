# Metadata process SDK guest acceptance — 2026-11-07

## Scope and artifacts

The existing disposable Windows Server 2025 Standard Evaluation AMD64 SSH guest
reported build 10.0.26100.32230 and Windows PowerShell 5.1.26100.7462. No reboot,
hostname mutation, account/password change or SSH configuration change was made.
No credentials, host address, media or raw operational metadata are committed.

Agent commit `5837d5e0004bb923f994ef71dc8eb4e6017698cc` includes the failed-start
CLI fix discovered during this run. NoCloud/hostname EXEs were built from
`697298fc717ffa4f306844794fd3746ab73df94e`; their implementation/SDK source did
not change in the intervening commit. Go 1.27.3 cross-build hashes were verified
on the guest:

| Artifact | SHA-256 |
| --- | --- |
| Agent | `e9ecad8c69ce4f871575cb13ef2a5d84a1277a60d7377e366eba4e1b206797a9` |
| NoCloud EXE | `d196312078d397066c8c12ab32ca4476da73f2bbbcd1261c3ad2fa36155c49d5` |
| Hostname EXE | `2981dc97ffd85a2c297c36d47498d0ef0aab95c751610bb8f8063b55be219e3b` |

## Native process tests

Actual EXEs passed the metadata protocol outcome table, identity/kind/version
mismatches, idempotent-contract refusal, unknown outcomes, crashes, pin and
privileged-approval refusal, error redaction and scrubbed environment tests.
The real NoCloud EXE preserved NoCloud hostname spelling and reported absent
metadata correctly. Hostname reconciled the existing configured name twice
without a native Set operation or changed before/after snapshots.

The declarative role fixture verified the actual elevated token and current
job: one process, 256 MiB process/total memory, kill-on-close and removed local /
remote shutdown privileges. Its attempted native child could not run. Existing
PowerShell native tests also passed, including Unicode, fixed environment,
terminating errors, native cmd/EXE exit 23, 3010 result and output overflow;
the PowerShell eight-process role remained distinct.

Follow-up cancellation fixtures signal readiness inside handshake, Describe or
Apply before root cancellation. This avoids describing startup cancellation as
an RPC cancellation test. The private-session timeout also proves it reached
Apply. These synchronized tests and the native NoCloud/hostname/role tests were
rerun after the agent fix. A temporary TestMain build overlay located reviewed
cross-built fixture images, because the guest had no Go toolchain. Test bodies
and production code were unchanged; no SDK consumer build is claimed on the VM.

## LocalSystem service pipeline

No prior wininit service, default configuration directory or review Registry
key was present. A development CLI installation (not MSI) registered a temporary
LocalSystem service with protected configuration/images and a local seed. State
used `HKLM\SOFTWARE\WininitSDKReview`. Only external NoCloud and external hostname
were enabled. The target was the unchanged active/configured computer name.

Five checks passed:

- Both separate EXEs completed through the actual lifecycle pipeline.
- Service restart increased the hostname attempt counter and completed without
  inventing a pending rename or reboot.
- Wrong NoCloud image SHA-256 failed before hostname provisioning. A fast SCM
  failure initially exposed the old `service start` wait loop; the fixed agent
  returned the numeric stopped-service error promptly instead of hanging.
- Disabled plugins ignored missing EXEs and previous checkpoints; all seven
  logical descriptors reported disabled.
- Controlled user-data marker bytes were absent from state/status/logs. The
  datasource implementation/tests read only meta-data, never user-data.

Native service wait-state regressions passed as well. An early test-controller
ACL command combined incompatible icacls options; ownership/grants were split
into separate commands in both the controller and operator documentation.

The boot timestamp stayed `2026-11-06T14:37:06.1947340Z`. Active and configured
names remained equal before/after, with no 3010 or rename requested by these
plugins. No reboot command was issued.

## Cleanup and remaining matrix

The test-owned service was stopped/uninstalled; its default configuration and
isolated Registry state were removed. Fixture/image/seed/log/controller files
and any empty test-only Registry containers were removed after native tests.
A fresh SSH command verified absence of owned resources and unchanged hostname
and boot timestamp. Existing system wininit.exe was never terminated; only the
owned development CLI waiting for failed service start was stopped by verified
PID/path during diagnosis.

This validates local-seed SDK execution and unchanged-name reconciliation on one
Server 2025 guest. It does not validate Server 2022, attached media, real hostname
rename/reboot/resume, domain rename, hostile-code isolation or MSI on this VM.
Windows/MSI CI remains separate evidence. The independent external-module SDK
consumer build runs in local/CI tests, not on this guest.
