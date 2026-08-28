# Development Setup

Install the Go version declared by `go.mod` (currently Go 1.25.0) and clone the repository on Windows, Linux, or macOS. The canonical Go module path is `github.com/akira-init1/ChannelTerm`. The supported desktop architectures are `amd64` and `arm64`.

```powershell
git clone https://github.com/akira-init1/ChannelTerm.git
Set-Location ChannelTerm
```

From the repository root, confirm the toolchain and module graph:

```powershell
go version
go mod download
go run ./cmd/channelterm --help
```

The project uses Go modules. Its direct dependencies provide MCP, TOML, serial access, operating-system support, and terminal raw mode. Serial unit tests use fakes and do not require physical hardware or external services.

For ordinary development, inspect the worktree before editing. If the current branch is `main` or `master` and the task requires repository changes, create and switch to a focused task branch unless the maintainer explicitly requests work directly on the default branch:

```powershell
git status
git branch --show-current
git switch -c feature/short-name
```

Continue on an existing branch that already matches the task. Keep the same feature's implementation, tests, documentation, and follow-up fixes together instead of creating another branch for a small addition to the same goal. Default-branch changes go through a pull request; normal development must not push directly to `main` or `master`.

Do not discard unrelated worktree changes. Keep implementation under the existing `internal` ownership boundary unless a public Go API is intentionally designed and reviewed.

Real serial validation additionally requires a known device, endpoint, and confirmed baud/data/parity/stop/flow settings. Do not infer them from examples. MCP HTTP testing does not require a device for server startup or tool discovery, but tools that open a serial Session do.
