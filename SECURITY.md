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
- No external plugin execution is implemented in the foundation.
- NoCloud metadata and hostname provisioning are independent built-in plugins,
  both disabled by default. NoCloud is restricted to local seeds and supported
  CIDATA media; hostname mutation rejects domain-joined machines.
- User-data is never opened or executed. ConfigDrive, additional provisioners,
  and external executable plugins remain future trust boundaries.
- Diagnostic archives are intended to contain only allowlisted, sanitized
  information, but reporters should inspect an archive before sharing it.

See the detailed [security model](docs/security.md).
