[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> 本文是简体中文入口。行为、参数和兼容性以英文 [README](README.md)、当前源码及
> `channelterm --help` 为准。

ChannelTerm 让人类和 AI 共享同一个硬件终端 Session。它不会替代你的终端：AI 可以读取、
操作设备，而你仍能保持连接、看到 AI 的操作、直接输入并在需要时介入。

ChannelTerm 支持 Windows、Linux 和 macOS 上的串口通信与 SSH PTY shell。它本身不内置 AI，而是通过 MCP 将真实终端 Session 提供给人类和外部 AI 客户端。

## 为什么使用 ChannelTerm？

传统串口终端让人类持有物理连接：

```text
Human ---- terminal ---- Serial device
```

仅供 AI 使用的串口工具让 AI 持有连接，但人类无法加入实时终端：

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm 将一条物理连接保留在共享 Session 中：

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

这样，AI 的操作在硬件调试过程中始终可见，人类也始终可以参与。共享不只是重新打开同一个端口：每个客户端通过独立游标读取保留的原始输出，所有写入都经过同一个 Session。

## 支持的能力

- 由 Host 持有一条串口连接，供人类 CLI 与 MCP 客户端共享。
- 独立的输出和活动游标，避免一个读取者消耗另一个读取者的数据。
- 在已连接终端中显示 Agent 写入，同时保持原始 Session 输出不变。
- AI 保持连接时，人类仍可直接输入，包括向远端发送 `Ctrl+C`。
- 退出后来加入的客户端而不关闭共享 Session；退出拥有临时 Host 的连接时，该 Host 及其 Sessions 也会关闭。
- 使用 Windows、Linux 和 macOS 的原生目标名称发现串口，支持 TOML Profile，以及不需要共享时的私有直连。
- 通过 stdio 或 Streamable HTTP 提供 MCP；共享 CLI 流程使用 HTTP Session Host。
- 通过已有串口 Session 进行有界的 CLI 文件/目录发送与接收，支持 SHA-256 校验和安全的暂存目录解包。

## 安装

从 [Releases](https://github.com/akira-init1/ChannelTerm/releases) 下载与你的操作系统和架构匹配的
压缩包，并与同一版本的 `SHA256SUMS` 核对。解压后，在解压目录中运行
`.\channelterm.exe install`（Windows）或 `./channelterm install`（Linux/macOS）。
使用预编译程序无需安装 Go。

从源码构建需要 Go 1.26 或更高版本。请使用当前仍受支持的补丁版本，然后在仓库根目录构建：

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

支持的桌面目标为 Windows、Linux、macOS 的 amd64/arm64。详见
[从源码构建并安装](docs/getting-started/build.md)、
[`install` / `uninstall` 完整契约](docs/reference/cli.md#install-and-uninstall)、
[配置位置](docs/reference/configuration.md)和[构建与测试](docs/development/building-and-testing.md)。

## 快速开始：一条命令，一个终端

列出检测到的串口设备、已保存的 Profile，以及默认 HTTP Host 可用时其中的 Session：

```bash
channelterm list
```

Host 不必处于运行状态。如果无法连接，`list` 会将 MCP 报告为 offline，同时继续显示本机设备和
Profile。

完整参数契约见 [CLI 参考](docs/reference/cli.md#list)。

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

连接期间可使用以下按键：

```text
Ctrl+C      向远端终端发送 0x03
Ctrl+] q    退出当前 CLI；若它拥有临时 Host，该 Host 也会停止
Ctrl+] ?    显示本地转义帮助
```

完整本地快捷键见 [CLI 参考](docs/reference/cli.md#attach)；客户端配置和由 AI 创建 Session 的流程见 [MCP 使用流程](docs/getting-started/mcp-server.md)。

如需获得可复用的工作流指导，请在支持 Agent Skills 的客户端中安装仓库内置的
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)。
`channelterm init --mcp` 只配置 MCP 客户端，不会安装 Skill。

HTTP Host 需要 Bearer token，默认只监听回环地址。内置 CLI 会读取本机用户 token；它也可以
[监听受信任的局域网](docs/getting-started/mcp-server.md#listen-on-a-lan)，远程客户端必须使用 Host
可达的局域网地址，并通过 `Authorization` 请求头发送相同的 token。不要把未经 TLS 和额外网络
访问控制保护的端点直接暴露到不可信网络。

<a id="使用场景"></a>
<a id="嵌入式-linux-调试"></a>
<a id="ai-操作人类观察"></a>
<a id="人类介入"></a>

## 共享终端实际示例

已连接的 CLI 会把 Agent 命令显示为本地 `AI` 活动块。`terminal_exec` 不会将该命令写入空闲 Bash 提示符的历史记录；活动块之后的内容仍是设备的原始终端输出，短期命令租约释放后，人类即可继续输入：

```bash
root@board:~#

──────── AI ────────
[18:52:31] >> echo agent-check
────────────────────
agent-check
root@board:~# echo human-still-here
human-still-here
root@board:~#
```

命令的具体输出取决于所连接的设备。`AI` 活动块是根据结构化 Session 事件生成的本地显示，不会插入设备字节流或存储的终端输出。原始按键和交互程序仍使用 `terminal_write`，保留设备原本的历史记录行为。

## 共享 SSH shell

使用已授权的私钥（加密私钥会提示输入口令），或按提示输入登录密码，两者均不保存。首次连接未知服务器时，核验显示的指纹后输入
`yes`，将服务器公钥保存到 `~/.ssh/known_hosts`；已知服务器的密钥发生变化时拒绝连接。交互提示需要真实终端：

```bash
# 终端 1：打开 shell，在远端提示符下运行 uname -a。
channelterm ssh user@192.168.1.50

# 终端 2：查看并使用同一个 shell。
channelterm attach SSH-1
```

保持第一个命令运行；退出它会关闭该 Host 及其 Session。默认本机共享端口必须空闲；如已占用，可用 `--listen` 指定另一个 Host，并使用输出中的 attach endpoint。私钥路径、主机校验、端口和当前限制见 [SSH 使用流程](docs/getting-started/ssh-terminal.md)。

## 通过 SSH 传输文件

使用 SFTP 传输和管理远端文件，或使用传统 SCP 连接兼容的 POSIX 服务器：

```bash
channelterm sftp user@192.168.1.50 ls /tmp
channelterm sftp user@192.168.1.50 put ./firmware.bin /tmp/firmware.bin
channelterm sftp user@192.168.1.50 get /tmp/log.txt ./log.txt
channelterm scp user@192.168.1.50 put ./release /tmp/release --recursive
```

这些命令复用 SSH 的密钥/密码认证与主机校验，使用独立连接，不占用终端 Session。
覆盖已有文件需要 `--overwrite`。目录规则、取消行为和协议限制见
[文件传输](docs/getting-started/file-transfer.md#sftp-and-scp-over-ssh)。

通过共享终端 Session 传输文件使用轻量的 Linux-shell 协议。独立 SSH 文件连接使用 SFTP 或传统 SCP。

<a id="ai文件与本地控制"></a>

## 通过共享 Session 传输文件

同一个共享串口 Session 也可以传输普通文件和目录，无需 AI 客户端，也无需在板端另行安装传输代理。在正在运行的共享 `attach` 中，先按 `Ctrl+]`，再按 `f`，即可打开引导式发送/接收菜单。目录传输使用标准 tar 和带确认、便于取消的数据分块；同样的操作也可通过命令完成：

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

传输需要空闲的 Linux shell，以及[文件传输流程](docs/getting-started/file-transfer.md)列出的工具。传输会校验 SHA-256，并选择新名称以避免覆盖已有目标。传输期间，在共享附件中按 `Ctrl+C` 会询问是否取消；清理完成并释放租约前，其他写入者仍被阻止。

## 通过 MCP 使用用户态 GDB

在原生 Linux 上，MCP 客户端可通过 `debug_start` / `debug_command` / `debug_events` / `debug_stop` 工具接管空闲 Bash Session、设置断点、步进、查看变量/线程/寄存器并分离。首版要求 ChannelTerm、GDB、gdbserver 和 ELF 位于同一 Linux 环境，支持单 inferior、all-stop 调试；只有恢复终端状态并通过唯一标记 Shell 探测后，才开放普通写入。

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[工作流、MCP 调用、安全 attach 和本机 PTY 测试范围](docs/getting-started/gdb-debugging.md).

独立运行的 GDB 可通过 `debug_bridge_*` 创建的认证 TLS/TCP 桥接访问已准备好的串口 Session。操作者准备兼容的 Linux gdbserver 并独立运行 GDB；`channelterm rsp-client` 负责连接。桥接不限制开发板型号或目标 CPU 架构。断线后会在有限的恢复期内保留独占所有权。恢复验证成功可保留 Session；显式关闭或过期会关闭 Session。Windows/WSL2 配置、续期和恢复见[外部桥接工作流](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)。裸机调试不在支持范围内。

## 工作原理

```text
Human CLI -----+
               +--> Session Host --> Session <--> Channel <--> Serial / SSH endpoint
AI via MCP ----+                         |
                                        +-- retained output, activity, events
                                        `-- serialized writes
```

Session Host 拥有连接并管理 Session 生命周期。Serial 或 SSH Transport 建立协议无关的 Channel；Session 读取该 Channel，在内存中保留有界的输出、活动和事件，并串行化写入。CLI 与 MCP 适配器复用相同的 Core 行为。高亮和 `AI` 活动块等显示处理不会改变存储的终端原始数据。

读取者拥有独立游标。原始写入会串行化，但不会协调 shell 命令的语义；应用层租约会在命令执行、文件传输和调试期间暂时阻止其他写入者。所有权和并发规则见[共享 Session](docs/getting-started/shared-session.md)。

`SER-1` 等共享引用仅在所属 Host 进程内有效。停止 Host 会关闭其 Sessions。后来加入的连接可以单独退出，但退出拥有临时 Host 的连接或创建 Host 的 SSH 命令时，也会关闭其 Sessions。各层职责见[架构概览](docs/architecture/overview.md)。

## 配置

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
- [SSH shell 使用流程](docs/getting-started/ssh-terminal.md)
- [共享 Session](docs/getting-started/shared-session.md)
- [文件传输流程](docs/getting-started/file-transfer.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI 参考](docs/reference/cli.md)
- [MCP 工具参考](docs/reference/mcp-tools.md)
- [标识符与引用](docs/reference/identifiers.md)
- [架构概览](docs/architecture/overview.md)

准确的命令参数及默认值以运行程序的 `--help` 输出为准。

## 路线图

串口与 SSH PTY shell 已实现为 Transport；专用 SFTP 和传统 SCP 文件客户端也已提供。任意 SSH exec、Telnet 和更强的多写入者协调仍是未来方向，不属于已实现功能或版本承诺。

ChannelTerm 目前尚未实现 JTAG/OpenOCD、虚拟串口设备、内置 AI 或多 Agent 编排。

## 许可

ChannelTerm 使用 [Apache License 2.0](LICENSE)。
