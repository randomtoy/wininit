# Roadmap

The roadmap is ordered by dependency and risk, not by promised dates.

## 1. Foundation

Deliver the service/CLI host, configuration, durable lifecycle state,
diagnostics, EXE/MSI packaging, CI, and truthful status reporting.

Exit criteria: the foundation release acceptance criteria in [MVP](mvp.md) are
verified and no provisioning capability is claimed.

## 2. NoCloud and first provisioner

Implement independently opt-in built-in NoCloud and hostname plugins. Support
an explicit seed directory or unambiguous mounted `CIDATA` ISO9660/FAT volume,
required `instance-id`, optional `local-hostname`, conservative domain-aware
rename, and a durable administrator-managed restart checkpoint. Do not open or
execute user-data.

Implementation status: code and automated component/native integration tests
are in this milestone. Both plugins remain disabled by default and external
plugins remain deferred.

Remaining exit criterion: repeated execution and reboot/checkpoint/resume must
be validated against real Windows Server 2022 and 2025 guests. Until that is
complete, the compatibility matrix keeps real-VM support pending.

## 3. OpenStack ConfigDrive compatibility

Add supported OpenStack ConfigDrive layouts and metadata normalization without
leaking datasource-specific details into provisioners. Preserve independent
datasource enablement and fail on ambiguous selection.

Implementation status: the opt-in ConfigDrive plugin supports local seeds and
mounted `config-2` FAT/ISO9660 volumes, versioned JSON, `uuid`, and normalized
`hostname`. Fixture tests cover malformed input, disablement, fallback, source
conflicts, and simulated hostname resume. No source precedence is guessed.

Remaining exit criteria: validate media attachment, discovery, rename, and
restart/resume against real OpenStack/QEMU Windows guests. That environment is
not currently available. Networking, EC2 layouts, injected files, and user-data
are outside this milestone.

## 4. Identity and access provisioning

Add users, password policy, and SSH authorized keys through idempotent Windows
platform adapters.

Exit criteria: secrets are never placed in state, logs, or diagnostics; ACLs
and rerun behavior are tested.

## 5. PowerShell user-data

Add bounded PowerShell execution with explicit policy, durable results, output
redaction, and restart/reboot semantics.

Exit criteria: threat model and administrative controls are documented and
malicious-input tests cover command, path, and output boundaries.

## 6. External plugin protocol and Go SDK

After the internal interfaces stabilize, define a versioned protobuf/gRPC
handshake and Go SDK for datasource and provisioner processes.

Exit criteria: compatibility policy, checksum/identity verification,
least-privilege execution, deadlines, cancellation, process cleanup, and secret
redaction are enforced and tested.

## 7. End-to-end matrix and release hardening

Automate QEMU/KVM and OpenStack tests for Windows Server 2022/2025, then add
code signing, artifact provenance, and SBOM publication.

Exit criteria: clean-image install, initialization, reboot/resume, upgrade,
uninstall, and failure recovery are exercised without distributing Windows
media or license keys.
