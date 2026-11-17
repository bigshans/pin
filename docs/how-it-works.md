# How pin works

[中文](how-it-works.zh.md) · [README](../README.md)

The short version: one small Go binary, hard-linked under the name of every
pinned command, which reads six lines of text and hands the process over to node.

```
$PIN_HOME (default ~/.pin)
├── bin/<tool>          hard link (a copy when the filesystem can't link) of the pin binary
├── tools/<tool>.txt    per-shim metadata, 7 plain lines, read before every invocation
├── pkgs/<name>/        where the package itself was installed
└── pins.json           the registry — read by the CLI only, never by a shim
```

## The shim path

`pin` and every pinned command are the same file. `argv[0]` says which one it is:
called as `pin` it runs the CLI, called as `ni` it reads `tools/ni.txt` and
execs.

```
pin1
node
/home/user/.local/share/fnm/node-versions/v20.20.2/installation/bin/node
/home/user/.pin/pkgs/ni/node_modules/@antfu/ni/bin/ni.mjs
ni
20.20.2
0.2.0
```

kind · node · entry · owning package · runtime version · the pin build that wrote
the file. The runtime version lets a shim heal itself when a manager moves or
uninstalls a version; the build stamp is what lets `pin doctor` notice that the
binary was replaced (a rebuild creates a new inode and leaves every shim behind,
indistinguishable from a copy).

Everything the shim does before the handover: one read (5 syscalls, 17
allocations total, ~4µs). On Linux and macOS it `exec`s, so no extra process
exists; Windows has no `exec`, so it starts a child inside a job object — the
child and everything it spawns die with the shim, and Ctrl+C is passed through
rather than swallowed.

All shims are hard links to one file, so ten pinned commands cost one 3MB copy on
disk, not ten.

## Which runtime a tool gets

The node path is stored absolutely, so the binding does not depend on PATH,
version manager state, or which shell you are in. When that path disappears, the
shim re-finds the same version label once, rewrites itself, and carries on.

`pin` looks for Node in the managers it knows about. Locations whose variables
are unset are skipped, so it does not matter which one you use:

| Manager | Versions live in | `--install` forwards to |
|---|---|---|
| fnm | `$FNM_DIR/node-versions/*`, `~/scoop/apps/fnm/current/node-versions/*` | `fnm install <ver>` |
| nvm | `$NVM_DIR/versions/node/*`, `~/.nvm/versions/node/*` | `nvm install <ver>` |
| mise | `$MISE_DATA_DIR/installs/node/*`, `~/.local/share/mise/installs/node/*` | `mise install node@<ver>` |
| asdf | `$ASDF_DATA_DIR/installs/nodejs/*`, `~/.asdf/installs/nodejs/*` | `asdf install nodejs <ver>` |
| proto | `$PROTO_HOME/tools/node/*`, `~/.proto/tools/node/*` | `proto install node@<ver>` |
| vfox | `~/.vfox/cache/nodejs/v-*` | `vfox install nodejs@<ver>` |
| volta | `~/.volta/tools/image/node/*` | `volta install node@<ver>` |

Adding a manager is one entry in `runtimeManagers`; lookup, healing, hints and
`--install` all follow from it. When no manager matches, the directory is derived
from `exec.LookPath("node")`, and `-n <absolute path>` always works.

## PATH, and the child processes a tool starts

The pinned runtime is used for the entry file, which is an absolute path. The
children a tool starts are a different question.

Tools like `ni`, `nr` or `npm run` only forward to the project's own package
manager, so whatever the shell's version manager put on PATH has to win —
otherwise `nr test` in a Node 14 repo would run your tests on Node 20. So the
runtime directory is **appended** to PATH, answering only when nothing else
provides node.

The reverse need — a tool whose own child processes must stay on the pinned
runtime (native modules, wrappers that spawn node) — is `PIN_STRICT_RUNTIME=1`,
which prepends instead.

## Where packages come from

`pin --pm <name>` sets the manager for one run, `PIN_PM` for the shell, and the
default is npm. `pin` never remembers what a package was installed with: the same
command means the same thing every time.

