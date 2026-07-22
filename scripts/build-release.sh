#!/usr/bin/env bash
#
# build-release.sh — cross-compile telescope binaries and package them for a
# release. Produces per-target archives + a checksums file under dist/.
#
# Usage: ./scripts/build-release.sh <version>
#   e.g. ./scripts/build-release.sh v0.1.0
#
set -euo pipefail

VERSION="${1:-dev}"
BINARY="telescope"
PKG="./cmd/telescope"
VERSION_PKG="github.com/footprintai/telescope/internal/version"
DIST="dist"

# GOOS/GOARCH targets to build.
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
)

rm -rf "$DIST"
mkdir -p "$DIST"

LDFLAGS="-s -w -X ${VERSION_PKG}.V=${VERSION}"

for target in "${TARGETS[@]}"; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  name="${BINARY}_${VERSION}_${GOOS}_${GOARCH}"
  workdir="${DIST}/${name}"
  mkdir -p "$workdir"

  bin="${BINARY}"
  [ "$GOOS" = "windows" ] && bin="${BINARY}.exe"

  echo "==> building ${GOOS}/${GOARCH}"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "$LDFLAGS" -o "${workdir}/${bin}" "$PKG"

  cp README.md LICENSE "$workdir/" 2>/dev/null || true

  # Package: zip for Windows, tar.gz elsewhere.
  if [ "$GOOS" = "windows" ]; then
    ( cd "$DIST" && zip -qr "${name}.zip" "$name" )
  else
    tar -czf "${DIST}/${name}.tar.gz" -C "$DIST" "$name"
  fi
  rm -rf "$workdir"
done

# Checksums for every archive.
( cd "$DIST" && sha256sum ./*.tar.gz ./*.zip 2>/dev/null > checksums.txt || true )

echo "==> artifacts:"
ls -1 "$DIST"
