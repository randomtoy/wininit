# ADR 0007: Reproducible artifacts and explicit version mapping

- Status: Accepted
- Date: 2026-11-05

## Context

Go release metadata, semantic version tags, and MSI ProductVersion have
different constraints. Users need to identify an executable and verify that
the MSI and checksum came from the same source revision.

## Decision

Build Windows AMD64 first with deterministic Go flags where practical. Inject
version, commit, and build timestamp/source date through linker flags. Map
accepted release tags deterministically to MSI-compatible numeric versions and
reject ambiguous or out-of-range input. Produce the EXE, MSI, and SHA-256
checksums from the release workflow. Do not use UPX.

MVP artifacts are unsigned and documented as such. Signing, provenance, and
SBOM publication remain release-hardening work.

## Consequences

Release scripts become the authoritative version conversion path and require
tests. Rebuilding with the same source date and inputs should minimize
unexplained differences, though MSI/toolchain metadata may require additional
normalization work.

## Alternatives considered

- Use the Git tag verbatim as ProductVersion: rejected because MSI accepts a
  narrower numeric format.
- Omit build metadata: rejected because incident diagnosis and artifact/source
  correlation would suffer.
