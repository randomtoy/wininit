PROJECT := wininit
DIST_DIR := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell git show -s --format=%cI HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
PACKAGE_VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || echo 0.0.0)

.PHONY: all build test lint package clean

all: lint test build

build:
	VERSION='$(VERSION)' COMMIT='$(COMMIT)' BUILD_DATE='$(BUILD_DATE)' ./scripts/build.sh

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || { echo "gofmt is required for:"; gofmt -l .; exit 1; }
	go vet ./...

package:
	@command -v pwsh >/dev/null 2>&1 || { echo "make package requires PowerShell and the .NET SDK; run it on Windows or install pwsh."; exit 1; }
	pwsh -NoLogo -NoProfile -File ./scripts/package.ps1 -Version '$(PACKAGE_VERSION)'

clean:
	@echo "Removing generated artifacts from $(DIST_DIR)"
	rm -rf -- '$(DIST_DIR)'
