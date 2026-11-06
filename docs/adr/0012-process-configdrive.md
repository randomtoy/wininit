# ADR 0012: Experimental ConfigDrive process plugin in the same repository

- Status: Accepted
- Date: 2026-11-06
- Supersedes: the deferral in ADR 0006 for ConfigDrive datasource processes only

## Context

The project initially compiled every implementation into the agent. The user
requested a Terraform-style executable plugin and an explicit repository
decision while advancing identity/access provisioning.

## Decision

Ship ConfigDrive as a separate optional EXE using pinned HashiCorp go-plugin
1.8.0, gRPC, protobuf service v1, local mTLS, a framework handshake, and service
identity verification. Keep source/experimental SDK in this repository so
compatibility and Windows restrictions can be tested atomically. Produce a
separate release artifact/checksum. Do not create another GitHub repository.

Keep the built-in execution mode for existing v1 files. Explicit
`execution: external` selects the process adapter exclusively. The logical
ConfigDrive enabled flag remains false by default. No automatic download,
registry scanning, argument injection, reattachment, or built-in fallback is
allowed.

Require a pinned SHA-256 and protected local executable/parent ACLs. Keep the
verified Windows image locked against replacement. Discard child output and
sanitize remote errors. Bound all RPCs/startup by 1–60 seconds and messages by
128 KiB; validate responses against the internal metadata contract.

Launch on Windows with a restricted token, deny-only caller/Administrators,
low integrity, and disabled privileges except traversal. Create suspended,
assign to a kill-on-close job with active-process limit 1, 256 MiB memory
limit, and no window, then resume. Omit job UI limits because they prevent
nesting in an existing host job; job containment remains mandatory. Inherit only protocol bootstrap and
Windows directory settings, never the host environment or stdin.

This policy supports reviewed, explicitly trusted binaries. It is not a
hostile-code sandbox: OS restrictions do not completely isolate filesystem
reads or outbound networking. The reviewed provider confines reads to local
ConfigDrive metadata and makes no network requests. Network-denying
AppContainer isolation and arbitrary third-party execution remain deferred.

## Consequences

The first public wire surface is experimental and only supports ConfigDrive
datasources. Changes within v1 may add compatible optional fields; changed
semantics require another version and fail-closed negotiation. External
provisioners and a general stable SDK are not implemented.

One repository gives atomic source/protocol/native CI changes now. A separate
repository/module becomes useful when protocol stability and independent
maintainer/release requirements justify the coordination cost. Process/plugin
artifacts may already be updated independently within compatible v1.

Portable development tests run with caller credentials and process-group
cleanup. Windows tests must validate the actual token, ACLs, job restrictions,
deadline/cancellation, environment isolation, identity, and secret redaction.
They do not replace real guest media/reboot acceptance.

## Alternatives considered

- Extract hostname first: rejected because it needs privileged mutation.
- Remove built-in ConfigDrive immediately: rejected as a v1 behavior break.
- Create another repository before protocol stabilizes: deferred to avoid
  cross-repository coordination for each early interface change.
- Hand-roll a JSON shell protocol or load Go shared libraries: rejected in
  favor of maintained process lifecycle and a typed versioned wire contract.
- Treat a checksum as a sandbox: rejected; it verifies approved bytes only.

References: [go-plugin](https://github.com/hashicorp/go-plugin),
[restricted tokens](https://learn.microsoft.com/en-us/windows/win32/secauthz/restricted-tokens).
