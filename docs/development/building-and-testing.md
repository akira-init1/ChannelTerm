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

`.github/workflows/release.yml` runs only when a tag matching `v*` is pushed. It validates that the
tag is `v` followed by a SemVer version, strips the `v` for the embedded CLI version, tests and
cross-builds with `CGO_ENABLED=0`, and packages all six supported targets. Windows archives are ZIP
files containing `channelterm.exe`; Linux and macOS archives are tarballs containing `channelterm`.
All archives also include `THIRD_PARTY_NOTICES` for the SSH dependency.
Public macOS archives use `macos` in their filenames, such as
`channelterm_0.1.0_macos_arm64.tar.gz`; the underlying Go build target and intermediate artifact
names retain the toolchain's `darwin` identifier.
The workflow writes `SHA256SUMS` for those six archives, verifies the archive contents, checksums,
and injected version, and then creates or updates the GitHub Release for the same tag. Ordinary
branch pushes and pull requests cannot run this release workflow.

Release publishing uses the GitHub CLI and the workflow's `contents: write` permission. The Release
is kept as a draft while assets are uploaded and checked, and is published only after all seven
expected assets are present. A failed build, package, checksum, upload, or remote asset check
therefore cannot publish a new incomplete Release.

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
  and Linux PTYs and reports unsupported runtime resizing explicitly.
- `go vet` performs static analysis; it is not a runtime test.
- A cross-build confirms compilation only.
- Native console raw-mode behavior must be checked on the target OS.
- Serial behavior must be checked with a real device and its known settings.
- Network exposure and client interoperability need an actual MCP client/environment when those
  boundaries change.

Report exact commands and results. Do not claim real hardware, another OS, or external MCP client
verification based only on unit tests or cross-compilation.

## Local OpenSSH integration tests

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
and packet dropping after setup. The resize characterization test passes only when it observes
the documented current limitation; it is **not** evidence of runtime resize support. See the
[SSH workflow](../getting-started/ssh-terminal.md#current-validation-limits-and-recovery) for the
remaining lifecycle and recovery boundaries. No native Windows/macOS or physical network-failure
claim follows from this localhost suite.
