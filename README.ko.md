[English](README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | 한국어

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

## 빠른 시작: 하나의 시리얼 Session 공유

개별 CLI가 종료된 뒤에도 Session을 유지하려면 전용 Host를 먼저 실행합니다.

터미널 1 — 로컬 Host 시작:

```bash
channelterm mcp --transport http
```

다음 줄이 출력되면 준비가 끝난 것입니다. 이후 출력이 없는 것은 정상입니다.

```text
MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp
```

터미널 2 — 운영체제의 시리얼 대상을 확인한 뒤 열고 연결:

```bash
channelterm list --kind device --transport serial --no-mcp

# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

현재 플랫폼에 맞는 명령 하나만 실행하면 됩니다. `--label board`는 선택적인 표시 이름이며
식별자가 아닙니다. `channelterm attach board`로는 연결할 수 없습니다.

터미널 3 — 다른 사용자 또는 다른 터미널에서 같은 Session 연결:

```bash
channelterm list --kind session
channelterm attach SER-1
```

각 연결은 독립적인 읽기 커서를 가지지만 동일한 장치의 원시 출력을 받습니다. `Ctrl+] q`를 누르면
공유 Session을 닫지 않고 현재 CLI만 분리됩니다. Host와 모든 Session을 닫을 때만 터미널 1에서
`Ctrl+C`를 누르십시오.

한 터미널에서 빠르게 사용할 때는 `channelterm attach COM8` 또는 운영체제의 `/dev/...` 대상을
직접 실행할 수 있습니다. 이 경우 임시 Host가 자동으로 시작되지만, 이를 만든 연결이 종료되면 Host와
모든 Session도 중지됩니다. 공유 작업에는 전용 Host 방식을 권장합니다.

## 대화형 키

```text
Ctrl+C      원격 터미널에 0x03 전송
Ctrl+] q    공유 Session을 닫지 않고 현재 CLI 분리
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
사용자 token을 자동으로 읽습니다. TLS와 추가 네트워크 접근 제어 없이 신뢰할 수 없는 네트워크에
엔드포인트를 직접 노출하지 마십시오.

## 파일 전송

공유 Session을 통해 파일과 디렉터리를 보내거나 받을 수 있습니다.

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

대상은 필요한 표준 명령을 갖춘 Linux Shell이어야 합니다. 요구 사항, 덮어쓰기 방지, 취소 및
SHA-256 검증 규칙은 [파일 전송 워크플로](docs/getting-started/file-transfer.md)를 참고하십시오.

## 설치 및 빌드

ChannelTerm에는 Go 1.25 이상이 필요합니다. 현재 지원되는 패치 릴리스를 사용하십시오.

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
```

지원되는 데스크톱 대상은 Windows, Linux, macOS의 amd64/arm64입니다. 자세한 내용은
[소스에서 빌드](docs/getting-started/build.md)와
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
