# Compatibility

This document distinguishes product targets from environments actually
verified by automated tests. A build result is not a compatibility claim.

## Operating systems

| Environment | Architecture | Status |
| --- | --- | --- |
| Windows Server 2022 | AMD64 | Product target; real-VM E2E pending |
| Windows Server 2025 | AMD64 | Identity, SSH, external ConfigDrive local-seed, SCM, and MSI guest checks passed; media/reboot E2E pending |
| GitHub-hosted `windows-2025` runner | AMD64 | Native CI; exact image/build is recorded by workflow logs |
| Windows on ARM64 | ARM64 | Future compatibility goal; not an MVP release target |
| Linux/macOS host | AMD64/ARM64 | Development, unit tests, and Windows cross-build only; no Windows runtime support |

CI deliberately pins GitHub's `windows-2025` label rather than the moving
`windows-latest` label. The hosted image can still change over time and does
not demonstrate support inside QEMU/KVM or OpenStack Windows Server 2025
guests. It also does not test the Windows Server 2022 target.

## Platforms and metadata

| Platform/capability | Status |
| --- | --- |
| QEMU/KVM guest operation | Partial Windows Server 2025 Q35 VM checks; full matrix and KVM acceleration not verified |
| OpenStack guest operation | Target; real-cloud testing planned |
| NoCloud explicit seed directory | Implemented; disabled by default |
| Mounted `CIDATA` ISO9660/FAT discovery | Implemented on Windows; disabled by default |
| OpenStack ConfigDrive local seed / mounted `config-2` FAT/ISO9660 | Implemented; disabled by default |
| Metadata network service | Not in foundation scope |

## Provisioning

| Capability | Status |
| --- | --- |
| Hostname | Implemented conservatively; disabled by default |
| Standard local users / initial passwords | Implemented; disabled by default; groups/domain/admin accounts excluded |
| Local SAM password/lockout policy | Implemented; disabled by default; domain-joined hosts refused |
| Per-user SSH managed key blocks | Implemented; disabled by default; existing-server login/revocation verified on a 2025 guest; sshd setup unmanaged |
| PowerShell user-data | Separate opt-in EXE; bounded hash-approved execution; real reboot matrix pending |
| Hostname restart checkpoint/reconciliation | Implemented; no automatic restart |
| External ConfigDrive | Experimental v1 gRPC process; opt-in; trusted binaries only |

NoCloud accepts `instance-id` and optional `local-hostname`; ConfigDrive accepts
`uuid` and optional `hostname`, with explicit FQDN-to-first-label normalization.
Supported ConfigDrive versions and exclusions are listed in
[Built-in plugins](plugins.md). Datasources do not open user-data; the separate
PowerShell provisioner requires its own explicit approval. Hostname mutation
accepts only the documented 1–15 character ASCII label and refuses a
domain-joined host.

Unit and native Windows integration tests do not yet establish full product
compatibility. Real Windows Server 2022/2025 guest validation of CIDATA/config-2
discovery, rename, administrator restart, and resume reconciliation is pending.
The [2026-11-06 PR 4 review](../tests/e2e/2026-11-06-pr4-review.md) records the
narrower SSH guest checks and their cleanup. No restart was authorized or run.

## Artifacts

The foundation release target is an unsigned Windows AMD64 executable and MSI.
The MSI is intended for per-machine unattended installation and upgrade. Code
signing, provenance, and SBOM publication are release-hardening roadmap items.
The optional ConfigDrive executable is a separate artifact in the same
repository and checksum manifest; MSI does not install it automatically.
