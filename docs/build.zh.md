# 构建

Go 1.21 以上，无第三方依赖。

```bash
git clone git@github.com:bigshans/pin.git
cd pin
./build.sh install      # 编译本机并重链已有的 shim
```

`install` 会把二进制写到 `$PIN_BIN_DIR`（默认 `~/.local/bin`，可用这个变量改），然后跑一次 `pin shims`，让已有 shim 指到新文件上。版本号来自 `git describe`，所以没有 tag 的检出会报成 `v0.2.0-3-g12f29c7` 这样。

shim 是硬链接，所以**每次重建都要重链**：`go build -o` 写的是新 inode，旧的那份还活着，被所有 shim 握着。`./build.sh install` 替你做了这一步，手工构建的话 `pin doctor` 会提醒你。

### Windows

`build.bat` 是同样两个命令的纯批处理版本：不依赖 PowerShell，没有第二个脚本，也不牵扯执行策略。

```bat
build.bat install      :: 编译本机的 pin.exe 并重链 shim
build.bat              :: dist\：五个目标 + SHA256SUMS
```

`install`（也可以写成 `i`，跟 pin 自己的动词一样）把 `pin.exe` 写到 `%PIN_BIN_DIR%`（默认 `%USERPROFILE%\bin`），然后跑 `pin shims`。Windows 上多两件事：一是顺手把 `%USERPROFILE%\.pin\bin\pin.exe` 也重新链接一次——这个 shim 目录在 PATH 上，留着旧的硬链接的话，`pin` 会一直报旧版本；二是构建完用 `fc /b` 逐个比对 shim 和新二进制的字节，因为 `pin doctor` 比的是构建戳，同一个版本号构建两次它分辨不出来。硬链接不能跨卷：`%PIN_BIN_DIR%` 要和 `%PIN_HOME%` 在同一个盘，否则 shim 会退化成副本。

`SHA256SUMS` 用 `certutil` 计算、并以 LF 结尾写出：批处理的 `echo` 只会给 CRLF，而 `sha256sum -c` 会去找 `pin-linux-amd64<CR>`。

## 交叉编译

```bash
./build.sh          # dist/：五个目标 + SHA256SUMS
```

| 产物 | |
|---|---|
| `pin-linux-amd64` | |
| `pin-linux-arm64` | |
| `pin-windows-amd64.exe` | |
| `pin-windows-arm64.exe` | |
| `pin-darwin-arm64` | |

产物约 2.2MB，静态链接，`CGO_ENABLED=0`，用 `-trimpath -ldflags "-s -w -X main.version=$(git describe …)"` 构建。

## 发版

```bash
git tag -a vX.Y.Z -m "pin vX.Y.Z"
./build.sh                                    # dist/ + SHA256SUMS
gh release create vX.Y.Z --title vX.Y.Z \
  --notes-file notes.md dist/pin-* dist/SHA256SUMS
```

**先打 tag 再构建**：产物里带的版本号就是 `pin version` 的输出版本，而 `pin doctor` 是拿它跟每个 shim 的构建戳比较的。

升级的用户需要换掉二进制后跑一次 `pin shims`（或 `pin doctor --fix`）—— release notes 里要写这一句。

## 测试

```bash
go test ./...                                  # 单元测试
go test -run xxx -bench . ./...                # 注册表、shim 读取、热路径
```

`shim_bench_test.go` 里的 `BenchmarkShimPlan` 覆盖 shim 交接前的全部工作，改热路径时盯它。
