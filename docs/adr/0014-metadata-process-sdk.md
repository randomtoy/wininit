# ADR 0014: Metadata process SDK and separate NoCloud/hostname executables

- Status: Accepted
- Date: 2026-11-07
- Extends: ADRs 0006, 0012, 0013

## Context

Roadmap stage 6 requires process contracts for both datasources and provisioners.
ConfigDrive v1 and PowerShell userdata v1 already exist, but datasource identity
was fixed to ConfigDrive and one-shot scripts are unsuitable for generic
idempotent reconciliation. New plugins must be separate EXEs, independently
opt-in and disabled by default. Guest validation for this change excludes reboot.

## Decision

Extend datasource v1 with named SDK registration, preserving ConfigDrive's old
entry point and protobuf wire. Add a separate metadata-only provisioner v1 with
Describe/Apply, explicit idempotent semantics and a closed outcome enum. Keep
state, machine lock and configuration enablement in the host. Do not expose
arbitrary configuration/credentials or allow scripts onto this retryable API.

Ship separate NoCloud and hostname reference EXEs. Existing v1 files keep
builtin modes unless external mode is explicit. External hostname requires
additional privileged approval and a pinned EXE. Failure never falls back to
builtin behavior. Reuse reviewed pinned-image, mTLS, deadline, redaction,
suspended-start and cleanup paths. NoCloud uses the read-only restricted role;
hostname uses elevated caller identity with shutdown privileges removed, a
private high-integrity desktop and a one-process/256 MiB kill-on-close job.
PowerShell's process role and durable one-shot contract stay distinct.

Keep source and SDK together while experimental, with separate artifacts and
no MSI payload changes. Version compatibility and conformance tests are the
boundary for a later repository split. Add an external-module SDK build test.
No new stable API or automatic arbitrary-plugin registration is promised.

## Consequences

Operators can replace reviewed metadata plugins independently of the agent.
Provider authors must make Apply idempotent and context-aware. Trusted elevated
code retains administrative power; the job is not hostile-code isolation.
Checksum pins and explicit privileged approval remain necessary. The existing
seven logical plugin identities/status entries remain unchanged.

Native tests verify unchanged hostname reconciliation and role limits without
renaming shared CI hosts. This guest run must not reboot or mutate hostname;
real rename/reboot, attached media and full Server 2022/2025 release acceptance
remain roadmap stage 7 work.

## Alternatives considered

- Reuse userdata v1: its leases/journal authorize one-shot scripts, which have
  different replay semantics.
- Generic Any/JSON provisioning requests: would expand sensitive configuration
  and mutation authority beyond the reviewed metadata contract.
- Replace existing builtin defaults: breaks deployed v1 configuration.
- Separate repositories now: adds release synchronization before a mature
  compatibility/conformance boundary exists.
