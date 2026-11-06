#!/usr/bin/env sh
set -eu
# Fail if a host build ever starts linking the independently shipped executor.
if go list -deps ./cmd/wininit | rg -q '^github.com/randomtoy/wininit/plugins/provisioner/powershell$'; then
    printf '%s\n' 'The PowerShell implementation must remain outside the agent dependency graph.' >&2
    exit 1
fi
