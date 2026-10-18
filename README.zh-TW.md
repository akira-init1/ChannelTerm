[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> 本文是繁體中文入口。行為、參數與相容性以英文 [README](README.md)、目前原始碼及
> `channelterm --help` 為準。

ChannelTerm 讓人類與 AI 共用同一個硬體終端 Session。AI 不會取代你的終端：AI 可以讀取、
操作裝置，而你仍能保持連線、看到 AI 的操作、直接輸入並在需要時介入。

ChannelTerm 支援 Windows、Linux 與 macOS 上的序列埠通訊與 SSH PTY shell。它本身不內建 AI，而是透過 MCP 將真實終端 Session 提供給人類及外部 AI 用戶端。

## 為什麼使用 ChannelTerm？

傳統序列埠終端機讓人類持有實體連線：

```text
Human ---- terminal ---- Serial device
```

僅供 AI 使用的序列埠工具讓 AI 持有連線，但人類無法加入即時終端機：

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm 將一條實體連線保留在共用 Session 中：

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

如此一來，AI 的操作在硬體除錯過程中始終可見，人類也能持續參與。共用不只是重新開啟同一個連接埠：每個用戶端透過獨立游標讀取保留的原始輸出，所有寫入都經過同一個 Session。

## 支援的能力

- 由 Host 持有一條序列埠連線，供人類 CLI 與 MCP 用戶端共用。
- 獨立的輸出與活動游標，避免一個讀取者消耗另一個讀取者的資料。
- 在已連線的終端機中顯示 Agent 寫入，同時保持原始 Session 輸出不變。
- AI 保持連線時，人類仍可直接輸入，包括向遠端傳送 `Ctrl+C`。
- 退出後來加入的用戶端而不關閉共用 Session；退出擁有臨時 Host 的連線時，該 Host 及其 Sessions 也會關閉。
- 使用 Windows、Linux 與 macOS 的原生目標名稱探索序列埠，支援 TOML Profile，以及不需要共用時的私人直接連線。
- 透過 stdio 或 Streamable HTTP 提供 MCP；共用 CLI 流程使用 HTTP Session Host。
- 透過既有序列埠 Session 進行有界的 CLI 檔案/目錄傳送與接收，支援 SHA-256 驗證及安全的暫存目錄解壓縮。

## 安裝

從 [Releases](https://github.com/akira-init1/ChannelTerm/releases) 下載符合你的作業系統與架構的
壓縮檔，並與同一版本的 `SHA256SUMS` 核對。解壓縮後，在解壓縮目錄中執行
`.\channelterm.exe install`（Windows）或 `./channelterm install`（Linux/macOS）。
使用預先編譯的程式無需安裝 Go。

從原始碼建置需要 Go 1.26 或更新版本。請使用目前仍受支援的修補版本，然後在儲存庫根目錄建置：

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` 會將目前執行的二進位檔複製到目前使用者的標準程式目錄，建立 `channelterm` 與 `cterm`
命令，只在需要時將該目錄加入使用者 PATH，並在缺少設定時初始化最小設定。若命令提示 PATH 已變更，
請開啟新的終端機。`channelterm uninstall` 會刪除安裝器管理的命令與 PATH 變更，但保留使用者資料；
`channelterm uninstall --purge` 必須明確確認後才會刪除設定、裝置狀態與本機 HTTP 驗證資訊。

支援的桌面目標為 Windows、Linux、macOS 的 amd64/arm64。詳見
[從原始碼建置並安裝](docs/getting-started/build.md)、
[`install` / `uninstall` 完整契約](docs/reference/cli.md#install-and-uninstall)、
[設定位置](docs/reference/configuration.md)與[建置及測試](docs/development/building-and-testing.md)。

## 快速開始：一條命令，一個終端機

列出偵測到的序列埠裝置、已儲存的 Profile，以及預設 HTTP Host 可用時其中的 Session：

```bash
channelterm list
```

Host 不必處於執行狀態。若無法連線，`list` 會將 MCP 回報為 offline，同時繼續顯示本機裝置與
Profile。

完整參數契約請參閱 [CLI 參考](docs/reference/cli.md#list)。

若需要 AI 用戶端加入 Session，請在連入前為支援的用戶端執行一次設定：

```bash
channelterm init --mcp
```

選擇 HTTP，或直接按 Enter 採用預設的 HTTP。設定包含本機 Bearer 憑證；若 AI 用戶端沒有立即
讀取新設定，請重新載入或啟動用戶端。

接著只執行目前平台對應的一條命令：

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

這一條 `attach` 命令會開啟序列埠並讓目前終端機連入。如果預設端點沒有相容的 Host，它也會
自動啟動暫時的本機 HTTP MCP Session Host。典型輸出如下：

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

快速使用時不必另外開一個終端機執行 MCP 伺服器。只要此附件保持執行，已設定的 HTTP MCP 用戶端
即可透過 `http://127.0.0.1:37099/mcp` 操作同一個 `SER-1`。建立暫時 Host 的附件離開後，該 Host
與其所有 Session 都會停止。只有 Session 必須獨立於目前終端機持續存在時，才需要另外執行持久
Host。

`--label board` 只是選用顯示名稱，不是識別碼。其他用戶端應使用 `SER-1` 或完整 Session ID。

### 常用方式

| 目標 | 命令 |
| --- | --- |
| 開啟共用序列埠 Session，並自動啟動暫時 Host | `channelterm attach COM8 --baud 115200` |
| 為支援的 AI 用戶端設定共用 HTTP Host | `channelterm init --mcp` |
| 讓 Host 與 Session 獨立持續執行 | `channelterm mcp --transport http` |
| 列出或加入既有 Session | `channelterm list --kind session`，接著執行 `channelterm attach SER-1` |
| 開啟不透過 MCP 共用的私有序列埠 | `channelterm attach COM8 --private --baud 115200` |
| 觀察結構化 Session 活動 | `channelterm events SER-1` |
| 在目前共用附件中開啟引導式檔案傳輸選單 | 先按 `Ctrl+]`，再按 `f` |
| 透過共用 Session 傳送或接收檔案 | `channelterm file send firmware.bin --session SER-1` 或 `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

持久 Host 是獨立且長時間執行的程序。它顯示
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp` 後，其他 Shell 與已設定的 AI
用戶端即可建立或加入 Session。就緒訊息之後沒有輸出是正常現象；需要關閉 Host 及其所有
Session 時按 `Ctrl+C`。

連線期間可使用以下按鍵：

```text
Ctrl+C      將 0x03 傳送到遠端終端
Ctrl+] q    離開目前 CLI；若它擁有暫時 Host，該 Host 也會停止
Ctrl+] ?    顯示本機跳脫說明
```

完整本機快捷鍵見 [CLI 參考](docs/reference/cli.md#attach)；用戶端設定與由 AI 建立 Session 的流程見 [MCP 使用流程](docs/getting-started/mcp-server.md)。

若要取得可重複使用的工作流程指引，請在支援 Agent Skills 的客戶端中安裝儲存庫內附的
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)。
`channelterm init --mcp` 只會設定 MCP 客戶端，不會安裝 Skill。

HTTP Host 需要 Bearer token，預設僅監聽回環位址。內建 CLI 會讀取本機使用者 token；它也可以
[監聽受信任的區域網路](docs/getting-started/mcp-server.md#listen-on-a-lan)，遠端用戶端必須使用 Host
可連線的區域網路位址，並透過 `Authorization` 標頭傳送相同的 token。請勿將沒有 TLS 與額外
網路存取控制保護的端點直接暴露到不受信任的網路。

<a id="使用情境"></a>
<a id="嵌入式-linux-除錯"></a>
<a id="ai-操作人類觀察"></a>
<a id="人類介入"></a>

## 共用終端機實際範例

已連線的 CLI 會將 Agent 命令顯示為本機 `AI` 活動區塊。`terminal_exec` 不會將該命令寫入閒置 Bash 提示符的歷程記錄；區塊之後的內容仍是裝置的原始終端機輸出，短期命令租約釋放後，人類即可繼續輸入：

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

命令的實際輸出取決於所連線的裝置。`AI` 活動區塊是依據結構化 Session 事件產生的本機顯示，不會插入裝置位元組串流或儲存的終端機輸出。原始按鍵與互動程式仍使用 `terminal_write`，保留裝置原本的歷程記錄行為。

## 共用 SSH shell

使用已授權的私鑰（加密私鑰會提示輸入口令），或依提示輸入登入密碼，兩者均不儲存。首次連線至未知伺服器時，核驗顯示的指紋後輸入
`yes`，將伺服器公鑰儲存至 `~/.ssh/known_hosts`；已知伺服器的金鑰發生變更時拒絕連線。互動提示需要實際終端機：

```bash
# 終端機 1：開啟 shell，在遠端提示符下執行 uname -a。
channelterm ssh user@192.168.1.50

# 終端機 2：查看並使用同一個 shell。
channelterm attach SSH-1
```

保持第一個命令執行；結束它會關閉該 Host 及其 Session。預設本機共用連接埠必須閒置；若已占用，可用 `--listen` 指定另一個 Host，並使用輸出中的 attach endpoint。私鑰路徑、主機驗證、連接埠與目前限制見 [SSH 使用流程](docs/getting-started/ssh-terminal.md)。

## 透過 SSH 傳輸檔案

使用 SFTP 傳輸及管理遠端檔案，或使用傳統 SCP 連線至相容的 POSIX 伺服器：

```bash
channelterm sftp user@192.168.1.50 ls /tmp
channelterm sftp user@192.168.1.50 put ./firmware.bin /tmp/firmware.bin
channelterm sftp user@192.168.1.50 get /tmp/log.txt ./log.txt
channelterm scp user@192.168.1.50 put ./release /tmp/release --recursive
```

這些命令沿用 SSH 的金鑰/密碼驗證與主機核驗，使用獨立連線，不占用終端 Session。
覆寫現有檔案需要 `--overwrite`。目錄規則、取消行為及協定限制請見
[檔案傳輸](docs/getting-started/file-transfer.md#sftp-and-scp-over-ssh)。

透過共用終端 Session 傳輸檔案使用輕量的 Linux-shell 協定。獨立 SSH 檔案連線使用 SFTP 或傳統 SCP。

<a id="ai檔案與本機控制"></a>

## 透過共用 Session 傳輸檔案

同一個共用序列埠 Session 也能傳輸一般檔案與目錄，無需 AI 用戶端，也無需在板端另行安裝傳輸代理。在執行中的共用 `attach` 中，先按 `Ctrl+]`，再按 `f`，即可開啟引導式傳送/接收選單。目錄傳輸使用標準 tar 與帶確認、便於取消的資料區塊；相同操作也能透過命令完成：

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

傳輸需要閒置的 Linux shell，以及[檔案傳輸流程](docs/getting-started/file-transfer.md)列出的工具。傳輸會驗證 SHA-256，並選擇新名稱以避免覆寫既有目標。傳輸期間，在共用附件中按 `Ctrl+C` 會詢問是否取消；清理完成並釋放租約前，其他寫入者仍被阻擋。

## 透過 MCP 使用使用者態 GDB

在原生 Linux 上，MCP 用戶端可透過 `debug_start` / `debug_command` / `debug_events` / `debug_stop` 工具接管閒置 Bash Session、設定中斷點、逐步執行、查看變數/執行緒/暫存器並分離。首版要求 ChannelTerm、GDB、gdbserver 和 ELF 位於同一 Linux 環境，支援單一 inferior、all-stop 除錯；只有還原終端機狀態並通過唯一標記 Shell 探測後，才開放一般寫入。

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[工作流程、MCP 呼叫、安全 attach 與本機 PTY 測試範圍](docs/getting-started/gdb-debugging.md).

獨立執行的 GDB 可透過 `debug_bridge_*` 建立的驗證 TLS/TCP 橋接存取已準備好的序列埠 Session。操作者準備相容的 Linux gdbserver 並獨立執行 GDB；`channelterm rsp-client` 負責連線。橋接不限制開發板型號或目標 CPU 架構。斷線後會在有限的復原期間保留獨占所有權。復原驗證成功可保留 Session；明確關閉或到期會關閉 Session。Windows/WSL2 設定、續期與復原見[外部橋接工作流程](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)。裸機除錯不在支援範圍內。

## 運作原理

```text
Human CLI -----+
               +--> Session Host --> Session <--> Channel <--> Serial / SSH endpoint
AI via MCP ----+                         |
                                        +-- retained output, activity, events
                                        `-- serialized writes
```

Session Host 擁有連線並管理 Session 生命週期。Serial 或 SSH Transport 建立協定無關的 Channel；Session 讀取該 Channel，在記憶體中保留有界的輸出、活動及事件，並序列化寫入。CLI 與 MCP 介面層重用相同的 Core 行為。醒目提示和 `AI` 活動區塊等顯示處理不會改變儲存的終端機原始資料。

讀取者擁有獨立游標。原始寫入會序列化，但不會協調 shell 命令的語意；應用層租約會在命令執行、檔案傳輸及除錯期間暫時阻擋其他寫入者。所有權與並行規則見[共用 Session](docs/getting-started/shared-session.md)。

`SER-1` 等共用參照僅在所屬 Host 程序內有效。停止 Host 會關閉其 Sessions。後來加入的連線可以單獨退出，但退出擁有臨時 Host 的連線或建立 Host 的 SSH 命令時，也會關閉其 Sessions。各層職責見[架構概觀](docs/architecture/overview.md)。

## 設定

`config.toml` 儲存使用者維護的序列埠 Profile 與連線原則；`state.json` 是 ChannelTerm 自動維護的
裝置身分狀態；`http-auth-token` 是 MCP HTTP 用戶端的驗證密鑰，請勿分享或提交。

```powershell
# 儲存並重複使用序列埠 Profile。
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# 本次操作使用另一個 config.toml。
channelterm serial --config ./channelterm.toml --profile board
```

常用參數包括 `--profile`、`--save`、`--config`、`--port` 與 `--baud`；MCP Host 也支援
`--connection-policy ask|auto|deny`。`--config` 只會替換所選的 `config.toml`，不會移動預設的
`state.json` 或 `http-auth-token`。完整欄位、優先順序、驗證與持久化行為請參閱
[序列埠 Profile](docs/getting-started/serial-profiles.md)與[設定參考](docs/reference/configuration.md)。

## 文件

- [文件索引](docs/README.md)
- [序列埠終端流程](docs/getting-started/serial-terminal.md)
- [SSH shell 使用流程](docs/getting-started/ssh-terminal.md)
- [共享 Session](docs/getting-started/shared-session.md)
- [檔案傳輸流程](docs/getting-started/file-transfer.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI 參考](docs/reference/cli.md)
- [MCP 工具參考](docs/reference/mcp-tools.md)
- [識別碼與引用](docs/reference/identifiers.md)
- [架構概覽](docs/architecture/overview.md)

確切的命令參數與預設值以執行中程式的 `--help` 輸出為準。

## 路線圖

序列埠與 SSH PTY shell 已實作為 Transport；專用 SFTP 與傳統 SCP 檔案用戶端也已提供。任意 SSH exec、Telnet 與更強的多寫入者協調仍是未來方向，不屬於已實作功能或版本承諾。

ChannelTerm 目前尚未實作 JTAG/OpenOCD、虛擬序列埠裝置、內建 AI 或多 Agent 協作調度。

## 授權

ChannelTerm 採用 [Apache License 2.0](LICENSE)。
