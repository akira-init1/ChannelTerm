[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | 繁體中文

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

## 快速開始：一條命令，一個終端機

先列出原生序列埠目標；此操作不要求 MCP 伺服器已在執行：

```bash
channelterm list --kind device --transport serial --no-mcp
```

如果已經知道 `COM50` 這類原生目標，可以略過此步驟。

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

## 互動按鍵

```text
Ctrl+C      將 0x03 傳送到遠端終端
Ctrl+] q    離開目前 CLI；若它擁有暫時 Host，該 Host 也會停止
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

HTTP Host 需要 Bearer token，預設僅監聽回環位址。內建 CLI 會讀取本機使用者 token；它也可以
[監聽受信任的區域網路](docs/getting-started/mcp-server.md#listen-on-a-lan)，遠端用戶端必須使用 Host
可連線的區域網路位址，並透過 `Authorization` 標頭傳送相同的 token。請勿將沒有 TLS 與額外
網路存取控制保護的端點直接暴露到不受信任的網路。

## 檔案傳輸

共享 Session 可以傳送或接收檔案與目錄。在執行中的共用 `attach` 內，先按 `Ctrl+]`，再按 `f`，
即可開啟引導式傳送/接收選單；相同操作也能透過命令完成：

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

傳送時省略遠端目標路徑，檔案或目錄會預設儲存至 `/tmp/cterm/mcp-files/`；互動快捷鍵預設
使用 `/tmp/cterm/user-files/`。ChannelTerm 會依需要建立目錄，並為同名目標選擇 `_1`、`_2`
等不覆寫既有內容的名稱。

目標端必須是具備所需標準命令的 Linux Shell。詳細前提、覆寫保護、取消與 SHA-256 驗證規則
請參閱[檔案傳輸流程](docs/getting-started/file-transfer.md)。

## 安裝與建置

ChannelTerm 需要 Go 1.25 或更新版本。請使用目前仍受支援的修補版本，然後在儲存庫根目錄建置：

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

預設位置如下；安裝器每次執行後也會顯示實際路徑：

| 平台 | 命令目錄 | 安裝記錄 | 預設設定 |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json`；未設定時為 `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml`；未設定時為 `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

支援的桌面目標為 Windows、Linux、macOS 的 amd64/arm64。詳見
[從原始碼建置並安裝](docs/getting-started/build.md)、
[`install` / `uninstall` 完整契約](docs/reference/cli.md#install-and-uninstall)、
[設定位置](docs/reference/configuration.md)與[建置及測試](docs/development/building-and-testing.md)。

## 設定

ChannelTerm 將本機檔案儲存在各平台的使用者設定目錄中：

| 平台 | 預設目錄 |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/`；未設定時為 `~/.config/channelterm/` |
| macOS | `~/Library/Application Support/channelterm/` |

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
- [共享 Session](docs/getting-started/shared-session.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI 參考](docs/reference/cli.md)
- [MCP 工具參考](docs/reference/mcp-tools.md)
- [識別碼與引用](docs/reference/identifiers.md)

## 授權

ChannelTerm 採用 [Apache License 2.0](LICENSE)。
