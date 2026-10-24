# Building and Testing

## Required checks

For a normal Go change, format every changed Go file, then run the repository checks from the root:

```powershell
gofmt -w <changed-go-files>
go test ./...
go vet ./...
go test -race ./...
```

Documentation-only changes do not require formatting unchanged Go files but still require repository
tests and vet before completion.

### Repeated native testing on Windows

For repeated local Windows runs, use the stable-path runner instead of starting a new temporary
test executable on every invocation:

```powershell
./scripts/test-windows.ps1
./scripts/test-windows.ps1 -Packages ./internal/transport/ssh -Run '^TestHostTrust' -Count 3
```

The runner compiles each selected test package with `go test -c` under the ignored
`bin/tests/windows-<arch>/<import-path>/tests.exe` tree, then runs it from its source directory.
Paths remain stable between runs, including helper subprocesses created by tests. The default is
one uncached run with the standard ten-minute test timeout. `-Race` additionally requires the same
Windows C compiler/CGO setup as ordinary `go test -race`. Cross-compilation overrides are rejected
because this runner executes native Windows binaries. Avoid concurrent runs sharing this output tree.

Windows Firewall identifies traditional program rules by executable path. Go's temporary build/test
paths can therefore trigger repeated access prompts. A stable path can reuse an existing decision,
although the first run or managed security policies can still prompt. This script does not create
firewall exceptions, disable protection, or expand network listeners. Network unit fixtures listen
on loopback only. CI and Linux/macOS can continue using the ordinary `go test` commands above.

Useful command-surface checks are:

```powershell
go run ./cmd/channelterm --help
go run ./cmd/channelterm attach --help
go run ./cmd/channelterm list --help
go run ./cmd/channelterm mcp --help
go run ./cmd/channelterm serial --help
go run ./cmd/channelterm ssh --help
```

Run the Session buffer benchmarks without unit tests with:

```powershell
go test -run '^$' -bench . -benchmem ./internal/core/session
```

## Build scripts

