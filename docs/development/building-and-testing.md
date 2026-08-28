# Building and Testing

## Required checks

For a normal Go change, format every changed Go file, then run the repository checks from the root:

```powershell
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

Documentation-only changes do not require formatting unchanged Go files but still require repository tests and vet before completion.

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

The PowerShell script reports size, timestamp, and SHA-256 for each artifact. The Bash script additionally requires `stat`, `sha256sum`, and `awk`. Do not keep unrelated files only in `dist/` when running either script.

## Evidence boundaries

- Unit tests confirm fake-backed package behavior, error semantics, cancellation, buffering, and adapters.
- `go vet` performs static analysis; it is not a runtime test.
- A cross-build confirms compilation only.
- Native console raw-mode behavior must be checked on the target OS.
- Serial behavior must be checked with a real device and its known settings.
- Network exposure and client interoperability need an actual MCP client/environment when those boundaries change.

Report exact commands and results. Do not claim real hardware, another OS, or external MCP client verification based only on unit tests or cross-compilation.
