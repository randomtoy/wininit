# Project instructions

## Project mission and current scope

`wininit` is an independent Go implementation of a Windows initialization
agent for Windows Server 2022 and 2025 guests on QEMU/KVM and OpenStack. It
consists of one Windows Service/CLI executable, a lifecycle pipeline, versioned
configuration, Registry-backed persistent state, diagnostics, Windows
packaging, and CI.

The current scope includes NoCloud and ConfigDrive datasources plus hostname,
password_policy, local_users, authorized_keys, and external PowerShell user-data
provisioners. All seven logical
plugins are independently opt-in and disabled by default. NoCloud, ConfigDrive and hostname can
explicitly use separate verified EXEs; builtin mode remains the compatible
default. PowerShell is a separate EXE only, never installed by the agent MSI.
Do not claim that a VM was provisioned merely because the
service started, metadata was found, or a reboot checkpoint was saved.

## Mandatory maintenance rule

Every change to architecture, public or internal interfaces, workflows,
packaging, build commands, state layout, security policy, or development
conventions must update this file in the same change.

## Architectural principles

- Prefer simple, explicit components and the Go standard library.
- Keep implementation details under `internal`. Add packages under `pkg` only
  for stable contracts that external consumers need.
- Define small interfaces at the point of consumption.
- Propagate `context.Context` cancellation across application, lifecycle,
  state, and platform boundaries.
- Keep Windows-specific code behind Windows build constraints and provide only
  truthful portable behavior on other platforms.
- Distinguish SCM service state, agent lifecycle state, plugin enablement and
  runtime state, and provisioning state in code, logs, status output, and
  documentation.
- Overall provisioning stays `running` until enabled provisioners return;
  never persist a completed aggregate before the platform operation finishes.
- Constructors for every known built-in may populate the catalog but must
  perform no I/O. Invoke `Discover` or `Apply` only when that plugin is enabled
  by current configuration. Configuration disablement always wins over old
  persistent checkpoints.
- The experimental datasource v1 SDK supports named ConfigDrive/NoCloud
  processes. The metadata provisioner v1 SDK supports reviewed hostname
  reconciliation. The separate userdata v1 SDK supports PowerShell. Neither is a stable general protocol, hostile-code
  sandbox, or automatic plugin distribution mechanism.

## Directory structure

```text
cmd/wininit/              executable entry point
cmd/wininit-plugin-configdrive/ separate datasource process entry point
cmd/wininit-plugin-powershell/ separate user-data process entry point
cmd/wininit-plugin-nocloud/ separate metadata datasource entry point
cmd/wininit-plugin-hostname/ separate metadata provisioner entry point
cmd/wininit-release/      builder-only bundle inventory/verifier (not distributed)
sdk/datasource/v1/        experimental datasource protobuf/gRPC contract
sdk/userdata/v1/          experimental prepare/execute protobuf/gRPC contract
sdk/provisioner/v1/       experimental idempotent metadata protobuf/gRPC contract
internal/app/             composition and command execution
internal/config/          versioned configuration and validation
internal/diagnostics/     sanitized diagnostic archive creation
internal/lifecycle/       lifecycle engine and valid transitions
internal/plugin/          internal datasource/provisioner contracts and catalog
internal/platform/        Windows operations behind narrow internal adapters
internal/service/         SCM/console hosting and administration
internal/state/           state model and storage implementations
internal/version/         embedded build information
internal/releasebundle/   fixed release subjects and build-info/SBOM validation
plugins/datasource/       built-in datasource implementations
plugins/provisioner/      built-in provisioner implementations
packaging/wix/            WiX MSI source and pinned tooling
scripts/                  build and packaging helpers
tests/                    integration, fixtures, automated no-reboot and manual E2E
docs/                     architecture, operations, and ADRs
.github/workflows/        CI and release workflows
```

Do not create empty directories to imply unimplemented functionality.

## Go conventions

- Use the Go version declared in `go.mod` (currently Go 1.27.3). The module file
  is authoritative.
- Format all Go sources with `gofmt`.
- Wrap errors with useful operation context and preserve the underlying error.
- Avoid global mutable state. Narrow exceptions are build information set by
  linker flags and `sync.Once` initialization of process-lifetime Windows
  resources such as the protected private namespace.
