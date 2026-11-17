# pin

把全局安装的 CLI 钉在一个指定的 Node 版本上。

Volta 停维护后，它最有价值的那部分语义没有替代品：`volta install -g` 装的工具会绑定安装时的 Node 版本，之后你切到哪个项目、用哪个 Node，它都照常工作。`pin` 只做这一件事，代码不到 1100 行，不引入任何下载器。

英文文档：[README.en.md](README.en.md)

```
$ fnm use 14            # 当前 shell 是 Node 14
$ ni -v                 # ni 仍然用安装时选定的 Node 20 跑
```

## 要求

Go 1.21 以上，无第三方依赖。Node 由你已有的版本管理器提供（fnm、nvm、mise、asdf、proto、vfox、volta 都可以），`pin` 自己不做下载。

```bash
git clone git@github.com:bigshans/pin.git
cd pin
./build.sh install      # 编译到 ~/.pin/bin/pin，并重链已有的 shim
```

然后把 `~/.pin/bin`（Windows 是 `%USERPROFILE%\.pin\bin`）加到 PATH 里，位置要在版本管理器之前。之后 `pin` 和所有被钉住的命令都从这个目录来。

## 用法

```bash
pin @antfu/ni                 # 用当前 PATH 上的 node 安装并钉住
pin -n 20 @antfu/ni           # 指定 Node 20（20 / 20.20 / 20.20.2 都可以）
pin -n 20 cowsay@1.6.0        # 同时指定包版本
pin --install -n 18 cowsay    # 本机没有 Node 18，先交给版本管理器装
pin                           # 列出已钉住的包
pin w ni                      # 看 ni 这个 shim 实际会执行什么
pin r cowsay                  # 卸载，也可以用 bin 名：pin r cowthink
pin d                         # 检查 runtime、入口文件和 shim 是否都还在
pin d --fix                   # 检查并修复：重绑失效的 shim，删掉没有归属的 shim
pin s                         # 重新链接所有 shim（换过 pin 二进制之后要跑）
pin completion bash           # 输出补全脚本
pin v
```

第一个词不是命令时当作包名，所以 `pin add ...`、`pin @antfu/ni` 等价。命令也接受全称：`add`、`ls`、`rm`、`which`、`doctor`、`shims`。

## 命令补全

补全是动态的，会补出当前钉住的包名、bin 名和本机已有的 Node 版本：

```bash
# bash，写进 ~/.bashrc
eval "$(pin completion bash)"

# zsh，写进 ~/.zshrc
eval "$(pin completion zsh)"

# fish，写进 ~/.config/fish/config.fish
pin completion fish | source
```

PowerShell 没有 `complete` 这种内建命令（那是 bash 的），走 `Register-ArgumentCompleter`，把生成的一行追加到 `$PROFILE` 即可：

```powershell
Add-Content $PROFILE 'pin completion powershell | Out-String | Invoke-Expression'
```

然后重开一个 pwsh 窗口。补全由 PowerShell 的 `Register-ArgumentCompleter -Native` 提供，`pin <TAB>`、`pin -n <TAB>`、`pin r <TAB>`、`pin completion <TAB>` 都能用。

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

## 工作原理

```
$PIN_HOME (默认 ~/.pin)
├── bin/<tool>         指向 pin 二进制的硬链接，靠 argv[0] 判断自己是谁
├── tools/<tool>.txt   6 行纯文本：kind、node、入口、所属包、版本标签
├── pkgs/<name>/       npm install -g --prefix 的安装目录
└── pins.json          注册表，只有 CLI 读，shim 不读
```

shim 的执行路径只做三件事：读一个两百来字节的文本文件，把钉住的 runtime 目录放到 PATH 最前，然后把进程交给 node。Linux 和 macOS 用 `syscall.Exec` 直接替换进程，Windows 没有 exec(3)，只能多起一个子进程。所有 shim 是同一个二进制文件的硬链接，十个命令也只占一份磁盘。

把 runtime 目录放到 PATH 最前是为了让工具自己拉起的 `npm`、`pnpm`、`node` 子进程用同一个版本。如果你更希望它们跟随项目，删掉 `runShim` 里的 `prependPath` 那一行就行。

## 性能

Windows 11，Node 20.20.2，各跑 20 次取均值：

| 场景 | 单次 |
|---|---|
| `node hello.mjs` | 114.3 ms |
| 经 shim 执行同一个脚本 | 126.7 ms |

多出来的约 12ms 里，7 到 8ms 是 Windows 创建进程本身的开销（`cmd.exe /c echo` 也是这个量级），其余是 Go 运行时的启动。Linux 上因为用 exec 替换进程，预期在 1 到 2ms，但我手边没有 Linux 环境，没实测过。

## 跨平台编译

```bash
./build.sh          # dist/ 下五个目标 + SHA256SUMS
./build.sh install  # 只编译本机，直接装进 $PIN_HOME/bin 并重链 shim
```

产物约 2.9MB。

## 环境变量

| 变量 | 作用 |
|---|---|
| `PIN_HOME` | 安装根目录，默认 `~/.pin` |
| `PIN_NODE` | 省略 `-n` 时使用的 runtime 目录 |

## 已知限制

- 不下载 Node。缺版本要么用 `--install` 交给版本管理器，要么自己装好再用。
- `bin` 是原生二进制时直接执行，不注入 Node；`.cmd` / `.bat` 在 Windows 上走 `cmd.exe /c`。
- 同一个包只能钉一个版本，需要多版本共存得先 `pin r` 再装。
- Windows 下 Ctrl+C 依赖共享控制台，子进程在独立进程中运行时不会跟父进程一起终止。
- 换机器或换系统架构要重新 `pin`，因为存的是绝对路径。

## License

MIT
