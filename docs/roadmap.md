# Roadmap

The roadmap is ordered by dependency and risk, not by promised dates.

## 1. Foundation

Deliver the service/CLI host, configuration, durable lifecycle state,
diagnostics, EXE/MSI packaging, CI, and truthful status reporting.

Exit criteria: the foundation release acceptance criteria in [MVP](mvp.md) are
verified and no provisioning capability is claimed.

## 2. NoCloud and first provisioner

Discover NoCloud media, parse and validate metadata, define the initial
internal datasource contract, and implement an idempotent hostname provisioner.

Exit criteria: repeated execution produces no unnecessary change; required
reboot/checkpoint/resume behavior survives process and VM restart; tests pass
against a real Windows guest.

## 3. OpenStack ConfigDrive compatibility

Add supported ConfigDrive layouts and OpenStack metadata normalization without
leaking datasource-specific details into provisioners.

Exit criteria: documented fixtures and real OpenStack/QEMU tests cover media
selection, malformed input, and datasource precedence.

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
