# Compatibility

This document distinguishes product targets from environments actually
verified by automated tests. A build result is not a compatibility claim.

## Operating systems

| Environment | Architecture | Status |
| --- | --- | --- |
| Windows Server 2022 | AMD64 | Product target; real-VM E2E pending |
| Windows Server 2025 | AMD64 | Product target; real-VM E2E pending |
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
| QEMU/KVM guest operation | Target; real-VM testing planned |
| OpenStack guest operation | Target; real-cloud testing planned |
| NoCloud explicit seed directory | Implemented; disabled by default |
| Mounted `CIDATA` ISO9660/FAT discovery | Implemented on Windows; disabled by default |
| OpenStack ConfigDrive datasource | Planned |
| Metadata network service | Not in foundation scope |

## Provisioning

| Capability | Status |
| --- | --- |
| Hostname | Implemented conservatively; disabled by default |
| Users and groups | Planned |
| Password policy | Planned |
| SSH authorized keys | Planned |
| PowerShell user-data | Planned |
| Hostname restart checkpoint/reconciliation | Implemented; no automatic restart |
| External plugins | Planned; no stable protocol yet |

NoCloud currently accepts required `instance-id` and optional
`local-hostname` only. User-data is not opened or executed. Hostname mutation
accepts only the documented 1–15 character ASCII label and refuses a
domain-joined host.

Unit and native Windows integration tests do not yet establish full product
compatibility. Real Windows Server 2022/2025 guest validation of CIDATA
discovery, rename, administrator restart, and resume reconciliation is pending.

## Artifacts

The foundation release target is an unsigned Windows AMD64 executable and MSI.
The MSI is intended for per-machine unattended installation and upgrade. Code
signing, provenance, and SBOM publication are release-hardening roadmap items.
