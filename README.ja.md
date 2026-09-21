[English](README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | 日本語 | [한국어](README.ko.md)

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

## クイックスタート：1 つのシリアル Session を共有する

個々の CLI が切断した後も Session を維持するには、専用の Host を先に起動します。

端末 1 — ローカル Host を起動：

```bash
channelterm mcp --transport http
```

次の行が表示されれば準備完了です。その後、出力がないのは正常です。

```text
MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp
```

端末 2 — OS ネイティブのシリアルターゲットを一覧し、開いて接続：

```bash
channelterm list --kind device --transport serial --no-mcp

# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

使用中のプラットフォームに対応するコマンドだけを実行してください。`--label board` は任意の
表示名であり、識別子ではありません。`channelterm attach board` では接続できません。

端末 3 — 別の人または別の端末から同じ Session に接続：

```bash
channelterm list --kind session
channelterm attach SER-1
```

各アタッチメントは独立した読み取りカーソルを持ちますが、同じデバイスの生出力を受信します。
`Ctrl+] q` で共有 Session を閉じずに現在の CLI だけを切断できます。Host とすべての Session を
終了するときだけ、端末 1 で `Ctrl+C` を押してください。

1 つの端末ですぐに使う場合は、`channelterm attach COM8` または OS ネイティブの `/dev/...`
ターゲットを直接実行できます。この場合は一時 Host が自動起動しますが、作成元のアタッチメント
が終了すると Host とすべての Session も停止します。共有用途では専用 Host を推奨します。

## 操作キー

```text
Ctrl+C      リモート端末へ 0x03 を送信
Ctrl+] q    共有 Session を閉じずに現在の CLI を切断
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
はローカルユーザーの token を自動的に読み取ります。TLS と追加のネットワークアクセス制御なしで
信頼できないネットワークへ公開しないでください。

## ファイル転送

共有 Session を通してファイルやディレクトリを送受信できます。

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

ターゲット側は必要な標準コマンドを備えた Linux Shell である必要があります。前提条件、上書き
保護、キャンセル、SHA-256 検証については[ファイル転送ワークフロー](docs/getting-started/file-transfer.md)
を参照してください。

## インストールとビルド

ChannelTerm には Go 1.25 以降が必要です。現在サポートされているパッチリリースを使用してください。

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
```

対応デスクトップターゲットは Windows、Linux、macOS の amd64/arm64 です。詳細は
[ソースからのビルド](docs/getting-started/build.md)および
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