- Prefer table-driven tests where they clarify multiple cases.
- Use temporary directories and unique Registry test keys. Tests own and clean
  up every resource they create.
- Windows security tests compare ACE types, masks, and SIDs rather than SDDL
  formatting; Windows can expand generic rights into numeric object rights.
- Never use PowerShell `New-Item -Force` on an existing Registry key: it erases
  values and subkeys. Add test sentinels with `New-ItemProperty` only.
- Never add a fake implementation or a test which passes without exercising
  the behavior named by the test.

## Build and test commands

```text
make build       build the Windows AMD64 agent and separate plugin executables
make test        run unit tests
make lint        validate formatting and run static analysis
make package     build EXEs, MSI, per-EXE SBOMs, manifest and checksums on Windows
go test ./...    run tests directly
go vet ./...     run Go static analysis directly
```

MSI construction, Registry integration, SCM integration, and silent installer
smoke tests require Windows. Cross-compiling the EXE does not validate native
Windows behavior.

`tests/e2e/Invoke-NoRebootAcceptance.ps1` is the repeatable elevated guest
suite. It refuses existing service/ProgramData/install/state roots, exercises
MSI baseline-to-candidate upgrade, LocalSystem external plugin success/failure/
replay/disablement, request-only exit 3010 and uninstall, and cleans only
proven-owned resources. Verify installed MSI payload against the candidate EXE.
Baseline CI MSI is a same-source lower-version fixture, not historical-release
migration evidence. Record boot/name before and after; never rename or reboot.
PowerShell 5.1 runs this same script in CI. The separate manual rename/media/
reboot contract in `tests/e2e/README.md` remains pending for the full matrix.

`tests/e2e/2026-11-06-pr4-review.md` records a narrower Windows Server 2025 SSH
guest run for identity/access, local-seed external ConfigDrive, SCM, and MSI.
No restart was authorized or run. SSH access-group setup is an administrator
precondition outside wininit; do not imply the plugin manages groups or sshd.

## Configuration rules

- Configuration has an explicit schema version. Reject unsupported versions
  and invalid values with actionable field-level errors.
- Reject unknown fields unless a documented compatibility decision changes
  this rule.
- Defaults must be safe and must not enable unimplemented capabilities.
- The version 1 schema remains backward compatible. Missing built-in plugin
  fields mean disabled; never infer enablement from old state or installed
  files.
- All datasource/provisioner enablement flags are independent. A single global plugin flag
  is not an acceptable substitute.
- `state.registry_path` must name a non-empty, dedicated subkey below
  `HKLM\SOFTWARE`. Never accept the `SOFTWARE` root or a shared broad parent as
  the lifecycle state target.
- Installer upgrades must not overwrite administrator-owned configuration.
- Never log, persist, or archive passwords, tokens, private keys, or raw
  user-data. Add redaction tests when a new sensitive field is introduced.

## Lifecycle and Registry state rules

- Production lifecycle state is stored in the Windows Registry, never SQLite
  or another database.
- The state schema is versioned. State schema version 3 is current and must
  migrate valid versions 1 and 2 without inventing plugin or script success. Reject
  unknown future schemas rather than guessing their meaning.
- Detect an unsupported future schema in every checkpoint slot and fail closed,
  including when an older understood slot is marked active. `Load` must not hide
  future state by choosing the active/older slot, and `Save` must not overwrite
  it as though no valid state existed.
- A save must write and validate a complete versioned checkpoint before
  changing the active checkpoint marker. On Windows, flush the committed marker
  with `RegFlushKey` before returning so the checkpoint preceding a platform
  mutation is on disk. A load may recover only from a complete, validated prior
  checkpoint.
- `RegFlushKey` is expensive and system-wide at the hive level. Keep state saves
  at low-frequency lifecycle/plugin boundaries; never add a polling save loop.
- Keep the selected instance ID in aggregate lifecycle state. Plugin input
  checkpoints store only SHA-256 of instance, metadata, plugin name, and
  configuration fingerprint. Never store raw targets, passwords, or password
  hashes. Redact complete identity lists from diagnostic configuration.
- State-store implementations own the monotonically increasing checkpoint
  generation; callers must not choose or decrement it.
