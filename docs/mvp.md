# Foundation MVP

## Objective

The foundation MVP provides a production-shaped host for later VM
initialization work. It proves service lifecycle, configuration, persistence,
diagnostics, packaging, and delivery without pretending that provisioning has
already happened.

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

Items are part of the MVP contract. A repository revision is release-ready only
when the corresponding tests and workflows pass; source presence alone is not
verification.

## Explicitly excluded

- metadata media discovery or parsing;
- NoCloud and OpenStack ConfigDrive datasource implementations;
- hostname, user, password, or SSH key changes;
- PowerShell user-data execution;
- provisioner idempotency and reboot orchestration;
- external executable plugin execution;
- a public protobuf/gRPC plugin API or Go SDK;
- real Windows Server 2022/2025 VM end-to-end verification; and
- production code signing.

The excluded work is tracked in the [roadmap](roadmap.md).

## Release acceptance

A foundation release requires passing Go validation, Windows cross-build, native
Windows state tests, MSI construction/validation, and the documented elevated
smoke tests when the runner permits them. Any skipped native check must be
visible in release notes and must not be described as successful.
