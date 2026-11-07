# External metadata plugin SDK

Roadmap stage 6 adds named datasource v1 providers and a separate provisioner v1
contract. NoCloud and hostname are reference implementations shipped as separate
EXEs. ConfigDrive and PowerShell keep their existing protocols. All seven
logical plugins remain disabled by default. MSI installs only the agent.

## Distribution and compatibility

Keep these plugins and the SDK in this repository while the contracts are
experimental. Ship distinct `wininit-plugin-nocloud-windows-amd64.exe` and
`wininit-plugin-hostname-windows-amd64.exe` artifacts and checksums. A future
repository/module split can retain the wire protocol and conformance suite.
There is no plugin registry, automatic installation, directory scanning or
fallback after an external failure.

Existing v1 configuration keeps builtin NoCloud/hostname unless `execution:
external` is explicit. ConfigDrive's original `datasourcev1.Serve(provider)`
and `PluginKey` stay source/wire compatible. `ServeNamed(name, provider)` adds
explicit datasource identities without changing the v1 protobuf messages.

The host checks the framework handshake, service version, exact plugin name,
kind, and (for provisioners) the idempotent contract. Unknown versions and
outcomes fail closed. Compatible v1 changes may add optional fields; changed
meaning, required fields or execution semantics require a new protocol version.
There is no silent version downgrade or net/rpc fallback. The SDK is experimental;
this compatibility rule does not promise a frozen public Go API.

## Explicit configuration

Install reviewed EXEs in administrator-controlled directories. Apply the image
ownership/ACL and pinned-path requirements in [external plugins](external-plugins.md).
Replace both hash placeholders with exact independently verified SHA-256 values.

```yaml
version: 1
plugins:
  datasources:
    nocloud:
      enabled: true
      execution: external
      seed_directory: 'D:\seed'
      discover_volumes: false
      process:
        executable: 'C:\Program Files\WinInit\plugins\wininit-plugin-nocloud.exe'
        sha256: 'REPLACE_WITH_THE_NOCLOUD_EXE_SHA256'
        timeout_seconds: 30
  provisioners:
    hostname:
      enabled: true
      execution: external
      allow_privileged: true
      process:
        executable: 'C:\Program Files\WinInit\plugins\wininit-plugin-hostname.exe'
        sha256: 'REPLACE_WITH_THE_HOSTNAME_EXE_SHA256'
        timeout_seconds: 30
reboot:
  policy: never
```

Setting `enabled: false` prevents opening, hashing or launching that EXE,
regardless of old state or configured execution mode. `execution` does not
itself enable a plugin. An enabled external hostname additionally requires
`allow_privileged: true`. NoCloud is read-only and does not need that approval.

NoCloud keeps the same bounded `meta-data` parsing, volume selection, and
`instance-id`/`local-hostname` contract. It never opens user-data. Host validation
preserves NoCloud's exact hostname spelling rather than applying ConfigDrive's
normalization rules. All enabled datasources are probed; ambiguity fails before
provisioning.

Hostname keeps the conservative Windows hostname contract and native domain
mutation refusal. An unchanged active/configured name returns unchanged; a name
already configured for the next boot returns reboot required without setting it
again. A new target returns changed/reboot required. The agent never reboots.

## Provisioner v1

`sdk/provisioner/v1/provisioner.proto` has two RPCs:

- `Describe`: protocol version, name, kind and idempotent semantics.
- `Apply`: host-selected instance ID, local hostname and datasource; returns one
  closed enum outcome: unchanged, changed, reboot required, changed plus reboot
  required, or skipped. Unspecified/unknown outcomes are errors.

No arbitrary JSON/Any configuration, credentials, file contents, user-data,
Registry handles, platform callbacks or raw error text cross this API. The host
catalog currently binds this contract to hostname; arbitrary names from third
parties are not automatically registered. New host integrations require review,
explicit configuration, an appropriate process role and their own enablement.
Password/identity provisioning is outside this metadata-only contract.

Providers must reconcile safely on each enabled process start, including after
an unknown outcome from cancellation/crash. They must check context before
mutation. This is not a one-shot execution API. PowerShell continues to use
userdata v1 and its durable execution-ID journal; never move scripts onto this
retryable contract. The host owns checkpointing and the machine-wide lock.

The whole session (image verification, startup, Describe and Apply) has a
configured 1–60 second deadline. SDK provider calls also have a 60-second upper
bound. Private host session deadlines for both datasources and provisioners remain
errors even if the root lifecycle deadline expires concurrently. Successful
root cancellation retains its normal cancellation error; a datasource timeout
retains failed status and error evidence instead of resetting it to pending. Each call starts a fresh process, uses local mTLS and
128 KiB message limits, discards stdout/stderr, maps remote errors to fixed
status codes, and closes its process/job on every exit path. There is no retry
loop or persistent plugin daemon.

## Windows roles and trust

NoCloud uses ConfigDrive's restricted read-only role: deny-only Administrators,
restricting SIDs, low integrity, a private desktop, one process and 256 MiB.
Hostname needs administrative native rename access and retains the elevated
agent identity. Its high-integrity private desktop and kill-on-close job allow
one process and 256 MiB total/per-process; direct local/remote shutdown
privileges are removed. Processes start suspended and enter the job before
resume. They inherit only explicit stdio handles and a scrubbed environment.
PowerShell retains its separate eight-process/512 MiB role unchanged.

These are trusted, hash-approved plugins. Elevated code can use administrative
mechanisms outside the job; the role is not a hostile-code sandbox. Do not enable
unreviewed EXEs or infer authority from the handshake cookie. A checksum is
explicit approval of an image, not proof that the code is safe.

## Building a consumer

Only public packages under `sdk/datasource/v1`, `sdk/provisioner/v1` and
`sdk/userdata/v1` belong in an external consumer. Do not import `internal` or
in-process lifecycle interfaces. Implement `Provider`, use generated request /
response types, and call `ServeNamed` or `Serve`. For example, a declaration is:

```go
var _ provisionerv1.Provider = (*MyProvider)(nil)

func (p *MyProvider) Apply(ctx context.Context, req *provisionerv1.ApplyRequest) (*provisionerv1.ApplyResponse, error) {
    // Validate desired state, reconcile through reviewed native APIs, then
    // return the actual outcome. Never return raw secrets as error text.
    return p.reconcile(ctx, req.GetMetadata())
}

func main() {
    if err := provisionerv1.Serve("my_plugin", &MyProvider{}); err != nil {
        os.Exit(1)
    }
}
```

This illustrates the SDK shape, not an automatically installable host plugin.
The independent-consumer test creates a separate Go module outside this repo
and builds against only the public SDK. Process conformance tests exercise real
EXEs for identity/version/outcome validation, crashes, pins, cancellation,
redaction and both Windows roles. Native hostname tests target the configured
name and assert unchanged before/after snapshots; shared CI never renames or
reboots a host. Real rename/reboot and Server 2022 matrices remain separate
acceptance work. See [ADR 0014](adr/0014-metadata-process-sdk.md).
