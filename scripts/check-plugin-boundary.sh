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
