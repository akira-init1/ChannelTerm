[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> この文書は日本語版の入口です。動作、オプション、互換性については、英語版
> [README](README.md)、現在のソースコード、および `channelterm --help` が基準です。

ChannelTerm は、人間と AI が同じハードウェア端末 Session を共有するためのツールです。
AI が端末を置き換えるのではありません。AI がデバイスを読み書きしている間も、人間は接続を
維持し、AI の操作を確認し、直接入力し、必要に応じて介入できます。

ChannelTerm は Windows、Linux、macOS でシリアル通信と SSH PTY シェルをサポートします。AI は内蔵せず、実際の端末 Session を人間と外部 AI クライアントに MCP 経由で提供します。

## ChannelTerm を使う理由

従来のシリアル端末では、人間が物理接続を利用します。

```text
Human ---- terminal ---- Serial device
```

AI 専用のシリアルツールでは AI が接続を利用し、人間はライブ端末に参加できません。

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm は 1 本の物理接続を共有 Session に保持します。

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

これにより、ハードウェアのデバッグ中も AI の操作を観察でき、人間も参加し続けられます。共有は同じポートを開き直すだけではありません。各クライアントは独立したカーソルで保持された生の出力を読み、すべての書き込みは同じ Session を通ります。

## できること

- Host が所有する 1 本のシリアル接続を、人間の CLI と MCP クライアントで共有。
- 独立した出力・アクティビティカーソルにより、ある読み取り側が他の読み取り側のデータを消費することを防止。
- 生の Session 出力を変更せず、接続中の端末に Agent の書き込みを表示。
- AI が接続したまま、人間がリモートへの `Ctrl+C` を含む直接入力を実行。
- 追加で接続したクライアントだけを切断し、共有 Session を維持。一時 Host を所有するアタッチメントを終了すると、その Host と Session も終了。
- Windows、Linux、macOS のネイティブなターゲット名によるシリアル検出、TOML Profile、共有不要時のプライベート直接接続。
- stdio または Streamable HTTP 上の MCP。共有 CLI ワークフローは HTTP Session Host を使用。
- 既存のシリアル Session 上で、サイズを制限した転送による CLI のファイル／ディレクトリ送受信、SHA-256 検証、安全な一時領域でのディレクトリ展開。

## インストール

[Releases](https://github.com/akira-init1/ChannelTerm/releases) から OS とアーキテクチャに対応する
アーカイブをダウンロードし、同じリリースの `SHA256SUMS` と照合してください。展開先のディレクトリで
Windows は `.\channelterm.exe install`、Linux/macOS は `./channelterm install` を実行します。
ビルド済みバイナリの使用には Go は不要です。

ソースからのビルドには Go 1.26 以降が必要です。現在サポートされているパッチリリースを使い、
リポジトリのルートからビルドしてください。

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

対応デスクトップターゲットは Windows、Linux、macOS の amd64/arm64 です。詳細は
[ソースからのビルドとインストール](docs/getting-started/build.md)、
[`install` / `uninstall` の完全な契約](docs/reference/cli.md#install-and-uninstall)、
[設定パス](docs/reference/configuration.md)、および
[ビルドとテスト](docs/development/building-and-testing.md)を参照してください。

## クイックスタート：1 コマンド、1 端末

検出されたシリアルデバイス、保存済み Profile、および既定の HTTP Host が利用可能な場合はその
Session を一覧します。

```bash
channelterm list
```

Host は実行中でなくてもかまいません。接続できない場合、`list` は MCP を offline と報告し、
ローカルのデバイスと Profile は引き続き表示します。

完全なオプション仕様は [CLI リファレンス](docs/reference/cli.md#list)を参照してください。

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

接続中は次のキーを使用できます。

```text
Ctrl+C      リモート端末へ 0x03 を送信
Ctrl+] q    現在の CLI を終了（一時 Host の所有者なら Host も停止）
Ctrl+] ?    ローカルのエスケープヘルプを表示
```

ローカル操作の一覧は [CLI リファレンス](docs/reference/cli.md#attach)、クライアント設定と AI による Session 作成は [MCP ガイド](docs/getting-started/mcp-server.md)を参照してください。

再利用可能なワークフローガイダンスを利用するには、Agent Skills 対応クライアントに同梱の
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)を
インストールしてください。`channelterm init --mcp` は MCP クライアントを設定しますが、
Skill はインストールしません。

HTTP Host は Bearer token を必要とし、既定ではループバックだけを待ち受けます。組み込み CLI
はローカルユーザーの token を自動的に読み取ります。[信頼できる LAN で待ち受ける](docs/getting-started/mcp-server.md#listen-on-a-lan)
こともできますが、リモートクライアントは Host の到達可能な LAN アドレスを使用し、
`Authorization` ヘッダーで同じ token を送信する必要があります。TLS と追加のネットワークアクセス
制御なしで信頼できないネットワークへ公開しないでください。

<a id="ワークフローの例"></a>
<a id="組み込み-linux-のデバッグ"></a>
<a id="ai-が操作し人間が観察する"></a>
<a id="人間が介入する"></a>

## 共有端末の実例

接続中の CLI は Agent のコマンドをローカルの `AI` アクティビティブロックとして表示します。`terminal_exec` はそのコマンドをアイドル状態の Bash の履歴に残しません。ブロックに続く行はデバイスの生の端末出力であり、短いコマンドリースが解放されると人間が再び入力できます。

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

実際のコマンド出力は接続先のデバイスによって異なります。`AI` ブロックは構造化 Session イベントから生成されるローカル表示で、デバイスのバイトストリームや保存された端末出力には挿入されません。生のキー入力や対話型プログラムには引き続き `terminal_write` を使用し、デバイス本来の履歴動作を保ちます。

## SSH シェルの共有

認証を許可された秘密鍵を使用するか、要求されたらパスワードを入力します。暗号化された秘密鍵にはパスフレーズが必要です（どちらも保存されません）。
未知のサーバーでは表示されたフィンガープリントを確認し、`yes` を入力すると公開鍵が `~/.ssh/known_hosts` に保存されます。
変更されたサーバー鍵は拒否されます。これらの入力には対話型ターミナルが必要です。

```bash
# 端末 1：シェルを開き、そのプロンプトで uname -a を実行します。
channelterm ssh user@192.168.1.50

# 端末 2：同じシェルの出力を確認し、操作します。
channelterm attach SSH-1
```

最初のコマンドを実行したままにしてください。終了すると、この Host と Session も閉じます。既定のローカル共有ポートが使用中の場合は、`--listen` と表示された attach エンドポイントで別の Host を使います。鍵のパス、ホスト検証、ポート、現在の制限は [SSH ワークフロー](docs/getting-started/ssh-terminal.md)を参照してください。

## SSH 経由のファイル転送

SFTP でファイル転送とリモートファイル管理を行えます。対応する POSIX サーバーでは従来の SCP も使用できます。

```bash
channelterm sftp user@192.168.1.50 ls /tmp
channelterm sftp user@192.168.1.50 put ./firmware.bin /tmp/firmware.bin
channelterm sftp user@192.168.1.50 get /tmp/log.txt ./log.txt
channelterm scp user@192.168.1.50 put ./release /tmp/release --recursive
```

SSH と同じ鍵・パスワード認証とホスト検証を使い、端末 Session とは独立した接続で動作します。
既存ファイルの上書きには `--overwrite` が必要です。ディレクトリの扱い、キャンセル、制限は
[ファイル転送](docs/getting-started/file-transfer.md#sftp-and-scp-over-ssh)を参照してください。

共有端末 Session 経由のファイル転送は小規模な Linux-shell プロトコルを使用します。
独立した SSH ファイル接続は SFTP または従来の SCP を使用します。

<a id="aiファイルローカル操作"></a>

## 共有 Session でのファイル転送

同じ共有シリアル Session で通常のファイルやディレクトリも転送でき、AI クライアントや別途インストールするボード側の転送エージェントは不要です。実行中の共有 `attach` で `Ctrl+]`、続けて `f` を押すと、ガイド付き送受信メニューが開きます。ディレクトリには標準 tar と、確認応答を伴いキャンセルしやすいチャンク転送を使用します。同じ操作はコマンドでも実行できます。

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

転送には、待機中の Linux シェルと[ファイル転送ガイド](docs/getting-started/file-transfer.md)に記載されたツールが必要です。SHA-256 を検証し、既存の宛先を上書きせず新しい名前を選びます。転送中に共有アタッチメントで `Ctrl+C` を押すとキャンセルの確認が表示されます。後処理がリースを解放するまで、他の書き込みはブロックされます。

## MCP 経由のユーザー空間 GDB

ネイティブ Linux では、MCP クライアントが `debug_start` / `debug_command` / `debug_events` / `debug_stop` ツールで待機中の Bash Session を引き継ぎ、ブレークポイント、ステップ実行、変数・スレッド・レジスタの確認、デタッチを行えます。初版では ChannelTerm、GDB、gdbserver、ELF が同じ Linux 環境にある必要があります。単一 inferior の all-stop デバッグに対応し、端末状態の復元と一意のマーカーによるシェル確認が成功してから通常の書き込みを再開します。

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[手順、MCP 呼び出し、安全な attach、ローカル PTY テストの範囲](docs/getting-started/gdb-debugging.md).

独立して実行する GDB には、`debug_bridge_*` が準備済みのシリアル Session を認証付き TLS/TCP ブリッジとして公開します。操作者が互換性のある Linux gdbserver を準備し、GDB を独立して実行します。`channelterm rsp-client` は接続を担います。ボードの型番やターゲット CPU アーキテクチャの制限はありません。切断後も、期限のある復旧期間は排他的な所有権を保持します。復元の検証に成功すれば Session を維持し、明示的な終了や期限切れでは Session を閉じます。Windows/WSL2 の設定、更新、復旧は[外部ブリッジのガイド](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)を参照してください。ベアメタルデバッグは対象外です。

## 仕組み

```text
Human CLI -----+
               +--> Session Host --> Session <--> Channel <--> Serial / SSH endpoint
AI via MCP ----+                         |
                                        +-- retained output, activity, events
                                        `-- serialized writes
```

Session Host は接続と Session のライフサイクルを所有します。Serial または SSH Transport がプロトコルに依存しない Channel を確立し、Session はその Channel を読み取り、出力・アクティビティ・イベントを上限付きでメモリに保持し、書き込みを直列化します。CLI と MCP のアダプターは同じ Core の動作を再利用します。ハイライトや `AI` アクティビティブロックなどの表示処理は、保存された端末データの外で行います。

読み手ごとに独立したカーソルを使います。生の書き込みは直列化されますが、シェルコマンドの意味までは調整しません。コマンド実行、転送、デバッグ中は、アプリケーションのリースが他の書き込みを一時的にブロックします。所有権と並行性については[共有 Session](docs/getting-started/shared-session.md)を参照してください。

`SER-1` などの共有参照は、所有元の Host プロセス内でのみ有効です。Host を停止すると、その Session は閉じます。追加のアタッチメントは単独で終了できますが、一時 Host を所有するアタッチメントや所有元の SSH コマンドを終了すると、それらの Session も閉じます。レイヤーの境界は[アーキテクチャ概要](docs/architecture/overview.md)を参照してください。

## 設定

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
- [SSH シェルワークフロー](docs/getting-started/ssh-terminal.md)
- [共有 Session](docs/getting-started/shared-session.md)
- [ファイル転送ワークフロー](docs/getting-started/file-transfer.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI リファレンス](docs/reference/cli.md)
- [MCP ツールリファレンス](docs/reference/mcp-tools.md)
- [識別子と参照](docs/reference/identifiers.md)
- [アーキテクチャ概要](docs/architecture/overview.md)

正確なコマンドフラグと既定値は、実行中のプログラムの `--help` 出力を参照してください。

## ロードマップ

シリアルと SSH PTY シェルは実装済みの Transport です。専用の SFTP および従来の SCP ファイルクライアントも利用できます。任意の SSH exec、Telnet、より強い複数書き込み側の協調は今後の方向性であり、実装済み機能やリリースの約束ではありません。

ChannelTerm は現在、JTAG/OpenOCD、仮想シリアルデバイス、組み込み AI、マルチエージェントのオーケストレーションを実装していません。

## ライセンス

ChannelTerm は [Apache License 2.0](LICENSE) の下で提供されます。
