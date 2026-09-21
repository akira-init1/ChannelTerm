[English](README.md) | [简体中文](README.zh-CN.md) | 繁體中文 | [日本語](README.ja.md) | [한국어](README.ko.md)

# ChannelTerm

> 本文是繁體中文入口。行為、參數與相容性以英文 [README](README.md)、目前原始碼及
> `channelterm --help` 為準。

ChannelTerm 讓人類與 AI 共用同一個硬體終端 Session。AI 不會取代你的終端：AI 可以讀取、
操作裝置，而你仍能保持連線、看到 AI 的操作、直接輸入並在需要時介入。

```text
              實體序列埠裝置
                    |
              Serial Transport
                （單一連線）
                    |
                    v
          ChannelTerm Session SER-1
             /                 \
            /                   \
         人類終端              AI / MCP 用戶端
    channelterm attach          讀取與寫入工具
```

目前僅實作 Windows、Linux 與 macOS 上的序列埠 Transport。SSH 與 Telnet 是未來方向，
不是目前功能。ChannelTerm 本身也不是內建 AI；它透過 MCP 將真實終端 Session 提供給外部
AI 用戶端。

## 快速開始：共用一個序列埠 Session

若希望個別 CLI 離開後 Session 仍持續存在，請先獨立啟動持久 Host。

終端 1——啟動本機 Host：

```bash
channelterm mcp --transport http
```

出現下列訊息即代表已就緒，之後沒有輸出是正常現象：

```text
MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp
```

終端 2——列出原生序列埠目標，然後開啟並連入：

```bash
channelterm list --kind device --transport serial --no-mcp

# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

只需執行目前平台對應的命令。`--label board` 是選用的顯示名稱，不是識別碼，不能使用
`channelterm attach board` 連入。

終端 3——其他人或另一個終端連入同一 Session：

```bash
channelterm list --kind session
channelterm attach SER-1
```

每個附件都有獨立的讀取游標，但看到的是同一裝置的原始輸出。按 `Ctrl+] q` 只會離開目前
CLI，不會關閉共享 Session。只有需要關閉 Host 及其所有 Session 時，才在終端 1 按
`Ctrl+C`。

若只需要快速單終端操作，可直接執行 `channelterm attach COM8` 或原生 `/dev/...` 目標，
程式會自動啟動臨時 Host。但建立該 Host 的附件離開後，Host 與所有 Session 都會停止，
因此多人共用時建議使用上述持久 Host 流程。

## 互動按鍵

```text
Ctrl+C      將 0x03 傳送到遠端終端
Ctrl+] q    離開目前 CLI，但不關閉共享 Session
Ctrl+] ?    顯示本機跳脫說明
Ctrl+] ]    將字面 Ctrl+] 傳送到遠端
Ctrl+] t    切換本機 Shell 提示字元時間戳
Ctrl+] f    開啟本機檔案傳送/接收選單
Ctrl+] Esc  取消本機跳脫模式
```

## 讓 AI 連入同一 Session

執行：

```bash
channelterm init --mcp
```

選擇 HTTP 後，支援的 Codex、Claude Code、OpenCode 或 Zoo Code 用戶端會使用本機共享端點
`http://127.0.0.1:37099/mcp`。AI 可以列出、讀取及操作 `SER-1`；在已知閒置的 Bash 提示字元
應優先使用 `terminal_exec`，原始按鍵與互動程式則使用 `terminal_write`。

HTTP Host 需要 Bearer token，預設僅監聽回環位址。內建 CLI 會讀取本機使用者 token；請勿
將沒有 TLS 與額外網路存取控制保護的端點直接暴露到不受信任的網路。

## 檔案傳輸

共享 Session 可以傳送或接收檔案與目錄：

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

目標端必須是具備所需標準命令的 Linux Shell。詳細前提、覆寫保護、取消與 SHA-256 驗證規則
請參閱[檔案傳輸流程](docs/getting-started/file-transfer.md)。

## 安裝與建置

ChannelTerm 需要 Go 1.25 或更新版本。請使用目前仍受支援的修補版本，然後在儲存庫根目錄建置：

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
```

支援的桌面目標為 Windows、Linux、macOS 的 amd64/arm64。詳見
[從原始碼建置](docs/getting-started/build.md)與[建置及測試](docs/development/building-and-testing.md)。

## 文件

- [文件索引](docs/README.md)
- [序列埠終端流程](docs/getting-started/serial-terminal.md)
- [共享 Session](docs/getting-started/shared-session.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI 參考](docs/reference/cli.md)
- [MCP 工具參考](docs/reference/mcp-tools.md)
- [識別碼與引用](docs/reference/identifiers.md)

## 授權

ChannelTerm 採用 [Apache License 2.0](LICENSE)。
