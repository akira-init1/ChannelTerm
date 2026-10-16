[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

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

ChannelTerm 支持 Windows、Linux 和 macOS 上的串口通信与 SSH PTY shell。它本身不内置 AI，而是通过 MCP 将真实终端 Session 提供给人类和外部 AI 客户端。

## 通过 MCP 使用用户态 GDB

在原生 Linux 上，MCP 客户端可通过 `debug_start` / `debug_command` / `debug_events` / `debug_stop` 工具接管空闲 Bash Session、设置断点、步进、查看变量/线程/寄存器并分离。首版要求 ChannelTerm、GDB、gdbserver 和 ELF 位于同一 Linux 环境，支持单 inferior、all-stop 调试；只有恢复终端状态并通过唯一标记 Shell 探测后，才开放普通写入。

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[工作流、MCP 调用、安全 attach 和本机 PTY 测试范围](docs/getting-started/gdb-debugging.md).

独立运行的 GDB 可通过 `debug_bridge_*` 创建的认证 TCP 桥接访问已准备好的串口 Session。ChannelTerm 不启动 GDB 或 gdbserver。由 GDB 调用 `channelterm rsp-client` 接入；认证连接在 RSP 无流量时也会自动保活，最长生命周期为 24 小时。断线后保留独占租约，直到 Shell/TTY 恢复验证成功，显式关闭桥接则同时关闭 Session。Windows/WSL2 配置及 PTY 与开发板分别验证的步骤见[外部桥接工作流](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)。

外部桥接适用于所有运行 Linux 的开发板：板端需有可通过已准备好的串口连接通信的兼容 gdbserver，并使用与目标匹配的 GDB。桥接不限制开发板型号或目标 CPU 架构；裸机调试不在支持范围内。

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

## 快速开始：一条命令，一个终端

列出检测到的串口设备、已保存的 Profile，以及默认 HTTP Host 可用时其中的 Session：

```bash
channelterm list
```

Host 不必处于运行状态。如果无法连接，`list` 会将 MCP 报告为 offline，同时继续显示本机设备和
Profile。

| 常用参数 | 作用 |
| --- | --- |
| `--kind device`、`profile` 或 `session` | 只显示指定类型；可以使用逗号组合多个值。 |
| `--transport serial` | 只显示串口结果。 |
| `--no-mcp` | 跳过 MCP Host 查询，只显示本机来源。 |
| `--long`、`-l` | 在表格中显示完整的不透明 Session ID。 |
| `--json` | 输出供脚本处理的结构化 JSON。 |
| `--endpoint URL` | 从非默认 HTTP Host 查询 Session。 |

完整参数契约见 [CLI 参考](docs/reference/cli.md#list)。

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

连接期间可使用以下按键：

```text
Ctrl+C      向远端终端发送 0x03
Ctrl+] q    退出当前 CLI；若它拥有临时 Host，该 Host 也会停止
Ctrl+] ?    显示本地转义帮助
Ctrl+] ]    向远端发送字面量 Ctrl+]
Ctrl+] t    开关本地 Shell 提示符时间戳
Ctrl+] f    打开本地文件发送/接收菜单
Ctrl+] Esc  取消本地转义模式
```

要让受支持的 AI 客户端加入同一 Host，请运行 `channelterm init --mcp`，选择 HTTP，再让客户端列出或操作 `SER-1`。认证与客户端使用流程见 [MCP Server](docs/getting-started/mcp-server.md)。

如需获得可复用的工作流指导，请在支持 Agent Skills 的客户端中安装仓库内置的
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)。
`channelterm init --mcp` 只配置 MCP 客户端，不会安装 Skill。

从 v0.2.0 起，[Releases](https://github.com/akira-init1/ChannelTerm/releases) 提供
`channelterm_skills_VERSION.zip`。解压后，按上述 Skill 安装说明复制完整的
`cterm-serial-debug` 目录。

AI 也可以自己创建共享 Session。先运行一次 `channelterm init --mcp` 并选择 HTTP 来配置客户端，
然后运行 `channelterm mcp --transport http` 启动前台持久 Host，再向 AI 提供准确的串口目标与通信
参数。AI 可以列出串口、调用 `terminal_open_serial`，再报告生成的 `SER-N`，供人类执行
`channelterm attach SER-N` 加入。仅启动 Host 不会打开任何串口；发现策略返回 `ask` 时仍需用户
明确批准。`auto` 返回 `connect`，但 AI 仍须调用 `terminal_open_serial`；Host 永远不会自动打开
串口。正常使用 `Ctrl+C` 停止 Host；停止时会关闭它拥有的 Sessions。

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

## AI、文件与本地控制

同一个共享串口 Session 也可以传输普通文件和目录，无需 AI 客户端，也无需在板端另行安装传输代理。在正在运行的共享 `attach` 中，先按 `Ctrl+]`，再按 `f`，即可打开引导式发送/接收菜单。目录传输使用标准 tar 和带确认、便于取消的数据分块；同样的操作也可通过命令完成：

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

命令省略远端目标路径时，默认保存到 `/tmp/cterm/mcp-files/`；交互快捷键使用 `/tmp/cterm/user-files/`。ChannelTerm 会创建缺失的目标目录层级，并在后续传输中复用。Linux shell 需要原生的 `mkdir`、`stty`、`dd`、`wc`、`sha256sum` 和 `sleep`，目录传输还需要 `tar`。ChannelTerm 以大小受限的分块传输，报告进度，并对文件字节及目录 tar 数据流进行端到端 SHA-256 校验。目录传输使用目标端临时归档，因此取消会在当前分块结束后停止，无需继续传完整个目录。临时文件传输租约会阻止其他 ChannelTerm 写入者，直到命令结束。租约有效期间，在任一随附的终端客户端中按 `Ctrl+C`，会打开默认选择“否”的本地取消确认；其他时候则保持正常的远端终端含义。前提和限制见[文件传输流程](docs/getting-started/file-transfer.md)。

要让 AI 加入同一个 Session，请将其 MCP 客户端配置为使用同一个 Streamable HTTP 端点：

```text
http://127.0.0.1:37099/mcp
```

AI 可以通过 MCP Session 工具列出和读取 `SER-1`，使用 `terminal_exec` 在空闲 Bash 中运行命令，或使用 `terminal_write` 明确发送原始终端字节；详见 [MCP 使用流程](docs/getting-started/mcp-server.md)和[工具参考](docs/reference/mcp-tools.md)。运行 `channelterm init --mcp` 安装客户端配置，或运行 `channelterm init --mcp-show` 打印配置；两者都会提示选择 HTTP 或 stdio，默认使用 HTTP。打印的 HTTP 配置包含本机 Bearer 凭据，不可分享或提交。

按 `Ctrl+]` 进入本地转义模式，ChannelTerm 会在本地显示可用命令。`Ctrl+] f` 在当前附件中启动引导式文件/目录发送或接收，默认路径为 `/tmp/cterm/user-files/<basename>` 和 `./<basename>`，并通过 `_1`、`_2` 选择不覆盖已有内容的名称。`Ctrl+] t` 默认关闭，仅在可靠识别的 shell 提示符前添加本地 `[HH:MM:SS]` 时间戳，不改变共享 Session 或 MCP 输出。

HTTP Host 需要 Bearer token，默认只监听回环地址。内置 CLI 会读取本机用户 token；它也可以
[监听受信任的局域网](docs/getting-started/mcp-server.md#listen-on-a-lan)，远程客户端必须使用 Host
可达的局域网地址，并通过 `Authorization` 请求头发送相同的 token。不要把未经 TLS 和额外网络
访问控制保护的端点直接暴露到不可信网络。

## 支持的能力

- 由 Host 持有一条串口连接，供人类 CLI 与 MCP 客户端共享。
- 独立的输出和活动游标，避免一个读取者消耗另一个读取者的数据。
- 在已连接终端中显示 Agent 写入，同时保持原始 Session 输出不变。
- AI 保持连接时，人类仍可直接输入，包括向远端发送 `Ctrl+C`。
- 客户端可以分离，而不关闭其他客户端正在使用的 Session。
- 使用 Windows、Linux 和 macOS 的原生目标名称发现串口，支持 TOML Profile，以及不需要共享时的私有直连。
- 通过 stdio 或 Streamable HTTP 提供 MCP；共享 CLI 流程使用 HTTP Session Host。
- 通过已有串口 Session 进行有界的 CLI 文件/目录发送与接收，支持 SHA-256 校验和安全的暂存目录解包。

## 使用场景

### 嵌入式 Linux 调试

只需连接开发板一次。AI 读取保留的启动日志并执行有针对性的诊断，工程师同时观察相同的设备输出，并在实时 shell 中响应。

### AI 操作，人类观察

在空闲 Bash 提示符下，AI 通过 `terminal_exec` 发送非交互命令。已连接的 CLI 将命令显示一次为 `AI` 活动块，随后显示设备返回的真实字节；命令和内部引导代码都不会写入 Bash 历史。

### 人类介入

人类可以向共享 Session 输入，或向远端终端发送 `Ctrl+C`，然后继续观察，或按 `Ctrl+] q` 分离。短期 `terminal_exec` 租约会暂时锁定人类输入；普通原始写入只提供字节级串行化，不仲裁 shell 状态，因此客户端仍应避免冲突的原始命令序列。

## 工作原理

```text
Human CLI ---------+
                   |
                   v
            Session Host / Manager
                   |
AI via MCP --------+--> Session --> Serial Transport <--> Device
                        |    |
                        |    `-- serialized writes
                        `------- retained raw output
```

Session Host 持有物理端口并管理 Session 生命周期。Serial Transport 打开协议无关的 Channel；Session 拥有唯一的 Channel 读取者、有界的内存输出与活动保留，以及写入串行化。CLI 和 MCP 适配器复用相同的 Core 行为；高亮与 `AI` 活动块等显示处理位于存储的终端数据之外。

`SER-1` 等共享 Session 引用仅在所属 Host 进程内有效。停止 Host 会关闭其 Session；单个客户端分离不会。

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