- Preserve enough error metadata for diagnosis without storing untrusted or
  secret payloads.
- Do not store credentials, password hashes, private keys, access tokens, or
  raw metadata/user-data in lifecycle state.
- Tests use the memory store or an isolated Registry subtree. They must not
  touch the production Registry key.
- Do not expose the Registry store directly to future external plugins.

## Windows service rules

- Report correct SCM pending, running, stop-pending, and stopped states.
- Accept stop and system shutdown controls and propagate cancellation.
- Avoid polling and busy loops; wait on cancellation or owned work.
- Write structured, non-sensitive logs.
- Service management errors must be visible to the caller and SCM.
- MSI releases use MSI-native service installation and control. Direct CLI
  service installation is for development and manual deployment, not for
  mutating an MSI-managed installation.

## Plugin design constraints

- `Datasource` and `Provisioner` are small internal interfaces. Do not expose
  them as a stable public Go API.
- Built-in implementations remain separate under
  `plugins/datasource/nocloud`, `plugins/datasource/configdrive`, and
  `plugins/provisioner/hostname`.
- Local datasource configuration shares the internal `LocalDatasourceConfig`
  settings, but each plugin has its own enabled flag and source selection.
- Probe all enabled datasources before provisioners. Exactly one must return
  metadata. Multiple available sources fail before mutation regardless of
  catalog order; only `ErrUnavailable` allows fallback, never malformed data.
- The pipeline executes enabled plugins only. Disabled plugins do not discover,
  parse, mutate, resume, or reconcile work.
- Built-in constructors perform no I/O; all discovery and platform access stays
  behind the execution-time enablement gate.
- On each process start, rediscover metadata and reconcile every enabled
  declarative provisioner once. PowerShell is a one-shot exception: a durable
  execution journal prevents replay across process starts. Do not add automatic
  retries or a busy loop.
- Before discovery or saving a new configuration hash, reset every enabled
  provisioner to `pending` and clear its prior input hash/reboot flag. Missing,
  malformed, or ambiguous sources must not expose an earlier result as current.
  Preserve attempt counters until a different instance is selected.
- The built-in NoCloud datasource accepts only required `instance-id` and
  optional `local-hostname`. It must never open or execute `user-data`.
- Use either an explicit seed directory or one unambiguous, already-mounted
  `CIDATA` ISO9660/FAT volume. Fail instead of choosing between candidates.
- Skip query failures and unsupported filesystems on unrelated local drives;
  they are not eligible NoCloud candidates. Keep discovery pending until all
  enabled datasource candidates have returned unavailable.
- Share mounted-media discovery through `internal/platform/seedmedia`.
  NoCloud uses `CIDATA`; ConfigDrive uses `config-2`, case-insensitively, on
  supported FAT/ISO9660 volumes only. Constructors must not enumerate drives.
- ConfigDrive consumes only `uuid` and optional `hostname` from one bounded
  `openstack/<version>/meta_data.json`. Select the newest supported dated
  directory (2025-04-04 through 2012-08-10); use `latest` only if no supported
  dated directory exists. Never downgrade when a selected directory/file is
  malformed. Reject duplicate JSON keys, excessive nesting, and symlink paths.
- ConfigDrive explicitly maps a valid DNS hostname/FQDN to its lowercase first
  label; never truncate it to 15 characters. The existing hostname provisioner
  remains responsible for Windows validation. Do not use the display `name`.
- The ConfigDrive datasource must never open `user_data`, `password`, vendor/network metadata, EC2
  layouts, or injected file contents. Do not fetch network metadata.
- The hostname provisioner accepts only the documented conservative 1–15
  character ASCII label, rejects all-numeric names, refuses domain-joined
  rename, and compares the active computer name idempotently.
- `reboot.policy` remains `never`. Persist `reboot_required`, let an
  administrator restart Windows, then rediscover fresh metadata and reconcile
  only on a later enabled run. The seed must remain accessible across restart.
- A changed metadata input is new work. A missing seed after restart reports
  `no_datasource`; never replay a raw target from state.

## Identity and external process constraints

- Run enabled provisioners in policy, local-user, authorized-key, hostname,
  PowerShell order after unambiguous datasource selection. Stop at the first failure;
  completed mutations are not rolled back.
