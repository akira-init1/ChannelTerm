# Development Setup

Install Go 1.26 or newer, using a currently supported patched toolchain for ordinary development,
and clone the repository on Windows, Linux, or macOS. The canonical Go module path is
`github.com/akira-init1/ChannelTerm`. The supported desktop architectures are `amd64` and `arm64`.

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

The project uses Go modules. Its direct dependencies provide MCP, TOML, serial access,
operating-system support, terminal raw mode, and SSH. Serial unit tests use fakes; SSH tests use
loopback test servers and generated ephemeral keys. They require no physical hardware, installed
sshd, personal credentials, or external services. SSH uses the BSD-licensed `golang.org/x/crypto`
module. The minimum Go version is 1.26 so the SSH dependency includes current security fixes;
its module requirements also select newer `x/sys` and `x/term`. Update older Go installations
before building this release; the six supported desktop targets and CGO-free builds are unchanged.
The required redistribution notice is in [THIRD_PARTY_NOTICES](../../THIRD_PARTY_NOTICES).

For ordinary development, inspect the worktree before editing. If the current branch is `main` or
`master` and the task requires repository changes, create and switch to a focused task branch unless
the maintainer explicitly requests work directly on the default branch:

```powershell
git status
git branch --show-current
git switch -c feature/short-name
```

Continue on an existing branch that already matches the task. Keep the same feature's
implementation, tests, documentation, and follow-up fixes together instead of creating another
branch for a small addition to the same goal. Default-branch changes go through a pull request;
normal development must not push directly to `main` or `master`.

Do not discard unrelated worktree changes. Keep implementation under the existing `internal`
ownership boundary unless a public Go API is intentionally designed and reviewed.

Real serial validation additionally requires a known device, endpoint, and confirmed
baud/data/parity/stop/flow settings. Do not infer them from examples. MCP HTTP testing does not
require a device for server startup or tool discovery, but tools that open a serial Session do.
