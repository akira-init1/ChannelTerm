# Code Style

Repository-wide rules are defined in [AGENTS.md](../../AGENTS.md); this page summarizes the coding conventions most
relevant to implementation work.

## Go style

- Use idiomatic Go and run `gofmt` on changed Go files.
- Use lowercase package and directory names, `PascalCase` for exported identifiers, and `camelCase`
  for unexported identifiers.
- Name tests `Test<Behavior>` and keep `*_test.go` beside the package under test.
- Keep interfaces small and close to their consumers.
- Preserve useful error chains with `%w`; add sentinel errors only when callers need semantic
  comparison.
- Avoid global mutable state and unnecessary dependencies.
- Keep OS-specific behavior in small files with appropriate build constraints.

## TypeScript style

For `gui/electron`, use strict TypeScript and the project's Prettier commands. Keep credentials,
MCP calls, runtime validation, and privileged APIs in Main; Preload exposes individual typed
methods, while React and xterm remain presentation code. Review cancellation, stale attachments,
bounded queues, and safe IPC errors when changing those boundaries. See
[GUI architecture](../architecture/electron-gui.md) for the ownership and security contract.

## Documentation comments

Every exported package, type, interface, function, method, constant, and variable requires an
English Go doc comment. Declaration comments normally begin with the declaration name; package
comments use `// Package <name> ...`.

Comments should explain responsibilities, invariants, ownership, concurrency, cancellation, side
effects, protocol limits, security boundaries, and important errors that signatures cannot express.
Do not translate the next line into prose or use Doxygen tags.

Use `// TODO:`, `// FIXME:`, and `// Deprecated:` consistently. A deprecation comment should name
the preferred replacement when one exists.

## Architecture placement

Use the [repository map](../architecture/repository-map.md) to select the owning package and
[architecture rules](../../AGENTS.md#2-ownership-and-architecture) for dependency and lifecycle
boundaries. Core behavior stays independent of CLI/MCP presentation.

## Change quality

Follow the [change workflow](../../AGENTS.md#3-change-workflow-and-compatibility),
[documentation impact review](../../AGENTS.md#4-documentation), and
[completion gate](../../AGENTS.md#10-completion-gate-and-report). Exact test and build commands
belong in [Building and testing](building-and-testing.md).