- Use native APIs for standard local accounts and SAM policy. Refuse
  domain-joined hosts, built-in, disabled, and administrator accounts. Preserve
  memberships and existing passwords; only absent users read initial secrets.
  Clear owned password buffers. Never invoke a shell with credentials.
- Password files and parent directories require SYSTEM/Administrators ownership
  and protected access. Reject network drives, streams, hard links, and reparse
  traversal. Password files must stay outside log.directory.
  Parent-directory ACL checks must reject FILE_DELETE_CHILD grants to ordinary
  users as well as DELETE/write grants; a child's DACL cannot negate that right.
- Native file helpers must request FILE_READ_ATTRIBUTES before validating a
  handle, including traversal-only directories and write-only replacements.
  Keep syscall causes without echoing untrusted paths or payloads.
- Pin directories by denying FILE_SHARE_DELETE while allowing FILE_SHARE_WRITE
  for Windows rename internals. New replacement files allow delete sharing for
  handle-relative rename, but deny foreign writes. Verified images/secrets
  retain exclusive write/delete protection.
- Protected files/executables must reject ADS, DOS-device names, dot/space
  aliases, and relative components, so NT verification and Win32 execution
  identify the same object. Handle-relative opens accept one component only.
- Password policy is machine-wide, bounded, and independently opt-in. Preserve
  force-logoff/complexity settings. Native integration tests restore exact raw
  prior policy even on failure; run them only on a disposable elevated host.
- SSH keys use SID-resolved profiles, protected target-user/SYSTEM/Admin ACLs,
  pinned directory handles, no reparse traversal, no hard links, and atomic
  flushed replacements. Preserve unmanaged bytes; empty key lists revoke only
  the managed block. Do not install/configure sshd or use shared admin key files.
  Distinguish an absent authorized_keys file from an existing zero-byte file;
  revocation must repair unsafe ACLs on an empty file without creating a new
  file, profile, or .ssh directory when none exists. Cover these cases with
  native regression tests.
- Repair the .ssh directory ACL with a handle-only NtSetSecurityObject call;
  SetSecurityInfo would propagate inheritable ACLs into untrusted hard-linked
  children. A native regression must prove outside target ACLs stay unchanged.
- CreateProfile uses a MAX_PATH-sized output buffer; its RPC size constraint
  is narrower than ordinary Windows long-path filesystem support.
- External ConfigDrive requires explicit mode/enablement, absolute reviewed
  image, SHA-256 pin, trusted image/parent ACLs, locked verified image,
  matching handshake/identity, local mTLS, bounded messages, deadlines across
  framework calls, cancellation, and cleanup.
  Pin every path ancestor without delete sharing until process cleanup; locking
  the image alone does not prevent a Win32 launch path from being redirected
  by a directory rename. Test ancestor swaps and release on Windows.
- Read-only ConfigDrive/NoCloud Windows children retain caller identity but use deny-only Administrators
  plus ordinary Users/Authenticated Users/Everyone restricting SIDs for a second
  ACL access check, disabled privileges, low
  integrity, and a suspended start before assignment to a kill-on-close job
  limited to one process and 256 MiB. Start without a window and scrub inherited
  environment/stdin. Omit job UI limits because Windows disallows nesting them
  inside host jobs. Test actual restrictions on Windows; never bypass failed
  job assignment.
- Give each launch a fresh private restricting SID and a matching default
  object DACL so Windows/runtime-created objects pass both ACL checks without
  granting object control to all ordinary users.
- Restricted token handles need TOKEN_ADJUST_DEFAULT for low-integrity setup;
  validate the actual child token and current job, not just constructor flags.
- Restricted children use a fresh noninteractive low-integrity window station
  and desktop, never an inherited host desktop. Serialize the temporary parent
  window-station switch on a pinned thread and restore it before child creation;
  the host is headless. Native CreateProcessAsUser specifies this desktop,
  sorted scrubbed environment, and an explicit three-handle stdio allowlist.
  Keep the initial thread handle for job assignment before resume. Close UI,
  token, job, process, thread, pipe, and cancellation resources on every path.
