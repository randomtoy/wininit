# ADR 0001: Project scope and component boundaries

- Status: Accepted
- Date: 2026-11-05

## Context

Windows cloud guests need initialization without requiring a Python runtime.
Copying an existing implementation would also copy its internal constraints and
would obscure which compatibility behavior is intentional.

## Decision

Build an independent Go agent. The foundation contains service/CLI hosting,
configuration, lifecycle state, diagnostics, and packaging. Datasources,
provisioners, and external plugins remain separate component boundaries.
ADR 0009 later adds two built-in implementation packages without making those
boundaries public; external process plugins remain future work. Internal
implementation stays under `internal`; public packages require a stable
external consumer.

## Consequences

The original foundation is operational without provisioning. ADR 0009 extends
it with independently opt-in built-in NoCloud and hostname implementations;
external plugin APIs remain out of scope. Compatibility must be earned through
fixtures and real-VM tests. Status and documentation must distinguish agent
health, plugin enablement, restart-required state, and provisioning completion.

## Alternatives considered

- Port Cloudbase-Init line by line: rejected because it imports implementation
  constraints instead of defining deliberate Go/Windows boundaries.
- Implement every planned feature in the first release: rejected because it
  would stabilize untested interfaces prematurely.

## References

- [Cloudbase-Init metadata service documentation](https://github.com/cloudbase/cloudbase-init/blob/master/doc/source/services.rst), consulted for compatibility concepts only
