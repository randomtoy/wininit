# Project instructions

## Goal

Build a production-quality Windows initialization agent for Windows Server 2022 and 2025. The initial release establishes a service, CLI, lifecycle engine, Registry-backed state, diagnostics, packaging, and CI without claiming provisioning support.

## Mandatory maintenance rule

Every architectural, interface, workflow, build, or development-convention change must update this file in the same change.

## Foundations

- Use idiomatic Go and prefer the standard library.
- Keep implementation details under `internal`; expose only stable reusable contracts under `pkg`.
- Persist production lifecycle state in the Windows Registry, never a database.
- Do not store credentials, private keys, tokens, or raw user-data.
- Treat metadata and plugins as untrusted inputs.
- Keep implemented and planned behavior clearly distinct.

## Commands

The project will provide `make build`, `make test`, `make lint`, and `make package`. Until the implementation lands, use `go test ./...` for available Go validation.

## Definition of done

A change is done when relevant tests pass, Windows cross-builds remain valid, documentation describes actual behavior, security-sensitive data is excluded, and this file reflects any architectural or workflow change.
