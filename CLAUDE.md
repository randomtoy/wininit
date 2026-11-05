# Project instructions

## Project mission and current scope

`wininit` is an independent Go implementation of a Windows initialization
agent for Windows Server 2022 and 2025 guests on QEMU/KVM and OpenStack. The
foundation consists of one Windows Service/CLI executable, a small lifecycle
engine, versioned configuration, Registry-backed persistent state,
diagnostics, Windows packaging, and CI.

No datasource or provisioner is implemented in the foundation. Do not claim
that a VM was provisioned merely because the service started or a lifecycle
checkpoint was saved.

## Mandatory maintenance rule

Every change to architecture, public or internal interfaces, workflows,
packaging, build commands, state layout, security policy, or development
conventions must update this file in the same change.

## Architectural principles

- Prefer simple, explicit components and the Go standard library.
- Keep implementation details under `internal`. Add packages under `pkg` only
  for stable contracts that external consumers need.
- Define small interfaces at the point of consumption.
- Propagate `context.Context` cancellation across application, lifecycle,
  state, and platform boundaries.
- Keep Windows-specific code behind Windows build constraints and provide only
  truthful portable behavior on other platforms.
- Distinguish SCM service state, agent lifecycle state, and future provisioning
  state in code, logs, status output, and documentation.
- Do not expose a public plugin protocol until its lifecycle and threat model
  are stable.

## Directory structure

```text
cmd/wininit/              executable entry point
internal/app/             composition and command execution
internal/config/          versioned configuration and validation
internal/diagnostics/     sanitized diagnostic archive creation
internal/lifecycle/       lifecycle engine and valid transitions
internal/service/         SCM/console hosting and administration
internal/state/           state model and storage implementations
internal/version/         embedded build information
internal/windows/         other Windows platform adapters, when needed
packaging/wix/            WiX MSI source and pinned tooling
scripts/                  build and packaging helpers
tests/                    integration, fixtures, and future E2E tests
docs/                     architecture, operations, and ADRs
.github/workflows/        CI and release workflows
```

Do not create empty directories to imply unimplemented functionality.

## Go conventions

- Use the Go version declared in `go.mod` (currently Go 1.27.3). The module file
  is authoritative.
- Format all Go sources with `gofmt`.
- Wrap errors with useful operation context and preserve the underlying error.
- Avoid global mutable state. Build information set by linker flags is the
  narrow exception.
- Prefer table-driven tests where they clarify multiple cases.
- Use temporary directories and unique Registry test keys. Tests own and clean
  up every resource they create.
- Never add a fake implementation or a test which passes without exercising
  the behavior named by the test.

## Build and test commands

```text
make build       build the Windows AMD64 executable
make test        run unit tests
make lint        validate formatting and run static analysis
make package     build release EXE, MSI, and checksums on Windows
go test ./...    run tests directly
go vet ./...     run Go static analysis directly
```

MSI construction, Registry integration, SCM integration, and silent installer
smoke tests require Windows. Cross-compiling the EXE does not validate native
Windows behavior.

## Configuration rules

- Configuration has an explicit schema version. Reject unsupported versions
  and invalid values with actionable field-level errors.
- Reject unknown fields unless a documented compatibility decision changes
  this rule.
- Defaults must be safe and must not enable unimplemented capabilities.
- Installer upgrades must not overwrite administrator-owned configuration.
- Never log, persist, or archive passwords, tokens, private keys, or raw
  user-data. Add redaction tests when a new sensitive field is introduced.

## Lifecycle and Registry state rules

- Production lifecycle state is stored in the Windows Registry, never SQLite
  or another database.
- The state schema is versioned. Reject unknown future schemas rather than
  guessing their meaning.
- A save must write and validate a complete versioned checkpoint before
  changing the active checkpoint marker. A load may recover only from a
  complete, validated prior checkpoint.
- State-store implementations own the monotonically increasing checkpoint
  generation; callers must not choose or decrement it.
- Preserve enough error metadata for diagnosis without storing untrusted or
  secret payloads.
- Do not store credentials, password hashes, private keys, access tokens, or
  raw metadata/user-data in lifecycle state.
- Tests use the memory store or an isolated Registry subtree. They must not
  touch the production Registry key.
