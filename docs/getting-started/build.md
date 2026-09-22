# Build and Install from Source

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

Install that executable for the current user without administrator privileges:

```powershell
./channelterm install
```

The command copies the running executable to the platform's per-user program location, creates both
the `channelterm` and `cterm` commands, adds the command directory to the current-user PATH only when
needed, and creates the minimal default `config.toml` only when it is absent. Open a new terminal if
the command reports that PATH changed.

On Linux and macOS the short command is a symbolic link to the single installed binary. On Windows
the two `.exe` files are synchronized copies because creating symbolic links is not reliably
available to an unelevated process. Rerunning a newer downloaded executable with `install` updates
an installer-owned installation. An older semantic version is rejected unless
`--allow-downgrade` is explicit. Existing command files without an installation manifest are never
overwritten; `--adopt` accepts them only when their checksum matches the running binary.

Ordinary uninstall removes installed commands, an installer-added PATH entry, and installation
state while preserving user data:

```powershell
channelterm uninstall
```

Destructive removal requires an explicit confirmation, or `--yes` for intentional automation:

```powershell
channelterm uninstall --purge
channelterm uninstall --purge --yes
```

`--purge` removes the known default `config.toml`, `state.json`, and `http-auth-token` files but
does not edit third-party MCP client configurations or delete unknown files from the configuration
directory. On Windows a temporary helper finishes deleting the running installed executable after
the uninstall command exits.

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
