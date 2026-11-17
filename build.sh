#!/usr/bin/env bash
# Cross-compile pin for the platforms we care about (Linux + Windows).
set -euo pipefail
cd "$(dirname "$0")"

PIN_HOME="${PIN_HOME:-$HOME/.pin}"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
LDFLAGS="-s -w -X main.version=$VERSION"
export CGO_ENABLED=0
mkdir -p dist

# ./build.sh install  → build for this machine straight into $PIN_HOME/bin and
# relink every shim to it (shims are hard links, so they must be recreated).
if [ "${1:-}" = "install" ]; then
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) name=pin.exe ;;
    *) name=pin ;;
  esac
  mkdir -p "$PIN_HOME/bin"
  go build -trimpath -ldflags "$LDFLAGS" -o "$PIN_HOME/bin/$name" .
  echo "installed $PIN_HOME/bin/$name"
  "$PIN_HOME/bin/$name" s
  exit 0
fi

build() { # os arch out
  GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "$LDFLAGS" -o "dist/$3" .
  echo "built dist/$3"
}

build linux   amd64 pin-linux-amd64
build linux   arm64 pin-linux-arm64
build windows amd64 pin-windows-amd64.exe
build windows arm64 pin-windows-arm64.exe
build darwin  arm64 pin-darwin-arm64

if command -v sha256sum >/dev/null; then
  # Only the binaries, not a previous SHA256SUMS (glob order would include it).
  (cd dist && rm -f SHA256SUMS && sha256sum pin-* > SHA256SUMS)
fi
ls -l dist
