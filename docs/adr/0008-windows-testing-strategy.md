# ADR 0008: Layer Windows validation and real-VM testing

- Status: Accepted
- Date: 2026-11-05

## Context

Portable unit tests cannot validate the Registry, SCM, MSI, or real guest
reboot behavior. GitHub-hosted Windows runners provide native APIs and often
administrative access but are not equivalent to Windows Server 2022/2025 cloud
guests.

## Decision

Use four layers:

1. portable unit/race tests for configuration, serialization, lifecycle, and
   error behavior;
2. Windows tests with isolated Registry keys and SCM behavior exercised on a
   disposable runner with pre-existing-service safeguards;
3. native packaging tests for MSI contents, silent install/uninstall, service
   registration, upgrades, and preserved state; and
4. future Packer/QEMU/OpenStack E2E tests using externally supplied licensed
   Windows Server 2022/2025 images.

Check elevation explicitly. A release-required native test fails when its
requirements are absent; an optional pull-request test may skip only with a
clear reason. Workflow logs record the actual runner OS.

## Consequences

Fast portable feedback remains available while Windows-specific defects are
caught natively. Hosted CI cannot establish target-server compatibility, so
documentation must keep those claims pending until the VM layer exists.

## Alternatives considered

- Rely on cross-compilation: rejected because it exercises no Windows runtime
  behavior.
- Treat hosted Windows CI as real-server certification: rejected because its
  OS and virtualization context differ from target guests.

## References

- [GitHub-hosted runner images](https://github.com/actions/runner-images)
- [GitHub-hosted runner documentation](https://docs.github.com/actions/using-github-hosted-runners/about-github-hosted-runners)