- Do not expose the Registry store directly to future external plugins.

## Windows service rules

- Report correct SCM pending, running, stop-pending, and stopped states.
- Accept stop and system shutdown controls and propagate cancellation.
- Avoid polling and busy loops; wait on cancellation or owned work.
- Write structured, non-sensitive logs.
- Service management errors must be visible to the caller and SCM.
- MSI releases use MSI-native service installation and control. Direct CLI
  service installation is for development and manual deployment, not for
  mutating an MSI-managed installation.

## Plugin design constraints

Datasource, provisioner, and external executable plugin layers are planned,
not part of the foundation's stable API. A future external protocol must have:

- an explicit, versioned handshake and protobuf/gRPC contract;
- plugin identity and checksum verification;
- bounded execution time and cancellation;
- deliberate least-privilege process credentials and filesystem access;
- controlled, redacted logs; and
- compatibility and deprecation policy.

Never execute an arbitrary plugin with inherited LocalSystem privileges by
default.

## Security constraints

- Treat metadata, user-data, configuration, plugin output, filenames, archive
  entries, and Registry contents as untrusted.
- Apply least privilege and restrictive ACLs where Windows permits it.
- Do not disable certificate or integrity checks to make a test pass.
- Diagnostics must use an allowlist and sanitized configuration, not an
  indiscriminate copy of data directories.
- Do not commit credentials, tokens, private keys, Windows media, or license
  keys.

## Packaging rules

- Build Windows AMD64 first; ARM64 support remains preparatory until validated.
- Pin WiX Toolset 7.0.1 and its extensions exactly until a reviewed dependency
  update changes the project. Keep the required `<AcceptEula>wix7</AcceptEula>`
  acknowledgement. Builders and redistributors remain responsible for
  reviewing and complying with WiX OSMF terms.
- Use MSI-native service registration and control with stable component GUIDs
  and a stable UpgradeCode.
- Map release versions deterministically to MSI-compatible numeric versions.
- Write `SHA256SUMS` as UTF-8 without a byte-order mark and with LF line
  endings so standard Windows and Unix verification tools agree.
- Preserve operational state and administrator-owned configuration through
  upgrades and ordinary uninstall.
- ProgramData currently inherits Windows Installer-created system ACLs;
  explicitly review and harden ACLs before adding plugins or unprivileged
  writers.
- Do not add the install directory to the machine-wide `PATH`.
- Unsigned artifacts are acceptable only while the MVP documentation states
  this limitation. Never describe unsigned artifacts as trusted.

## CI/CD instructions

- CI must verify dependencies, formatting, vetting, tests, race tests, and the
  Windows AMD64 build.
- Native Windows CI must exercise isolated Registry tests and MSI validation.
  It must check elevation explicitly before tests which require SCM or MSI
  administration and report the documented outcome; it must not fabricate a
  successful smoke test.
- Release jobs run only for trusted version tags, use minimal token
  permissions, produce EXE/MSI/checksums, and publish those exact artifacts.
- Pin third-party actions to immutable commits where practical and keep them
  current through dependency automation.
- The Windows jobs currently use `actions/setup-dotnet` 6.1.0 to install the
  .NET 10 SDK used by the pinned WiX build; action-major updates require a full
  Windows MSI smoke run.
- A GitHub-hosted Windows runner does not prove Windows Server 2022 or 2025 VM
  compatibility.

## Pull request requirements

- Keep the change focused and explain user-visible behavior.
- Add or update meaningful tests.
- Update documentation and ADRs when a decision changes.
- Update this file for any architectural or workflow change.
- Run relevant local checks and report Windows-only checks which could not be
  run.
- Preserve unrelated work and never commit generated secrets or private data.

## Definition of done

A change is done only when:

- relevant unit and integration tests pass;
- `gofmt` and `go vet` pass;
- the Windows AMD64 executable still builds;
- Windows/MSI behavior is validated in an appropriate environment or clearly
  reported as unverified;
- failure paths are visible and do not claim false success;
- state, logs, and diagnostics contain no sensitive payloads;
- user-facing and architectural documentation describes actual behavior; and
- this file reflects every architecture, interface, workflow, build,
  packaging, security, or convention change.
