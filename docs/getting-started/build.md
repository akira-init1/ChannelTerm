# Build from Source

ChannelTerm requires Go 1.25 or newer. Use a currently supported patched Go toolchain for production
and release builds. Run commands from the repository root.

```powershell
go version
go run ./cmd/channelterm --help
go run ./cmd/channelterm version
```

An ordinary direct source build prints output in this form (Go version and platform depend on the
local environment):

```text
channelterm devel
commit:   unknown
built:    unknown
go:       GO_VERSION
platform: linux/amd64
```

Tagged release artifacts receive their version through the documented release script. Build a native
development executable with:

```powershell
go build ./cmd/channelterm
```

Before relying on a local build, run:

```powershell
go test ./...
go vet ./...
```

The repository also includes scripts that rebuild `dist/` for the six supported desktop targets with
`CGO_ENABLED=0`:

- PowerShell: `./scripts/build.ps1`
- Bash on Linux: `./scripts/build.sh`

Both scripts inject the current 12-character Git commit and one UTC RFC 3339 build timestamp into
every artifact. A commit is suffixed with `-dirty` when tracked or untracked worktree changes exist.

Set `CHANNELTERM_VERSION` only when intentionally producing versioned artifacts. Maintainers use
`./scripts/release.sh VERSION` from a clean annotated `vVERSION` tag; it runs the release checks and
creates `dist/SHA256SUMS` without publishing anything. Pushing that tag starts the GitHub Release
workflow, which rebuilds and packages the same six targets, verifies the archives and checksums, and
publishes them only after every expected asset is present.

The targets are Windows, Linux, and macOS on `amd64` and `arm64`. Cross-compilation proves that the
code builds for a target; it does not prove native console behavior or communication with a physical
serial device. Go and the intermediate build artifacts identify macOS as `darwin`, while published
release archive filenames use the more recognizable `macos` label.

See [Building and testing](../development/building-and-testing.md) for the full development checks.
