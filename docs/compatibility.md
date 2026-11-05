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
| NoCloud datasource | Planned |
| OpenStack ConfigDrive datasource | Planned |
| Metadata network service | Not in foundation scope |

## Provisioning

| Capability | Status |
| --- | --- |
| Hostname | Planned next milestone |
| Users and groups | Planned |
| Password policy | Planned |
| SSH authorized keys | Planned |
| PowerShell user-data | Planned |
| Reboot/resume provisioning | Planned |
| External plugins | Planned; no stable protocol yet |

## Artifacts

The foundation release target is an unsigned Windows AMD64 executable and MSI.
The MSI is intended for per-machine unattended installation and upgrade. Code
signing, provenance, and SBOM publication are release-hardening roadmap items.
