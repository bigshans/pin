#!/usr/bin/env bash
# Cross-compile pin for the platforms we care about (Linux + Windows).
set -euo pipefail
cd "$(dirname "$0")"

PIN_HOME="${PIN_HOME:-$HOME/.pin}"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
LDFLAGS="-s -w -X main.version=$VERSION"
export CGO_ENABLED=0
mkdir -p dist

# ./build.sh install  → build for this machine into $PIN_BIN_DIR (which only has
# to be on PATH, and on the same filesystem as $PIN_HOME for shims to stay hard
# links), then relink every shim to it (shims are hard links, so they must be
# recreated). PIN_BIN_DIR defaults to ~/.local/bin: the binary does not need to
# live in the shim directory.
if [ "${1:-}" = "install" ]; then
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) name=pin.exe ;;
    *) name=pin ;;
  esac
  PIN_BIN_DIR="${PIN_BIN_DIR:-$HOME/.local/bin}"
  mkdir -p "$PIN_BIN_DIR"
  go build -trimpath -ldflags "$LDFLAGS" -o "$PIN_BIN_DIR/$name" .
  echo "installed $PIN_BIN_DIR/$name"
  "$PIN_BIN_DIR/$name" s
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