- CreateDesktop attaches its calling thread. Use a native creator thread and a
  single cached Go callback; pass no Go pointers as native parameters. Restore
  the process station and join the native thread before returning handles.
  Keep request state in an atomic Go reference under the creation mutex.
  Never return a UI-affiliated thread to Go's pool or try to retire its initial
  runtime thread.
- These are reviewed trusted-code plugins, not an AppContainer sandbox.
  Outbound networking and filesystem reads are not completely OS-isolated.
  Do not claim otherwise or enable arbitrary unreviewed code. Protocol endpoints
  are local; the reviewed ConfigDrive implementation makes no network requests.
- Discard plugin stdout/stderr and remote error payloads. No password paths,
  key lists or Registry handles go to a child. The PowerShell protocol carries
  source selection, expected metadata, hashes, lease IDs, budgets, and numeric
  outcomes, never script or output bytes.
- Keep the first plugin/experimental SDK in this repo with separate EXE and
  checksum artifacts. Builtin ConfigDrive remains for old v1 files; external
  failure must never fall back to it. plugins.directory is not scanned.

## Security constraints

- Treat metadata, user-data, configuration, plugin output, filenames, archive
  entries, and Registry contents as untrusted.
- Only the enabled separate PowerShell plugin may open selected-source user-data.
  Require allow_privileged, exact script/image SHA-256 pins, and execution_id.
  Never compile its interpreter/executor package into cmd/wininit.
- Prepare validates and snapshots at most 1 MiB of UTF-8 #ps1/#ps1_sysnative
  without execution. Revalidate selected source instance/hostname first.
  Persist/flush started intent before dispatch; failed, interrupted or unknown
  outcomes require a new execution_id. A changed script pin cannot reuse an ID.
  Keep at most 128 journal entries without eviction; preserve them across instances.
- PowerShell retains the elevated caller account in a suspended, kill-on-close
  job (8 processes, 512 MiB total/per-process), private desktop and scrubbed
  environment. Remove direct shutdown privileges; this is trusted privileged
  code, not a hostile script sandbox. Native execution requires that job/token.
- Execute only the pinned native System32 Windows PowerShell 5.1 interpreter
  via a constant bootstrap and private stdin. Do not put script bytes in argv
  or temporary payload files; do not bypass execution policy or disable AMSI,
  script logging or other OS auditing. Document those external artifact limits.
- Exit 3010 requests an administrator reboot; never reboot automatically. Use a
  protected volatile Registry boot marker so service restarts do not clear it.
  Journal contents are hashes, times, codes and byte counts only. See ADR 0013.
- Refuse ambiguous media/datasource selection and domain-joined hostname mutation.
- Apply least privilege and restrictive ACLs where Windows permits it.
- Do not disable certificate or integrity checks to make a test pass.
- Diagnostics must use an allowlist and sanitized configuration, not an
  indiscriminate copy of data directories.
- Do not commit credentials, tokens, private keys, Windows media, or license
  keys.

## Concurrency rules

- Service and foreground lifecycle runs must acquire the Windows private-
  namespace mutex `WininitPrivate\Provisioning` before loading or mutating
  lifecycle state.
- Create the private namespace with a boundary containing the Builtin
  Administrators SID and no session SID, making it machine-wide across sessions.
  Apply a protected DACL which grants access only to SYSTEM and Builtin
  Administrators on both the namespace and mutex.
- Windows mutex ownership is thread-affine. A worker goroutine locked to its OS
  thread must wait for, own, and release the mutex. Accept abandoned ownership
  as successful acquisition so Registry checkpoints can drive recovery.
- Release must be idempotent and callable from any goroutine, while the pinned
  owner thread performs `ReleaseMutex` and closes the mutex handle. The private
  namespace handle is retained until process termination.
- The lock applies regardless of the configured Registry state path. Do not
  weaken it to a per-key or per-configuration lock. A second process must fail
  clearly rather than run concurrently or wait in a hidden retry loop.

## Cancellation rules

- Propagate plugin, state, and platform errors even when cancellation happens
  concurrently.
- Treat lifecycle termination as a clean cancellation only when the returned
  error's cause is the root context cancellation. Do not swallow an unrelated
  failure merely because the root context is also done.
