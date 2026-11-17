# 工作原理

一句话：一个小 Go 二进制，用每条已钉住命令的名字硬链接出去，读七行文本，然后把进程交给 node。

[English](how-it-works.md) · [中文 README](../README.zh.md)

```
$PIN_HOME（默认 ~/.pin）
├── bin/<tool>          指向 pin 二进制的硬链接（文件系统不支持时退化成拷贝）
├── tools/<tool>.txt    每个 shim 的元数据，7 行纯文本，每次调用前都会读
├── pkgs/<name>/        包本身装在哪
└── pins.json           注册表 —— 只有 CLI 读，shim 永不读
```

## shim 的执行路径

`pin` 和每条已钉住的命令是同一个文件，靠 `argv[0]` 区分：叫 `pin` 就走 CLI，叫 `ni` 就读 `tools/ni.txt` 然后 exec。

```
pin1
node
/home/user/.local/share/fnm/node-versions/v20.20.2/installation/bin/node
/home/user/.pin/pkgs/ni/node_modules/@antfu/ni/bin/ni.mjs
ni
20.20.2
0.2.0
```

依次是 kind · node · 入口 · 所属包 · runtime 版本 · 写这个文件的 pin 构建。runtime 版本让 shim 在管理器搬走或卸载某个版本时能自愈；最后那个构建戳是 `pin doctor` 判断「二进制被换过」的依据 —— 重新构建会产生新 inode，把所有 shim 留在旧文件上，从 stat 上跟拷贝分辨不出来。

shim 在交接之前要做的事：一次 read（总共 5 个 syscall、17 次分配、约 4µs）。Linux 和 macOS 上它直接 `exec`，不会多出一个进程；Windows 没有 `exec`，就起一个子进程并放进 job object —— 子进程和它派生的一切跟着 shim 一起死，Ctrl+C 是透传而不是被吃掉。

所有 shim 都是指向同一个文件的硬链接，所以十个钉住的命令在磁盘上只占一份 3MB，不是十份。

## 工具跑在哪个运行时上

node 路径是绝对路径存下来的，所以绑定不依赖 PATH、不依赖版本管理器的状态、也不依赖你在哪个 shell 里。路径消失时，shim 会按记录的版本号重新找一次、改写自己，然后继续跑。

`pin` 在它认识的管理器里找 Node。变量没设置的位置会被跳过，所以用哪个都行：

| 管理器 | 版本目录 | `--install` 转发为 |
|---|---|---|
| fnm | `$FNM_DIR/node-versions/*`、`~/scoop/apps/fnm/current/node-versions/*` | `fnm install <ver>` |
| nvm | `$NVM_DIR/versions/node/*`、`~/.nvm/versions/node/*` | `nvm install <ver>` |
| mise | `$MISE_DATA_DIR/installs/node/*`、`~/.local/share/mise/installs/node/*` | `mise install node@<ver>` |
| asdf | `$ASDF_DATA_DIR/installs/nodejs/*`、`~/.asdf/installs/nodejs/*` | `asdf install nodejs <ver>` |
| proto | `$PROTO_HOME/tools/node/*`、`~/.proto/tools/node/*` | `proto install node@<ver>` |
| vfox | `~/.vfox/cache/nodejs/v-*` | `vfox install nodejs@<ver>` |
| volta | `~/.volta/tools/image/node/*` | `volta install node@<ver>` |

要支持新的管理器，往 `runtimeManagers` 表里加一项即可 —— 查找、自愈、提示、`--install` 会一起生效。全都匹配不上时，目录由 `exec.LookPath("node")` 推导，`-n <绝对路径>` 随时兜底。

## PATH，以及工具自己拉起的子进程

入口文件用的是绝对路径的钉住 runtime。工具拉起的子进程是另一回事。

`ni`、`nr`、`npm run` 这类工具只是把活交给项目自己的包管理器，所以 shell 的版本管理器放在 PATH 上的那个 node 必须优先 —— 否则在 Node 14 的仓库里 `nr test` 会用 Node 20 跑测试。因此 runtime 目录是**追加**到 PATH 末尾的，只在别处都没有 node 时才兜底。

反过来的需求 —— 工具自己的子进程必须跟着钉住的 runtime 走（原生模块、内部再起 node 的包装器）—— 是 `PIN_STRICT_RUNTIME=1`，它会改成放到 PATH 最前。

## 包从哪来

`pin --pm <name>` 管这一次，`PIN_PM` 管这个 shell，都没有就是 npm。`pin` 不记「上次用的谁」：同一个命令在任何时候含义都一样。

