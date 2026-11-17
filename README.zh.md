# pin

[![release](https://img.shields.io/github/v/release/bigshans/pin?style=flat-square)](https://github.com/bigshans/pin/releases)
[![license](https://img.shields.io/github/license/bigshans/pin?style=flat-square)](LICENSE)
[![go](https://img.shields.io/github/go-mod/go-version/bigshans/pin?style=flat-square)](go.mod)
[![platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macos%20%7C%20windows-lightgrey?style=flat-square)](https://github.com/bigshans/pin/releases)

**把全局安装的 CLI 钉在一个固定的 Node 运行时上，之后切到哪个项目都不受影响。**

`npm install -g` 装的工具跟着 PATH 上的 node 走 —— 在不同 Node 版本的项目之间切来切去，工具也跟着换。`pin` 把每个工具绑到你指定的那个运行时上，到哪都照常工作。

```console
$ fnm use 14          # 当前 shell 是 Node 14
$ ni -v               # ni 仍然用安装时选定的 Node 20 跑
```

[English](README.md) · [工作原理](docs/how-it-works.zh.md) · [构建](docs/build.zh.md)

## 安装

```bash
# macOS arm64 / linux amd64 / linux arm64 / windows amd64 … 见 Releases
curl -L -o ~/.local/bin/pin \
  https://github.com/bigshans/pin/releases/latest/download/pin-linux-amd64
chmod +x ~/.local/bin/pin
```

二进制放哪都行，只要在 PATH 上 —— 不必放进 shim 目录。Windows 下载 `pin-windows-amd64.exe`，改名 `pin.exe`。

然后做一次：把 shim 目录加到 PATH，**位置要在版本管理器之前**。

```bash
export PATH="$HOME/.pin/bin:$PATH"      # Windows: %USERPROFILE%\.pin\bin
```

从源码编译：`./build.sh install` —— 见[构建](docs/build.zh.md)。

Node 由你已有的版本管理器提供（fnm、nvm、mise、asdf、proto、vfox、volta 都认），`pin` 自己不做下载。

## 用法

| 命令 | 做什么 |
|---|---|
| `pin <pkg>[@ver]` | 安装并钉住 |
| `pin update [<pkg>…]` | 升级，绑定关系不动 |
| `pin -n <ver> <pkg>` | 钉到指定运行时；对已钉住的包：只换绑，不重装 |
| `pin list` | 看钉了哪些、各自跑在哪个运行时上 |
| `pin rm <pkg\|bin>…` | 卸载 |
| `pin which <bin>` | 看某个 shim 实际会执行什么 |
| `pin doctor [--fix]` | 检查运行时、入口文件、shim，并修复失效的 |
| `pin shims` | 重链全部 shim（等同 `pin doctor --fix`） |

```console
$ pin -n 20 cowsay@1.6.0      # 包版本和运行时一起指定
$ pin --pm pnpm @antfu/ni     # 用 pnpm 装（npm | pnpm | yarn）
$ pin --install -n 18 cowsay  # 本机没有 Node 18，交给版本管理器装
$ pin -n 22 ni                # 已钉住的包：只换绑，不重装
$ pin rm cowthink             # 用 bin 名也能卸载 cowthink
```

三个动作各管一件事：`pin x` 只安装（已钉住的会被拒绝），`pin update` 只升级，`-n` 只换绑。所以同一个命令在任何时候含义都一样。

也接受简写：`pin r`、`pin ls`、`pin d` —— `pin help` 里列全了。

## 补全

```bash
eval "$(pin completion zsh)"     # 写进 ~/.zshrc      （bash | zsh | fish | powershell）
eval "$(pin completion bash)"    # 写进 ~/.bashrc
pin completion fish | source     # 写进 ~/.config/fish/config.fish
```

也可以把生成的文件丢进各 shell 自己会扫的目录 —— [细节](docs/how-it-works.zh.md#补全)。

## 文档

| | |
|---|---|
| [工作原理](docs/how-it-works.zh.md) | shim 布局、运行时解析、PATH 规则、性能、已知限制 |
| [构建](docs/build.zh.md) | 从源码编译、交叉编译、发布产物 |
| [English](README.md) | English docs |

MIT · 本体约 2200 行 Go，单个二进制，无下载器，无后台进程。