- Reuse an equal/earlier RPC parent deadline rather than installing a competing
  timer. The host owns deadline timing; RPC contexts forward Done/Err without
  grpc-timeout so a rounded remote timer cannot win the race. The SDK retains
  its independent provider upper bound. An early remote DeadlineExceeded stays
  a provider failure, not host cancellation. Report only fixed RPC status
  codes, never remote error payloads.

## Packaging rules

- Build Windows AMD64 first; ARM64 support remains preparatory until validated.
- Pin WiX Toolset 7.0.1 and its extensions exactly until a reviewed dependency
  update changes the project. Keep the required `<AcceptEula>wix7</AcceptEula>`
  acknowledgement. Builders and redistributors remain responsible for
  reviewing and complying with WiX OSMF terms.
- Use MSI-native service registration and control with stable component GUIDs
  and a stable UpgradeCode.
- Map release versions deterministically to MSI-compatible numeric versions.
- Keep WiX intermediate/restore outputs per bundle under DIST_DIR/obj so a
  lower-version fixture cannot reuse the candidate's compiler/binder cache.
  Ignore generated artifacts/ diagnostics and legacy packaging/wix/bin and obj
  outputs for truthful source cleanliness.
- Write `SHA256SUMS` as UTF-8 without a byte-order mark and with LF line
  endings so standard Windows and Unix verification tools agree.
- Preserve operational state and administrator-owned configuration through
  upgrades and ordinary uninstall.
- MSI-created ProgramData root/log/plugin directories and configuration use
  protected SYSTEM/Administrators ACLs. Preserve configuration contents across
  upgrades. Test actual installed DACLs; do not infer protection from XML.
- Build/distribute ConfigDrive, PowerShell, NoCloud and hostname EXEs separately; MSI
  installs only the agent. SHA256SUMS covers six binaries, five CycloneDX 1.6
  per-EXE SBOMs and release-manifest.json (twelve fixed LF-terminated entries).
  Use build-time cyclonedx-gomod v1.12.0 bin with stdlib and short PURLs;
  compare module sets/versions against each binary's build info. Do not invent
  OS/installer/license coverage. No replaced modules without reviewed policy.
  The builder-only release command stays outside agent and plugin graphs.
- Do not add the install directory to the machine-wide `PATH`.
- Unsigned artifacts are acceptable only while the MVP documentation states
  this limitation. Never describe unsigned artifacts as trusted.
- Optional signing uses an existing store/provider certificate thumbprint and
  HTTPS RFC3161 timestamp, SHA-256, and fail-closed SignTool verification.
  Sign all EXEs before constructing MSI, then sign MSI, then generate metadata.
  Never accept private-key passwords in argv or modify machine trust stores.
  Hosted candidates remain unsigned; production signing validation is pending.
- Require clean source for CI/release inventories. Local dirty builds must
  record source_modified. Integrity verification is not publisher authentication.

## CI/CD instructions

- CI must verify dependencies, formatting, vetting, tests, race tests, and the
  Windows AMD64 build.
- Validate workflows with build-time actionlint v1.7.12 and the explicit custom
  self-hosted labels in .github/actionlint.yaml; it is not a runtime dependency.
- Native Windows CI must exercise isolated Registry tests and MSI validation.
  It must check elevation explicitly before tests which require SCM or MSI
  administration and report the documented outcome; it must not fabricate a
  successful smoke test.
- Run isolated integration tests even when native unit tests fail, to surface
  independent account/ACL/policy failures. Keep the original unit failure fatal.
  CI and release use separate native-command steps so PowerShell cannot mask an
  earlier verification/test failure behind a successful final command.
- Process startup diagnostics may report fixed stages and numeric OS/exit
  codes; never expose raw child handshake, stderr, or RPC error contents.
- Stage failure logs from ProgramData into the workspace artifact directory
  before upload; Windows artifacts cannot share a root across different drives.
- Keep structured startup milestones around lock acquisition and the first
  Registry checkpoint so SCM-running/engine-starting failures are diagnosable.
- Release jobs run only for trusted version tags and prepare draft prereleases.
  After tests, a separate minimally permitted OIDC job attests SHA256SUMS
  provenance and five EXE SBOMs, then uploads offline verification bundles.
  Pin actions/attest v4.2.2; do not give PR jobs signing permissions. Actual tag
  attestation remains unexecuted until an authorized release tag exists.
