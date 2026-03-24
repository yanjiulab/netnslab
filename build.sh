#!/usr/bin/env bash
set -euo pipefail

# Build netnslab locally with version info injected,
# using the same ldflags scheme as CI.
#
# Usage:
#   build.sh [version]
# Example:
#   build.sh v0.1.0
#   build.sh          # auto-detect from git (fallback: dev)

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"

LDFLAGS="-s -w \
-X main.Version=${VERSION} \
-X main.Commit=${COMMIT} \
-X main.Date=${DATE} \
-X main.BuiltBy=local"

echo "Building netnslab locally with:"
echo "  VERSION=${VERSION}"
echo "  COMMIT=${COMMIT}"
echo "  DATE=${DATE}"
echo "  GOOS=${GOOS:-$(go env GOOS)}"
echo "  GOARCH=${GOARCH:-$(go env GOARCH)}"

export CGO_ENABLED="${CGO_ENABLED:-0}"

go build -trimpath -ldflags "${LDFLAGS}" -o netnslab ./cmd/netnslab

echo "Done."
echo "  ./netnslab -v"