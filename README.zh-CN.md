[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | 简体中文 | [繁體中文](README.zh-TW.md)

# ChannelTerm

> 本文是简体中文入口。行为、参数和兼容性以英文 [README](README.md)、当前源码及
> `channelterm --help` 为准。

ChannelTerm 让人类和 AI 共享同一个硬件终端 Session。它不会替代你的终端：AI 可以读取、
操作设备，而你仍能保持连接、看到 AI 的操作、直接输入并在需要时介入。

```text
              物理串口设备
                    |
              Serial Transport
                （单一连接）
                    |
                    v
          ChannelTerm Session SER-1
             /                 \
            /                   \
         人类终端              AI / MCP 客户端
    channelterm attach          读取和写入工具
```

目前只实现了 Windows、Linux 和 macOS 上的串口 Transport。SSH 和 Telnet 是未来方向，
不是当前功能。ChannelTerm 本身也不是内置 AI；它通过 MCP 把真实终端 Session 提供给外部
AI 客户端。

## 快速开始：一条命令，一个终端

先列出原生串口目标；此操作不要求 MCP 服务器已经运行：

```bash
channelterm list --kind device --transport serial --no-mcp
```

如果已经知道 `COM50` 这样的原生目标，可以跳过这一步。

如果需要 AI 客户端加入 Session，请在接入前为受支持的客户端执行一次配置：

```bash
channelterm init --mcp
```

选择 HTTP，或直接按回车采用默认的 HTTP。配置中包含本机 Bearer 凭据；如果 AI 客户端没有立即
读取新配置，请重新加载或重启客户端。

然后只执行当前平台对应的一条命令：

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

这一条 `attach` 命令会打开串口并让当前终端接入。如果默认端点没有兼容的 Host，它还会自动
启动一个临时的本机 HTTP MCP Session Host。典型输出如下：

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

快速使用时不需要另外打开一个终端运行 MCP 服务器。只要这个附件保持运行，已配置的 HTTP MCP
客户端就能通过 `http://127.0.0.1:37099/mcp` 操作同一个 `SER-1`。创建临时 Host 的附件退出后，
该 Host 及其全部 Session 都会停止。只有当 Session 必须独立于当前终端继续存在时，才需要单独
运行持久 Host。

`--label board` 只是可选显示名称，不是标识符。其他客户端应使用 `SER-1` 或完整 Session ID。

### 常用玩法

| 目标 | 命令 |
| --- | --- |
| 打开共享串口 Session，并自动启动临时 Host | `channelterm attach COM8 --baud 115200` |
| 为受支持的 AI 客户端配置共享 HTTP Host | `channelterm init --mcp` |
| 让 Host 和 Session 独立持续运行 | `channelterm mcp --transport http` |
| 列出或加入已有 Session | `channelterm list --kind session`，然后执行 `channelterm attach SER-1` |
| 打开不通过 MCP 共享的私有串口 | `channelterm attach COM8 --private --baud 115200` |
| 观察结构化 Session 活动 | `channelterm events SER-1` |
| 在当前共享附件中打开引导式文件传输菜单 | 先按 `Ctrl+]`，再按 `f` |
| 通过共享 Session 发送或接收文件 | `channelterm file send firmware.bin --session SER-1` 或 `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

持久 Host 是一个单独长期运行的进程。它显示
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp` 后，其他 Shell 和已配置的 AI
客户端就可以创建或加入 Session。就绪提示之后没有输出是正常现象；需要关闭 Host 及其全部
Session 时按 `Ctrl+C`。

## 交互按键

```text
Ctrl+C      向远端终端发送 0x03
Ctrl+] q    退出当前 CLI；若它拥有临时 Host，该 Host 也会停止
Ctrl+] ?    显示本地转义帮助
Ctrl+] ]    向远端发送字面量 Ctrl+]
Ctrl+] t    开关本地 Shell 提示符时间戳
Ctrl+] f    打开本地文件发送/接收菜单
Ctrl+] Esc  取消本地转义模式
```

## 让 AI 接入同一 Session

运行：

```bash
channelterm init --mcp
```

选择 HTTP 后，受支持的 Codex、Claude Code、OpenCode 或 Zoo Code 客户端会使用本机共享端点
`http://127.0.0.1:37099/mcp`。AI 可以列出、读取和操作 `SER-1`；在已知空闲的 Bash 提示符
上应优先使用 `terminal_exec`，原始按键和交互程序使用 `terminal_write`。

HTTP Host 需要 Bearer token，默认只监听回环地址。内置 CLI 会读取本机用户 token；它也可以
[监听受信任的局域网](docs/getting-started/mcp-server.md#listen-on-a-lan)，远程客户端必须使用 Host
可达的局域网地址，并通过 `Authorization` 请求头发送相同的 token。不要把未经 TLS 和额外网络
访问控制保护的端点直接暴露到不可信网络。

## 文件传输

共享 Session 可以发送或接收文件和目录。在正在运行的共享 `attach` 中，先按 `Ctrl+]`，再按 `f`，
即可打开引导式发送/接收菜单；同样的操作也可以通过命令完成：

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

发送时省略远端目标路径，文件或目录会默认保存到 `/tmp/cterm/mcp-files/`；交互快捷键默认
使用 `/tmp/cterm/user-files/`。ChannelTerm 会按需创建目录，并为同名目标选择 `_1`、`_2`
等不覆盖现有内容的名称。

目标端必须是具备所需标准命令的 Linux Shell。具体前提、覆盖保护、取消和 SHA-256 校验规则
见[文件传输流程](docs/getting-started/file-transfer.md)。

## 安装与构建

ChannelTerm 需要 Go 1.25 或更高版本。请使用当前仍受支持的补丁版本，然后在仓库根目录构建：

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` 会把当前运行的二进制复制到当前用户的标准程序目录，创建 `channelterm` 和 `cterm`
命令，仅在必要时把该目录加入用户 PATH，并在缺少配置时初始化最小配置。若命令提示 PATH 已更改，
请打开新终端。`channelterm uninstall` 会删除安装器管理的命令和 PATH 修改，但保留用户数据；
`channelterm uninstall --purge` 必须明确确认后才会删除配置、设备状态和本地 HTTP 认证信息。

默认位置如下；安装器每次运行后也会打印实际路径：

| 平台 | 命令目录 | 安装记录 | 默认配置 |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json`；未设置时为 `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml`；未设置时为 `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

支持的桌面目标为 Windows、Linux、macOS 的 amd64/arm64。详见
[从源码构建并安装](docs/getting-started/build.md)、
[`install` / `uninstall` 完整契约](docs/reference/cli.md#install-and-uninstall)、
[配置位置](docs/reference/configuration.md)和[构建与测试](docs/development/building-and-testing.md)。

## 配置

ChannelTerm 将本地文件保存在各平台的用户配置目录中：

| 平台 | 默认目录 |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/`；未设置时为 `~/.config/channelterm/` |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` 保存用户维护的串口 Profile 和连接策略；`state.json` 是 ChannelTerm 自动维护的
设备身份状态；`http-auth-token` 是 MCP HTTP 客户端的认证密钥，请勿分享或提交。

```powershell
# 保存并复用串口 Profile。
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# 本次操作使用另一个 config.toml。
channelterm serial --config ./channelterm.toml --profile board
```

常用参数包括 `--profile`、`--save`、`--config`、`--port` 和 `--baud`；MCP Host 还支持
`--connection-policy ask|auto|deny`。`--config` 只替换所选的 `config.toml`，不会移动默认的
`state.json` 或 `http-auth-token`。完整字段、优先级、校验和持久化行为见
[串口 Profile](docs/getting-started/serial-profiles.md)和[配置参考](docs/reference/configuration.md)。

## 文档

- [文档索引](docs/README.md)
- [串口终端流程](docs/getting-started/serial-terminal.md)
- [共享 Session](docs/getting-started/shared-session.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI 参考](docs/reference/cli.md)
- [MCP 工具参考](docs/reference/mcp-tools.md)
- [标识符与引用](docs/reference/identifiers.md)

## 许可

ChannelTerm 使用 [Apache License 2.0](LICENSE)。
