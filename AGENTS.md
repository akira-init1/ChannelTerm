# AGENTS.md

This file defines repository-wide rules for AI coding agents and agent-assisted contributions to ChannelTerm. The same engineering standards apply whether a change is written by a human or an AI agent.

The goal is not to maximize the amount of code changed. The goal is to keep ChannelTerm easy to review, test, document, merge, and maintain as the project grows.

## 1. Project Scope and Source of Truth

ChannelTerm is shared terminal-session infrastructure for human and AI access to terminal-like transports.

The current implementation is serial-first and is primarily focused on hardware, embedded-system, and terminal debugging. The Core is transport-neutral: serial is the only current concrete Transport, while other transports such as SSH or Telnet are future directions rather than implemented features.

When repository information disagrees, use this order of authority:

1. Current source code and tests.
2. Actual CLI `--help` output, protocol schemas, and runtime behavior.
3. Current public documentation under `docs/`.
4. README examples and summaries.
5. Issues, pull requests, comments, old commits, and planning material.

Do not infer implemented behavior from plans, TODOs, old documentation, issue discussions, or future-looking comments.

Never describe a planned capability as implemented.

## 2. Stable Ownership Boundaries

This file records stable ownership boundaries, not a duplicate source tree. The sole canonical detailed repository and package map is [`docs/architecture/repository-map.md`](docs/architecture/repository-map.md).

| Area | Stable responsibility |
| ---- | --------------------- |
| `cmd/channelterm/` | Process entry point and dependency composition |
| `internal/cli/` | CLI commands, local presentation, interactive input, and OS terminal adaptation |
| `internal/init/mcp/` | Supported MCP-client discovery plus client-configuration rendering and safe installation |
| `internal/core/app/` | Adapter-neutral application use cases |
| `internal/core/session/` | Session lifecycle, shared I/O, buffering, activity, and Session management |
| `internal/core/config/` | Configuration models, loading, saving, and overrides |
| `internal/core/device/` | Device discovery and state registry abstractions |
| `internal/core/connectionpolicy/` | Connection and Session-reuse policy decisions |
| `internal/core/tool/` | Protocol-neutral tool contracts and registry |
| `internal/core/transport/` | Transport contract and concrete Transport implementations |
| `internal/mcp/` | MCP hosting, schemas, and translation to application use cases |
| `scripts/` | Build and repository automation |
| `docs/` | Public, durable technical documentation |

Tests stay next to the package they test and use `*_test.go` names.

### Repository-map maintenance

Update `docs/architecture/repository-map.md` in the same change when any of the following happens:

- a top-level directory is added, removed, or renamed;
- a package under `internal/` is added, removed, renamed, or moved;
- package ownership/responsibility changes materially;
- dependency direction changes;
- a new adapter family or Transport implementation is introduced;
- a formerly internal implementation becomes a stable public boundary.

Do **not** update the repository map merely because a helper file was added inside an existing package and that package's responsibility did not change.

Do not copy the detailed tree into `AGENTS.md`, README, or another document. Summaries may identify stable boundaries and must link to the canonical repository map.

Do not create empty placeholder directories or documentation files only to match a future tree.

## 3. Where New Functionality Belongs

Place code according to architectural responsibility, not according to the feature name.

