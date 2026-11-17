# pin

Pins globally installed CLIs to a specific Node runtime.

A tool installed with `npm install -g` follows whatever node is on PATH, so moving between projects with different Node versions moves the tool with them. Volta's `volta install -g` did it differently: it bound the tool to the Node version it was installed with, so the tool kept working no matter which project or Node version you switched to. That binding has no replacement since Volta stopped being maintained, and `pin` does only that — around 2200 lines, no downloader.

Chinese docs: [README.md](README.md)

```
$ fnm use 14            # this shell is Node 14
$ ni -v                 # ni still runs on the Node 20 it was pinned to
```

## Install
Prebuilt binaries live in [Releases](https://github.com/bigshans/pin/releases); verify them with `SHA256SUMS`. The binary can live anywhere on PATH:

```bash
# linux amd64
curl -L -o ~/.local/bin/pin \
  https://github.com/bigshans/pin/releases/latest/download/pin-linux-amd64
chmod +x ~/.local/bin/pin
```

On Windows grab `pin-windows-amd64.exe`, rename it to `pin.exe` and put it in any directory on PATH.

Building from source needs Go 1.21 or newer and no third-party dependencies:

```bash
git clone git@github.com:bigshans/pin.git
cd pin
./build.sh install      # builds into ~/.local/bin/pin (override with PIN_BIN_DIR), relinks shims
```

Add `~/.pin/bin` (`%USERPROFILE%\.pin\bin` on Windows) to your PATH, ahead of your version manager. That directory holds nothing but shims: the `pin` binary does not need to be there, since a shim is a hard link to it and two paths on one filesystem cost one copy (across filesystems it degrades to a copy — 3MB per shim).

After replacing the pin binary, `pin doctor` reports every shim as stale — each one records the build that wrote it, so a new build no longer matches — and `pin doctor --fix` (or `pin shims`) relinks them. `go build -o` always produces a new inode, so every rebuild needs it.

Node comes from whatever version manager you already use (fnm, nvm, mise, asdf, proto, vfox and volta are all recognised); `pin` never downloads anything itself.

## Usage

```bash
pin @antfu/ni                 # install and pin using the node currently on PATH
pin -n 20 @antfu/ni           # pin to Node 20 (20, 20.20 and 20.20.2 all work)
pin -n 20 cowsay@1.6.0        # package version and runtime together
pin --install -n 18 cowsay    # Node 18 isn't installed; let the manager do it first
pin --pm pnpm @antfu/ni       # install with pnpm (npm / pnpm / yarn)
pin update                    # upgrade everything pinned, bindings untouched
pin update cowsay@1.6.0       # upgrade to a specific version
pin -n 22 ni                  # already pinned: just re-point it, no reinstall
pin                           # list pinned tools (same as pin list)
pin which ni                  # show what the ni shim actually executes
pin rm cowsay                 # unpin; a bin name works too: pin rm cowthink
pin doctor                    # check runtimes, entry points and shims
pin doctor --fix              # same, but re-bind stale shims and drop orphans
pin shims                     # relink every shim; same as pin doctor --fix
pin completion bash           # print the completion script
pin version
```

Every verb has a full name and short spellings, all equivalent: `install` (`add`, `a`, `i`), `update` (`up`), `list` (`ls`, `l`), `rm` (`r`, `remove`, `uninstall`, `u`), `which` (`w`), `doctor` (`d`), `shims` (`s`), `version` (`v`), `help` (`h`). `pin help` lists the full names only.

Re-running `pin -n <ver>` on something already pinned only re-points its shims at
another Node: the entry file does not change, plain JavaScript does not care which
Node runs it, so it takes tens of milliseconds and no network. Packages carrying
native code (`.node` / `binding.gyp`) are reinstalled instead of re-pointed, since
that compiled output only fits one node ABI — no flag to remember.

Three verbs, one job each:

| Command | What it does |
|---|---|
| `pin <pkg>` | **install only**. An already pinned package — or one of its bin names — is refused with a pointer to the other two |
| `pin update [<pkg>@<ver>]...` | **upgrade only**. No arguments upgrades everything; bindings do not move |
| `pin -n <ver> <pkg>` | **re-point only**. No reinstall, no upgrade |

They are separate because `pin cowsay` doing both install and upgrade would mean
different things depending on whether you had run it before. Only an explicit
`-n` (or `PIN_NODE`) moves a pin, so a command means the same thing in every
shell — `pin update ni` inside a Node 14 project will not drag ni from 20 down to 14.

If the first argument isn't a command it's treated as a package name, so `pin add ...` and `pin @antfu/ni` are the same.

## Completion

Completions are dynamic: they offer pinned package names, their bin names and the Node versions actually installed on the machine. Two ways to install, pick one:

**1. eval it (simplest; runs `pin` once per shell start)**

```bash
# bash, in ~/.bashrc
eval "$(pin completion bash)"

# zsh, in ~/.zshrc
eval "$(pin completion zsh)"

# fish, in ~/.config/fish/config.fish
pin completion fish | source
```

**2. drop it in as a file (zero startup cost; the shell's completion system loads it lazily)**

```bash
pin completion zsh  > ~/.local/share/zsh/site-functions/_pin   # must be in $fpath before compinit
pin completion bash > ~/.local/share/bash-completion/completions/pin
pin completion fish > ~/.config/fish/completions/pin.fish
```

bash's XDG directory and fish's `completions/` are scanned by default, so nothing else to configure. zsh needs the directory on `$fpath`, added before `compinit` runs. The first line of `pin completion zsh` is `#compdef pin`: without that tag zsh's compinit skips the file silently and only the eval route works.

PowerShell has no `complete` builtin (that is a bash thing); it uses `Register-ArgumentCompleter`, so append the generated line to your `$PROFILE`:

```powershell
Add-Content $PROFILE 'pin completion powershell | Out-String | Invoke-Expression'
```

Then open a new pwsh window. Completion comes from `Register-ArgumentCompleter -Native` and covers `pin <TAB>`, `pin -n <TAB>`, `pin rm <TAB>` and `pin completion <TAB>`.

## Where the runtime comes from

`pin` doesn't assume a particular version manager. Runtimes are looked up through this table; locations whose environment variable isn't set are skipped, so it doesn't matter which managers you have installed.

| Manager | Version directory | `--install` runs |
|---|---|---|
| fnm | `$FNM_DIR/node-versions/*`, `~/scoop/apps/fnm/current/node-versions/*` | `fnm install <ver>` |
| nvm | `$NVM_DIR/versions/node/*`, `~/.nvm/versions/node/*` | `nvm install <ver>` |
| mise | `$MISE_DATA_DIR/installs/node/*`, `~/.local/share/mise/installs/node/*` | `mise install node@<ver>` |
| asdf | `$ASDF_DATA_DIR/installs/nodejs/*`, `~/.asdf/installs/nodejs/*` | `asdf install nodejs <ver>` |
| proto | `$PROTO_HOME/tools/node/*`, `~/.proto/tools/node/*` | `proto install node@<ver>` |
| vfox | `~/.vfox/cache/nodejs/v-*` | `vfox install nodejs@<ver>` |
| volta | `~/.volta/tools/image/node/*` | `volta install node@<ver>` |

If nothing matches, the directory is derived from `exec.LookPath("node")`; `-n <absolute path>` always works as an escape hatch. Supporting a new manager means adding one entry to the `runtimeManagers` table, which also covers discovery, self-healing, fix hints and `--install`.

The resolved node path is written into the shim at install time, so a pin binds a path rather than a version constraint. If that path disappears later (manager upgrade, `fnm uninstall`), the shim looks the recorded version up once more before failing; when it does fail, it prints the command that would fix it.

## Where the package manager comes from

Installing a package is handed to one of three managers. There are two settings,
no hidden state: `pin --pm <name>` for this run, `PIN_PM` for the environment, and
npm when neither is set.

```bash
PIN_PM=pnpm pin cowsay      # the default for this shell (put it in your profile)
pin --pm yarn cowsay        # just this once
```

pin deliberately does not remember what installed a package last time: the same
command should mean the same thing whenever you run it. Which manager was
actually used is recorded as a fact and shown by `pin list`; change the setting and
the next install follows it.

All three work: `npm install`, `pnpm add`, `yarn add`. `$PIN_HOME/pkgs/<key>` is
treated as a real project directory — pin drops a `package.json` with nothing
but `name` and `private` in it, lets the chosen manager fill in `node_modules`,
and then reads the package's own `package.json` `bin` field for the entries. So
npm's flat tree and pnpm's symlinked store both work: pin never has to understand
the layout.

Switching managers wipes the directory first; the two layouts do not mix. yarn 2+
defaults to Plug'n'Play, which has no `node_modules` at all, so pin refuses it
with a pointer to `nodeLinker: node-modules` in `.yarnrc.yml`.

## How it works

```
$PIN_HOME (default ~/.pin)
├── bin/<tool>         hard link to the pin binary; argv[0] decides the role
├── tools/<tool>.txt   seven plain lines: kind, node, entry, tool, runtime version, pin build
├── pkgs/<name>/       where npm install -g --prefix puts the package
└── pins.json          registry, read only by the CLI, never by a shim
```

Running a shim does three things: read a couple hundred bytes of text, append the pinned runtime directory to PATH, hand the process over to node. Linux and macOS use `syscall.Exec`, so the shim is replaced rather than forked; Windows has no exec(3) and costs one extra process — which is put in a job object, so it and everything it spawns live and die with the shim. Every shim is a hard link to the same binary, so ten commands still occupy one copy on disk.

The entry file is always executed through the absolute pinned runtime path, so "`ni` runs on Node 20" holds regardless of PATH. Child processes are a different matter: forwarders such as `ni`, `nr` and `npm run` just hand the work to the project's own package manager, so a `node`/`npm` already on PATH (whichever version the shell's manager selected) has to win. Otherwise `nr test` inside a Node 14 repo would run the suite on Node 20. Hence the runtime directory is appended to PATH, serving only as a fallback when no node is on PATH at all.

For the opposite need — a tool whose own children must stay on the pinned runtime (native modules, wrappers that spawn node again) — set `PIN_STRICT_RUNTIME=1` to put the runtime directory back at the front of PATH.

## Performance

Windows 11, Node 20.20.2, averaged over 30 warm runs:

| Case | Per run |
|---|---|
| `node hello.mjs` | 74.1 ms |
| Same script through a shim | 87.4 ms |
| Reference: `cmd /c echo hi` (process creation alone) | 25.5 ms |
| Reference: `pin v` (one pin startup) | 26.6 ms |

The ~13ms difference is one extra process, Go runtime startup, a 200-byte read and four syscalls for the job object. Process creation alone costs about 25ms on this machine, so absolute numbers move with the machine and its antivirus; the delta is the honest part. On Linux the exec path should land at 1 to 2ms, but I don't have a Linux box to measure on.

## Cross-compiling

```bash
./build.sh          # five targets into dist/, plus SHA256SUMS
./build.sh install  # build for this machine into $PIN_BIN_DIR (default ~/.local/bin), relink shims
```

The binary is about 2.9MB.

## Environment

| Variable | Meaning |
|---|---|
| `PIN_HOME` | Install root, defaults to `~/.pin` |
| `PIN_NODE` | Runtime directory used when `-n` is omitted |
| `PIN_PM` | Package manager used when `--pm` is omitted and the package has no record yet |
| `PIN_STRICT_RUNTIME` | When non-empty, put the pinned runtime first on PATH so child processes follow it instead of the project |

## Known limitations

- No downloading. Either let `--install` hand it to your manager, or install the version yourself first.
- yarn 2+ Plug'n'Play is not supported: PnP has no `node_modules` and the entry lives inside a zip, which would need a loader injected into the shim. Set `nodeLinker: node-modules` for packages installed by pin.
- A native binary shim is executed directly without injecting Node; `.cmd` and `.bat` go through `cmd.exe /c` on Windows.
- One version per package. For several, `pin rm` first; concurrent versions would need a different registry key. The install directory and the registry key are the last path segment of the package name, so `@a/cli` and `@b/cli` cannot coexist either — the second is refused instead of quietly replacing the first.
- On Windows, Ctrl+C is broadcast by the shared console to the tool and the process tree it spawned; the shim neither swallows it nor exits first, it just waits for the tool to finish. If the tool detached itself into its own process group (`detached: true`) the broadcast never arrives, so it is killed after a 5 second grace and the shim exits 130; a second Ctrl+C skips the wait. Closing the terminal window or ending pin in Task Manager takes the tool and everything it spawned with it — that is the job object's doing, and it needs no code of ours to run.
- Pins store absolute paths, so moving to another machine or architecture means pinning again.

## License

MIT
