# ADR 0006: External plugin protocol direction

- Status: Proposed
- Date: 2026-11-05

## Context

Future deployments need independently shipped datasources and provisioners.
Loading Go shared objects is poorly suited to stable cross-version Windows
extension points, while launching external processes creates a substantial
privilege and supply-chain boundary.

## Decision

Explore separate executable plugins with an explicit versioned handshake and
protobuf/gRPC messages, informed by maintained process-plugin lifecycle
patterns. A future Go SDK may wrap the protocol.

Do not publish the protocol or execute plugins until identity/checksum
verification, compatibility negotiation, deadlines, cancellation, process-tree
cleanup, restricted credentials, filesystem/network policy, and controlled
logging are designed and tested.

## Consequences

Plugins can evolve independently and need not share the agent's Go toolchain.
Serialization and process management add complexity. This proposed ADR creates
no current public API or promise of compatibility with any existing framework.

## Alternatives considered

- Go `plugin`: rejected as the intended Windows extension mechanism because of
  platform and toolchain coupling.
- In-process DLL ABI: deferred because ABI stability and privilege isolation
  would be difficult.
- Arbitrary executables without a protocol: rejected as unsafe and
  unmanageable.

## References

- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin)
- [go-plugin protocol internals](https://github.com/hashicorp/go-plugin/blob/main/docs/internals.md)
