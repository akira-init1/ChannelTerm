# Code Style

Repository-wide rules are defined in `AGENTS.md`; this page summarizes the coding conventions most relevant to implementation work.

## Go style

- Use idiomatic Go and run `gofmt` on changed Go files.
- Use lowercase package and directory names, `PascalCase` for exported identifiers, and `camelCase` for unexported identifiers.
- Name tests `Test<Behavior>` and keep `*_test.go` beside the package under test.
- Keep interfaces small and close to their consumers.
- Preserve useful error chains with `%w`; add sentinel errors only when callers need semantic comparison.
- Avoid global mutable state and unnecessary dependencies.
- Keep OS-specific behavior in small files with appropriate build constraints.

## Documentation comments

Every exported package, type, interface, function, method, constant, and variable requires an English Go doc comment. Declaration comments normally begin with the declaration name; package comments use `// Package <name> ...`.

Comments should explain responsibilities, invariants, ownership, concurrency, cancellation, side effects, protocol limits, security boundaries, and important errors that signatures cannot express. Do not translate the next line into prose or use Doxygen tags.

Use `// TODO:`, `// FIXME:`, and `// Deprecated:` consistently. A deprecation comment should name the preferred replacement when one exists.

## Architecture placement

- CLI parsing and presentation belong in `internal/cli`.
- MCP names and schemas belong in `internal/mcp`.
- adapter-neutral orchestration belongs in `internal/core/app`.
- Session lifecycle and retained data belong in `internal/core/session`.
- live protocol behavior belongs in a Transport implementation.
- discovery, connection decisions, configuration, and Tool contracts remain in their dedicated Core packages.

Core must not import CLI or MCP. Presentation must not alter stored Session output. Transport must not own terminal history.

## Change quality

Keep one independently verifiable goal together with its tests and required documentation. Do not mix unrelated cleanup, dependency upgrades, renames, or refactors. Update CLI, MCP, configuration, identifier, architecture, or module documentation whenever the corresponding public contract or ownership changes.

Before completion, inspect `git diff`, run `git diff --check`, run the applicable tests and vet, and report unverified real-hardware or external-service boundaries.
