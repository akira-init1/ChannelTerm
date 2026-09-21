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
1.25 release line declared by `go.mod`, tests natively on Linux, Windows, and macOS with the current
stable Go release, and runs formatting, `go vet`, the race detector, all six cross-builds, and
`govulncheck`. The Windows job also validates `scripts/build.ps1` and its version injection. A
release candidate should not be tagged until every job passes.

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
tag or artifacts.

## Evidence boundaries

- Unit tests confirm fake-backed package behavior, error semantics, cancellation, buffering, and
  adapters.
- `go vet` performs static analysis; it is not a runtime test.
- A cross-build confirms compilation only.
- Native console raw-mode behavior must be checked on the target OS.
- Serial behavior must be checked with a real device and its known settings.
- Network exposure and client interoperability need an actual MCP client/environment when those
  boundaries change.

Report exact commands and results. Do not claim real hardware, another OS, or external MCP client
verification based only on unit tests or cross-compilation.
