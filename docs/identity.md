# Local identity and access

Three independent provisioner plugins are available: `password_policy`,
`local_users`, and `authorized_keys`. All default to disabled, including for
older version 1 configurations. They run only after exactly one enabled
datasource supplies an instance identity. Their desired settings come from
administrator-owned configuration, never metadata or user-data.

The order is password policy, local users, SSH keys, then hostname. A failure
stops later plugins. Operations already completed are not rolled back; a later
enabled run reconciles actual Windows state. Disabling a plugin performs no
discovery, secret-file read, account operation, or ACL change and does not undo
accounts, policy, or keys which Windows already accepted.

## Example

Keep an existing NoCloud/ConfigDrive datasource enabled, then opt in explicitly:

```yaml
version: 1
plugins:
  datasources:
    configdrive:
      enabled: true
      seed_directory: 'D:\'
      discover_volumes: false
  provisioners:
    password_policy:
      enabled: true
      minimum_length: 14
      maximum_age_days: 90
      minimum_age_days: 1
      history_length: 24
      lockout_threshold: 10
      lockout_duration_minutes: 15
      lockout_window_minutes: 15
    local_users:
      enabled: true
      users:
        - name: operator01
          full_name: 'Guest operator'
          description: 'Local operations account'
          password_file: 'C:\ProgramData\WinInit\secrets\operator01.password'
          password_never_expires: false
    authorized_keys:
      enabled: true
      users:
        - name: operator01
          public_keys:
            - 'ssh-ed25519 REPLACE_WITH_A_VALID_PUBLIC_KEY'
```

The public-key placeholder is intentionally invalid. Replace it with a real
public key before validating the configuration. Never put a private key or
plaintext password in YAML. Restart the agent after validating changes.

## Local users and initial passwords

`local_users` accepts 1–32 standard local accounts. Names are 1–20 ASCII
characters, start with a letter, and contain only letters, digits, hyphens, or
underscores. Names are unique without regard to case. Built-in accounts,
administrator-group members, disabled accounts, domain-joined computers, and
domain controllers are refused. This version does not manage group membership,
grant administrative rights, delete users, or re-enable disabled accounts.

Missing accounts are created through `NetUserAdd` with a nonempty initial
password, normal-user privileges, and password expiry enabled by default.
Windows enforces its current password policy and complexity filters. An
explicit `password_never_expires: true` sets that account flag.

Existing accounts reconcile only `full_name`, `description`, and the password
expiry flag. Login restrictions, memberships, account expiry, and the password
are preserved. An existing password is never reset, even if the secret file
changes. Password rotation and forced first-login changes are outside this
version. Administrators use their existing password-management procedure.

The password file is read only when the account is absent. After successful
creation it can be removed; reruns do not need it. A failed run may have already
created an account. On retry, its existing password is preserved and remaining
attributes are repaired.

Use a local regular UTF-8 file containing one password line, at most 1024
bytes/256 UTF-16 code units. One terminal LF/CRLF is removed; other whitespace
is preserved. NUL, interior line endings, invalid UTF-8, hard links, reparse
points, network drives, and alternate data streams are rejected.

The file and its containing directory must be owned by SYSTEM or Builtin
Administrators. Only those principals may read the password file; other
principals must not have write/delete/ACL/ownership access to either object.
This includes FILE_DELETE_CHILD on the containing directory, even if the file
itself has a protected DACL.
Keep secrets outside `log.directory`. For an existing dedicated directory/file,
apply protected ACLs from an elevated session:

```powershell
icacls.exe 'C:\ProgramData\WinInit\secrets' /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)' /setowner '*S-1-5-32-544'
icacls.exe 'C:\ProgramData\WinInit\secrets\operator01.password' /inheritance:r /grant:r '*S-1-5-18:(R)' '*S-1-5-32-544:(R)' /setowner '*S-1-5-32-544'
```

