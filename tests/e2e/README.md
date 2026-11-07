# Guest E2E acceptance

## Automated suite without reboot

Run in native 64-bit PowerShell on a disposable, licensed, elevated,
non-domain-joined Windows AMD64 guest. A 32-bit/ARM64 shell is refused before
reading installation/state paths, so Registry and ProgramFiles views agree.
The script refuses a pre-existing `wininit` service, ProgramData/ProgramFiles
WinInit directory, production Registry state, registered related MSI product or
pending hostname change. It
owns only resources proven absent before installation; ordinary MSI uninstall
preserves configuration/state, after which the harness removes its own fixtures.
It never changes accounts, passwords, SSH policy, active/configured hostname or
reboots Windows. It records boot/name before and after and fails if either
changes. Take a snapshot before an acceptance run; a failed cleanup preserves
resources still used by a remaining service for diagnosis.

Build a lower-version fixture and candidate on Windows, or download the exact
`wininit-windows-amd64` artifact from successful push CI. CI packages include
`baseline/wininit-windows-amd64.msi` from the same source with version `0.0.0`.
Do not describe this fixture as a historical-release migration test.

```powershell
./scripts/package.ps1 -Version 0.0.0 -DistDirectory ./dist/baseline -RequireClean
./scripts/package.ps1 -Version 0.1.0 -RequireClean
./tests/e2e/Invoke-NoRebootAcceptance.ps1 -DistDirectory ./dist `
  -BaselineMsiPath ./dist/baseline/wininit-windows-amd64.msi -BaselineVersion 0.0.0 `
  -ArtifactsDirectory './artifacts/msi logs' -Provider qemu -ExpectedServer 2025
```

The same harness runs under Windows PowerShell 5.1 in native CI. It checks
installed protected ACLs, default disablement and agent-only MSI payload,
major-upgrade preservation, machine-wide LocalSystem lock, separate verified
NoCloud/ConfigDrive/hostname processes, explicit pinned PowerShell success,
failure/replay protection, missing disabled EXEs, redacted output and uninstall.
Exit 3010 must persist a reboot request across service restarts without replay
or an actual Windows reboot.
NoCloud and ConfigDrive use local seeds and the existing hostname. PowerShell
writes only a benign fixture counter and requires LocalSystem identity.
Every service restart waits for fresh checkpoint generation/source attempts;
an earlier stored success/failure cannot satisfy the next assertion.

Keep `acceptance.json`, `plugin-cases.json` and verbose MSI logs together.
The report records source commit/dirty status, candidate and baseline hashes,
Windows/PowerShell versions, model, declared provider, boot/name checks, cleanup,
passed cases and explicit skipped cases. Diagnostics contain fixture data only;
review before sharing. A failed script must not be summarized as a passing run.
See [release gates](../../docs/release.md) for remaining validation.

## Administrator-provisioned matrix runners

`.github/workflows/e2e.yml` is manual-only and has four cells:
Server 2022/2025 × QEMU/OpenStack. Provision disposable isolated licensed guests
and an elevated self-hosted runner per cell with labels `wininit-e2e`,
`server-2022` or `server-2025`, and `qemu` or `openstack` (plus the normal
`self-hosted`, `Windows`, `X64`). Git, GitHub CLI, runner dependencies and outbound
artifact access must already work; Windows PowerShell 5.1 runs acceptance.

Dispatch only a reviewed repository commit and provide its successful **push**
CI run ID. The workflow checks repository, workflow identity, successful
conclusion and exact source SHA before downloading artifacts, then checks the
manifest source. Never route pull-request/fork code to these privileged guests.
Runner labels declare placement; they do not prove KVM acceleration or cloud
identity. No Windows images/keys or cloud credentials are distributed and no
runner/cloud VM is provisioned automatically by this workflow.

This matrix entry point covers the no-reboot subset only. Clean-image boot,
media attachment, real rename/reboot/resume and historical-release migrations
still require dedicated infrastructure and separately recorded runs. The
following manual contract remains pending across the full matrix.

## Datasource and hostname VM acceptance with administrator reboot

This procedure is pending execution. It requires a disposable, licensed,
non-domain-joined Windows Server 2022 or 2025 AMD64 VM. Hosted CI does not run a
real rename or reboot. Do not run this procedure on a workstation, production
VM, domain member, or shared CI host.

The [2026-11-06 PR 4 guest review](2026-11-06-pr4-review.md) separately records
identity/access, existing OpenSSH login, external ConfigDrive local-seed, SCM,
and MSI checks. It did not run this rename/reboot procedure; restart was not
authorized.

## Prepare

1. Take a VM snapshot and install the candidate MSI with verbose logging.
2. Record the original active hostname and verify all seven logical plugins are disabled in
   `wininit.exe status --json`.
3. Copy `tests/fixtures/nocloud/meta-data` to a local seed directory in the VM,
   or attach a FAT/ISO9660 volume labelled `CIDATA` with `meta-data` at its root.
   Choose a target hostname different from the VM's current hostname. Keep the
   seed available through the reboot.
4. Stop the service and configure the existing ProgramData `wininit.yaml`:

   ```yaml
   version: 1
   plugins:
     datasources:
       nocloud:
         enabled: true
         seed_directory: 'C:\wininit-test-seed'
         discover_volumes: false
     provisioners:
       hostname:
         enabled: true
   reboot:
     policy: never
   ```

   For media discovery, use an empty `seed_directory` and
   `discover_volumes: true` instead.

5. Validate the configuration before starting the service.

