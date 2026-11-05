# ADR 0003: Use strict, versioned YAML configuration

- Status: Accepted
- Date: 2026-11-05

## Context

Administrators need a readable configuration format for paths and policy. The
format must evolve without interpreting a new configuration incorrectly on an
older agent.

## Decision

Use YAML with an explicit, required schema version. Decode strictly, reject
unknown fields and unsupported versions, apply safe defaults to optional fields,
and validate the complete result before use. Do not require fields for features
which are not implemented. The installer supplies a default file but preserves
administrator-owned configuration on upgrade.

## Consequences

YAML adds a small parser dependency and requires care around ambiguous scalar
syntax. Strict decoding and tests make mistakes visible. Future schema changes
need a compatibility or migration decision.

## Alternatives considered

- JSON: viable but less convenient for hand-maintained Windows configuration.
- TOML: viable but offers no decisive benefit for this small schema.
- Registry-only configuration: rejected because review, distribution, and
  source-controlled operator configuration are less convenient.