The Electron GUI is a separate build target with its own dependency lock and build output under
`gui/electron/`. Its [development guide](electron-gui.md#checks-and-isolated-host) owns the npm
checks and native desktop acceptance test. CLI commands below never invoke Node or npm; the GUI
production build does not compile or copy Go Core. Existing tagged CLI releases remain unchanged.

PowerShell:

```powershell
./scripts/build.ps1
```

Linux/Bash:

```bash
./scripts/build.sh
```

Both scripts delete and recreate `dist/`, set `CGO_ENABLED=0`, and build:

```text
windows/amd64  windows/arm64
linux/amd64    linux/arm64
darwin/amd64   darwin/arm64
```

The PowerShell script reports size, timestamp, and SHA-256 for each artifact. The Bash script
additionally requires `stat`, `sha256sum`, `awk`, and `date`. Do not keep unrelated files only in
`dist/` when running either script.

Direct Go builds report version `devel` with unknown commit and build time. Both repository build
scripts inject the current 12-character Git commit and one UTC RFC 3339 timestamp into all six
artifacts; a dirty worktree adds `-dirty` to the commit. Set `CHANNELTERM_VERSION` to inject one
version into both CLI output and MCP server metadata:

```bash
CHANNELTERM_VERSION=0.1.0 ./scripts/build.sh
```

## Continuous integration

`.github/workflows/ci.yml` runs on pushes, pull requests, and manual dispatch. It verifies the Go
1.26 release line declared by `go.mod`, tests natively on Linux, Windows, and macOS with the current
stable Go release, and runs formatting, `go vet`, the race detector, all six cross-builds, and
`govulncheck`. The Windows job also validates `scripts/build.ps1` and its version injection. A
release candidate should not be tagged until every job passes.

A separate Linux job installs GDB/gdbserver and runs both the managed and external GDB integration
suites with race detection and `CHANNELTERM_TEST_GDB=1`. It uses disposable local processes and
PTYs, without hardware or changes to system ptrace policy. Missing tools or denied ptrace access
fail this opted-in job instead of silently skipping the suite. The 310-second idle test remains
separately opt-in through `CHANNELTERM_TEST_RSP_LONG=1`.

`.github/workflows/release.yml` runs only when a tag matching `v*` is pushed. It validates that the
tag is `v` followed by a SemVer version, strips the `v` for the embedded CLI version, tests and
cross-builds with `CGO_ENABLED=0`, and packages all six supported targets. Windows archives are ZIP
files containing `channelterm.exe`; Linux and macOS archives are tarballs containing `channelterm`.
All executable archives also include `THIRD_PARTY_NOTICES` for the SSH dependency.
Public macOS archives use `macos` in their filenames, such as
`channelterm_0.1.0_macos_arm64.tar.gz`; the underlying Go build target and intermediate artifact
names retain the toolchain's `darwin` identifier.
Starting with v0.2.0, a platform-independent `channelterm_skills_VERSION.zip` contains the tracked
files under `skills/`, with each skill directory at the archive root, plus the project `LICENSE`.
The packaging step checks the archive layout and file bytes against the source files. See the
[Skill installation workflow](../getting-started/mcp-server.md#install-the-bundled-agent-skill)
for the archive layout and client setup.
The workflow writes `SHA256SUMS` for all seven archives, verifies the archive contents, checksums,
and injected version, and then creates or updates the GitHub Release for the same tag. Ordinary
branch pushes and pull requests cannot run this release workflow.

Release publishing uses the GitHub CLI and the workflow's `contents: write` permission. The Release
is kept as a draft while assets are uploaded and checked, and is published only after all eight
expected assets (seven archives and `SHA256SUMS`) are present. A failed build, package, checksum,
upload, or remote asset check therefore cannot publish a new incomplete Release.

## Tagged release verification

On Linux, from a clean final commit, create an annotated tag, run the release verifier, inspect the
artifacts, and only then push the tag:

```bash
git tag -s v0.1.0 -m "ChannelTerm v0.1.0"
./scripts/release.sh 0.1.0
git push origin v0.1.0
```

Use `git tag -a` instead of `git tag -s` only when signing is unavailable. `scripts/release.sh`
requires a SemVer 2.0 version and rejects leading-zero core identifiers, malformed prerelease/build
identifiers, a dirty worktree, or a missing/mismatched/lightweight tag. It runs tests, vet, and the
race detector; rebuilds all six targets with version and provenance metadata; verifies all five
metadata fields in the native Linux binary; and writes `dist/SHA256SUMS`. It does not publish the
tag or artifacts. After this local verification succeeds, pushing the `vVERSION` tag starts the
GitHub Release workflow described above.

## Evidence boundaries

- Unit tests confirm fake-backed package behavior, error semantics, cancellation, buffering, and
  adapters.
- SSH transport and CLI tests start loopback SSH servers with ephemeral keys. They confirm
  authentication, PTY/shell requests, cancellation, cleanup, and shared Session access through the
  existing HTTP tools, including separate CLI processes. These generated-key servers do not
  prove OpenSSH interoperability. The separate opt-in local suite below runs actual OpenSSH
  and Linux PTYs and requires runtime dimension synchronization and full-screen redraw.
- `go vet` performs static analysis; it is not a runtime test.
- SFTP tests exchange real protocol packets with an in-memory filesystem over an ephemeral-key
  loopback SSH server. SCP tests cover framing, binary payloads, normal peer EOF, cancellation,
  and hostile record/path rejection. These are separate from real OpenSSH/server validation.
- A cross-build confirms compilation only.
- Native console raw-mode behavior must be checked on the target OS.
- Serial behavior must be checked with a real device and its known settings.
- Network exposure and client interoperability need an actual MCP client/environment when those
  boundaries change.

Report exact commands and results. Do not claim real hardware, another OS, or external MCP client
verification based only on unit tests or cross-compilation.

## Local OpenSSH integration tests

Ordinary package tests also verify password fallback/retry, explicit first-host trust persistence,
rejected changed/revoked keys, cancellation, and concurrent trust writes using ephemeral credentials.
On Linux, `go test ./internal/cli/command -run '^TestSSHInteractiveAuthentication$'` drives the real CLI
through PTYs against a generated-key loopback server, including hidden password input and termios
restoration. It does not require sshd. On Windows,
`go test ./internal/cli/terminalinput -run '^TestPromptWindowsConsole$'`
launches an isolated hidden console to check Unicode input, hidden
passwords, cancellation, and console-mode restoration without altering the user's console.
The opt-in real OpenSSH suite below remains key-authenticated; password-only remote-server acceptance
and native macOS console interaction still need their own deployment validation.

On Linux/WSL2, `bash scripts/test-ssh-local.sh` runs the real OpenSSH suite with race detection.
It requires a non-root user, OpenSSH server (`sshd`) and `ssh-keygen`, Python 3, Vim, htop, and Go.
Use `CHANNELTERM_TEST_SSHD=/absolute/path/to/sshd` and
`CHANNELTERM_TEST_HTOP=/absolute/path/to/htop` when those binaries are not on PATH. Extracted
binaries can use a caller-supplied `LD_LIBRARY_PATH`; the isolated test shell receives that path.
The script does not install packages, configure the system SSH service, or use personal keys.

The script creates and destroys a dedicated localhost server and invokes:

```bash
CHANNELTERM_TEST_OPENSSH_CONFIG=/path/to/test-config.json \
  go test -race ./internal/cli/command -run '^TestOpenSSH' -count=1 -v
```

The temporary JSON contains `user`, `host` (a loopback IP), `port`, `identity`, `known_hosts`,
and `htop` paths. Only use this opt-in with a dedicated local test server running as the current
user: tests write a temporary Vim file and one test terminates its own SSH connection's server
process. The packet-drop relay requires a dedicated unhashed `[host]:port` host-key entry.
The script creates these inputs, uses a clean Bash shell, disables forwarding and passwords,
and permits only its generated client key. It disables server `StrictModes` for that temporary
credential location; it does not bypass ChannelTerm host-key verification.

Without `CHANNELTERM_TEST_OPENSSH_CONFIG`, ordinary `go test ./...` skips these external-server
tests and continues to run self-contained loopback unit tests. Test fixture keys never enter the
repository. Logs and temporary credentials are removed by the runner when it exits; on failure,
server diagnostics are printed before cleanup.

The real suite checks initial PTY negotiation, Vim and htop interaction, Ctrl+C, local escape,
owner/observer shutdown, terminal restoration, server connection termination, explicit reopening,
and packet dropping after setup. Resize tests require remote `stty size` to follow the owner
from `36 100` to `43 132` and through successive/burst changes. They verify that secondary
attachments do not compete, initial flag overrides persist until a local change, zero dimensions
are ignored, Vim reports updated dimensions, and htop redraws at the new height. The unit suite
also covers invalid sizes, watcher cancellation, concurrent Resize/Close, and a blocked resize
released by Close. See the
[SSH workflow](../getting-started/ssh-terminal.md#current-validation-limits-and-recovery) for the
remaining lifecycle and recovery boundaries. No native Windows/macOS or physical network-failure
claim follows from this localhost suite.

## Local GDB integration tests

On native Linux with GDB, gdbserver, GCC, Bash, and util-linux on PATH, run:

```bash
CHANNELTERM_TEST_GDB=1 go test -race ./internal/mcp -run '^TestGDBLocal' -count=1 -v
```

To run the same managed and external coverage as CI:

```bash
CHANNELTERM_TEST_GDB=1 go test -race ./internal/mcp -run '^TestGDB(Local|ExternalPTY)' -timeout 10m -count=1 -v
```

The suite creates disposable ELF executables and PTYs and uses the real MCP SDK, GDB/MI, and
gdbserver. It needs local Unix sockets and ptrace permission for its own test processes. It does not
change ptrace policy or contact hardware. Without this opt-in, ordinary tests skip these cases.
Extracted binaries may use a caller-supplied PATH and LD_LIBRARY_PATH. GDB/gdbserver are runtime test
tools, not bundled Go dependencies. Coverage includes the addition workflow, thread inspection,
normal exit, safe attach/detach, automatic lease renewal, disconnect cleanup, authorization/retries,
ELF mismatch, unsafe attach rejection, and missing-shell recovery that must fail closed. Ordinary
unit tests cover binary fragmentation, empty reads, invalid UTF-8, buffer loss, final ACK tracking,
lease conflicts/expiry, input tracking, and MI cancellation.

Attach fixtures close inherited non-stdio descriptors and signal readiness before tracing starts.
Tests deliberately pass a pipe and PTY to confirm isolation; separate cases retain them and require
attach rejection. The disconnect fixture handles `SIGINT` and resumes interrupted sleeps, then must
remain alive and lose its tracer within a bounded wait. This tests cleanup with a signal-tolerant
target, not survival of arbitrary applications after an interrupted debugging connection.

See [the workflow](../getting-started/gdb-debugging.md#validation-and-diagnostics) for operational
diagnostics. These tests do not validate physical serial settings or boards. Other desktop builds
still compile, but `debug_start` rejects non-Linux hosts; cross-build success does not validate their
terminal behavior.


## External RSP bridge verification

Ordinary tests include fake serial Sessions, real loopback TLS sockets, and (on Linux) an actual PTY
with a `cat` byte-stream simulator. The latter is named
`TestRSPBridgePTYBinaryAndShellRestoration`; it checks bytes, independent target preparation, and
exact terminal-mode restoration. It does not validate GDB, Windows/WSL networking, or a physical board.
Port tests cover cursor advancement after release, wraparound, active/external port conflicts,
strict exhaustion/dynamic fallback with a reduced test pool, concurrent allocation and reuse,
and reservation through authentication/disconnect. Exact cursor and reduced-pool concurrency tests
use a per-allocator bind fake so other processes cannot take ports between a probe and the asserted
allocation. Separate real TCP tests retain OS bind exclusion, exhaustion/fallback, concurrent
allocation/release, and authentication/disconnect coverage. Control tests exercise
missing/silent/wrong-response clients, separate-stream authentication, no heartbeat bytes in RSP,
lease expiry, the lifetime cap, and final-byte draining. Silent TCP peers simulate application
stalls/half-open behavior; they are not an OS packet-loss or Windows firewall test.
The relevant concurrency checks are:

```bash
go test -race ./internal/core/debugger ./internal/core/app ./internal/mcp ./internal/cli/command -run 'TestRSP|TestBridge|TestExternalBridge|TestDebug|TestMI|TestRelay|TestFinalExitACK' -count=1
```

The optional wall-clock idle test runs for 310 seconds using real TLS/control sockets and a fake
serial Session (no GDB or hardware). It sends no RSP data and calls no management renewal:

```bash
CHANNELTERM_TEST_RSP_LONG=1 go test -race ./internal/core/app -run '^TestRSPBridgeIdleFiveMinutes$' -timeout 7m -count=1 -v
```

Tests need local socket permission; a sandbox refusal to bind is an environment failure. Linux PTY
coverage uses `sh`, `stty`, and `cat` and reports a skip if these are unavailable. The original opt-in
managed GDB suite remains separate. The external real-GDB test prepares gdbserver through existing
terminal APIs, then independently launches GDB and the `rsp-client` pipe helper:

```bash
CHANNELTERM_TEST_GDB=1 go test -race ./internal/mcp -run '^TestGDBExternalPTY$' -count=1 -v
```

This test requires GDB/gdbserver/GCC and local ptrace permission, builds a temporary helper, and
checks breakpoints, step/next/finish, a variable value, registers, backtrace, detach, and exact TTY restoration.
It also reports actual external commands/results through MCP and reads them from Session events.
Ordinary tests cover report authorization, retry ordering, rejected secrets, cancellation and
heartbeat independence, event-history overwrite, multiple attach observers, retained replay, and
control-escaped previews. Report failures are followed by successful byte relay in both directions.
It uses native Linux executables and a PTY, not an ARM board. GDB is launched by the test driver,
not by the Application bridge. Extracted test tools can use private PATH/LD_LIBRARY_PATH settings.
The expanded external suite adds real GDB/helper process termination and MCP closure. Enable the
310-second paused-breakpoint test separately so ordinary CI stays fast:

```bash
CHANNELTERM_TEST_GDB=1 CHANNELTERM_TEST_RSP_LONG=1 go test -race ./internal/mcp -run '^TestGDBExternalPTY' -timeout 10m -count=1 -v
```

During the long test no MCP renewal or RSP output occurs; the test checks both leases, then resumes
step/inspection/detach and verifies exact TTY recovery. Crash tests retain write exclusion and close
the Session without claiming board-side cleanup. They require Linux pidfds for signalling only the
disposable helper process. Their fixture builds a native executable and a private connection file.

On a Windows machine, run `go test ./internal/core/debugger -run '^TestBridge' -count=1 -v` to check
native loopback TLS, port reservation, and keepalive semantics. An equivalent cross-compiled test
executable may be invoked through Windows interop from WSL; record the OS actually executing it.
This is endpoint validation, not COM/board validation. Packet blackholes, Windows firewall policy,
and real serial hardware still require the separate acceptance workflow.

Cross-builds cover all six desktop targets with CGO disabled;
they are not native Windows validation. Follow the
[external workflow's evidence checklist](../getting-started/gdb-debugging.md#evidence-boundaries-and-diagnostics)
for Windows/WSL2/Linux-board acceptance and record each tested board and architecture separately.
