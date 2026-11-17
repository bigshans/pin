# pin

把**全局安装的 CLI 工具**钉在某个 Node runtime 上——Volta 的“包级 pin”语义，只做这一件事。

```bash
pin @antfu/ni                 # 用当前 node 安装并钉住（默认动作 = add）
pin -n 20 @antfu/ni           # 用 node 20（支持部分版本：20 / 20.20 / 20.20.2）
pin -n 20 cowsay@1.6.0        # 包版本与 runtime 可同时指定
pin --install -n 18 cowsay    # runtime 没装就先交给版本管理器安装
pin                           # 列表（= pin l）
pin w ni                      # 这个 shim 到底会执行什么
pin r cowsay                  # 卸载（也可用 bin 名：pin r cowthink）
pin d                         # 体检
pin d --fix                   # 体检并修复：重绑失效 shim、清孤儿 shim
pin s                         # 升级 pin 二进制后重新链接所有 shim
pin v                         # 版本
```

之后无论 `fnm use 14` 还是 `nvm use 16`，`ni` 始终用安装时选定的 Node 运行。

## runtime 来源（不绑定任何一家）

`pin` 既不自带下载器，也不假定你用哪个版本管理器。下面这张表驱动了**目录探测**、
**自愈**、**修复提示**和 `--install` 的转发：

| manager | 版本目录 | `--install` 转发 |
|---|---|---|
| fnm | `$FNM_DIR/node-versions/*`、`~/scoop/apps/fnm/current/node-versions/*` | `fnm install <ver>` |
| nvm | `$NVM_DIR/versions/node/*`、`~/.nvm/versions/node/*` | `nvm install <ver>` |
| mise | `$MISE_DATA_DIR/installs/node/*`、`~/.local/share/mise/installs/node/*` | `mise install node@<ver>` |
| asdf | `$ASDF_DATA_DIR/installs/nodejs/*`、`~/.asdf/installs/nodejs/*` | `asdf install nodejs <ver>` |
| proto | `$PROTO_HOME/tools/node/*`、`~/.proto/tools/node/*` | `proto install node@<ver>` |
| vfox | `~/.vfox/cache/nodejs/v-*` | `vfox install nodejs@<ver>` |
| volta | `~/.volta/tools/image/node/*` | `volta install node@<ver>` |

找不到时再退化到 `exec.LookPath("node")` 推导；`-n <目录>` 永远可以用绝对路径兜底。
管理器表里加一行即可支持新的管理器。

## 设计

```
$PIN_HOME (默认 ~/.pin)
├── bin/<tool>        ← 本可执行文件的硬链接（失败则复制），靠 argv[0] 分发
├── tools/<tool>.txt  ← 5 行纯文本：kind/node/entry/tool
├── pkgs/<name>/      ← npm install -g --prefix 的目标
└── pins.json         ← 注册表，只有 CLI 读，shim 从不解析
```

- **shim 不解析 JSON**：热路径只读一个 ~200 字节的文本文件，然后 exec。
- **Linux/macOS 用 `syscall.Exec`**：进程被直接替换，shim 自身零额外开销。
- **Windows 用 `CreateProcess`**：无 exec(2)，多一个进程，约 8–12ms。
- **所有 shim 是同一个 inode 的硬链接**：8 个命令只占一份 3MB。
- **绝对路径**：node 与入口文件都在安装时写死，与当前 shell 的版本管理器无关。
- **PATH 注入**：只把**被钉住的 runtime 目录**放到 PATH 最前，因此工具派生出的
  `npm/pnpm/yarn`、`node` 子进程也使用同一版本。要“用项目的包管理器”的话，删掉
  `runShim` 里的 `prependPath` 一行即可。

## 构建

需要 Go 1.21+，无第三方依赖。

```bash
./build.sh install   # 本机编译 → $PIN_HOME/bin/pin(.exe)，并自动 pin s 重链 shim
./build.sh           # 交叉编译五个目标到 dist/ + SHA256SUMS
```

产物约 2.9MB（windows/amd64），Linux 约 2.9MB。

## 实测（Windows 11, node 20.20.2, 20 次取均值）

| 场景 | 单次耗时 |
|---|---|
| `node hello.mjs` | 114.3 ms |
| 经 shim 执行同一脚本 | 126.7 ms |
| **shim 开销** | **≈12.4 ms** |

其中约 7–8ms 是 Windows 进程创建本身（`cmd.exe /c echo` 同量级），剩余是 Go 运行时启动。
Linux 上因为用 `exec` 替换进程，预期 1–2ms（本机无 Linux 环境，未实测）。

## 环境变量

| 变量 | 作用 |
|---|---|
| `PIN_HOME` | 安装根目录，默认 `~/.pin` |
| `PIN_NODE` | 省略 `-n` 时的默认 runtime 目录 |

`-n` 既接受目录，也接受版本号（`20`、`20.20`、`20.20.2`、`v20.20.2`），会按最新匹配在
fnm / nvm / mise / asdf / proto / volta / vfox 的默认安装位置里查找。

## 限制

- 不做下载：runtime 必须已经由 fnm/nvm/mise 等装好（这是刻意的，避免重造一遍 Volta 的坑）。
- 带 native 扩展的工具固定 runtime 即可免疫 ABI 问题，但换机器/换 OS 需要重装。
- `bin` 指向非 JS（原生二进制）时按 direct 执行；`.cmd/.bat` 在 Windows 上经 `cmd.exe /c`。
- Windows 的 `ctrl+c` 依赖共享控制台；子进程被独立创建（无 job object）时不会被一并终止。
- 不支持同一包多版本并存（`pin rm` 后再装即可；多版本并存需要改 registry 主键）。