All three work (`npm install`, `pnpm add`, `yarn add`) because `pin` treats
`$PIN_HOME/pkgs/<key>` as a real project directory — it writes a `package.json`
with just `name` and `private`, lets the manager install into it, then reads
`node_modules/<pkg>/package.json` for the `bin` map. npm's flat tree and pnpm's
symlinked store both satisfy that; `pin` never needs to understand their layouts.
Switching manager deletes the old directory first, because the two layouts cannot
be mixed.

yarn 2+ Plug'n'Play has no `node_modules`, so `pin` refuses it and asks you to set
`nodeLinker: node-modules`.

## Completion

Completion is dynamic: it offers pinned packages, their bin names and the Node
versions actually installed.

Two ways to install it.

```bash
# 1. eval it — simplest, runs `pin` once per shell start
eval "$(pin completion zsh)"                 # in ~/.zshrc
eval "$(pin completion bash)"                # in ~/.bashrc
pin completion fish | source                 # in ~/.config/fish/config.fish
```

```bash
# 2. drop it in as a file — no startup cost, lazily loaded
pin completion zsh  > ~/.local/share/zsh/site-functions/_pin   # must be in $fpath before compinit
pin completion bash > ~/.local/share/bash-completion/completions/pin
pin completion fish > ~/.config/fish/completions/pin.fish
```

The bash XDG directory and fish's `completions/` are scanned by default. zsh needs
its directory on `$fpath` before `compinit` runs, and the generated file starts
with `#compdef pin` — without that tag compinit skips it silently and only the
`eval` route works.

PowerShell has no `complete` builtin, so append the generated line to `$PROFILE`:

```powershell
Add-Content $PROFILE 'pin completion powershell | Out-String | Invoke-Expression'
```

## Performance

Windows 11, Node 20.20.2, 30 runs each after a warm-up:

| | |
|---|---|
| `node hello.mjs` | 74.1 ms |
| the same script through a shim | 87.4 ms |
| reference: `cmd /c echo hi` (process creation only) | 25.5 ms |
| reference: `pin v` (one pin startup) | 26.6 ms |

Linux, same measurement: `nuxt --version` through a shim 49.9 ms against 48.7 ms
for the identical node and entry invoked directly — the shim is ~1.2 ms, of which
~4 µs is its own code, and the rest is Go's runtime starting.

That is the whole cost model: the process handover, not the code. Anything that
matters is inside node's own startup.

## Environment

| Variable | Meaning |
|---|---|
| `PIN_HOME` | install root, default `~/.pin` |
| `PIN_NODE` | runtime directory used when `-n` is omitted |
| `PIN_PM` | package manager when neither `--pm` nor history decides |
| `PIN_STRICT_RUNTIME` | non-empty: put the pinned runtime first on PATH for child processes |

## Known limits

- `pin` does not download Node. Either `--install` hands it to your manager, or
  install the version yourself.
- yarn 2+ Plug'n'Play is not supported (no `node_modules`; the entry lives in a
  zip and needs a loader). Use `nodeLinker: node-modules` for pinned packages.
- A `bin` that is a native executable runs directly, without node injected;
  `.cmd` / `.bat` on Windows go through `cmd.exe /c`.
- One version per package. For more, `pin rm` first. The registry key and
  install directory are the last path segment of the package name, so `@a/cli`
  and `@b/cli` cannot coexist either — the second is refused rather than quietly
  replacing the first.
- On Windows, Ctrl+C is broadcast by the shared console to the tool and its whole
  tree; the shim does not swallow it and does not exit early. A tool that put
  itself in its own process group (`detached: true`) never sees the broadcast and
  is killed after 5 seconds, with the shim exiting 130; a second Ctrl+C skips the
  wait. Closing the terminal or ending pin kills the tool and its descendants via
  the job object, without relying on pin still being able to run code.
- Moving to another machine or architecture means re-running `pin`: absolute paths
  are stored, on purpose.
