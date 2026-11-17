# pin

把全局安装的 CLI 钉在一个指定的 Node 版本上。

`npm install -g` 装的工具跟着 PATH 上的 node 走，所以在多版本项目之间切来切去时，它们会换一个 Node 重新跑。Volta 的 `volta install -g` 换了个做法：把工具绑定到安装时选定的 Node 版本，之后你切到哪个项目、用哪个 Node，它都照常工作。Volta 停维护后这个语义没有替代品，`pin` 就只做这一件事 —— 代码 2200 行，不引入任何下载器。

英文文档：[README.en.md](README.en.md)

```
$ fnm use 14            # 当前 shell 是 Node 14
$ ni -v                 # ni 仍然用安装时选定的 Node 20 跑
```

## 安装
预编译二进制在 [Releases](https://github.com/bigshans/pin/releases) 里，可用 `SHA256SUMS` 校验。二进制本身放哪都行，只要在 PATH 上：

```bash
# linux amd64
curl -L -o ~/.local/bin/pin \
  https://github.com/bigshans/pin/releases/latest/download/pin-linux-amd64
chmod +x ~/.local/bin/pin
```

Windows 下载 `pin-windows-amd64.exe`，改名成 `pin.exe`，放进任意一个 PATH 目录。

从源码编译需要 Go 1.21 以上，无第三方依赖：

```bash
git clone git@github.com:bigshans/pin.git
cd pin
./build.sh install      # 编译到 ~/.local/bin/pin（可用 PIN_BIN_DIR 改），并重链已有的 shim
```

然后把 `~/.pin/bin`（Windows 是 `%USERPROFILE%\.pin\bin`）加到 PATH 里，位置要在版本管理器之前。这个目录只放 shim：`pin` 自己的二进制不需要在里面 —— shim 是指向它的硬链接，两个路径在同一个文件系统上就只占一份，跨文件系统时会退化成拷贝（每个 shim 各占一份 3MB）。

换了 pin 二进制之后，`pin doctor` 会把每个 shim 认成旧的（元数据里记着写它的那个构建，换了构建就对不上），`pin doctor --fix` 或 `pin shims` 重链一次。`go build -o` 每次都换 inode，所以重建一次就要重链一次。

Node 由你已有的版本管理器提供（fnm、nvm、mise、asdf、proto、vfox、volta 都可以），`pin` 自己不做下载。

## 用法

```bash
pin @antfu/ni                 # 用当前 PATH 上的 node 安装并钉住
pin -n 20 @antfu/ni           # 指定 Node 20（20 / 20.20 / 20.20.2 都可以）
pin -n 20 cowsay@1.6.0        # 同时指定包版本
pin --install -n 18 cowsay    # 本机没有 Node 18，先交给版本管理器装
pin --pm pnpm @antfu/ni       # 用 pnpm 装（npm / pnpm / yarn 三选一）
pin update                    # 升级全部已钉住的包，绑定不变
pin update cowsay@1.6.0       # 升到指定版本，绑定不变
pin -n 22 ni                  # 已钉住的包：只换 node，不重装
pin                           # 列出已钉住的包（同 pin list）
pin which ni                  # 看 ni 这个 shim 实际会执行什么
pin rm cowsay                 # 卸载，也可以用 bin 名：pin rm cowthink
pin doctor                    # 检查 runtime、入口文件和 shim 是否都还在
pin doctor --fix              # 检查并修复：重绑失效的 shim，删掉没有归属的 shim
pin shims                     # 重链所有 shim，等同 pin doctor --fix
pin completion bash           # 输出补全脚本
pin version
```

每个动词都有全称和简写，两者等价：`install`（`add`、`a`、`i`）、`update`（`up`）、`list`（`ls`、`l`）、`rm`（`r`、`remove`、`uninstall`、`u`）、`which`（`w`）、`doctor`（`d`）、`shims`（`s`）、`version`（`v`）、`help`（`h`）。`pin help` 只列全称。

对一个已经钉住的包再跑 `pin -n <ver>`，只是把它的 shim 指向另一个 node —— 入口文件没变，纯 JS 不在意谁在跑它，所以不重装、不联网，几十毫秒完成。包里有原生模块时（`.node` / `binding.gyp`）会自动改成重装，因为那份编译产物只认一个 node ABI —— 不需要你记哪个工具有编译依赖。

三个动作，各管一件事，互不兼任：

| 命令 | 做什么 |
|---|---|
| `pin <pkg>` | **只安装**。已经钉住（或用它的 bin 名调用）会被拒绝，并告诉你该用哪个命令 |
| `pin update [<pkg>@<ver>]...` | **只升级**。不带参数就升级全部，绑定关系不变 |
| `pin -n <ver> <pkg>` | **只换绑**。不重装、不升级 |

之所以拆开：`pin cowsay` 如果既能装又能升级，那它的含义就取决于「你有没有装过」，同一条命令在不同时间做不同的事。现在三者都是幂等于意图的。

只有显式给了 `-n`（或设了 `PIN_NODE`）才会换绑，所以同一个命令在任何 shell 里含义一致 —— 你在 node 14 的仓库里跑 `pin update ni`，不会把 ni 从 20 拖到 14。

第一个词不是命令时当作包名，所以 `pin add ...`、`pin @antfu/ni` 等价。

## 命令补全

补全是动态的，会补出当前钉住的包名、bin 名和本机已有的 Node 版本。两种装法，选一个：

**一、eval（最简单，每个 shell 启动时跑一次 `pin`）**

```bash
# bash，写进 ~/.bashrc
eval "$(pin completion bash)"

# zsh，写进 ~/.zshrc
eval "$(pin completion zsh)"

# fish，写进 ~/.config/fish/config.fish
pin completion fish | source
```

**二、丢成文件（启动零开销，由各自的补全机制懒加载）**

```bash
pin completion zsh  > ~/.local/share/zsh/site-functions/_pin   # 需在 compinit 之前入 fpath
pin completion bash > ~/.local/share/bash-completion/completions/pin
pin completion fish > ~/.config/fish/completions/pin.fish
```

bash 的 XDG 目录和 fish 的 `completions/` 都是默认扫描的，不用改配置；zsh 需要把目录放进 `$fpath`（且必须在 `compinit` 之前加）。`pin completion zsh` 的第一行是 `#compdef pin` —— 没有这个 tag，zsh 的 compinit 会不做声地跳过这个文件，只有 eval 那条路能生效。

PowerShell 没有 `complete` 这种内建命令（那是 bash 的），走 `Register-ArgumentCompleter`，把生成的一行追加到 `$PROFILE` 即可：

```powershell
Add-Content $PROFILE 'pin completion powershell | Out-String | Invoke-Expression'
```

然后重开一个 pwsh 窗口。补全由 PowerShell 的 `Register-ArgumentCompleter -Native` 提供，`pin <TAB>`、`pin -n <TAB>`、`pin rm <TAB>`、`pin completion <TAB>` 都能用。

## Node 运行时从哪来

`pin` 不假定你用哪个版本管理器，运行时按下面这张表查找。变量没设置的位置会被跳过，所以装了哪个管理器都不影响结果。

| 管理器 | 版本目录 | `--install` 转发为 |
|---|---|---|
| fnm | `$FNM_DIR/node-versions/*`、`~/scoop/apps/fnm/current/node-versions/*` | `fnm install <ver>` |
| nvm | `$NVM_DIR/versions/node/*`、`~/.nvm/versions/node/*` | `nvm install <ver>` |
| mise | `$MISE_DATA_DIR/installs/node/*`、`~/.local/share/mise/installs/node/*` | `mise install node@<ver>` |
| asdf | `$ASDF_DATA_DIR/installs/nodejs/*`、`~/.asdf/installs/nodejs/*` | `asdf install nodejs <ver>` |
| proto | `$PROTO_HOME/tools/node/*`、`~/.proto/tools/node/*` | `proto install node@<ver>` |
| vfox | `~/.vfox/cache/nodejs/v-*` | `vfox install nodejs@<ver>` |
| volta | `~/.volta/tools/image/node/*` | `volta install node@<ver>` |

都找不到时退回 `exec.LookPath("node")` 推导目录，`-n <绝对路径>` 随时可以兜底。要支持新的管理器，往 `runtimeManagers` 表里加一项即可，探测、自愈、提示和 `--install` 会一起生效。

安装时把解析出的 node 路径写死进 shim，所以绑定的是路径而不是版本约束。路径失效时（管理器升级、`fnm uninstall`），shim 会按记录的版本号重新查找一次再执行；找不到才报错，并给出对应的安装命令。

## 包管理器从哪来

装包这一步交给谁，只有两处配置：`pin --pm <name>` 管这一次，`PIN_PM` 管这个 shell，都没有就是 `npm`。

```bash
PIN_PM=pnpm pin cowsay      # 这个 shell 里的默认（写进 profile 就是持久配置）
pin --pm yarn cowsay        # 只这一次
```

pin 不记「上次用的谁」：同一个命令在任何时候含义都一样。实际用的是谁只作为事实记进注册表，`pin list` 的 PM 列能看到；改了 `PIN_PM` 之后重装就按新配置来。

三家都支持：`npm install`、`pnpm add`、`yarn add`。做法是把 `$PIN_HOME/pkgs/<key>` 当成一个真正的项目目录（pin 先放一个只有 `name` / `private` 的 `package.json` 进去），让选中的包管理器往里面装，装完只读 `node_modules/<pkg>/package.json` 的 `bin` 找入口 —— 所以 npm 的扁平树和 pnpm 的符号链接商店都能用，pin 不需要理解它们的布局差异。

换包管理器时会先删掉旧目录再装 —— 两家的布局不能混。yarn 2+ 默认的 Plug'n'Play 没有 `node_modules`，pin 会直接报错并提示：在 `.yarnrc.yml` 里设 `nodeLinker: node-modules`。

## 工作原理

```
$PIN_HOME (默认 ~/.pin)
├── bin/<tool>         指向 pin 二进制的硬链接，靠 argv[0] 判断自己是谁
├── tools/<tool>.txt   7 行纯文本：kind、node、入口、所属包、runtime 版本、写它的 pin 构建
├── pkgs/<name>/       npm install -g --prefix 的安装目录
└── pins.json          注册表，只有 CLI 读，shim 不读
```

shim 的执行路径只做三件事：读一个两百来字节的文本文件，把钉住的 runtime 目录挂到 PATH 末尾，然后把进程交给 node。Linux 和 macOS 用 `syscall.Exec` 直接替换进程，Windows 没有 exec(3)，只能多起一个子进程；那个子进程会被放进一个 job object，于是它和它派生的一切都跟 shim 同生共死。所有 shim 是同一个二进制文件的硬链接，十个命令也只占一份磁盘。

入口文件始终用绝对路径的钉住 runtime 跑，所以「`ni` 一定跑在 Node 20 上」这件事不受 PATH 影响。子进程是另一回事：`ni`、`nr`、`npm run` 这类转发器只是把活交给项目自己的包管理器，PATH 上已有的 `node`/`npm`（版本管理器按 shell 切好的那个）必须优先，否则在 Node 14 的仓库里 `nr test` 会用 Node 20 跑测试。所以 runtime 目录是追加到 PATH 末尾的，只在 PATH 上完全没有 node 时兜底。

反过来的需求 —— 工具自己拉起的子进程必须跟钉住的 runtime 一致（原生模块、内部再起 node 的包装器）—— 用 `PIN_STRICT_RUNTIME=1`，它会恢复成把 runtime 目录放到 PATH 最前。

## 性能

Windows 11，Node 20.20.2，各跑 30 次取均值（先预热）：

| 场景 | 单次 |
|---|---|
| `node hello.mjs` | 74.1 ms |
| 经 shim 执行同一个脚本 | 87.4 ms |
| 参考：`cmd /c echo hi`（纯进程创建） | 25.5 ms |
| 参考：`pin v`（pin 启动一次） | 26.6 ms |

多出来的约 13ms 是：多创建一次进程、Go 运行时启动、读一次 200 字节的 txt、以及 job object 的四次系统调用。这台机器上单次进程创建就要 25ms 左右，所以绝对数字会随机器和杀软状态浮动，看差额比看绝对值有意义。Linux 上因为用 exec 替换进程，预期在 1 到 2ms，但我手边没有 Linux 环境，没实测过。

## 跨平台编译

```bash
./build.sh          # dist/ 下五个目标 + SHA256SUMS
./build.sh install  # 只编译本机，装进 $PIN_BIN_DIR（默认 ~/.local/bin）并重链 shim
```

产物约 2.9MB。

## 环境变量

| 变量 | 作用 |
|---|---|
| `PIN_HOME` | 安装根目录，默认 `~/.pin` |
| `PIN_NODE` | 省略 `-n` 时使用的 runtime 目录 |
| `PIN_PM` | 省略 `--pm` 且该包没有历史记录时使用的包管理器 |
| `PIN_STRICT_RUNTIME` | 设为非空时，把钉住的 runtime 放到 PATH 最前，子进程跟随它而不是跟随项目 |

## 已知限制

- 不下载 Node。缺版本要么用 `--install` 交给版本管理器，要么自己装好再用。
- 不支持 yarn 2+ 的 Plug'n'Play（PnP 没有 `node_modules`，entry 在 zip 里需要 loader 注入）；用 pin 装包时请设 `nodeLinker: node-modules`。
- `bin` 是原生二进制时直接执行，不注入 Node；`.cmd` / `.bat` 在 Windows 上走 `cmd.exe /c`。
- 同一个包只能钉一个版本，需要多版本共存得先 `pin rm` 再装。目录名和注册表 key 都取包名的最后一段，所以 `@a/cli` 和 `@b/cli` 也只能存在一个：装第二个会被拒绝，而不是静静地覆盖第一个。
- Windows 上 Ctrl+C 由共享控制台广播给工具及其子进程树，shim 自己不吞这个信号、也不抢先退出，只等它收尾。工具如果自己把自己放进了独立进程组（`detached: true`），收不到广播，那就等 5 秒后被强杀，shim 以 130 退出；再按一次 Ctrl+C 跳过等待。关掉终端窗口或在任务管理器里结束 pin 时，工具连同它派生的进程会一起死——这由 job object 保证，不依赖 pin 自己还能执行代码。
- 换机器或换系统架构要重新 `pin`，因为存的是绝对路径。

## License

MIT
