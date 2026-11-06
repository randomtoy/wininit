# Security policy

## Supported versions

`wininit` has not published a stable release. Security fixes are applied to the
current development line. A supported-release table will be added before the
first stable release.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting feature for this repository when
it is available. If the feature is not available, contact a repository
maintainer privately through their GitHub profile rather than disclosing the
issue in a public issue, discussion, or pull request.

Include the affected revision, environment, reproduction steps, impact, and
any proposed mitigation. Do not include real credentials, private metadata,
customer user-data, or Windows license material.

Maintainers should acknowledge a report, coordinate validation and remediation
privately, and disclose it after users have a reasonable opportunity to
update. Exact response-time commitments will be published once the project has
a stable maintainer and release process.

## Current security posture

- MVP artifacts are unsigned.
- The service may run with elevated Windows privileges.
- All six logical datasource/provisioner plugins are independently disabled
  by default. Local identity management refuses domain/admin accounts and
  reads protected initial passwords only when creating absent users.
- Optional external ConfigDrive uses pinned reviewed binaries, mTLS,
  restricted Windows tokens and Job Objects. It is not a hostile-code sandbox;
  filesystem reads and outbound network access are not completely OS-isolated.
- User-data is never opened or executed. Arbitrary third-party plugins and
  external privileged provisioners remain future trust boundaries.
- Diagnostic archives are intended to contain only allowlisted, sanitized
  information, but reporters should inspect an archive before sharing it.

See the detailed [security model](docs/security.md).
