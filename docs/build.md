# Build

[中文](build.zh.md) · [README](../README.md)

Go 1.21 or newer, no third-party dependencies.

```bash
git clone git@github.com:bigshans/pin.git
cd pin
./build.sh install      # build for this machine and relink the shims
```

`install` writes the binary to `$PIN_BIN_DIR` (default `~/.local/bin`; override it
to put the binary elsewhere), then runs `pin shims` so existing shims link to the
new file. The version string comes from `git describe`, so a checkout without tags
reports something like `v0.2.0-3-g12f29c7`.

Shims are hard links, so **every rebuild needs the relink**: `go build -o` writes
a new inode and leaves the old one behind, still held by all the shims. That is
what `./build.sh install` does for you, and what `pin doctor` warns about if you
build by hand.

### Windows

`build.bat` is the same two commands in plain batch — no PowerShell, no helper
script, and nothing to do with the execution policy:

```bat
build.bat install      :: build pin.exe for this machine and relink the shims
build.bat              :: dist\ with five targets + SHA256SUMS
```

`install` writes `pin.exe` to `%PIN_BIN_DIR%` (default `%USERPROFILE%\bin`)
and runs `pin shims`. Two Windows-only extras: it refreshes
`%USERPROFILE%\.pin\bin\pin.exe` as well — that shim directory sits on PATH, so a
stale hard link there would keep answering for the old build — and it then
compares every shim against the new binary byte for byte (`fc /b`), because
`pin doctor` compares build stamps and two builds of the same version string look
identical. A hard link needs one volume: keep `%PIN_BIN_DIR%` on the same drive
as `%PIN_HOME%`, or the shims become copies.

`SHA256SUMS` is written with LF endings and hashed with `certutil`: batch can
only echo CRLF, and `sha256sum -c` would look for `pin-linux-amd64<CR>`.

## Cross-compilation

```bash
./build.sh          # dist/: five targets + SHA256SUMS
```

| Artifact | |
|---|---|
| `pin-linux-amd64` | |
| `pin-linux-arm64` | |
| `pin-windows-amd64.exe` | |
| `pin-windows-arm64.exe` | |
| `pin-darwin-arm64` | |

Artifacts are ~2.2MB, statically linked, `CGO_ENABLED=0`, built with
`-trimpath -ldflags "-s -w -X main.version=$(git describe …)"`.

## Releasing

```bash
git tag -a vX.Y.Z -m "pin vX.Y.Z"
./build.sh                                    # dist/ + SHA256SUMS
gh release create vX.Y.Z --title vX.Y.Z \
  --notes-file notes.md dist/pin-* dist/SHA256SUMS
```

Build after tagging: the binaries carry the tag in `pin version`, and `pin doctor`
compares that stamp against each shim's.

Upgrading users replace the binary and run `pin shims` (or `pin doctor --fix`) —
the release notes should say so.

## Tests

```bash
go test ./...                                  # unit tests
go test -run xxx -bench . ./...                # registry, shim read, hot path
```

`BenchmarkShimPlan` in `shim_bench_test.go` covers everything a shim does before
exec; it is the one to watch when touching the hot path.