For ConfigDrive, copy `tests/fixtures/configdrive/openstack` to the seed root
or attach FAT/ISO9660 media labelled `config-2`. Replace the `nocloud` settings
above with `nocloud.enabled: false` and a `configdrive` block containing
`enabled: true`, `seed_directory: 'C:\wininit-test-seed'`, and
`discover_volumes: false`. Use `hostname` in `meta_data.json` as the target;
the FQDN's first label is the requested Windows name.

## Rename and resume

1. Start the service. Inspect `status --json`: the instance ID must match the
   seed, and hostname status must become `reboot_required`. The VM must remain
   running without an automatic restart.
2. Restart only the wininit service. The active hostname remains the old name,
   the configured name remains the target, and the structured provisioner log
   must report `changed=false, reboot_required=true`.
3. Reboot Windows through the VM administrator's normal mechanism.
4. After startup, verify the active hostname is the target. The automatic
   wininit service must report provisioning and hostname status `completed`,
   with no new rename or restart request.
5. Restart the service again and confirm idempotent `changed=false` behavior.

## Disablement and failures

- Before another test rename, disable `hostname`, restart the service, and
  verify the seed can be discovered but neither active nor configured name
  changes.
- Disable the selected datasource while leaving hostname enabled and the other
  datasource disabled, then restart. Verify
  `no_datasource` and no hostname operation.
- Disable all plugins while a reboot checkpoint exists. Restart the service
  and verify all descriptors say `disabled`; the agent must not reconcile old
  work. A rename already accepted by Windows remains queued by Windows and is
  not undone by changing the wininit configuration.
- Attach two supported CIDATA volumes and verify an ambiguity failure without
  a hostname change. Remove one and explicitly restart the service to retry.
- Repeat media ambiguity testing with two `config-2` volumes. Then enable both
  NoCloud and ConfigDrive with valid seeds and verify a source conflict without
  a hostname change; disable one source and restart to select the other.
- Remove the seed before resume and verify `no_datasource`; restore it and
  restart to resume from fresh metadata.
- Test an invalid hostname and verify durable `failed` status before any
  hostname mutation.

Collect sanitized diagnostics, MSI logs, OS edition/build, hypervisor/version,
source commit, and results for each case. Restore the snapshot after testing.
Do not commit Windows media, credentials, license keys, or raw user-data.

## Separate PowerShell acceptance

Use a disposable guest and the exact candidate artifacts/checksums. The
PowerShell procedure is independent of the hostname procedure above and does
not need a rename, local-account/password changes, or OpenSSH reconfiguration.

1. Record OS/build, boot time, hostname, existing service/configuration/state,
   and candidate commit. Refuse to replace an unrelated installation.
2. Install the MSI with `/qn /norestart`. Verify all seven plugins are disabled
   and neither separate plugin EXE was installed by MSI. Install the reviewed
   PowerShell EXE separately under protected SYSTEM/Administrators ownership.
3. Use an isolated Registry path, log directory and local NoCloud seed. Enable
   only the selected datasource and PowerShell with exact script/EXE hashes,
   `allow_privileged: true`, and a new execution ID. A benign script appends
   one byte to a test-owned counter and exits 0. Verify one append and numeric
   success. Restart the service, remove the payload, and verify cached success
   without another append.
4. Verify a wrong script pin cannot create the counter. Verify a terminating
   error appends once, remains blocked across service restart, and runs again
   only after a new execution ID. Exercise bounded output and timeout, SCM stop
   cancellation, and confirm native descendants are gone. Repeat source reading
   with ConfigDrive. Inspect state/logs/diagnostics for fixture payload/output.
5. Run a benign counter script ending with `exit 3010`. Verify no automatic
   reboot, `reboot_required`, and unchanged boot time. Restart only the service:
   the request remains pending and the counter stays unchanged.
6. **Only if the guest administrator explicitly authorizes reboot for this run**,
   reboot once. Wait for the guest to return (it may take ten minutes), confirm
   a changed OS boot time, automatic service start, `completed`, `reused: true`,
   unchanged counter and original journal 3010 evidence. Restart the service
   again and verify no replay. Never simulate this by deleting BootSession.
7. Collect sanitized evidence and uninstall the test-owned MSI. Remove only
   test-created configuration, seed, plugin, log and Registry resources; restore
   any saved administrator-owned state. Confirm SSH access still works.

Record actual runs separately with their scope and limitations. A cross-build,
hosted runner or simulated boot transition is not a guest reboot acceptance run.

The [2026-11-06 PowerShell run](2026-11-06-powershell-user-data.md) records
LocalSystem local-seed acceptance with an explicitly authorized real reboot.
It does not execute the hostname-rename procedure above.

## Metadata SDK acceptance without reboot

For the separate NoCloud/hostname EXEs, record their exact source/checksums and
original active/configured name and boot time. Use an isolated state key and
protected reviewed files. Enable external NoCloud with a local seed and external
hostname with privileged approval, targeting the name already configured by
Windows. Require a completed lifecycle, unchanged names and no reboot request.
Restart only the service and require a higher attempt counter plus another
completed reconciliation. Test wrong image pins and disabled plugins with
missing EXEs. Verify failed `service start` returns an error promptly. Remove
only owned service/configuration/state/files and recheck names/boot/SSH access.
Do not turn this procedure into a real rename/reboot test without authorization.

The [2026-11-07 metadata SDK run](2026-11-07-metadata-sdk.md) records native
process, LocalSystem lifecycle and cleanup results on the existing Server 2025
guest; its scope is separate from the full release matrix.
