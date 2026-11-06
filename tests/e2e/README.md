# Datasource and hostname VM acceptance

This procedure is pending execution. It requires a disposable, licensed,
non-domain-joined Windows Server 2022 or 2025 AMD64 VM. Hosted CI does not run a
real rename or reboot. Do not run this procedure on a workstation, production
VM, domain member, or shared CI host.

## Prepare

1. Take a VM snapshot and install the candidate MSI with verbose logging.
2. Record the original active hostname and verify all three plugins are disabled in
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
