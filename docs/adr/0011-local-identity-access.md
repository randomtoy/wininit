# ADR 0011: Independently gated local identity and access

- Status: Accepted
- Date: 2026-11-06

## Context

Roadmap milestone 4 needs users, initial passwords, local password policy, and
SSH public keys. Metadata/user-data must not become an implicit privileged
account-creation language. Reruns must not reset passwords or erase
operator-owned keys.

## Decision

Add three disabled-by-default built-ins: `password_policy`, `local_users`,
and `authorized_keys`. Configure desired accounts and keys in administrator-owned
version 1 YAML; require a freshly selected datasource instance identity.
Execute policy, accounts, keys, then hostname. Use Windows APIs behind narrow
adapters, with explicit failure and no automatic retries/rollback.

Create only standard local accounts. Reject domain-joined systems, built-in,
disabled, and administrator accounts. Read initial passwords only for absent
accounts from protected local files. Existing passwords and group memberships
are preserved; rotation is deferred. Update only named account attributes.

Manage supported SAM password/lockout settings using NetUserModals levels 0/3,
preserving unrelated forced-logoff and complexity settings. Document the
machine-wide effect and non-transactional API boundary.

Manage a marked key block in SID-resolved user profiles, preserving unmanaged
bytes and allowing explicit managed-key revocation. Reject key options,
certificates, weak keys, reparse paths, and hard-linked files. Publish a flushed
replacement with its final protected DACL by a handle-relative atomic rename.
Never modify an existing hard link's data or ACL.

Keep state schema 2; include the configuration fingerprint in checkpoint input
hashes. Never persist password bytes/hashes. Redact complete identity lists
from diagnostic configuration and exclude secret files from log collection.
MSI-created ProgramData directories/configuration receive protected
SYSTEM/Administrators ACLs.

## Consequences

Default upgrades remain inert. Enabling identity management deliberately grants
account/policy authority to configuration; OpenSSH installation, custom server
layouts, administrator keys, domain users, password rotation, and automatic
reboot are outside this version. Native isolated tests create/remove accounts
and profiles, restore exact prior policy, verify reruns/ACLs, and must run on a
disposable elevated Windows host. Real guest acceptance remains pending.

## Alternatives considered

- Inline YAML passwords: rejected to avoid diagnostics/configuration exposure.
- Reset existing passwords on every run: rejected as non-idempotent.
- Replace all SSH keys: rejected because it would erase operator access.
- Configure accounts from unrestricted user-data: deferred to its own policy.
- Shared administrator key files: refused to prevent unintended access grants.
