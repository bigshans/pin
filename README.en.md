# pin

Pins globally installed CLIs to a specific Node runtime.

When Volta stopped being maintained, its most useful behaviour went with it: `volta install -g` binds a tool to the Node version you installed it with, so the tool keeps working no matter which project or Node version you switch to. `pin` does only that. Under 1100 lines, no downloader.

Chinese docs: [README.md](README.md)

```
$ fnm use 14            # this shell is Node 14
$ ni -v                 # ni still runs on the Node 20 it was pinned to
```

## Install
Prebuilt binaries live in [Releases](https://github.com/bigshans/pin/releases); verify them with `SHA256SUMS`.

```bash
# linux amd64
mkdir -p ~/.pin/bin
curl -L -o ~/.pin/bin/pin \
  https://github.com/bigshans/pin/releases/latest/download/pin-linux-amd64
chmod +x ~/.pin/bin/pin
```

On Windows grab `pin-windows-amd64.exe`, rename it to `pin.exe` and drop it in `%USERPROFILE%\.pin\bin`.

Building from source needs Go 1.21 or newer and no third-party dependencies:

```bash
git clone git@github.com:bigshans/pin.git
cd pin
./build.sh install      # builds into ~/.pin/bin/pin and relinks existing shims
```

Add `~/.pin/bin` (`%USERPROFILE%\.pin\bin` on Windows) to your PATH, ahead of your version manager. `pin` and every pinned command then come from that one directory.

Node comes from whatever version manager you already use (fnm, nvm, mise, asdf, proto, vfox and volta are all recognised); `pin` never downloads anything itself.

## Usage

```bash
pin @antfu/ni                 # install and pin using the node currently on PATH
pin -n 20 @antfu/ni           # pin to Node 20 (20, 20.20 and 20.20.2 all work)
pin -n 20 cowsay@1.6.0        # package version and runtime together
pin --install -n 18 cowsay    # Node 18 isn't installed; let the manager do it first
pin                           # list pinned tools
pin w ni                      # show what the ni shim actually executes
pin r cowsay                  # unpin; a bin name works too: pin r cowthink
pin d                         # check runtimes, entry points and shims
pin d --fix                   # same, but re-bind stale shims and drop orphans
pin s                         # relink every shim (after replacing the binary)
pin completion bash           # print the completion script
pin v
```

If the first argument isn't a command it's treated as a package name, so `pin add ...` and `pin @antfu/ni` are the same. Full names also work: `add`, `ls`, `rm`, `which`, `doctor`, `shims`.

## Completion

Completions are dynamic: they offer pinned package names, their bin names and the Node versions actually installed on the machine.

```bash
# bash, in ~/.bashrc
eval "$(pin completion bash)"

# zsh, in ~/.zshrc
eval "$(pin completion zsh)"

# fish, in ~/.config/fish/config.fish
pin completion fish | source
```

PowerShell has no `complete` builtin (that is a bash thing); it uses `Register-ArgumentCompleter`, so append the generated line to your `$PROFILE`:

```powershell
Add-Content $PROFILE 'pin completion powershell | Out-String | Invoke-Expression'
```

Then open a new pwsh window. Completion comes from `Register-ArgumentCompleter -Native` and covers `pin <TAB>`, `pin -n <TAB>`, `pin r <TAB>` and `pin completion <TAB>`.

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

## How it works

```
$PIN_HOME (default ~/.pin)
├── bin/<tool>         hard link to the pin binary; argv[0] decides the role
├── tools/<tool>.txt   six plain lines: kind, node, entry, tool, version
├── pkgs/<name>/       where npm install -g --prefix puts the package
└── pins.json          registry, read only by the CLI, never by a shim
```

Running a shim does three things: read a couple hundred bytes of text, append the pinned runtime directory to PATH, hand the process over to node. Linux and macOS use `syscall.Exec`, so the shim is replaced rather than forked; Windows has no exec(3) and costs one extra process — which is put in a job object, so it and everything it spawns live and die with the shim. Every shim is a hard link to the same binary, so ten commands still occupy one copy on disk.

The entry file is always executed through the absolute pinned runtime path, so "`ni` runs on Node 20" holds regardless of PATH. Child processes are a different matter: forwarders such as `ni`, `nr` and `npm run` just hand the work to the project's own package manager, so a `node`/`npm` already on PATH (whichever version the shell's manager selected) has to win. Otherwise `nr test` inside a Node 14 repo would run the suite on Node 20. Hence the runtime directory is appended to PATH, serving only as a fallback when no node is on PATH at all.

For the opposite need — a tool whose own children must stay on the pinned runtime (native modules, wrappers that spawn node again) — set `PIN_STRICT_RUNTIME=1` to put the runtime directory back at the front of PATH.

## Performance

Windows 11, Node 20.20.2, averaged over 20 runs:

| Case | Per run |
|---|---|
| `node hello.mjs` | 114.3 ms |
| Same script through a shim | 126.7 ms |

Of the ~12ms difference, 7 to 8ms is Windows process creation itself (`cmd.exe /c echo` costs about the same) and the rest is Go runtime startup. On Linux the exec path should land at 1 to 2ms, but I don't have a Linux box to measure on.

## Cross-compiling

```bash
./build.sh          # five targets into dist/, plus SHA256SUMS
./build.sh install  # build for this machine into $PIN_HOME/bin and relink shims
```

The binary is about 2.9MB.

## Environment

| Variable | Meaning |
|---|---|
| `PIN_HOME` | Install root, defaults to `~/.pin` |
| `PIN_NODE` | Runtime directory used when `-n` is omitted |
| `PIN_STRICT_RUNTIME` | When non-empty, put the pinned runtime first on PATH so child processes follow it instead of the project |

## Known limitations

- No downloading. Either let `--install` hand it to your manager, or install the version yourself first.
- A native binary shim is executed directly without injecting Node; `.cmd` and `.bat` go through `cmd.exe /c` on Windows.
- One version per package. For several, `pin r` first; concurrent versions would need a different registry key.
- On Windows, Ctrl+C is broadcast by the shared console to the tool and the process tree it spawned; the shim neither swallows it nor exits first, it just waits for the tool to finish. If the tool detached itself into its own process group (`detached: true`) the broadcast never arrives, so it is killed after a 5 second grace and the shim exits 130; a second Ctrl+C skips the wait. Closing the terminal window or ending pin in Task Manager takes the tool and everything it spawned with it — that is the job object's doing, and it needs no code of ours to run.
- Pins store absolute paths, so moving to another machine or architecture means pinning again.

## License

MIT
