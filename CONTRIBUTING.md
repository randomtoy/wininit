# Contributing to wininit

Thank you for helping build `wininit`. The project is intentionally conservative
about Windows system changes: a false provisioning success or unsafe installer
is worse than an explicit failure.

## Before starting

Read:

- [Project instructions](CLAUDE.md)
- [Architecture](docs/architecture.md)
- [Development guide](docs/development.md)
- [Security model](docs/security.md)
- relevant [architecture decision records](docs/adr/README.md)

Open an issue before a large architectural change or a new public interface.
Datasource, provisioner, and plugin interfaces are not stable yet.

## Development workflow

1. Create a focused branch from the current default branch.
2. Add production behavior and meaningful tests together.
3. Run `make lint` and `make test`.
4. Build the Windows AMD64 executable with `make build`.
5. Run Windows-only tests when the change affects the Registry, SCM, or MSI.
6. Update documentation and `CLAUDE.md` when required.
7. Describe checks which could not be run in the pull request.

Use English for documentation, code comments, exported identifiers, commit
messages, issues, and pull requests.

## Tests and Windows resources

Tests must use temporary paths, unique service names, and dedicated Registry
subtrees. They must clean up resources they create and must not depend on or
modify the production `wininit` state key. A test requiring elevation must
detect it explicitly and follow the documented integration-test policy.

Do not replace a meaningful Windows test with a stub that always skips or
passes. Cross-compilation proves only that code compiles.

## Pull requests

Keep commits reviewable and use clear, imperative messages. A pull request
should explain:

- the problem and chosen approach;
- observable behavior and compatibility implications;
- state, security, or installer impact;
- tests run and their results; and
- any validation that remains outstanding.

Update or add an ADR when changing a durable architectural decision. Update
`CLAUDE.md` in the same pull request for architectural, interface, workflow,
build, packaging, state-layout, security-policy, or development-convention
changes.

## Reporting security issues

Do not open a public issue for a suspected vulnerability. Follow the private
reporting instructions in [SECURITY.md](SECURITY.md).