| Change                                    | Primary location                  | Must not own                                         |
| ----------------------------------------- | --------------------------------- | ---------------------------------------------------- |
| Process startup / dependency assembly     | `cmd/channelterm/`                | business logic, protocol logic                       |
| CLI command/flag parsing and local output | `internal/cli/command/`           | Session lifecycle implementation, Transport behavior |
| MCP client configuration discovery/install | `internal/init/mcp/`              | Session lifecycle, MCP host/tool implementation      |
| CLI-only highlighting/rendering           | `internal/cli/highlight/`         | stored terminal data, MCP output                     |
| Interactive local escape/input state      | `internal/cli/interactive/`       | Session ownership, OS raw-mode implementation        |
| OS terminal/raw console behavior          | `internal/cli/terminalinput/`     | remote terminal protocol semantics                   |
| Cross-adapter use-case orchestration      | `internal/core/app/`              | CLI formatting, MCP schemas                          |
| Config parsing/persistence/overrides      | `internal/core/config/`           | terminal lifecycle, UI decisions                     |
| Connection/reuse policy                   | `internal/core/connectionpolicy/` | physical Transport operations                        |
| Device discovery/state models             | `internal/core/device/`           | presentation formatting                              |
| Session lifecycle/buffering/activity      | `internal/core/session/`          | CLI/MCP behavior, protocol-specific configuration    |
| Protocol-neutral tool abstraction         | `internal/core/tool/`             | MCP-specific schemas                                 |
| Transport interface                       | `internal/core/transport/`        | terminal history, UI logic                           |
| Serial implementation/discovery           | `internal/core/transport/serial/` | Session history, CLI presentation                    |
| MCP server/transport hosting              | `internal/mcp/`                   | core business rules                                  |
| MCP terminal tool schema/translation      | `internal/mcp/terminal/`          | independent Session/Transport implementation         |
| Build/release helper scripts              | `scripts/`                        | product behavior                                     |

For a cross-cutting feature, split the implementation across the existing layers instead of creating a catch-all package such as `internal/features/`, `internal/common/`, or `internal/utils/`.

Example:

```text
new serial option
    |-- config semantics       -> internal/core/config
    |-- serial behavior        -> internal/core/transport/serial or core/app
    |-- CLI flag               -> internal/cli/command
    |-- MCP schema (if exposed)-> internal/mcp/terminal
    |-- tests                  -> next to each affected package
    `-- docs                   -> relevant reference/getting-started/module docs
```

If no existing package owns the responsibility cleanly, first decide whether a stable new boundary actually exists. Do not create a new package solely to reduce file size or satisfy a preferred pattern.

## 4. Architecture and Dependency Rules

Maintain one-way dependency flow:

```text
CLI / MCP adapters
        v
internal/core/app
        v
core services (Session, device, policy, tool, config)
        v
