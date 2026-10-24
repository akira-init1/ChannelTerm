# Serial dependency provenance and local patch

This directory contains `go.bug.st/serial` **v1.6.2**, copied from the Go module cache
verified by `go.sum` (`h1:kn9LRX3sdm+WxWKufMlIRndwGfPWsH1/9lCWXQCasq8=`).
Upstream: <https://github.com/bugst/go-serial/tree/v1.6.2>.
The upstream `.github` automation is omitted. Source, examples, tests, module files,
README and BSD-3-Clause `LICENSE` are retained. Do not run the example programs against
real devices as automated tests. Local modifications follow ChannelTerm's Apache-2.0
license; the upstream license and binary notice remain required.

The root module replaces only this dependency. Local changes are:

- `Mode.FlowControl` with native none, bidirectional XON/XOFF, or RTS/CTS.
  Invalid modes fail before opening a port. Zero preserves the upstream default.
- Windows DCB flow bits and Unix termios flags are applied at open/SetMode; no
  reflection, second OS handle, or alternative Serial/Channel/Session implementation.
- Unix nonblocking writes share the existing close-signal pipe. Closing waits for
  active reads/writes before releasing the fd, including CTS/XOFF-stopped writes.
  No flow restart or replay is used to make shutdown succeed.
- Hardware-free flag, transition, validation and Linux PTY lifecycle tests.
- Two stale upstream example names are corrected for modern Go vet; the portlist
  package comment is normalized by gofmt. Other upstream implementations are retained.

Run `go test go.bug.st/serial -run TestFlowControl -count=1` from the repository root.
On Linux add `-race`; the PTY test requires `/dev/ptmx`. This does not prove real
adapter wiring or driver handshaking. Upstream's separate socat tests are not selected
by that command. CI runs the patch tests explicitly because a nested module is not
included by the root `./...` pattern.

For updates, obtain the selected upstream module through Go checksum verification,
compare these modifications against it, and reapply only the documented patch. Run
repository tests, vet, race, patch tests and all six CGO-disabled cross-builds. Preserve
the upstream license and `THIRD_PARTY_NOTICES`; do not silently replace patched sources
with an unreviewed version.

Native settings follow [Windows DCB](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-dcb)
and [Unix termios](https://man7.org/linux/man-pages/man3/termios.3.html).
