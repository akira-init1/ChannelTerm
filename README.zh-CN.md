[English](README.md) | 简体中文 | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md)

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

## 快速开始：共享一个串口 Session

如果希望某个 CLI 退出后 Session 仍然存在，请先单独启动持久 Host。

终端 1——启动本机 Host：

```bash
channelterm mcp --transport http
```

出现下面一行表示已经就绪，之后保持安静是正常现象：

```text
MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp
```

终端 2——列出原生串口目标，然后打开并接入：

```bash
channelterm list --kind device --transport serial --no-mcp

# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

只需执行当前平台对应的一条命令。`--label board` 是可选显示名称，不是标识符，不能用
`channelterm attach board` 接入。

终端 3——其他人或另一个终端接入同一 Session：

```bash
channelterm list --kind session
channelterm attach SER-1
```

每个附件都有独立的读取游标，但看到的是同一个设备的原始输出。按 `Ctrl+] q` 只退出当前
CLI，不关闭共享 Session。只有在需要关闭 Host 及其全部 Session 时，才在终端 1 按
`Ctrl+C`。

快速单终端使用时，可以直接执行 `channelterm attach COM8` 或原生 `/dev/...` 目标，程序会
自动启动临时 Host。但创建该 Host 的附件退出后，Host 和全部 Session 都会停止，因此多人
共享时推荐使用上面的持久 Host 流程。

## 交互按键

```text
Ctrl+C      向远端终端发送 0x03
Ctrl+] q    退出当前 CLI，但不关闭共享 Session
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

共享 Session 可以发送或接收文件和目录：

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
```

支持的桌面目标为 Windows、Linux、macOS 的 amd64/arm64。详见
[从源码构建](docs/getting-started/build.md)和[构建与测试](docs/development/building-and-testing.md)。

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
