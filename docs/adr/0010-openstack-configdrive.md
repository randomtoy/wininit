# ADR 0010: Opt-in OpenStack ConfigDrive datasource

- Status: Accepted
- Date: 2026-11-06

## Context

OpenStack supplies ISO9660/VFAT media labelled `config-2` with versioned JSON
under `openstack/`. Its hostname commonly includes a DNS suffix. Datasource
selection must remain safe when both NoCloud and ConfigDrive are configured.

## Decision

Add a separate built-in `configdrive` datasource, independently disabled by
default. Configuration version 1 gains optional settings; existing files remain
valid and do not enable the new plugin. State schema version 2 is unchanged.

Share read-only volume enumeration through `internal/platform/seedmedia` while
keeping media labels and enablement separate. Use an explicit local root or
exactly one supported mounted `config-2` volume.

Select the newest supported directory from `2025-04-04`, `2020-10-14`,
`2018-08-27`, `2017-02-22`, `2016-10-06`, `2016-06-30`, `2015-10-15`,
`2013-10-17`, `2013-04-04`, and `2012-08-10`. Use `latest` only if no supported
dated directory exists. A malformed selected version never triggers downgrade.

Read one regular UTF-8 JSON object up to 64 KiB through `os.Root`. Reject
duplicate keys, excessive nesting, symlinked OpenStack path components, and
invalid consumed fields. Consume only required `uuid` and optional `hostname`.
Normalize a valid ASCII DNS hostname/FQDN to its lowercase first label, without
truncation or suffix configuration. Keep Windows validation in the provisioner.
Never use display `name`, user-data, password, network/vendor data, EC2 layouts,
or injected file paths.

Probe all enabled datasources before provisioners. Exactly one must succeed.
Only `ErrUnavailable` permits fallback; malformed input or multiple successful
sources fail before host mutation regardless of catalog order. Do not merge
metadata, even when source instance IDs match.

## Consequences

Existing NoCloud installations retain their behavior unless another source is
explicitly enabled. Configuration fingerprints change with the expanded catalog;
saved status is reconciled after service restart. No new dependency or public
plugin API is introduced. Symlink-only `latest` layouts are unsupported.
Real OpenStack/QEMU Windows media attachment and reboot testing remains pending.

## Alternatives considered

- Extend NoCloud: rejected because formats, labels, and control are independent.
- Prefer the first source: rejected because stale media could choose identity.
- Always use `latest`: rejected in favor of explicit supported dated versions.
- Truncate hostnames/use display names: rejected to avoid unexpected names or
  collisions.
- Implement networking/user-data now: deferred to separately gated plugins.

## References

- [OpenStack metadata and config-drive guide](https://docs.openstack.org/nova/latest/user/metadata.html)
- [Config-drive administration](https://docs.openstack.org/nova/latest/admin/config-drive.html)
- [Nova metadata versions and fields](https://github.com/openstack/nova/blob/master/nova/api/metadata/base.py)
