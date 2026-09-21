# Build from Source

ChannelTerm requires Go 1.25 or newer. Use a currently supported patched Go toolchain for production
and release builds. Run commands from the repository root.

```powershell
go version
go run ./cmd/channelterm --help
go run ./cmd/channelterm version
```

An ordinary source build prints:

```text
channelterm devel
```

Tagged release artifacts receive their version through the documented release script. Build a
native development executable with:

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

Set `CHANNELTERM_VERSION` only when intentionally producing versioned artifacts. Maintainers use
`./scripts/release.sh VERSION` from a clean annotated `vVERSION` tag; it runs the release checks and
creates `dist/SHA256SUMS` without publishing anything.

The targets are Windows, Linux, and macOS on `amd64` and `arm64`. Cross-compilation proves that the code builds for a target; it does not prove native console behavior or communication with a physical serial device.

See [Building and testing](../development/building-and-testing.md) for the full development checks.
