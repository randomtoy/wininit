# Foundation and first provisioning milestone

## Objective

The foundation provides the production-shaped host. The next milestone adds a
narrow, independently opt-in NoCloud-to-hostname pipeline without expanding
into general cloud-init behavior.

## Included

- [x] Go module and documented component boundaries
- [x] Single Windows Service and CLI executable design
- [x] Foreground execution and cancellation-aware lifecycle
- [x] Versioned configuration with validation and safe defaults
- [x] Versioned Registry state model with recovery strategy
- [x] In-memory state store for unit tests
- [x] Status that separates service and lifecycle/provisioning concepts
- [x] Sanitized diagnostic archive design
- [x] Reproducible Windows AMD64 EXE target
- [x] WiX-based MSI definition with native service management
- [x] CI and tag-based release workflows
- [x] Architecture, operations, security, and contribution documentation
- [x] Internal built-in `Datasource` and `Provisioner` boundaries
- [x] Independently disabled-by-default NoCloud and hostname configuration
- [x] NoCloud `instance-id` and optional `local-hostname` parsing
- [x] Explicit seed folder and unambiguous mounted `CIDATA` discovery
- [x] Conservative, domain-aware, idempotent hostname provisioner
- [x] State schema version 1 to version 2 migration
- [x] Durable `reboot_required` checkpoint with no automatic restart
- [x] Per-plugin enabled/disabled/runtime status

Items are part of the milestone contract. A repository revision is
release-ready only when the corresponding tests and workflows pass; source
presence alone is not verification. Real-VM validation remains explicitly
pending.

## Explicitly excluded

- OpenStack ConfigDrive and network metadata sources;
- metadata keys beyond required `instance-id` and optional `local-hostname`;
- users, passwords, or SSH keys;
- PowerShell user-data execution;
- automatic restart orchestration;
- external executable plugin execution;
- a public protobuf/gRPC plugin API or Go SDK;
- real Windows Server 2022/2025 VM end-to-end verification; and
- production code signing.

The excluded work is tracked in the [roadmap](roadmap.md).

## Release acceptance

A release requires passing Go validation, Windows cross-build, configuration
gating and state migration tests, NoCloud/hostname tests, native Windows state
tests, MSI construction/validation, and the documented elevated smoke tests
when the runner permits them. Any skipped native or real-VM check must be
visible in release notes and must not be described as successful.
