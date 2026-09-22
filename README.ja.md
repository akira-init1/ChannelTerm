[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | 日本語 | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> この文書は日本語版の入口です。動作、オプション、互換性については、英語版
> [README](README.md)、現在のソースコード、および `channelterm --help` が基準です。

ChannelTerm は、人間と AI が同じハードウェア端末 Session を共有するためのツールです。
AI が端末を置き換えるのではありません。AI がデバイスを読み書きしている間も、人間は接続を
維持し、AI の操作を確認し、直接入力し、必要に応じて介入できます。

```text
                 物理シリアルデバイス
                          |
                   Serial Transport
                    （単一の接続）
                          |
                          v
              ChannelTerm Session SER-1
                 /                 \
                /                   \
           人間の端末             AI / MCP クライアント
      channelterm attach             読み書きツール
```

現在実装されている Transport は、Windows、Linux、macOS のシリアル通信だけです。SSH と
Telnet は将来の方向性であり、現在の機能ではありません。ChannelTerm 自体に AI は内蔵されて
おらず、MCP を通して実際の端末 Session を外部 AI クライアントへ提供します。

## クイックスタート：1 コマンド、1 端末

実行中の MCP サーバーを必要とせず、OS ネイティブのシリアルターゲットを一覧できます。

```bash
channelterm list --kind device --transport serial --no-mcp
```

`COM50` などのネイティブターゲットが既知なら、この検出手順は省略できます。

AI クライアントを Session に参加させる場合は、接続前に対応クライアントを一度だけ設定します。

```bash
channelterm init --mcp
```

HTTP を選ぶか、Enter を押して既定の HTTP を使用します。設定にはローカル Bearer 認証情報が
含まれます。AI クライアントが変更をすぐに読み込まない場合は再読み込みまたは再起動してください。

次に、現在のプラットフォームに対応するコマンドを 1 つだけ実行します。

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

この 1 つの `attach` コマンドがシリアルデバイスを開き、現在の端末を接続します。既定の
エンドポイントで互換 Host が動作していない場合は、一時的なローカル HTTP MCP Session Host も
自動的に起動します。代表的な起動出力は次のとおりです。

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

このクイックパスでは、MCP サーバー用の別端末は不要です。アタッチメントが動作している間、設定済み
HTTP MCP クライアントは `http://127.0.0.1:37099/mcp` を使用して同じ `SER-1` を操作できます。
作成元のアタッチメントが終了すると、一時 Host とそのすべての Session も停止します。Session を
現在の端末から独立して維持する必要がある場合だけ、永続 Host を別に実行します。

`--label board` は任意の表示名であり、識別子ではありません。他のクライアントは `SER-1` または
完全な Session ID を使用します。

### よく使うワークフロー

| 目的 | コマンド |
| --- | --- |
| 共有シリアル Session を開き、一時 Host を自動起動 | `channelterm attach COM8 --baud 115200` |
| 対応 AI クライアントに共有 HTTP Host を設定 | `channelterm init --mcp` |
| Host と Session を独立して維持 | `channelterm mcp --transport http` |
| 既存 Session を一覧または接続 | `channelterm list --kind session` の後に `channelterm attach SER-1` |
| MCP 共有なしのプライベート接続を開く | `channelterm attach COM8 --private --baud 115200` |
| 構造化 Session アクティビティを監視 | `channelterm events SER-1` |
| 現在の共有アタッチメントでガイド付きファイル転送を開く | `Ctrl+]` を押してから `f` を押す |
| 共有 Session でファイルを送受信 | `channelterm file send firmware.bin --session SER-1` または `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

永続 Host は独立した長時間実行プロセスです。
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp` が表示された後、他の Shell や設定済み
AI クライアントが Session を作成または接続できます。準備完了後に出力がないのは正常です。Host と
そのすべての Session を終了するときは `Ctrl+C` を押します。

## 操作キー

```text
Ctrl+C      リモート端末へ 0x03 を送信
Ctrl+] q    現在の CLI を終了（一時 Host の所有者なら Host も停止）
Ctrl+] ?    ローカルのエスケープヘルプを表示
Ctrl+] ]    Ctrl+] バイトをリモートへ送信
Ctrl+] t    ローカルのシェルプロンプト時刻表示を切り替え
Ctrl+] f    ローカルのファイル送受信メニューを開く
Ctrl+] Esc  ローカルエスケープモードをキャンセル
```

## AI を同じ Session に接続する

次を実行します。

```bash
channelterm init --mcp
```

HTTP を選ぶと、対応する Codex、Claude Code、OpenCode、Zoo Code クライアントがローカル共有
エンドポイント `http://127.0.0.1:37099/mcp` を使用します。AI は `SER-1` を一覧、読み取り、操作
できます。既知のアイドル状態の Bash プロンプトでは `terminal_exec` を優先し、生キー入力や
対話型プログラムには `terminal_write` を使用します。

HTTP Host は Bearer token を必要とし、既定ではループバックだけを待ち受けます。組み込み CLI
はローカルユーザーの token を自動的に読み取ります。[信頼できる LAN で待ち受ける](docs/getting-started/mcp-server.md#listen-on-a-lan)
こともできますが、リモートクライアントは Host の到達可能な LAN アドレスを使用し、
`Authorization` ヘッダーで同じ token を送信する必要があります。TLS と追加のネットワークアクセス
制御なしで信頼できないネットワークへ公開しないでください。

## ファイル転送

共有 Session を通してファイルやディレクトリを送受信できます。実行中の共有 `attach` で
`Ctrl+]` を押してから `f` を押すと、ガイド付き送受信メニューが開きます。同じ操作は
コマンドからも実行できます。

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

送信時にリモートの宛先を省略すると、ファイルまたはディレクトリは既定で
`/tmp/cterm/mcp-files/` に保存されます。対話型ショートカットは
`/tmp/cterm/user-files/` を使用します。ChannelTerm は必要なディレクトリを作成し、同名の
宛先がある場合は既存内容を上書きしないよう `_1`、`_2` などの名前を選びます。

ターゲット側は必要な標準コマンドを備えた Linux Shell である必要があります。前提条件、上書き
保護、キャンセル、SHA-256 検証については[ファイル転送ワークフロー](docs/getting-started/file-transfer.md)
を参照してください。

## インストールとビルド

ChannelTerm には Go 1.25 以降が必要です。現在サポートされているパッチリリースを使用してください。

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install` は実行中のバイナリを現在のユーザーの標準プログラム領域へコピーし、`channelterm` と
`cterm` コマンドを作成します。必要な場合だけその場所をユーザーの PATH に追加し、設定がない場合は
最小構成を初期化します。PATH の変更が表示された場合は新しいターミナルを開いてください。
`channelterm uninstall` はインストーラーが所有するコマンドと PATH の変更を削除し、ユーザーデータを
保持します。`channelterm uninstall --purge` は明示的な確認後にのみ設定、デバイス状態、ローカルの
HTTP 認証情報を削除します。

既定の保存先は次のとおりです。インストーラーは実行後にも実際のパスを表示します。

| プラットフォーム | コマンドディレクトリ | インストール記録 | 既定の設定 |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json`、未設定の場合は `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml`、未設定の場合は `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

対応デスクトップターゲットは Windows、Linux、macOS の amd64/arm64 です。詳細は
[ソースからのビルドとインストール](docs/getting-started/build.md)、
[`install` / `uninstall` の完全な契約](docs/reference/cli.md#install-and-uninstall)、
[設定パス](docs/reference/configuration.md)、および
[ビルドとテスト](docs/development/building-and-testing.md)を参照してください。

## 設定

ChannelTerm はローカルファイルを各プラットフォームのユーザー設定ディレクトリに保存します。

| プラットフォーム | 既定のディレクトリ |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/`、未設定の場合は `~/.config/channelterm/` |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` にはユーザー管理のシリアル Profile と接続ポリシーが保存されます。`state.json` は
ChannelTerm が管理するデバイス識別状態です。`http-auth-token` は MCP HTTP クライアントの認証に
使用する秘密情報なので、共有したりコミットしたりしないでください。

```powershell
# シリアル Profile を保存して再利用します。
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# この操作で別の config.toml を使用します。
channelterm serial --config ./channelterm.toml --profile board
```

主なオプションは `--profile`、`--save`、`--config`、`--port`、`--baud` です。MCP Host は
`--connection-policy ask|auto|deny` にも対応します。`--config` が切り替えるのは選択した
`config.toml` だけで、既定の `state.json` や `http-auth-token` は移動しません。全フィールド、
優先順位、検証、永続化の動作については[シリアル Profile](docs/getting-started/serial-profiles.md)と
[設定リファレンス](docs/reference/configuration.md)を参照してください。

## ドキュメント

- [ドキュメント索引](docs/README.md)
- [シリアル端末ワークフロー](docs/getting-started/serial-terminal.md)
- [共有 Session](docs/getting-started/shared-session.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI リファレンス](docs/reference/cli.md)
- [MCP ツールリファレンス](docs/reference/mcp-tools.md)
- [識別子と参照](docs/reference/identifiers.md)

## ライセンス

ChannelTerm は [Apache License 2.0](LICENSE) の下で提供されます。