三家都支持（`npm install`、`pnpm add`、`yarn add`），因为 `pin` 把 `$PIN_HOME/pkgs/<key>` 当成一个真正的项目目录 —— 写一个只有 `name` 和 `private` 的 `package.json`，让管理器往里面装，装完读 `node_modules/<pkg>/package.json` 的 `bin` 映射。npm 的扁平树和 pnpm 的符号链接商店都满足这一点，`pin` 不需要理解它们的布局。换管理器时会先删旧目录，因为两种布局不能混。

yarn 2+ 的 Plug'n'Play 没有 `node_modules`，`pin` 会直接拒绝并让你设 `nodeLinker: node-modules`。

## 补全

补全是动态的：补出已钉住的包名、它们的 bin 名和本机真实装了的 Node 版本。

两种装法。

```bash
# 1. eval —— 最简单，每个 shell 启动时跑一次 pin
eval "$(pin completion zsh)"                 # 写进 ~/.zshrc
eval "$(pin completion bash)"                # 写进 ~/.bashrc
pin completion fish | source                 # 写进 ~/.config/fish/config.fish
```

```bash
# 2. 丢成文件 —— 启动零开销，由补全系统懒加载
pin completion zsh  > ~/.local/share/zsh/site-functions/_pin   # 需在 compinit 之前入 $fpath
pin completion bash > ~/.local/share/bash-completion/completions/pin
pin completion fish > ~/.config/fish/completions/pin.fish
```

bash 的 XDG 目录和 fish 的 `completions/` 都是默认扫描的。zsh 需要把目录放进 `$fpath`（在 `compinit` 之前），而且生成的文件以 `#compdef pin` 开头 —— 没有这个 tag，compinit 会不做声地跳过它，只有 `eval` 那条路能生效。

PowerShell 没有 `complete` 这种内建命令，把生成的一行追加到 `$PROFILE`：

```powershell
Add-Content $PROFILE 'pin completion powershell | Out-String | Invoke-Expression'
```

## 性能

Windows 11，Node 20.20.2，各跑 30 次取均值（先预热）：

| | |
|---|---|
| `node hello.mjs` | 74.1 ms |
| 同一个脚本经 shim 执行 | 87.4 ms |
| 参考：`cmd /c echo hi`（纯进程创建） | 25.5 ms |
| 参考：`pin v`（pin 启动一次） | 26.6 ms |

Linux 上同样的测法：`nuxt --version` 经 shim 49.9 ms，用完全相同的 node 和入口直接调是 48.7 ms —— shim 约 1.2 ms，其中约 4µs 是它自己的代码，其余是 Go 运行时启动。

这就是全部的成本模型：贵的是进程交接，不是代码。真正占大头的是 node 自己的启动。

## 环境变量

| 变量 | 作用 |
|---|---|
| `PIN_HOME` | 安装根目录，默认 `~/.pin` |
| `PIN_NODE` | 省略 `-n` 时使用的 runtime 目录 |
| `PIN_PM` | 无 `--pm` 且无历史记录时使用的包管理器 |
| `PIN_STRICT_RUNTIME` | 非空时，把钉住的 runtime 放到 PATH 最前，子进程跟随它 |

## 已知限制

- `pin` 不下载 Node。要么用 `--install` 交给版本管理器，要么自己装好。
- 不支持 yarn 2+ 的 Plug'n'Play（没有 `node_modules`，入口在 zip 里需要 loader 注入）。用 pin 装包时请设 `nodeLinker: node-modules`。
- `bin` 是原生二进制时直接执行，不注入 node；`.cmd` / `.bat` 在 Windows 上走 `cmd.exe /c`。
- 同一个包只能钉一个版本，要多版本先 `pin rm`。注册表 key 和安装目录都取包名的最后一段，所以 `@a/cli` 和 `@b/cli` 也不能共存 —— 第二个会被拒绝，而不是静静覆盖第一个。
- Windows 上 Ctrl+C 由共享的控制台广播给工具及其整棵进程树；shim 不吞这个信号、也不抢先退出，只等它收尾。把工具自己放进独立进程组（`detached: true`）就收不到广播，等 5 秒后被强杀、shim 以 130 退出，再按一次 Ctrl+C 跳过等待。关掉终端窗口或在任务管理器里结束 pin，工具连同它的派生进程会一起死 —— 由 job object 保证，不依赖 pin 自己还能执行代码。
- 换机器或换架构要重新 `pin`：存的是绝对路径，这是有意的。
