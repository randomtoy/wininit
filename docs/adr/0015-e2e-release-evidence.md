# ADR 0015: Repeatable no-reboot acceptance and release evidence

- Status: Accepted
- Date: 2026-11-07

## Context

Roadmap stage 7 needs repeatable guest evidence and an auditable distribution.
Only an existing Server 2025 SSH guest is available, without reboot permission.
Server 2022 images, OpenStack access and a production signing certificate are
not available. Existing one-off reports cannot substitute for the full matrix.

## Decision

Add a PowerShell 5.1 guest suite that refuses existing installations, installs a
baseline MSI, upgrades to a candidate, exercises separate pinned plugins under
LocalSystem, tests replay/failure/disablement and uninstalls. It checks installed
ACLs, preserved configuration/Registry sentinel, machine-wide contention and
unchanged boot/name. Own and clean only initially absent fixture resources.
CI runs the same suite with a same-source lower-version MSI. A manually
dispatched, four-cell self-hosted workflow binds artifacts to successful push
CI for the exact reviewed source; it never runs on pull-request events.
Guest provisioning, media attachment, rename/reboot and historical-release
migration remain separate acceptance gates, not synthetic passing results.

Generate per-EXE CycloneDX module inventories with a pinned upstream build-time
tool, validate them against Go binary build information, and bind exact bytes
with a versioned manifest and twelve-entry checksum set. Keep builder tooling
outside the agent dependency graph. Optional signing selects an existing
certificate provider, signs EXEs before MSI and then MSI, verifies timestamped
signatures, and generates metadata last. No production signing claim is made
without actual certificate validation.

Trusted-tag jobs attest the checksum manifest's provenance and each EXE's
SBOM with pinned GitHub Actions and minimal OIDC permissions after validation.
Attach verification bundles, then prepare a draft prerelease. Automatic stable
publication is premature while matrix/signing evidence remains incomplete.

## Consequences

Tests are repeatable without rebooting the supplied host or changing SSH,
passwords, account policy or hostname. Reports explicitly name skipped cases.
An upgrade fixture validates MSI mechanics but not historical state migration.
SBOMs describe Go module inventories, not a complete installer/OS/license audit.
Checksums require an authenticated attestation for publisher trust. Actual
production signing and tag-triggered attestations still need execution before
stable release readiness can be claimed.
