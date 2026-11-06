#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dist_dir=${DIST_DIR:-"$repo_root/dist"}
version=${VERSION:-$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || printf '%s' dev)}
commit=${COMMIT:-$(git -C "$repo_root" rev-parse --verify HEAD 2>/dev/null || printf '%s' unknown)}
build_date=${BUILD_DATE:-$(git -C "$repo_root" show -s --format=%cI HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)}

mkdir -p "$dist_dir"

ldflags="-s -w -buildid= -X=github.com/randomtoy/wininit/internal/version.Version=$version -X=github.com/randomtoy/wininit/internal/version.Commit=$commit -X=github.com/randomtoy/wininit/internal/version.BuildDate=$build_date"

(
    cd "$repo_root"
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
        -trimpath \
        -buildvcs=true \
        -ldflags "$ldflags" \
        -o "$dist_dir/wininit-windows-amd64.exe" \
        ./cmd/wininit
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
        -trimpath -buildvcs=true -ldflags "$ldflags" \
        -o "$dist_dir/wininit-plugin-configdrive-windows-amd64.exe" \
        ./cmd/wininit-plugin-configdrive
)

printf 'Built %s\n' "$dist_dir/wininit-windows-amd64.exe"
