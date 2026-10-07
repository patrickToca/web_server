#!/usr/bin/env bash
#
# Release build with git metadata injected via -ldflags -X.
#
# Usage:
#   scripts/build.sh [output-path]
#
# Defaults to bin/mywebapp. The script is idempotent and works from
# any working directory: it resolves the repository root from its own
# location.
#
# If the current HEAD is an exact tag, the tag name (e.g. "v0.7.0")
# becomes the version. Otherwise the version is `git describe` output,
# e.g. "v0.7.0-3-gabc1234" or "v0.7.0-3-gabc1234-dirty".
#
# All git commands tolerate a missing .git directory (tarball builds)
# by falling back to empty strings; the runtime VCS stamp in
# debug.ReadBuildInfo covers that case.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Package path for -X. Must match the import path used by main.go.
PKG="mywebapp/internal/version"

# Entry point. Change if your main package lives elsewhere.
MAIN="./cmd/api"

# ---------------------------------------------------------------------
# Gather git metadata. Every command has a safe fallback.
# ---------------------------------------------------------------------
SHORT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo "")"
BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

EXACT_TAG="$(git describe --tags --exact-match HEAD 2>/dev/null || echo "")"
DESCRIBE="$(git describe --tags --always --dirty 2>/dev/null || echo "")"

if [ -n "${VERSION_OVERRIDE:-}" ]; then
    # Explicit override always wins. Useful for tarball builds.
    VERSION="$VERSION_OVERRIDE"
elif [ -n "$EXACT_TAG" ]; then
    VERSION="$EXACT_TAG"
else
    VERSION="$DESCRIBE"
fi

if [ -z "$VERSION" ]; then
    VERSION="dev"
fi

# ---------------------------------------------------------------------
# Build.
# ---------------------------------------------------------------------
OUT="${1:-bin/mywebapp}"
mkdir -p "$(dirname "$OUT")"

LDFLAGS="-X ${PKG}.Version=${VERSION}"
LDFLAGS+=" -X ${PKG}.GitCommit=${SHORT}"
LDFLAGS+=" -X ${PKG}.GitBranch=${BRANCH}"
LDFLAGS+=" -X ${PKG}.BuildDate=${DATE}"
LDFLAGS+=" -X ${PKG}.GitState=${DESCRIBE}"

echo "building ${OUT}"
echo "  version: ${VERSION}"
echo "  commit:  ${SHORT:-<none>}"
echo "  branch:  ${BRANCH:-<none>}"
echo "  state:   ${DESCRIBE:-<none>}"
echo "  date:    ${DATE}"

go build \
    -trimpath \
    -ldflags "${LDFLAGS}" \
    -o "${OUT}" \
    "${MAIN}"

echo "built ${OUT}"