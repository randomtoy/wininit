#!/usr/bin/env sh
set -eu
# Inspect the actual Windows host graph, including Windows-only imports.
dependencies=$(GOOS=windows GOARCH=amd64 go list -deps ./cmd/wininit)
case "$dependencies" in
    *github.com/randomtoy/wininit/plugins/provisioner/powershell*)
        printf '%s\n' 'The PowerShell implementation must remain outside the agent dependency graph.' >&2
        exit 1
        ;;
esac
for package in plugins/datasource/nocloudprocess plugins/provisioner/hostnameprocess; do
    case "$dependencies" in
        *github.com/randomtoy/wininit/$package*)
            printf '%s\n' "External implementation $package is compiled into the agent." >&2
            exit 1
            ;;
    esac
done
# The SDK must be consumable without access to the agent's internal packages.
sdk_dependencies=$(go list -deps ./sdk/datasource/v1 ./sdk/provisioner/v1 ./sdk/userdata/v1)
case "$sdk_dependencies" in
    *github.com/randomtoy/wininit/internal/*)
        printf '%s\n' 'The public SDK must not import host internals.' >&2
        exit 1
        ;;
esac
