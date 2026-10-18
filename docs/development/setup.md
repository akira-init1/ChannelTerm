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

Before editing, inspect the worktree and follow the [branch and staging rules](../../AGENTS.md#8-git-and-commit-messages):

```powershell
git status
git branch --show-current
```

On Windows, build runnable debug binaries to a fixed path, for example
`go build -o ./bin/channelterm.exe ./cmd/channelterm`, and launch that binary for network workflows.
Repeated `go run` executions may use different temporary executable paths and cause repeated
Windows Firewall prompts. For native package tests, use the
[stable-path test runner](building-and-testing.md#repeated-native-testing-on-windows).
Keep loopback-only defaults; changing firewall policy or exposing the Host to a LAN is not required
for ordinary local development.

Real serial validation additionally requires a known device, endpoint, and confirmed
baud/data/parity/stop/flow settings. Do not infer them from examples. MCP HTTP testing does not
require a device for server startup or tool discovery, but tools that open a serial Session do.
