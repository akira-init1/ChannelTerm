[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

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

ChannelTerm は Windows、Linux、macOS でシリアル通信と SSH PTY シェルをサポートします。AI は内蔵せず、実際の端末 Session を人間と外部 AI クライアントに MCP 経由で提供します。

## MCP 経由のユーザー空間 GDB

ネイティブ Linux では、MCP クライアントが `debug_start` / `debug_command` / `debug_events` / `debug_stop` ツールで待機中の Bash Session を引き継ぎ、ブレークポイント、ステップ実行、変数・スレッド・レジスタの確認、デタッチを行えます。初版では ChannelTerm、GDB、gdbserver、ELF が同じ Linux 環境にある必要があります。単一 inferior の all-stop デバッグに対応し、端末状態の復元と一意のマーカーによるシェル確認が成功してから通常の書き込みを再開します。

```text
debug_start -> break_insert -> continue -> debug_events -> next/step/finish -> debug_stop
```

[手順、MCP 呼び出し、安全な attach、ローカル PTY テストの範囲](docs/getting-started/gdb-debugging.md).

独立して実行する GDB には、`debug_bridge_*` が準備済みのシリアル Session を認証付き TCP ブリッジで公開します。ChannelTerm は GDB や gdbserver を起動しません。GDB から `channelterm rsp-client` を使用します。認証済みの接続は RSP が無通信でもリースを自動更新し、最大有効期間は 24 時間です。切断後もシェル／TTY の復元を検証するまで排他リースを保持し、明示的な終了では Session も閉じます。Windows/WSL2 の設定と PTY・実機を区別した検証手順は[外部ブリッジのワークフロー](docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)を参照してください。

外部ブリッジは、準備済みのシリアル接続で通信できる互換 gdbserver と、ターゲットに対応する GDB があれば、Linux が動作するあらゆる開発ボードを対象にします。ブリッジはボードの機種やターゲットの CPU アーキテクチャを制限しません。ベアメタルのデバッグは対象外です。

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

## クイックスタート：1 コマンド、1 端末

検出されたシリアルデバイス、保存済み Profile、および既定の HTTP Host が利用可能な場合はその
Session を一覧します。

```bash
channelterm list
```

Host は実行中でなくてもかまいません。接続できない場合、`list` は MCP を offline と報告し、
ローカルのデバイスと Profile は引き続き表示します。

| よく使うオプション | 効果 |
| --- | --- |
| `--kind device`、`profile`、`session` | 指定した種類だけを表示します。カンマ区切りで複数指定できます。 |
| `--transport serial` | シリアルの結果だけを表示します。 |
| `--no-mcp` | MCP Host への問い合わせを省略し、ローカル情報だけを表示します。 |
| `--long`、`-l` | 表に完全な不透明 Session ID を追加します。 |
| `--json` | スクリプト向けの構造化 JSON を出力します。 |
| `--endpoint URL` | 既定以外の HTTP Host から Session を問い合わせます。 |

完全なオプション仕様は [CLI リファレンス](docs/reference/cli.md#list)を参照してください。

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

接続中は次のキーを使用できます。

```text
Ctrl+C      リモート端末へ 0x03 を送信
Ctrl+] q    現在の CLI を終了（一時 Host の所有者なら Host も停止）
Ctrl+] ?    ローカルのエスケープヘルプを表示
Ctrl+] ]    Ctrl+] バイトをリモートへ送信
Ctrl+] t    ローカルのシェルプロンプト時刻表示を切り替え
Ctrl+] f    ローカルのファイル送受信メニューを開く
Ctrl+] Esc  ローカルエスケープモードをキャンセル
```

対応する AI クライアントを同じ Host に接続するには、`channelterm init --mcp` を実行して HTTP を選び、クライアントから `SER-1` を一覧または操作します。認証とクライアントの利用手順は [MCP Server](docs/getting-started/mcp-server.md) を参照してください。

再利用可能なワークフローガイダンスを利用するには、Agent Skills 対応クライアントに同梱の
[`cterm-serial-debug` Agent Skill](docs/getting-started/mcp-server.md#install-the-bundled-agent-skill)を
インストールしてください。`channelterm init --mcp` は MCP クライアントを設定しますが、
Skill はインストールしません。

v0.2.0 以降は [Releases](https://github.com/akira-init1/ChannelTerm/releases) から
`channelterm_skills_VERSION.zip` をダウンロードできます。展開後、上記の Skill インストール手順に
従って `cterm-serial-debug` ディレクトリ全体をコピーしてください。

AI 自身が共有 Session を作成することもできます。まず `channelterm init --mcp` を一度実行して
HTTP を選び、クライアントを設定します。次に `channelterm mcp --transport http` でフォアグラウンド
の永続 Host を起動し、正確なシリアルターゲットと通信設定を AI に渡します。AI はポートを一覧し、
`terminal_open_serial` を呼び出し、生成された `SER-N` を報告できるため、人間は
`channelterm attach SER-N` で参加できます。Host の起動だけではポートは開きません。検出ポリシー
が `ask` の場合は明示的な承認が必要です。`auto` は `connect` を返しますが、AI は引き続き
`terminal_open_serial` を呼び出す必要があり、Host がポートを自動的に開くことはありません。
Host は通常 `Ctrl+C` で停止し、その際に Host が所有する Sessions も閉じます。

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

## AI、ファイル、ローカル操作

同じ共有シリアル Session で通常のファイルやディレクトリも転送でき、AI クライアントや別途インストールするボード側の転送エージェントは不要です。実行中の共有 `attach` で `Ctrl+]`、続けて `f` を押すと、ガイド付き送受信メニューが開きます。ディレクトリには標準 tar と、確認応答を伴いキャンセルしやすいチャンク転送を使用します。同じ操作はコマンドでも実行できます。

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

コマンドでリモートの宛先を省略すると `/tmp/cterm/mcp-files/` に保存され、対話型ショートカットは `/tmp/cterm/user-files/` を使用します。ChannelTerm は不足している宛先ディレクトリ階層を作成し、以後の転送で再利用します。Linux shell にはネイティブの `mkdir`、`stty`、`dd`、`wc`、`sha256sum`、`sleep` が必要で、ディレクトリには `tar` も必要です。ChannelTerm はサイズを制限したチャンクを転送して進捗を報告し、ファイルのバイト列とディレクトリの tar ストリームをエンドツーエンドの SHA-256 で検証します。ディレクトリ転送はターゲット側の一時アーカイブを使うため、キャンセル時は現在のチャンク完了後に停止し、残りのディレクトリ全体を送り切る必要はありません。一時的なファイル転送リースはコマンド終了まで他の ChannelTerm 書き込みをブロックします。リース中に付属のいずれかのアタッチメントで `Ctrl+C` を押すと、既定の選択が「いいえ」のローカルキャンセル確認が開きます。それ以外では通常のリモート端末操作として機能します。前提条件と制限は[ファイル転送ワークフロー](docs/getting-started/file-transfer.md)を参照してください。

AI を同じ Session に参加させるには、MCP クライアントに同じ Streamable HTTP エンドポイントを設定します。

```text
http://127.0.0.1:37099/mcp
```

AI は MCP Session ツールで `SER-1` を一覧・読み取りし、`terminal_exec` でアイドル状態の Bash コマンドを実行したり、`terminal_write` で明示的に生の端末バイトを送ったりできます。[MCP ワークフロー](docs/getting-started/mcp-server.md)と[ツールリファレンス](docs/reference/mcp-tools.md)を参照してください。`channelterm init --mcp` はクライアント設定をインストールし、`channelterm init --mcp-show` は設定を表示します。どちらも HTTP または stdio の選択を求め、既定は HTTP です。表示された HTTP 設定にはローカルの Bearer 資格情報が含まれるため、共有やコミットをしないでください。

`Ctrl+]` でローカルエスケープモードに入り、利用可能なコマンドをローカルに表示します。`Ctrl+] f` は現在のアタッチメントでファイル／ディレクトリのガイド付き送受信を開始します。既定の宛先は `/tmp/cterm/user-files/<basename>` と `./<basename>` で、既存データを上書きしないよう `_1`、`_2` を選びます。`Ctrl+] t` は既定でオフであり、確実に識別できるシェルプロンプトだけにローカルの `[HH:MM:SS]` 時刻を付けます。共有 Session や MCP 出力は変えません。

HTTP Host は Bearer token を必要とし、既定ではループバックだけを待ち受けます。組み込み CLI
はローカルユーザーの token を自動的に読み取ります。[信頼できる LAN で待ち受ける](docs/getting-started/mcp-server.md#listen-on-a-lan)
こともできますが、リモートクライアントは Host の到達可能な LAN アドレスを使用し、
`Authorization` ヘッダーで同じ token を送信する必要があります。TLS と追加のネットワークアクセス
制御なしで信頼できないネットワークへ公開しないでください。

## できること

- Host が所有する 1 本のシリアル接続を、人間の CLI と MCP クライアントで共有。
- 独立した出力・アクティビティカーソルにより、ある読み取り側が他の読み取り側のデータを消費することを防止。
- 生の Session 出力を変更せず、接続中の端末に Agent の書き込みを表示。
- AI が接続したまま、人間がリモートへの `Ctrl+C` を含む直接入力を実行。
- 他のクライアントが利用する Session を閉じずに、クライアントだけを切断。
- Windows、Linux、macOS のネイティブなターゲット名によるシリアル検出、TOML Profile、共有不要時のプライベート直接接続。
- stdio または Streamable HTTP 上の MCP。共有 CLI ワークフローは HTTP Session Host を使用。
- 既存のシリアル Session 上で、サイズを制限した転送による CLI のファイル／ディレクトリ送受信、SHA-256 検証、安全な一時領域でのディレクトリ展開。

## ワークフローの例

### 組み込み Linux のデバッグ

ボードへの接続は一度だけです。AI が保持された起動ログを読み、目的を絞った診断を実行する間、エンジニアは同じデバイス出力を観察してライブシェルで応答します。

### AI が操作し、人間が観察する

アイドル状態の Bash プロンプトで、AI が `terminal_exec` により非対話型コマンドを送ります。接続中の CLI はコマンドを `AI` アクティビティブロックとして一度表示し、その後にデバイスが返した実際のバイトを表示します。コマンドと内部の初期化処理は Bash の履歴に残りません。

### 人間が介入する

人間は共有 Session に入力したり、リモート端末に `Ctrl+C` を送ったりしてから、観察を続けるか `Ctrl+] q` で切断できます。短い `terminal_exec` リース中は人間の入力が一時的にロックされます。通常の生の書き込みはバイト単位で直列化されますが、シェル状態の調停は行わないため、クライアント間で競合する生のコマンド列を避ける必要があります。

## 仕組み

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

Session Host は物理ポートと Session のライフサイクルを所有します。Serial Transport はプロトコルに依存しない Channel を開き、Session は単一の Channel 読み取り処理、上限のあるメモリ内の出力・アクティビティ保持、書き込みの直列化を担当します。CLI と MCP のアダプターは同じ Core の動作を使います。ハイライトや `AI` アクティビティブロックなどの表示処理は、保存された端末データの外で行います。

`SER-1` などの共有 Session 参照は、所有元の Host プロセス内でのみ有効です。Host を停止するとその Session は閉じますが、クライアント 1 つの切断では閉じません。

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
