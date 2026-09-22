[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | 한국어 | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

# ChannelTerm

> 이 문서는 한국어 안내 문서입니다. 동작, 옵션, 호환성에 관한 기준은 영문
> [README](README.md), 현재 소스 코드와 `channelterm --help`입니다.

ChannelTerm은 사람과 AI가 하나의 하드웨어 터미널 Session을 공유하도록 합니다. AI가 사용자의
터미널을 대체하지 않습니다. AI가 장치를 읽고 조작하는 동안에도 사용자는 연결된 상태로 AI의 작업을
확인하고, 직접 입력하고, 필요할 때 개입할 수 있습니다.

```text
                 물리 시리얼 장치
                         |
                  Serial Transport
                    (단일 연결)
                         |
                         v
             ChannelTerm Session SER-1
                /                 \
               /                   \
            사용자 터미널          AI / MCP 클라이언트
       channelterm attach             읽기/쓰기 도구
```

현재 구현된 Transport는 Windows, Linux, macOS의 시리얼 통신뿐입니다. SSH와 Telnet은 향후
방향이며 현재 기능이 아닙니다. ChannelTerm 자체에 AI가 내장된 것도 아닙니다. MCP를 통해 실제
터미널 Session을 외부 AI 클라이언트에 제공합니다.

## 빠른 시작: 명령 하나, 터미널 하나

실행 중인 MCP 서버 없이 운영체제의 시리얼 대상을 먼저 확인합니다.

```bash
channelterm list --kind device --transport serial --no-mcp
```

`COM50`과 같은 기본 대상 이름을 이미 알고 있다면 이 검색 단계는 생략할 수 있습니다.

AI 클라이언트가 Session에 참여해야 한다면 연결하기 전에 지원되는 클라이언트를 한 번 설정합니다.

```bash
channelterm init --mcp
```

HTTP를 선택하거나 Enter를 눌러 기본 HTTP를 사용합니다. 설정에는 로컬 Bearer 인증 정보가 포함됩니다.
AI 클라이언트가 변경 사항을 즉시 읽지 않으면 다시 로드하거나 재시작하십시오.

그다음 현재 플랫폼에 맞는 명령 하나만 실행합니다.

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

이 `attach` 명령 하나가 시리얼 장치를 열고 현재 터미널을 연결합니다. 기본 엔드포인트에서 호환되는
Host가 실행 중이 아니면 임시 로컬 HTTP MCP Session Host도 자동으로 시작합니다. 일반적인 시작
출력은 다음과 같습니다.

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

이 빠른 경로에는 MCP 서버용 별도 터미널이 필요하지 않습니다. 연결이 유지되는 동안 설정된 HTTP MCP
클라이언트는 `http://127.0.0.1:37099/mcp`를 사용하여 같은 `SER-1`을 조작할 수 있습니다. 생성한
연결이 종료되면 임시 Host와 그 Host가 소유한 모든 Session도 중지됩니다. Session이 현재 터미널과
독립적으로 유지되어야 할 때만 영구 Host를 별도로 실행합니다.

`--label board`는 선택적인 표시 이름이지 식별자가 아닙니다. 다른 클라이언트는 `SER-1` 또는 전체
Session ID를 사용해야 합니다.

### 자주 쓰는 워크플로

| 목적 | 명령 |
| --- | --- |
| 공유 시리얼 Session을 열고 임시 Host 자동 시작 | `channelterm attach COM8 --baud 115200` |
| 지원되는 AI 클라이언트에 공유 HTTP Host 설정 | `channelterm init --mcp` |
| Host와 Session을 독립적으로 계속 실행 | `channelterm mcp --transport http` |
| 기존 Session 목록 확인 또는 연결 | `channelterm list --kind session` 실행 후 `channelterm attach SER-1` |
| MCP 공유 없는 비공개 시리얼 연결 열기 | `channelterm attach COM8 --private --baud 115200` |
| 구조화된 Session 활동 관찰 | `channelterm events SER-1` |
| 현재 공유 연결에서 안내식 파일 전송 메뉴 열기 | `Ctrl+]`를 누른 다음 `f` 누르기 |
| 공유 Session으로 파일 송수신 | `channelterm file send firmware.bin --session SER-1` 또는 `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

영구 Host는 독립적으로 장시간 실행되는 프로세스입니다.
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp`가 표시된 뒤 다른 Shell과 설정된 AI
클라이언트가 Session을 만들거나 연결할 수 있습니다. 준비 메시지 이후 출력이 없는 것은 정상입니다.
Host와 모든 Session을 닫을 때 `Ctrl+C`를 누르십시오.

## 대화형 키

```text
Ctrl+C      원격 터미널에 0x03 전송
Ctrl+] q    현재 CLI 종료(소유한 임시 Host가 있으면 Host도 중지)
Ctrl+] ?    로컬 이스케이프 도움말 표시
Ctrl+] ]    원격으로 Ctrl+] 바이트 전송
Ctrl+] t    로컬 셸 프롬프트 타임스탬프 전환
Ctrl+] f    로컬 파일 송수신 메뉴 열기
Ctrl+] Esc  로컬 이스케이프 모드 취소
```

## AI를 같은 Session에 연결

다음을 실행합니다.

```bash
channelterm init --mcp
```

HTTP를 선택하면 지원되는 Codex, Claude Code, OpenCode 또는 Zoo Code 클라이언트가 로컬 공유
엔드포인트 `http://127.0.0.1:37099/mcp`를 사용합니다. AI는 `SER-1`을 나열하고 읽고 조작할 수
있습니다. 유휴 상태임을 알고 있는 Bash 프롬프트에서는 `terminal_exec`를 우선 사용하고, 원시 키와
대화형 프로그램에는 `terminal_write`를 사용합니다.

HTTP Host에는 Bearer token이 필요하며 기본적으로 루프백 주소에서만 수신합니다. 내장 CLI는 로컬
사용자 token을 자동으로 읽습니다. [신뢰할 수 있는 LAN에서 수신](docs/getting-started/mcp-server.md#listen-on-a-lan)할
수도 있지만, 원격 클라이언트는 Host의 연결 가능한 LAN 주소를 사용하고 `Authorization` 헤더로 같은
token을 보내야 합니다. TLS와 별도의 네트워크 접근 제어 없이 신뢰할 수 없는 네트워크에 엔드포인트를
직접 노출하지 마십시오.

## 파일 전송

공유 Session을 통해 파일과 디렉터리를 보내거나 받을 수 있습니다. 실행 중인 공유 `attach`에서
`Ctrl+]`를 누른 다음 `f`를 누르면 안내식 송수신 메뉴가 열립니다. 같은 작업은 명령으로도
실행할 수 있습니다.

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

전송할 때 원격 대상 경로를 생략하면 파일이나 디렉터리는 기본적으로
`/tmp/cterm/mcp-files/`에 저장됩니다. 대화형 단축키는 `/tmp/cterm/user-files/`를
사용합니다. ChannelTerm은 필요한 디렉터리를 만들고, 같은 이름의 대상이 있으면 기존 내용을
덮어쓰지 않도록 `_1`, `_2` 등의 이름을 선택합니다.

대상은 필요한 표준 명령을 갖춘 Linux Shell이어야 합니다. 요구 사항, 덮어쓰기 방지, 취소 및
SHA-256 검증 규칙은 [파일 전송 워크플로](docs/getting-started/file-transfer.md)를 참고하십시오.

## 설치 및 빌드

ChannelTerm에는 Go 1.25 이상이 필요합니다. 현재 지원되는 패치 릴리스를 사용하십시오.

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
./channelterm install
```

`install`은 현재 실행 중인 바이너리를 현재 사용자의 표준 프로그램 위치로 복사하고 `channelterm`과
`cterm` 명령을 만듭니다. 필요한 경우에만 해당 위치를 사용자 PATH에 추가하며, 설정이 없으면 최소
설정을 초기화합니다. PATH 변경이 보고되면 새 터미널을 여십시오. `channelterm uninstall`은 설치
프로그램이 소유한 명령과 PATH 변경을 제거하지만 사용자 데이터는 보존합니다.
`channelterm uninstall --purge`는 명시적으로 확인한 뒤에만 설정, 장치 상태 및 로컬 HTTP 인증 정보를
삭제합니다.

기본 위치는 다음과 같습니다. 설치 프로그램은 실행 후 실제 경로도 출력합니다.

| 플랫폼 | 명령 디렉터리 | 설치 기록 | 기본 설정 |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json`, 설정되지 않은 경우 `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml`, 설정되지 않은 경우 `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

지원되는 데스크톱 대상은 Windows, Linux, macOS의 amd64/arm64입니다. 자세한 내용은
[소스에서 빌드 및 설치](docs/getting-started/build.md),
[`install` / `uninstall` 전체 계약](docs/reference/cli.md#install-and-uninstall),
[설정 경로](docs/reference/configuration.md)와
[빌드 및 테스트](docs/development/building-and-testing.md)를 참고하십시오.

## 설정

ChannelTerm은 로컬 파일을 각 플랫폼의 사용자 설정 디렉터리에 저장합니다.

| 플랫폼 | 기본 디렉터리 |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/`, 설정되지 않은 경우 `~/.config/channelterm/` |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml`에는 사용자가 관리하는 시리얼 Profile과 연결 정책이 저장됩니다. `state.json`은
ChannelTerm이 관리하는 장치 식별 상태입니다. `http-auth-token`은 MCP HTTP 클라이언트 인증에
사용되는 비밀 정보이므로 공유하거나 커밋하지 마십시오.

```powershell
# 시리얼 Profile을 저장하고 다시 사용합니다.
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# 이 작업에서 다른 config.toml을 사용합니다.
channelterm serial --config ./channelterm.toml --profile board
```

주요 옵션은 `--profile`, `--save`, `--config`, `--port`, `--baud`입니다. MCP Host는
`--connection-policy ask|auto|deny`도 지원합니다. `--config`는 선택한 `config.toml`만 변경하며
기본 `state.json`이나 `http-auth-token`은 이동하지 않습니다. 전체 필드, 우선순위, 검증 및 영속화
동작은 [시리얼 Profile](docs/getting-started/serial-profiles.md)과
[설정 레퍼런스](docs/reference/configuration.md)를 참고하십시오.

## 문서

- [문서 색인](docs/README.md)
- [시리얼 터미널 워크플로](docs/getting-started/serial-terminal.md)
- [공유 Session](docs/getting-started/shared-session.md)
- [MCP Server](docs/getting-started/mcp-server.md)
- [CLI 레퍼런스](docs/reference/cli.md)
- [MCP 도구 레퍼런스](docs/reference/mcp-tools.md)
- [식별자와 참조](docs/reference/identifiers.md)

## 라이선스

ChannelTerm은 [Apache License 2.0](LICENSE)에 따라 제공됩니다.