- Manual guest E2E workflow targets four administrator-provisioned, licensed,
  isolated self-hosted Server 2022/2025 × QEMU/OpenStack runners. Only reviewed
  workflow_dispatch is allowed; bind downloads to successful push CI from this
  repository and exact source commit. Never run PR/fork events on these runners.
  Provider labels are declared, not proof of placement/acceleration. Full media,
  clean-image, historical-upgrade and reboot matrix remain stage-7 gates.
- Pin third-party actions to immutable commits where practical and keep them
  current through dependency automation.
- The Windows jobs currently use `actions/setup-dotnet` 6.1.0 to install the
  .NET 10 SDK used by the pinned WiX build; action-major updates require a full
  Windows MSI smoke run.
- A GitHub-hosted Windows runner does not prove Windows Server 2022 or 2025 VM
  compatibility.

## Pull request requirements

- Keep the change focused and explain user-visible behavior.
- Add or update meaningful tests.
- Update documentation and ADRs when a decision changes.
- Update this file for any architectural or workflow change.
- Run relevant local checks and report Windows-only checks which could not be
  run.
- Preserve unrelated work and never commit generated secrets or private data.

## Definition of done

A change is done only when:

- relevant unit and integration tests pass;
- `gofmt` and `go vet` pass;
- the Windows AMD64 executable still builds;
- Windows/MSI behavior is validated in an appropriate environment or clearly
  reported as unverified;
- failure paths are visible and do not claim false success;
- state, logs, and diagnostics contain no sensitive payloads;
- user-facing and architectural documentation describes actual behavior; and
- this file reflects every architecture, interface, workflow, build,
  packaging, security, or convention change.

PowerShell build separation is enforced by `scripts/check-plugin-boundary.sh`
in Linux CI. Native tests must use the actual interpreter and job/token role;
protocol fixture servers test wire behavior only and do not stand in for it.

Native PowerShell tests also exercise selected-source execution through real
Registry checkpoints, success/failure replay prevention, and descendant cleanup
on cancellation. No test invokes reboot or hostname mutation on shared CI.

The constant PowerShell bootstrap dot-sources the approved ScriptBlock in its
own process scope and initializes only global:LASTEXITCODE so a local variable
does not shadow the native exit code; test both native
nonzero exits and terminating errors. Native tests allow a cold interpreter
startup without changing configured production session deadlines. The actual
interpreter result table allows 120 seconds on hosted runners; its first launch
can exceed a minute. Permission-test cleanup restores only the DACL when the
trusted owner has not changed, without requesting another ownership mutation.

Before Execute, started intent is committed and flushed into both alternating
checkpoint slots. Recovery from one damaged slot therefore retains replay
protection; terminal evidence may conservatively recover as started/unknown.

The PowerShell child environment supplies fixed PATHEXT (.COM/.EXE/.BAT/.CMD)
and native System32 ComSpec as well as fixed PATH/SystemRoot. Windows PowerShell
5.1 needs native executable classification to wait and preserve exit codes;
never inherit these values from the launching user. Verify cmd.exe and a real
standalone EXE returning a nonzero code under the actual privileged job.

Windows PowerShell 5.1 appends .CPL to its process PATHEXT during startup.
Native environment tests allow that OS addition while rejecting inherited
extensions/ComSpec and verifying actual native exit-code behavior.

`tests/e2e/2026-11-06-powershell-user-data.md` records LocalSystem PowerShell
acceptance on a replacement Windows Server 2025 guest, including one explicitly
authorized administrator reboot, unchanged one-shot journal/counter, automatic
service resume, and cleanup. It does not validate attached media, hostname
rename, or Server 2022. Guest deployment used the development CLI, not MSI.

