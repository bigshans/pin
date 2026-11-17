# pin

[![release](https://img.shields.io/github/v/release/bigshans/pin?style=flat-square)](https://github.com/bigshans/pin/releases)
[![license](https://img.shields.io/github/license/bigshans/pin?style=flat-square)](LICENSE)
[![go](https://img.shields.io/github/go-mod/go-version/bigshans/pin?style=flat-square)](go.mod)
[![platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macos%20%7C%20windows-lightgrey?style=flat-square)](https://github.com/bigshans/pin/releases)

**Pin globally installed CLIs to one Node runtime, so switching Node per project
doesn't break them.**

A tool installed with `npm install -g` follows whatever node is on PATH — move
between projects with different Node versions and the tool moves with them. `pin`
binds each tool to the runtime you chose, and it keeps working everywhere.

```console
$ fnm use 14          # this shell is Node 14
$ ni -v               # ni still runs on the Node 20 it was pinned to
```

[中文](README.zh.md) · [How it works](docs/how-it-works.md) · [Build](docs/build.md)

## Install

```bash
# macOS arm64 / linux amd64 / linux arm64 / windows amd64 … see Releases
curl -L -o ~/.local/bin/pin \
  https://github.com/bigshans/pin/releases/latest/download/pin-linux-amd64
chmod +x ~/.local/bin/pin
```

Anywhere on PATH works — the binary does not have to sit in the shim directory.
Windows: download `pin-windows-amd64.exe`, rename it to `pin.exe`.

Then, once, put the shim directory on PATH **ahead of your version manager**:

```bash
export PATH="$HOME/.pin/bin:$PATH"      # Windows: %USERPROFILE%\.pin\bin
```

From source: `./build.sh install` — see [Build](docs/build.md).

Node comes from the version manager you already use (fnm, nvm, mise, asdf,
proto, vfox, volta); `pin` never downloads Node.

## Use

| Command | What it does |
|---|---|
| `pin <pkg>[@ver]` | install and pin |
| `pin update [<pkg>…]` | upgrade, bindings untouched |
| `pin -n <ver> <pkg>` | pin to a runtime; on a pinned tool: re-point, no reinstall |
| `pin list` | what is pinned, and where it runs |
| `pin rm <pkg\|bin>…` | unpin |
| `pin which <bin>` | what a shim actually executes |
| `pin doctor [--fix]` | verify runtimes, entries, shims; repair what is stale |
| `pin shims` | relink every shim (same as `pin doctor --fix`) |

```console
$ pin -n 20 cowsay@1.6.0      # package version and runtime together
$ pin --pm pnpm @antfu/ni     # install with pnpm (npm | pnpm | yarn)
$ pin --install -n 18 cowsay  # Node 18 missing? let the manager install it
$ pin -n 22 ni                # already pinned: re-point it, no reinstall
$ pin rm cowthink             # a bin name is enough to unpin cowthink
```

One job per verb: `pin x` installs and refuses a package that is already pinned,
`pin update` only upgrades, `-n` only re-points. A command therefore means the
same thing every time you run it.

Verbs also accept short spellings — `pin r`, `pin ls`, `pin d` — `pin help`
lists them.

## Completion

```bash
eval "$(pin completion zsh)"     # in ~/.zshrc      (bash | zsh | fish | powershell)
eval "$(pin completion bash)"    # in ~/.bashrc
pin completion fish | source     # in ~/.config/fish/config.fish
```

Or drop the generated file where the shell already looks —
[details](docs/how-it-works.md#completion).

## Docs

| | |
|---|---|
| [How it works](docs/how-it-works.md) | shim layout, runtime resolution, PATH rules, performance, limits |
| [Build](docs/build.md) | from source, cross-compilation, release artifacts |
| [中文](README.zh.md) | 中文文档 |

MIT · the thing itself is ~2200 lines of Go, one binary, no downloader, no daemon.
