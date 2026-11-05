# Development

## Prerequisites

- Go 1.27.3; `go.mod` is authoritative
- Git
- GNU Make for convenience targets
- Windows for Registry/SCM integration and MSI creation
- WiX Toolset 7.0.1 through the repository-pinned .NET SDK project for MSI
  creation

Go and documentation work can be done on Windows, Linux, or macOS. A successful
Windows cross-build does not replace native Windows testing.

## Common commands

```bash
go mod verify
go test ./...
go vet ./...
make lint
make test
make build
```

`make build` produces `dist/wininit-windows-amd64.exe`. `make package` is the
release-style Windows target and produces the executable, MSI, and
`SHA256SUMS`. Consult the build script output if the local WiX dependency is
missing or has the wrong version.

Packaging also verifies that `SHA256SUMS` matches both artifacts and uses
portable LF line endings.

The WiX project contains `<AcceptEula>wix7</AcceptEula>`, the acknowledgement
required to use the WiX 7 build. Every builder and redistributor is independently
responsible for reviewing and complying with the current WiX Open Source
Maintenance Fee (OSMF) terms; the repository setting is not legal advice and
does not discharge that responsibility.

## Running in the foreground

Use foreground mode while developing lifecycle behavior:

```powershell
.\dist\wininit-windows-amd64.exe validate
.\dist\wininit-windows-amd64.exe run
```

Stop foreground mode with Ctrl+C. The same root-context cancellation path is
used by SCM stop and shutdown controls.

## Native Windows integration tests

Registry tests create a unique key beneath a dedicated test-only subtree and
delete it during cleanup. The current SCM lifecycle is exercised by the MSI
smoke test on a disposable runner after it verifies that no pre-existing
`wininit` service will be touched. Future direct SCM tests must use unique
service names. Never point a developer test at the production service or
`HKLM\SOFTWARE\wininit`.

Tests requiring administrative privileges must check the current token before
changing the Registry, SCM, or MSI state. The test suite must emit a clear,
policy-consistent skip when elevation is optional and fail when an explicitly
required release smoke test lacks elevation.

## MSI smoke testing

From an elevated Windows test environment:

```powershell
msiexec.exe /i .\dist\wininit-windows-amd64.msi /qn /norestart /l*v .\install.log
.\dist\wininit-windows-amd64.exe version
sc.exe query wininit
msiexec.exe /x .\dist\wininit-windows-amd64.msi /qn /norestart /l*v .\uninstall.log
sc.exe query wininit
```

The final query is expected to report that the service is absent. Tests must
also verify installed paths, UpgradeCode stability, ProductVersion mapping,
and preservation of configuration/operational state. A command which was not
run must not be reported as a successful smoke test.

## Build metadata and releases

Release builds inject a semantic version, Git commit, and reproducible build
date/source timestamp through linker flags. MSI ProductVersion accepts only
MSI-compatible numeric components, so the packaging script maps Git tags
deterministically and rejects versions it cannot represent safely.

Pushes of version tags matching `v*` invoke the release workflow. It validates
source, builds native artifacts, generates checksums, runs applicable tests,
and publishes a GitHub Release. MVP artifacts are unsigned.

## Diagnostics and troubleshooting

Use `wininit status` to inspect lifecycle state separately from SCM state, and
`wininit collect-logs` to create a sanitized archive. Review diagnostic archives
before sharing them. For MSI failures, retain the verbose `msiexec` log and the
workflow's uploaded failure diagnostics.

## Future VM tests

Packer/QEMU or equivalent automation will create ephemeral Windows Server
2022/2025 test guests from administrator-supplied media. The repository must
never contain or download copyrighted Windows installation media or license
keys.