PR 5 review hardening: before loading or saving production HKLM state, validate
the dedicated key and every ancestor from SOFTWARE. Only SYSTEM, Administrators
and Windows TrustedInstaller may own/mutate them; other principals may read.
Create missing keys atomically with protected SYSTEM/Administrators ACLs. Reject
unsafe existing keys without rewriting permissions or losing journal history.
Validate the boot marker ACL too. HKCU remains an isolated storage-test facility.
Allow the system SOFTWARE key's inheritable CREATOR OWNER placeholder while
independently validating every actual child owner and mutation ACE; new state
keys use explicit protected ACLs. Existing ancestors need only read access when
updating a protected child, not an unnecessary CREATE_SUB_KEY handle right.
The acceptance record also distinguishes later state and native process review
regressions on that guest from the earlier LocalSystem service/reboot acceptance.
The review's temporary TestMain overlay located cross-built fixtures only; test
bodies and production code were unchanged and no guest Go installation needed.

Lifecycle cancellation must match the root context error. A private PowerShell
session deadline has a distinct context cause and must retain failed/timeout
evidence even if SCM stop or a root deadline arrives concurrently.

## Metadata process SDK stage (ADR 0014)

- Datasource v1 supports explicit named registration; retain ConfigDrive's
  original Serve/PluginKey and protobuf wire compatibility. Host validates each
  datasource response with its own parser; never normalize NoCloud as ConfigDrive.
- Provisioner v1 is metadata-only and idempotent. Describe includes exact name,
  kind, version and idempotent semantics. Apply returns a closed outcome enum;
  unspecified/unknown values fail. No arbitrary config/Any, secrets, scripts,
  platform callbacks or state handles belong in this retryable contract.
- Ship NoCloud and hostname as separate EXEs, excluded from MSI and host wrapper
  dependency graph. Preserve builtin execution for existing v1 configs, explicit
  external selection, per-plugin enablement, pins and no failure fallback.
- External hostname additionally requires allow_privileged. Its role retains
  elevated caller identity, removes shutdown privileges, uses a high-integrity
  private desktop and a one-process/256 MiB kill-on-close job. Datasource and
  PowerShell roles keep their previous restrictions/limits. Never conflate them.
- Keep SDK packages independent of host internals. The independent-consumer test
  builds a separate temporary Go module; source and protobuf conformance stay
  together while experimental. No automatic arbitrary-name registration exists.
- Session deadlines include verification/startup/RPC; private provisioner timeout
  causes must not be mistaken for root cancellation. Discard child output and
  remote errors, enforce 128 KiB bounds, and close jobs/images on every path.
- Native hostname tests target the already configured name and verify unchanged
  before/after state; neither CI nor this guest acceptance renames or reboots.
  Guest verification uses the existing SSH host without changing identity/SSHD.

The SDK guest negative-pin acceptance exposed a fast SCM startup failure: a
stopped service must make `service start` return fixed numeric exit codes
immediately, rather than waiting forever for Running. Preserve ordinary stop
cleanup and context cancellation; the native wait-state regression covers a
StartPending-to-Stopped transition. Operator icacls examples set ownership and
grants in separate commands because combining /setowner with /grant is invalid.

Cancellation conformance fixtures signal readiness from the actual handshake,
Describe or Apply operation before the test cancels the root context. Do not
label a startup timeout as RPC cancellation; private-timeout tests also prove
they reached Apply. Fixture readiness files are isolated test resources.

`tests/e2e/2026-11-07-metadata-sdk.md` records native SDK/role tests and the
LocalSystem external NoCloud/hostname pipeline on Server 2025, with same-name
reconciliation, real service restart, negative pins and disablement. Hostname
and OS boot time remained unchanged; no reboot was authorized or performed.
The test-owned service/configuration/Registry/files were cleaned. This was a
development CLI deployment, not guest MSI or Server 2022 acceptance.

PR 6 review: external NoCloud/ConfigDrive use a distinct private timeout cause,
like hostname and PowerShell. When terminal persistence overlaps the real root
deadline, preserve the datasource failed checkpoint and return an error; never
classify the independent session limit as root cancellation. The regression
uses actual named fixture processes and waits only the failed-save response for
a real root deadline, covering both datasource names.

The PR 6 review follow-up in the SDK acceptance record verifies the distinct
datasource-timeout cause on the same Server 2025 guest, alongside another
LocalSystem normal/negative/disabled service pipeline run. The deadline timing
regression uses memory checkpoints; the separate service acceptance uses real
Registry storage. Preserve that distinction and the no-reboot/cleanup evidence.
