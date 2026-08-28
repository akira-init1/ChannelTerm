# Build from Source

ChannelTerm requires the Go version declared in `go.mod` (currently Go 1.25.0). Run commands from the repository root.

```powershell
go version
go run ./cmd/channelterm --help
go run ./cmd/channelterm version
```

The version command currently prints:

```text
channelterm 0.1.0
```

Build a native executable with:

```powershell
go build ./cmd/channelterm
```

Before relying on a local build, run:

```powershell
go test ./...
go vet ./...
```

The repository also includes scripts that rebuild `dist/` for the six supported desktop targets with `CGO_ENABLED=0`:

- PowerShell: `./scripts/build.ps1`
- Bash on Linux: `./scripts/build.sh`

The targets are Windows, Linux, and macOS on `amd64` and `arm64`. Cross-compilation proves that the code builds for a target; it does not prove native console behavior or communication with a physical serial device.

See [Building and testing](../development/building-and-testing.md) for the full development checks.
