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

The datasource, hostname, and identity packages are independently testable behind
small internal interfaces. Tests must cover default-disabled and explicitly
disabled behavior, including a state checkpoint from an older enabled run.
Configuration tests must prove that older version 1 files keep newly introduced
plugins disabled. State tests must cover deterministic schema 1/2 to schema 3
migration without inventing script execution evidence.

Lifecycle tests must also verify one reconciliation per process start, no
automatic retry loop, configuration-hash gating of saved status, fresh metadata
after restart, and disablement precedence over pending work. State-store tests
cover active-marker recovery, fail-closed future-schema behavior, and durable
Registry save behavior. Windows lock tests cover protected private-namespace
creation, protected permissions, contention, idempotent release from another
goroutine, reacquisition, and recovery after a helper process abandons
ownership. MSI smoke testing also starts a competing console lifecycle while
the installed LocalSystem service runs and requires a clear contention error.
An additional multi-process regression test verifies that a namespace creator
can exit while another process owns the mutex without admitting a split lock.
These Windows-specific checks execute in the elevated native CI job.

## Common commands

```bash
go mod verify
go test ./...
go vet ./...
make lint
make test
make build
```

ConfigDrive fixture/parser/discovery tests run with the normal suite. A bounded
parser fuzz check can also run on a development host:

```bash
go test ./plugins/datasource/configdrive -run '^$' -fuzz '^FuzzParse$' -fuzztime 15s
```

Datasource tests cover missing, malformed, duplicate, oversized, and symlinked
metadata; dated-version selection; FQDN normalization; source ambiguity; and
disabled execution. Windows tests enumerate labels without mounting media or
changing a hostname. MSI smoke verifies all seven logical plugins remain
disabled and installed ProgramData/configuration DACLs are protected.

`make build` produces `dist/wininit-windows-amd64.exe`, the separate
`dist/wininit-plugin-configdrive-windows-amd64.exe`, and
`dist/wininit-plugin-powershell-windows-amd64.exe`. `make package` is the
release-style Windows target and produces all three executables, MSI, and
`SHA256SUMS`. Consult the build script output if the local WiX dependency is
missing or has the wrong version.

Packaging also verifies that `SHA256SUMS` matches all six artifacts and uses
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

Identity integration tests create a uniquely named standard local account and
profile and delete both, verify password preservation and SSH ACL/rerun/
revocation, reject unsafe paths/secret ACLs, and round-trip SAM policy with exact
raw restoration in cleanup. Run `go test -tags=integration ./...` only on an
elevated disposable Windows host. Standard process tests execute real helper
binaries, including native token/job restrictions; they require elevation on
Windows. Portable process tests validate lifecycle/RPC behavior without
claiming Windows isolation.

The protobuf sources/generated Go files are checked in. To regenerate with
protoc 33.4 and pinned generators:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative sdk/datasource/v1/datasource.proto sdk/userdata/v1/userdata.proto
```

Ensure the Go binary directory is on PATH. Keep wire changes compatible within
v1 or introduce another version. [External plugins](external-plugins.md)
describes the contract and trust limits.

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

Volume-discovery tests use controlled fakes plus a native read-only test which
enumerates supported local volume labels and filesystem types without opening
volume contents or mutating them. Windows hostname tests must not rename a
shared CI host. Tests of the actual rename/restart path belong in disposable,
licensed Windows Server guests.

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
2022/2025 test guests from administrator-supplied media. It must validate
mounted CIDATA/config-2 discovery, hostname mutation, no automatic restart, an
administrator-initiated restart, and active-name reconciliation. This real-VM
validation is pending; the repository does not claim that a VM restart has been
performed or tested. The repository must never contain or download
copyrighted Windows installation media or license keys.

Follow the concrete [datasource and hostname VM acceptance procedure](../tests/e2e/README.md)
only on a disposable, licensed, non-domain-joined test VM. The procedure is
currently pending and unexecuted because no suitable VM is available. Record
the OS build, hypervisor, source commit, logs, and every acceptance result when
it is eventually run; do not convert the written procedure itself into a claim
of successful rename or restart validation.

## Separate PowerShell process

`make build` emits the PowerShell EXE separately. CI runs
`scripts/check-plugin-boundary.sh` to prevent its executor from entering the
agent dependency graph. Native Windows tests launch the actual OS interpreter;
portable tests cover source/envelope validation, RPC redaction and durable
one-shot recovery. See [operator policy](powershell-user-data.md) and ADR 0013.
Never reboot a guest unless that specific acceptance run authorizes it.

## Metadata process SDK

NoCloud and hostname have optional separate EXEs alongside ConfigDrive and
PowerShell. MSI installs only the agent; release/checksum artifacts include all
five EXEs and the MSI. Builtin NoCloud/hostname remain compatible defaults, all
plugins remain disabled by default, and external hostname requires privileged
approval. See [metadata SDK](external-sdk.md) for protocol, privilege, compatibility and test boundaries.