Remove any pre-existing explicit grants to other principals as well. Provision
the password through a secure administrative channel; do not put it in shell
arguments, command history, or example configuration. Agent-owned password
buffers are cleared after use. This does not promise removal of Windows or Go
runtime copies from memory.

## Password and lockout policy

`password_policy` manages local SAM minimum length (8–14), maximum age
(1–365 days), minimum age (0 through maximum), history (1–24), lockout threshold
(1–999), and lockout window/duration (1–1440 minutes, duration at least window).
Defaults are the values in the example and have no effect until enabled.

It uses `NetUserModalsGet/Set` levels 0 and 3, preserves forced-logoff settings,
and leaves password-complexity filters, reversible encryption, LSA policy, and
domain policy unchanged. Domain-joined systems are refused. These are
machine-wide settings, including effects on pre-existing local accounts.
Policy updates are idempotent but not a transaction across both API calls.

## SSH authorized keys

`authorized_keys` supports existing standard local users, including accounts
created by `local_users` earlier in the run. It obtains the profile path from
the account SID's ProfileList registration and creates a missing profile with
`CreateProfile`. No guessed `C:\Users\<name>` path is used.

The plugin creates/repairs `.ssh` and writes a managed block in
`.ssh\authorized_keys`. Bytes outside the block are preserved. Duplicate,
missing, or ambiguous block markers fail instead of guessing. Set
`public_keys: []` for a named user to revoke only the managed block. Disabling
the plugin preserves existing keys.

It accepts up to 64 Ed25519, ECDSA P-256/P-384/P-521, or RSA keys of at least
3072 bits per user. Comments are stripped, keys are sorted/deduplicated, and
options, certificates, private keys, malformed keys, and multiple-line entries
are refused. This does not enable legacy RSA/SHA-1 signatures in sshd.

Directory and file DACLs are protected and grant access only to the target
user, SYSTEM, and Builtin Administrators. Paths are opened relative to pinned
directory handles without reparse traversal. Hard-linked key files are
refused. A replacement receives its final ACL before an atomic rename;
existing file contents or ACLs are never modified through a hard link.
An existing empty file also receives ACL repair, including during revocation;
revocation does not create absent profiles, .ssh directories, or key files.
Directory ACL repair also updates only the opened directory object; it does
not propagate permissions through existing child files or their hard links.

Administrator accounts are refused because default Windows OpenSSH uses a
shared `administrators_authorized_keys` file. This plugin does not install or
start OpenSSH Server, change `sshd_config`, open firewall ports, or verify SSH
login. Custom `AuthorizedKeysFile` layouts are unsupported; use the standard
per-user layout and independently verify server configuration.
The account must also satisfy the host's `AllowUsers`/`AllowGroups` rules.
Prepare any required non-administrative group membership outside wininit;
the local-user plugin preserves it rather than managing SSH access groups.

## State, diagnostics, and validation

Aggregate Registry state retains the selected instance ID; plugin checkpoints
retain status, counters, and a SHA-256 of their instance/metadata/configuration
input. Password bytes and password hashes are never stored. Raw account lists,
password-file paths, and public keys are removed from sanitized diagnostic
configuration. Only the agent's own
`wininit.log` is eligible for archive collection, and configured password files
are excluded even if hard-linked to that log.

Portable tests exercise validation, reruns, secret-buffer clearing, key
normalization, preservation/revocation, and failure paths. Elevated Windows
integration tests create/delete a uniquely named account and profile, verify
its initial password survives reruns, inspect SSH ACLs and unsafe paths, and
round-trip local password policy with exact restoration. Run these only on a
disposable Windows test host. [PR 4 guest review](../tests/e2e/2026-11-06-pr4-review.md)
records a Windows Server 2025 SCM/identity/SSH login run. Windows Server 2022
and rename/reboot/resume acceptance remain pending.

References: [Windows OpenSSH keys](https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_keymanagement),
[NetUserAdd](https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/nf-lmaccess-netuseradd),
[password policy](https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-user_modals_info_0),
[lockout policy](https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-user_modals_info_3).
