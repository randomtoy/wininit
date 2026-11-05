# ADR 0004: Use one executable for Windows Service and CLI modes

- Status: Accepted
- Date: 2026-11-05

## Context

The agent needs SCM hosting, foreground diagnosis, status, validation,
diagnostics, version, and service administration. Separate executables would
duplicate versioning, packaging, and composition.

## Decision

Use one `wininit.exe`. It detects SCM execution through the supported Windows
service API and exposes explicit foreground and one-shot CLI commands. Both SCM
and foreground execution share application composition and context-based
cancellation.

MSI owns service registration for MSI installations. Direct CLI installation
and removal support development and manual deployments only.

## Consequences

There is one artifact and one set of embedded build metadata. Command parsing
must remain distinct from SCM dispatch, and administration commands must avoid
corrupting MSI product ownership.

## Alternatives considered

- Separate service and CLI binaries: rejected because it adds packaging and
  version-skew costs without a current security boundary benefit.
- PowerShell service wrapper: rejected because a native service host provides
  clearer lifecycle and fewer runtime dependencies.

## References

- [Go Windows service package](https://pkg.go.dev/golang.org/x/sys/windows/svc)
- [Go Windows Service Manager package](https://pkg.go.dev/golang.org/x/sys/windows/svc/mgr)
- [Microsoft Windows service sample](https://github.com/microsoft/Windows-classic-samples/tree/main/Samples/Win7Samples/winbase/service)
