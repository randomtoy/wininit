# Architecture decision records

ADRs record durable choices and their tradeoffs. `Accepted` means the current
foundation is expected to implement the decision. `Proposed` describes future
direction and is not a compatibility promise.

| ADR | Status | Decision |
| --- | --- | --- |
| [0001](0001-project-scope-and-boundaries.md) | Accepted | Independent foundation and component boundaries |
| [0002](0002-registry-lifecycle-state.md) | Accepted | Versioned lifecycle state in the Windows Registry |
| [0003](0003-versioned-yaml-configuration.md) | Accepted | Strict, versioned YAML configuration |
| [0004](0004-single-binary-service-and-cli.md) | Accepted | One executable for service and CLI modes |
| [0005](0005-wix-msi-packaging.md) | Accepted | MSI-native service lifecycle with WiX |
| [0006](0006-external-plugin-direction.md) | Proposed | Separate-process versioned plugin protocol |
| [0007](0007-build-release-and-versioning.md) | Accepted | Reproducible artifacts and MSI version mapping |
| [0008](0008-windows-testing-strategy.md) | Accepted | Layered Windows validation and future VM E2E |

## Template

New ADRs contain: title, status, date, context, decision, consequences, and
alternatives considered. Supersede an accepted ADR instead of rewriting its
history after release.