Transport
```

Mandatory rules:

- `internal/core/**` must not depend on `internal/cli/**` or `internal/mcp/**`.
- Core must not depend on a GUI/TUI, LLM SDK, cloud service, product edition, or presentation framework.
- CLI and MCP adapters must reuse `internal/core/app` / Core behavior rather than implementing competing lifecycle logic.
- Protocol-specific behavior belongs in its Transport implementation or the use case that configures it; it must not leak into the protocol-neutral Session contract.
- A Transport owns the live bidirectional byte stream and protocol resources. It must not own terminal history.
- A Session owns receive buffering, activity retention, write serialization, and Session lifecycle semantics.
- The shared access model is `Physical endpoint -> Transport -> Session -> Client/Attachment`. Use `Transport`, `Session`, and `Client` or `Attachment` consistently; do not invent another Session-consumer concept.
- Multiple Clients may share one Session. Each reader can maintain an independent output cursor and activity cursor.
- Writes pass through Session. The current guarantee is serialization at the Session boundary so complete write payloads do not interleave at the byte level.
- Write serialization is not semantic multi-writer coordination. It provides no writer ownership, exclusive lease, transaction, priority, arbitration, or shell-state coordination. Stronger multi-writer coordination is a future direction, not current behavior.
- Session output must remain raw. Presentation transformations such as highlighting belong outside Session.
- Session IDs are opaque identifiers. Short references such as `SER-1` are convenience references and are not interchangeable with opaque Session IDs.
- Target references such as `SER-COM8` identify targets, not live Session IDs.
- Interfaces should be small and close to their consumers.
- Keep implementation under `internal/` unless a stable external Go API is intentionally designed and reviewed.
- Avoid global mutable state when dependency injection or an owned long-lived object is appropriate.
- Do not introduce speculative plugin systems, SDK layers, extension points, registries, or abstractions for features that do not yet exist.

Any change that alters package ownership, dependency direction, lifecycle ownership, Session/Transport responsibility, or a major data path is an architecture change and must update the corresponding architecture documentation in the same contribution.

## 5. Public Compatibility Contracts

Treat these as user-visible compatibility contracts:

- CLI commands and subcommands;
- flags, defaults, validation, help text, and exit behavior;
- interactive escape behavior such as `Ctrl+C` and local escape commands;
- MCP tool names, descriptions, argument schemas, result schemas, and lifecycle semantics;
- configuration file fields, defaults, precedence, and persistence behavior;
- Session lifecycle and identifier/reference semantics;
- listener addresses, network exposure defaults, and security-sensitive behavior;
- output formats intended for scripts, including JSON fields;
- documented environment variables and config locations.

Do not silently rename, remove, reinterpret, or weaken these contracts.

A deliberate breaking change requires explicit maintainer approval, focused tests, documentation, and migration/deprecation guidance where practical.

CLI options are defined by the program. Actual `channelterm --help` and subcommand help are authoritative. Do not maintain a second manually copied full option list that can drift from the program.

## 6. Change-Unit and Scope Rules

One independently verifiable feature, bug fix, or refactor should normally be one change unit.

Keep strongly related work together:

- feature implementation;
- tests for that feature;
- CLI help changes;
- MCP schema changes;
- configuration changes;
- required documentation.

Do not split these only because they touch several files.

Do split work when there are multiple independent goals that can be reviewed and accepted separately.

Do not mix unrelated cleanup, formatting, renames, dependency upgrades, or refactors into a feature/fix contribution.

Before modifying files:

```bash
git status
git branch --show-current
```

Preserve unrelated existing changes. Never discard, overwrite, reset, or clean them to create a convenient worktree.

## 7. Feature Workflow

For a new feature or user-visible behavior change:

1. Define the smallest independently testable behavior and acceptance criteria.
2. Identify the owning architectural layer using Sections 2-4.
3. Read relevant implementation, tests, CLI help/schema, and public docs before changing code.
4. Identify all affected public compatibility contracts.
5. Reuse existing Core abstractions before adding new abstractions.
6. Implement only the required behavior.
7. Add/update automated tests.
8. Update all documentation required by Section 11 in the **same change**.
9. Verify the success path and at least one important error path.
10. Run formatting, tests, static checks, and relevant build checks.
11. Report evidence and any real-hardware/external-service validation still missing.

A feature is not complete when the code works but its tests, CLI/MCP schema, docs, examples, or repository map are stale.

## 8. Bug-Fix Workflow

For a bug fix:

1. Establish expected behavior from source/tests/contracts.
2. Reproduce the failure when practical.
3. Add a regression test that demonstrates the bug when the failure can be tested without real hardware.
4. Fix the bug at the lowest correct architectural layer.
5. Keep the fix minimal; do not hide it inside a broad refactor.
6. Check adjacent lifecycle, cancellation, concurrency, and error paths when they share the same root cause.
7. Update docs only if:
   - documented behavior was wrong;
   - user-visible behavior changes;
   - a durable limitation/constraint must be documented;
   - the bug reveals an incorrect architecture/module description.
8. Run targeted regression tests and the relevant repository-wide checks.

Do not create permanent public documentation that is merely a debugging diary. Investigation history belongs in the issue/pull request. Public docs record durable current behavior.

## 9. Refactor Workflow

A refactor must have a clear technical objective and preserve observable behavior unless a behavior change is explicitly part of the task.

- Keep independent refactors separate from features/fixes.
- Existing tests must continue to pass.
- Add tests when the refactor exposes previously unprotected behavior.
- Update `docs/architecture/` when dependency direction or ownership changes.
- Update `docs/modules/` when module responsibility, lifecycle, concurrency, or invariants change.
- Do not introduce an abstraction solely because a file is long or an agent prefers another design pattern.
- Do not rename public contracts as incidental cleanup.

## 10. Documentation Architecture

Canonical technical documentation is written in English. Explicitly requested README translations may use another language but remain non-canonical. Code identifiers, commands, paths, protocol names, and literal examples remain in their native technical form.

`README.md` is the canonical English public landing page and quick start, not the full manual. Detailed technical facts remain owned by the existing canonical documents under `docs/`; README must not become a second reference.

`docs/` stores durable, current technical knowledge. `docs/README.md` is navigation-only: it indexes every public document but does not become a second owner of technical facts.

Do not create empty placeholders. Add a document only when current implementation provides enough durable content and no existing canonical owner fits.

### Documentation responsibilities

- `docs/architecture/`: system boundaries, dependency direction, package ownership, composition, important data flow.
- `docs/modules/`: one module's contracts, lifecycle, ownership, concurrency, invariants, errors, and implementation constraints that maintainers need.
- `docs/getting-started/`: task-oriented workflows a user follows to achieve something.
- `docs/reference/`: precise user-visible factual contracts such as CLI, MCP, config, identifiers, defaults, and formats.
- `docs/development/`: build/test/platform/contribution procedures.
- README: product identity, installation/quick start, common examples, and links into docs.

### Canonical documentation owners

| Durable fact or workflow | Canonical owner |
| ------------------------ | --------------- |
| CLI public behavior | `docs/reference/cli.md` |
| MCP public tools and schemas | `docs/reference/mcp-tools.md` |
| Configuration | `docs/reference/configuration.md` |
| Identifiers and references | `docs/reference/identifiers.md` |
| Dependency direction and architectural layers | `docs/architecture/overview.md` |
| Detailed source/package tree and ownership | `docs/architecture/repository-map.md` |
| Runtime and data flow | `docs/architecture/data-flow.md` |
| Application use cases | `docs/modules/application.md` |
| Session behavior | `docs/modules/session.md` |
| Transport contract | `docs/modules/transport.md` |
| Buffer semantics | `docs/modules/buffer.md` |
| Device registry | `docs/modules/device-registry.md` |
| Connection policy | `docs/modules/connection-policy.md` |
| Tool registry | `docs/modules/tool-registry.md` |
| Shared Session workflow | `docs/getting-started/shared-session.md` |
| Serial terminal workflow | `docs/getting-started/serial-terminal.md` |
| MCP workflow | `docs/getting-started/mcp-server.md` |
| Build and test procedures | `docs/development/building-and-testing.md` |

Each durable fact has one canonical owner. Other documents may summarize it briefly, then link to that owner. Do not create a parallel package or document merely because a conceptual responsibility is mentioned in several places; update the existing owner.

Whenever a public document is added, removed, renamed, or materially changes responsibility, update `docs/README.md` in the same change. The index must include every public document and contain no broken links.

Do not add a public `docs/records/`, daily log, AI diary, or internal planning area merely to preserve development history.

## 11. Mandatory Documentation Maintenance Matrix

Every code change must perform a documentation-impact review. Updating docs is mandatory when the relevant row applies.

| Change | Documentation that must be reviewed/updated |
| ------ | ------------------------------------------- |
| Package placement, responsibility, or repository structure | `docs/architecture/repository-map.md`; review the stable ownership facts in `AGENTS.md` |
| Architecture, dependency direction, ownership, or major data flow | `docs/architecture/overview.md` and/or `docs/architecture/data-flow.md` |
| `internal/core/app` use-case contract | `docs/modules/application.md` and affected workflow/reference |
| Session lifecycle, state, sharing, concurrency, cursor, activity, or write semantics | `docs/modules/session.md` and `docs/getting-started/shared-session.md` when user-visible |
| Transport contract, invariant, or concrete behavior | `docs/modules/transport.md` and affected workflow/reference |
| CLI command, flag, default, help, output, or interactive behavior | `docs/reference/cli.md` and affected workflow; keep actual help and tests consistent |
| MCP endpoint, tool, schema, result, error, or lifecycle behavior | `docs/reference/mcp-tools.md`, `docs/modules/tool-registry.md` when core contracts change, and affected tests/workflow |
| Configuration field, default, precedence, or storage path | `docs/reference/configuration.md` and affected workflow |
| Target, Session, short-reference, `session_id`, or `device_id` semantics | `docs/reference/identifiers.md` and affected module/workflow |
| User workflow | the focused file under `docs/getting-started/` and affected reference |
| Build, test, toolchain, or supported-platform behavior | `docs/development/building-and-testing.md`, `docs/development/setup.md`, and/or `docs/development/code-style.md` |
| Network exposure, listener default, authentication, authorization, or security behavior | affected MCP/CLI/config reference and workflow, with security impact stated |
| Public document added, removed, renamed, or given a materially different responsibility | `docs/README.md` plus every inbound link |
| Internal implementation only, with no contract, responsibility, workflow, or procedure change | usually no public docs update; explain why in the completion report |
| Bug fix restoring already-documented intended behavior | usually no docs update unless documentation was wrong or a durable limitation was discovered |

### Documentation completion gate

Before declaring a coding task complete, the agent must explicitly review all fourteen questions:

1. Did package placement or responsibility change?
2. Did repository structure change?
3. Did architecture or dependency direction change?
4. Did an application use case change?
5. Did Session lifecycle, state, sharing, concurrency, cursor, activity, or write semantics change?
6. Did the Transport contract or concrete Transport behavior change?
7. Did public CLI behavior change?
8. Did public MCP behavior change?
9. Did configuration change?
10. Did identifier or reference semantics change?
11. Did a user workflow change?
12. Did build, test, toolchain, or supported-platform behavior change?
13. Did network exposure or security behavior change?
14. Was a public document added, removed, renamed, or given a materially different responsibility?

If any answer is yes, update the corresponding docs in the same contribution.

If all answers are no, the final report must state: `Documentation: no update required because ...` and provide the concrete reason.

## 12. Documentation Quality Rules

- Document current implemented behavior, not assumptions.
- Future-facing material must be clearly labeled as planned/proposed and should be rare in reference/module docs.
- Prefer updating an existing document over creating a new Markdown file for a minor change.
- One document should have one clear ownership purpose. Avoid duplicate sources of truth.
- Do not copy complete CLI `--help` text into multiple places.
- Do not manually duplicate MCP schemas when they can be generated from the actual registry/schema source.
- If a documentation generator exists, regenerate generated reference docs; do not hand-edit generated sections.
- Use relative repository links. Never commit developer-specific absolute filesystem paths.
- Examples must use non-sensitive placeholder devices, addresses, IDs, and authentication data.
- Do not publish non-public organizational plans, internal assistant workflows, access secrets, authentication data, or machine-specific notes.
- Keep docs concise and engineering-oriented. Explain behavior, contracts, ownership, invariants, workflows, and constraints rather than narrating development history.
- When moving/renaming files or docs, update inbound links in the same change.
- When adding a new docs directory/category, add it because multiple current documents share a stable purpose, not because it might be useful someday.

## 13. README Maintenance Boundary

README is intentionally separate from detailed docs.

Create a translated README such as `README.zh-CN.md` or `README.ja.md` only when the maintainer explicitly requests it. `README.md` remains the source of truth. When the canonical README changes, review any existing translations for impact. A translation must not claim behavior or capabilities absent from the English README or current implementation.

Review README when a change affects:

- project positioning/capability summary;
- installation or minimum setup;
- the primary quick-start flow;
- a flagship user workflow;
- supported platform summary;
- links to public documentation.

Do not update README for every internal feature or bug. Detailed behavior belongs in `docs/`.

Do not make README a second CLI reference, MCP schema reference, or architecture manual. Do not create a translated README speculatively.

## 14. Go Style and Naming

Use idiomatic Go and `gofmt`.

- packages/directories: lowercase;
- exported identifiers: `PascalCase`;
- unexported identifiers: `camelCase`;
- tests: `Test<Behavior>`;
- interfaces: small and consumer-oriented;
- errors: stable sentinel errors only when callers need semantic comparison;
- platform-specific behavior: small files/adapters with appropriate build constraints.

Avoid unnecessary dependency additions. A new dependency must have a concrete need and must be evaluated for supported-platform and licensing impact.

Do not copy source code with unknown or incompatible licensing into the repository.

## 15. Go Documentation Comments

Use standard Go Doc Comment style.

- Every exported package, type, interface, function, method, constant, and variable must have an appropriate documentation comment.
- Exported declaration comments should normally start with the declaration name.
- Package comments use `// Package <name> ...`.
- Comments are written in English.
- Prefer `//` doc comments; do not use Doxygen `@brief`, `@param`, `@return` conventions.
- Explain what signatures cannot express: responsibilities, reasons, preconditions, state changes, cancellation, deadlines, ownership, concurrency, error conditions, side effects, protocol constraints, and security limitations.
- Do not add comments that merely translate the next line of code.
- Complex unexported locking, state-machine, cancellation, backpressure, resource-ownership, validation, or protocol-boundary logic also needs comments explaining why it exists.
- For `context.Context`, document meaningful cancellation/deadline effects when they are part of the API contract.
- Use `// TODO:`, `// FIXME:`, and `// Deprecated:` consistently. `Deprecated:` must name the preferred replacement when one exists.

Before completing a Go change, review every changed `.go` file for comment quality and stale comments.

Missing required comments means the implementation is not complete.

## 16. Testing Rules

Use the standard Go `testing` package. Prefer table-driven tests where they improve clarity.

- New behavior requires tests.
- Bug fixes should include regression tests when practical.
- Tests stay next to the package they exercise.
- Transport tests should use fakes/test seams for normal unit tests.
- Unit tests must not unexpectedly open physical serial ports, send bytes to hardware, require network access, or depend on external authentication data.
- Real serial devices, remote hosts, MCP clients, or external services are integration/manual validation and must be identified as such.
- Changes involving cancellation, deadlines, Close, duplicate Close, ownership transfer, Session reuse, cursor behavior, buffer overwrite/drop, write serialization, or concurrent lifecycle paths require explicit tests for those semantics.
- Tests must verify public error semantics where callers depend on them.
- Do not weaken/remove tests merely to make a change pass unless the tested contract is intentionally changing and the change is approved/documented.

Run, as applicable:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

Run `gofmt` only on changed Go files; a focused change does not require formatting the entire repository.

For build-sensitive changes, run the repository build scripts as applicable:

```text
scripts/build.ps1
scripts/build.sh
```

The supported desktop build matrix is:

```text
windows/amd64
windows/arm64
linux/amd64
linux/arm64
darwin/amd64
darwin/arm64
```

Preserve `CGO_ENABLED=0` portability unless a change explicitly requires otherwise and its portability impact is deliberately reviewed.

Unit tests, cross-compilation, native-host tests, and real-device tests are different evidence. Never report one as proof of another.

## 17. User-Visible Verification Requirements

For a new or changed user-visible feature, the completion report must provide actual verification results, not only say "tested".

When applicable, provide:

1. What behavior/risk was verified.
2. Preconditions and environment.
3. Exact commands/actions executed.
4. Expected observable result.
5. Actual result: passed, failed, or not executed.
6. At least one important error path.
7. Safe exit/recovery behavior for interactive or long-running commands.
8. What diagnostics to collect if results differ.
9. Evidence level:
   - automated test confirmed;
   - local command confirmed;
   - cross-build confirmed;
   - real hardware/service confirmed;
   - still requires external confirmation.

For serial-device validation, do not guess communication settings. Record the actual known `port`, `baud`, `data_bits`, `parity`, `stop_bits`, and flow-control settings; mark unknown values as unknown.

For a long-running server, state whether silence after startup is normal, how readiness is confirmed, how a client connects, and how to stop it safely.

Never claim physical-device behavior was verified when only unit tests or cross-compilation were run.

## 18. Platform Rules

Supported desktop targets are Windows, Linux, and macOS on amd64/arm64.

- Keep OS-specific behavior behind small platform-specific files/adapters.
- Do not make Windows COM naming a global assumption.
- Do not make Unix `/dev/*` naming a global assumption.
- Do not make one terminal API or filesystem convention a Core contract.
- New dependencies must be evaluated against all six supported targets.
- Android/iOS are not current release targets; do not create placeholder mobile implementations without an actual mobile feature.
- Avoid unnecessary desktop-only assumptions in Core.
- Cross-build success does not replace native or real-device validation.

## 19. Security and Network-Facing Changes

ChannelTerm can expose control of real terminal Sessions. Treat network access and write-capable interfaces as security-sensitive.

- Never commit passwords, access secrets, private keys, device authentication data, real secrets, or personal configuration.
- Tests/examples must use obviously fake authentication data and non-sensitive addresses.
- Do not weaken localhost-only/default-safe listener behavior without explicit maintainer approval.
- Do not broaden remote access, authentication/authorization scope, or terminal write capability as incidental work.
- Changes that affect network exposure or command/write capability must document the security impact and test safe defaults.
- Do not silently log sensitive configuration or terminal contents.
- Validate untrusted network/protocol input at the correct boundary.
- Avoid exposing internal errors/state when a protocol-safe error is appropriate.

## 20. Dependency and Supply-Chain Rules

Before adding a dependency:

- verify it is actually needed;
- prefer the Go standard library when reasonable;
- check maintenance/activity and platform support;
- verify it is compatible with the supported build matrix and `CGO_ENABLED=0` goal;
- consider binary-size/runtime impact for a CLI/core tool;
- ensure its license is suitable for distribution with an Apache-2.0 project;
- add only the minimal package/module required.

Do not perform unrelated dependency upgrades inside a feature/fix unless needed for that change.

Do not vendor/copy third-party code from unknown sources or with unclear licensing.

If added third-party code or dependencies carry attribution or NOTICE requirements that must be redistributed, review and update the applicable legal notices in the same change. Do not create or remove attribution without a concrete requirement.

## 21. Git Branch and Commit Hygiene

Before a change:

```bash
git status
git branch --show-current
```

When the current branch is `main` or `master` and a task requires repository changes, create and switch to a focused task branch before editing unless the maintainer explicitly requests work directly on the default branch.

```text
feature/<name>
fix/<name>
refactor/<name>
docs/<name>
chore/<name>
```

If the current branch already matches the task, continue on it. Keep one feature's implementation, tests, documentation, and follow-up fixes on the same branch; do not mechanically create another branch for a small addition to the same goal.

Never push directly to `main` or `master`. Changes to the default branch go through a pull request. Force-pushing or rewriting history requires an explicit maintainer request.

For each completed, independent repository task (feature, bug fix, refactor, docs, test, build, CI, or chore), create one local Conventional Commit by default after the required implementation, tests, documentation, and verification have succeeded. Do not commit unfinished work or work with required checks that failed. A user instruction such as `do not commit` disables this default for that task.

Use Conventional Commits:

```text
feat: add ...
fix: correct ...
docs: document ...
test: cover ...
refactor: simplify ...
build: update ...
ci: update ...
chore: maintain ...
```

One independently acceptable feature, bug fix, or refactor normally forms one final commit, including its required tests and documentation. Do not mechanically split commits just because several files changed, and do not require every external contributor to have only one historical commit.

Before staging/committing:

```bash
git status
git diff
git diff --check
```

Explicitly stage only files related to the current task. Preserve unrelated working-tree changes: do not reset, clean, overwrite, stage, or commit them. Do not blindly use `git add .` or `git add -A` when unrelated changes may exist.

After the default local commit, stop the Git publishing workflow. Without explicit maintainer/user approval, do not:

- run `git push` to any other branch;
- force-push;
- rewrite Git history;
- run destructive `reset --hard` or `git clean`;
- rebase a shared branch;
- delete local/remote branches;
- delete or modify tags;
- create or merge a pull request;
- create/modify a release;
- change the repository license;
- rewrite authorship or commit dates.

## 22. AI-Agent Reviewability Rules

AI agents must optimize for reviewability and maintainability.

- Read enough context to identify the owning layer before editing.
- Do not rewrite working code solely to match a preferred style.
- Do not broaden scope without explaining why the additional change is required.
- Do not invent APIs, flags, tool names, schemas, behavior, test results, hardware results, documentation, or repository facts.
- Do not silently fix unrelated issues discovered during the task.
- Do not hide large generated rewrites in an otherwise small change.
- Preserve unrelated user/maintainer changes.
- Prefer deterministic, boring implementations over clever abstractions.
- Keep errors actionable and preserve useful error chains with `%w` when appropriate.
- When uncertain about architecture, a breaking public contract, security behavior, or ownership, stop and request maintainer direction rather than guessing.
- Do not modify governance rules in `AGENTS.md` merely to make a task easier.

## 23. AGENTS.md Maintenance

`AGENTS.md` owns long-lived repository governance and a small set of current facts needed to apply that governance. It does not own the detailed repository tree, feature inventory, implementation diary, or roadmap.

Agents may update current high-level facts when the repository has already changed durably, including stable ownership boundaries, supported-platform facts, and canonical documentation owners. Detailed tree and package information belongs only in `docs/architecture/repository-map.md`.

Agents must **not** remove, weaken, bypass, or reinterpret governance for scope, architecture, testing, documentation, Git safety, security, dependencies, or licensing without explicit maintainer approval.

Do not add temporary tasks, TODO lists, roadmap items, one-off implementation details, personal preferences, or development diary information to `AGENTS.md` without explicit maintainer approval.

When `AGENTS.md` changes, the completion report must say what rule/fact changed and why.

## 24. Completion Gate

A task is complete only when implementation, tests, documentation, and verification agree.

Before declaring completion, check:

### Code

- Is the change in the correct package/layer?
- Did it avoid unrelated modifications?
- Are public contracts intentional?
- Are comments accurate and sufficient?

### Tests

- Is new behavior tested?
- Is a bug protected by a regression test where practical?
- Were relevant lifecycle/error/concurrency paths considered?
- Were the claimed commands actually run?

### Documentation

- Was the documentation-impact matrix reviewed?
- Is `docs/architecture/repository-map.md` current if packages changed?
- Are module docs current if responsibilities/lifecycle/invariants changed?
- Are CLI/MCP/config/reference docs current if public behavior changed?
- Are getting-started documents current if workflows changed?
- Are development documents current if build or test procedures changed?
- If no docs changed, is there a concrete reason?

### Compatibility and security

- Any CLI/MCP/config/identifier breaking change?
- Any platform/build impact?
- Any network/security impact?
- Any dependency/license impact?

### Final report

Report at minimum:

1. **Change** - what behavior/structure changed.
2. **Placement** - why the affected packages are the correct architectural owners.
3. **Files/modules** - important areas changed.
4. **Tests** - exact commands and results.
5. **Documentation** - exact docs updated, or a specific reason none were needed.
6. **README Impact** - `Updated - <what changed>` or `None - <concrete reason>`.
7. **Compatibility/security** - public contract, platform, network, config, or migration effects.
8. **Unverified boundaries** - real hardware, another OS, external authentication, external service, or other validation not performed.

If any required item is stale or unverified, report the task as incomplete or partially verified rather than presenting it as fully complete.
