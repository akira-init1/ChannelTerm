# AGENTS.md

Repository-wide engineering rules for human and AI-assisted contributions to ChannelTerm.
Optimize for reviewability, correctness, and maintainability; keep each change focused.

## 1. Scope and Source of Truth

ChannelTerm provides shared terminal Sessions for human and AI access. It is serial-first and
focused on hardware/embedded debugging. Core is transport-neutral; Serial and SSH PTY shells are
implemented Transports, while Telnet remains a future direction.

Resolve conflicting information in this order:

1. Current source and tests.
2. Actual CLI help, protocol schemas, and runtime behavior.
3. Public documentation under `docs/`.
4. README examples and summaries.
5. Issues, PRs, comments, old commits, and plans.

Never infer implemented behavior from TODOs or plans, or invent repository facts, APIs, flags,
schemas, documentation, or verification results.

## 2. Ownership and Architecture

Read the [repository map](docs/architecture/repository-map.md) before choosing a package. It is
the sole detailed source/package map; do not duplicate its tree here, in README, or elsewhere.
Place code by responsibility, using these stable boundaries:

| Owner | Responsibility and boundary |
| ----- | --------------------------- |
| `cmd/channelterm/` | Startup and dependency composition; no business/protocol logic |
| `internal/cli/command/` | CLI parsing and local output; no Session/Transport lifecycle |
| `internal/cli/highlight/` | Presentation only; no stored-output or MCP transformations |
| `internal/cli/interactive/` | Local input/escape state; no Session ownership or OS raw mode |
| `internal/cli/terminalinput/` | OS console/raw mode; no remote protocol semantics |
| `internal/init/mcp/` | MCP-client discovery/configuration/install; no MCP hosting or Session lifecycle |
| `internal/install/` | Executable installation, aliases, PATH, ownership state; no user-config semantics or Session lifecycle |
| `internal/core/app/` | Adapter-neutral use cases; no CLI formatting or MCP schemas |
| `internal/core/channel/` | Established stream I/O/lifecycle; no connection setup, history, or presentation |
| `internal/core/session/` | Shared lifecycle, buffering, activity, serialized writes; no adapter/protocol-specific behavior |
| `internal/core/config/` | Config models, persistence, overrides; no terminal lifecycle or UI decisions |
| `internal/core/debugger/` | GDB/MI, authenticated external RSP endpoints, binary relay through Session services |
| `internal/core/connectionpolicy/` | Connection/reuse decisions; no physical Transport operations |
| `internal/core/device/` | Discovery/state registry; no presentation |
| `internal/core/tool/` | Protocol-neutral tools/registry; no MCP schemas |
| `internal/core/transport/` | Transport contract; no history or presentation |
| `internal/core/transport/serial/` | Serial setup/discovery; no Session history or CLI presentation |
| `internal/transport/ssh/` | SSH authentication, PTY/shell setup, private stream adaptation, dedicated SFTP/SCP clients; no Session history or CLI presentation |
| `internal/mcp/`, `internal/mcp/terminal/` | MCP hosting, schemas, translation to application use cases; no competing Core lifecycle/business logic |
| `scripts/`, `.github/workflows/` | Local automation and hosted CI/tagged releases respectively; no product behavior |
| `skills/` | AI workflows over public interfaces; no product logic, schemas, or duplicated reference docs |
| `gui/electron/` | Independent Electron client, safe IPC, terminal presentation, authenticated Host adapter; no Core or lease authority |
| `docs/` | Durable public technical documentation |

Mandatory architecture rules:

- Dependencies flow from CLI/MCP adapters to `core/app`, then Session/Core services and Transport/Channel. `internal/core/**` must not depend on CLI/MCP, GUI/TUI, LLM SDKs, cloud services, product editions, or presentation frameworks.
- Adapters reuse application/Core use cases. Protocol setup belongs in its Transport or configuring use case, never generic Channel/Session contracts.
- A Transport establishes a connection and transfers its live byte stream to a Channel; it owns no history. A Channel owns established bidirectional I/O and stream lifecycle. Optional capabilities such as resizing must remain optional.
- A Session owns buffering, activity, write serialization, and shared lifecycle above one Channel. Keep output raw; presentation belongs outside Session.
- Use `Physical endpoint -> Transport -> Channel -> Session -> Client/Attachment`. Multiple Clients can share a Session, with independent output/activity cursors; do not invent another consumer concept.
- Writes pass through Session without byte-level interleaving of complete payloads. This does not imply semantic multi-writer ownership, exclusive leases, transactions, priority, arbitration, or shell coordination; stronger coordination remains future work.
- Session IDs are opaque. Short references such as `SER-1` are conveniences, not interchangeable IDs. Native targets such as `COM8` or `/dev/ttyUSB0` identify serial endpoints, not live Sessions.
- CLI `TARGET` is transport-neutral: `attach`/`connect` accept native serial targets, `ssh` accepts `USER@HOST`. Document new transport grammars before use and keep them unambiguous with `<TRANSPORT>-<N>` references.
- Keep interfaces small and near consumers, implementation under `internal/` unless an external Go API is intentionally designed/reviewed, and mutable state on owned objects rather than globals.
- Split cross-cutting changes across existing layers. Do not introduce catch-all packages, speculative plugin/SDK/extension systems, placeholder directories, or abstractions merely to shorten files or satisfy a preferred style. A new package needs a stable responsibility.

Update architecture docs for changes to ownership, dependency direction, lifecycle, Session/Transport
responsibility, or major data flow. Update the repository map for top-level directory or `internal/`
package additions/removals/moves/renames, material responsibility/dependency changes, new adapter or
Transport families, or an internal implementation becoming a stable public boundary. A helper added
inside an unchanged package does not require a map update.

## 3. Change Workflow and Compatibility

Before editing, run `git status` and `git branch --show-current`; follow the Git rules below.
Preserve unrelated changes. One independently verifiable feature/fix/refactor is normally one change
unit, including its tests, help/schema/config changes, and docs. Separate independent goals, not files.
Do not mix unrelated cleanup, formatting, renames, upgrades, generated rewrites, or opportunistic fixes.
Explain any necessary scope expansion.

For every change:

1. Define expected behavior and acceptance criteria; read relevant source, tests, help/schemas, and docs.
2. Identify ownership and public contracts; reuse Core abstractions and implement the smallest coherent change.
3. Add/update tests and required documentation in the same contribution; verify success and an important error path.
4. Review comments, lifecycle/cancellation/concurrency risks, compatibility, security, and documentation impact; run required checks.
5. Inspect the diff, stage verified task changes, and report actual results and remaining boundaries.

For bugs, reproduce when practical, add a regression test when feasible without hardware, fix at the
lowest correct layer, and check adjacent lifecycle/error paths sharing the cause. For refactors,
state the technical objective, preserve observable behavior unless explicitly changing it, keep
existing tests passing, and cover newly exposed gaps. Do not rewrite working code for style alone.
Prefer straightforward deterministic implementations and actionable errors with useful `%w` chains.

Compatibility contracts include CLI commands/flags/defaults/validation/help/exit/escape behavior;
MCP names/descriptions/argument and result schemas/errors/lifecycle; config fields/defaults/precedence/
persistence; Session lifecycle and identifiers; listeners/exposure/security; script-facing output
including JSON; and documented environment variables/config locations. Do not silently rename,
remove, reinterpret, or weaken them. Deliberate breaking changes require explicit maintainer approval,
focused tests, docs, and practical migration/deprecation guidance. Actual CLI help is authoritative;
do not maintain another manually copied full option list.

If architecture, ownership, a breaking contract, or security behavior is uncertain, request maintainer
direction instead of guessing. Code alone is not completion when tests, schemas, help, or docs are stale.

## 4. Documentation

Canonical technical docs are English; keep identifiers, paths, commands, and literal examples intact.
Each fact has one canonical owner. Update existing docs rather than creating parallel references.

- `README.md`: English landing page, positioning, installation/quick start, major workflows, realistic examples, and links.
- `docs/architecture/`: boundaries, dependencies, composition, ownership, and data flow.
- `docs/modules/`: module contracts, lifecycle, concurrency, invariants, errors, implementation constraints.
- `docs/getting-started/`: task-oriented workflows; `docs/reference/`: precise public contracts/formats.
- `docs/development/`: build/test/platform/contribution procedures; `docs/README.md`: navigation only, indexing every public document.

### Impact review and canonical owners

Every code change must explicitly review all fourteen impact areas before completion: package
placement/responsibility, repository structure, architecture/dependencies, application use cases,
Session semantics, Transport behavior, CLI, MCP, config, identifiers, workflows, build/test/platforms,
network/security, and public-document structure/responsibility. Update affected owners in the same
contribution using this matrix; summaries elsewhere must link to the owner.

| Change or durable fact | Canonical owner / required review |
| ---------------------- | --------------------------------- |
| Packages, repository structure, ownership | `docs/architecture/repository-map.md`; review stable facts in `AGENTS.md` |
| Architecture, dependencies, ownership, major data flow | `docs/architecture/overview.md` and/or `docs/architecture/data-flow.md` |
| Application use cases | `docs/modules/application.md` and affected workflow/reference |
| Session lifecycle/state/sharing/concurrency/cursors/activity/writes | `docs/modules/session.md`; `docs/getting-started/shared-session.md` when user-visible |
| Transport contracts/invariants/behavior | `docs/modules/transport.md` and affected workflow/reference |
| Buffers, device registry, connection policy | `docs/modules/buffer.md`, `docs/modules/device-registry.md`, `docs/modules/connection-policy.md`, respectively |
| Protocol-neutral tool registry | `docs/modules/tool-registry.md` |
| CLI commands/flags/defaults/help/output/interaction | `docs/reference/cli.md` and affected workflow; align actual help and tests |
| MCP endpoints/tools/schemas/results/errors/lifecycle | `docs/reference/mcp-tools.md`, affected tests/workflow; `docs/modules/tool-registry.md` when Core contracts change |
| Config fields/defaults/precedence/persistence/paths | `docs/reference/configuration.md` and affected workflow |
| Targets, Sessions, short references, `session_id`, `device_id` | `docs/reference/identifiers.md` and affected module/workflow |
| User workflows | Focused `docs/getting-started/` file and affected reference; serial/MCP owners are `serial-terminal.md`/`mcp-server.md` there |
| Build/test/toolchain/platforms | `docs/development/building-and-testing.md`, `setup.md`, and/or `code-style.md` there |
| Exposure/listeners/authentication/authorization/security | Affected CLI/MCP/config reference and workflow; state security impact |
| Positioning/installation/first run/primary workflow/major capability/README demo | Review `README.md` using the boundary below |
| Public docs added/removed/renamed or materially reassigned | `docs/README.md` and all inbound links |

Internal-only changes need no public-doc update if contracts, responsibilities, workflows, and
procedures are unchanged. A bug restoring documented behavior needs docs only if they were wrong,
a durable limitation was found, or architecture/module descriptions need correction. If all impact
answers are no, report `Documentation: no update required because ...` with the concrete reason.

### Quality and README boundary

- Document current behavior, with examples traceable to source/tests/help/schemas/canonical docs. Label abbreviated or device-dependent output; clearly mark rare proposed material as such.
- Use relative links and non-sensitive placeholders. Do not publish secrets, personal/machine configuration, non-public organizational plans, or internal assistant workflows.
- Keep docs concise and engineering-oriented. Investigation history belongs in issues/PRs; do not add public diaries, `docs/records/`, planning areas, empty placeholders, or speculative categories. New docs need current substance and no suitable existing owner; new categories need multiple current documents.
- Regenerate generated references; do not hand-edit generated sections or duplicate generatable MCP schemas. Update inbound links when moving/renaming content.
- Record `README Impact: Yes` or `README Impact: No` with a concrete reason for every user-visible change. Update README for major capability/positioning, installation/setup/first run, primary/flagship workflows, representative demos, platform summaries, or landing-page links. Small options, niche commands, internal changes, and minor fixes normally belong in reference/workflow docs.
- Prefer a short realistic workflow/example/diagram over feature lists. README must not become another CLI/MCP reference or build manual.
- Create README translations only on explicit request; English remains canonical. Every English README edit requires synchronizing all maintained translations in the same contribution, including behavior, examples, navigation, and structure. Translation-only wording fixes may stay local but introduce no new facts.
- Adding/removing/renaming a translation also updates language navigation in English and every remaining translation, plus the project-entry list in `docs/README.md`.

## 5. Go Style and Comments

Use idiomatic Go and `gofmt`: lowercase packages/directories, `PascalCase` exports, `camelCase`
unexported names, `Test<Behavior>` tests. Use small consumer-owned interfaces, stable sentinel errors
only when callers need semantic comparison, and small platform-specific files with build constraints.

Every package and exported declaration needs an appropriate English Go doc comment. Declaration
comments normally start with the name; package comments use `// Package <name> ...`. Prefer `//`,
not Doxygen tags. Explain responsibilities, reasons, preconditions, state changes, cancellation/
deadlines, ownership, concurrency, errors, side effects, protocol constraints, and security limits
that signatures cannot convey. Complex unexported locking/state machines/backpressure/validation/
resource ownership also need explanations; avoid comments that merely restate code.

Use `TODO:`, `FIXME:`, and `Deprecated:` consistently; name the replacement for deprecations when
one exists. Review every changed Go file for missing/stale comments before completion.

## 6. Testing, Platforms, and Verification

Follow [building and testing](docs/development/building-and-testing.md). Use Go's `testing` package,
table-driven tests when helpful, and `*_test.go` files beside the tested package.

- New behavior requires tests; bugs need regression coverage where practical. Test public error semantics and newly exposed refactor gaps.
- Explicitly test affected cancellation/deadlines, Close/duplicate Close, ownership transfer, reuse, cursors, buffer overwrite/drop, serialized writes, and concurrent lifecycle semantics.
- Use fakes/test seams for normal Transport tests. Unit tests must not unexpectedly access physical serial ports, send hardware bytes, require network access, or depend on external credentials. Identify real devices/hosts/MCP clients/services as integration/manual validation.
- Do not weaken/remove tests to get a pass unless the changed contract is approved and documented.

Run applicable checks, formatting only changed Go files:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

The build/test guide defines additional checks, including race testing for normal Go changes;
documentation-only changes still require repository tests and vet. Build-sensitive changes use
`scripts/build.sh` or `scripts/build.ps1` as appropriate. Preserve `CGO_ENABLED=0` portability unless
a required exception and its impact are explicitly reviewed.

Supported desktop targets are Windows/Linux/macOS on amd64/arm64 (Go: `windows`, `linux`, `darwin`).
Evaluate dependencies against all six. Isolate OS details; do not turn Windows COM names, Unix
`/dev/*`, terminal APIs, or filesystem conventions into Core contracts. Avoid needless desktop-only
Core assumptions. Android/iOS are not release targets; add no placeholder mobile implementation.

For user-visible changes, report actual evidence: behavior/risk, environment/preconditions, exact
commands/actions, expected versus actual result (PASS/FAIL/not run), an important error path,
safe exit/recovery where relevant, and diagnostics for differing results. Distinguish automated,
local-command, cross-build, real-hardware/service, and still-unverified evidence. Cross-compilation
never proves native OS or hardware behavior.

For serial validation, record known `port`, `baud`, `data_bits`, `parity`, `stop_bits`, and flow
control; mark unknowns rather than guessing. For long-running servers, explain startup silence,
readiness, client connection, and safe shutdown.

## 7. Security and Dependencies

Terminal access and write-capable/network interfaces are security-sensitive:

- Never commit secrets, passwords, private keys, device credentials, or personal config. Tests/examples use obviously fake credentials and non-sensitive addresses.
- Do not weaken localhost/default-safe listeners without explicit maintainer approval, or incidentally broaden remote access, authentication/authorization, or write capability.
- Document exposure/command/write security effects and test safe defaults. Validate untrusted protocol input at the proper boundary, use protocol-safe errors, and avoid leaking internal state or silently logging sensitive config/terminal contents.

Add a dependency only for a concrete need; prefer the standard library. Check maintenance, all six
platforms, CGO portability, binary/runtime cost, and Apache-2.0 distribution compatibility; add the
minimum needed module/package. Avoid unrelated upgrades. Never copy/vendor code with unknown or
incompatible provenance/licensing. Review required attribution/NOTICE redistribution and update
legal notices in the same change; do not add/remove attribution without a concrete requirement.

## 8. Git and Commit Messages

- On `main`/`master`, create a focused `feature/`, `fix/`, `refactor/`, `docs/`, or `chore/` branch before editing unless explicitly told to work directly there. Continue an appropriate existing branch, including its tests/docs/follow-up fixes.
- Never push directly to `main`/`master`; default-branch changes go through a PR. Force-pushing/history rewriting requires an explicit maintainer request.
- Every completed repository modification must hand off verified task changes staged. Do not stage unfinished work or work with failed required checks. Do not commit or push automatically.
- Before staging, inspect `git status`, `git diff`, and `git diff --check`. Stage only explicit task paths with `git add path/to/file`; never `git add .`/`git add -A` without an explicit request.
- Preserve unrelated working-tree/index changes. For mixed files, safely isolate task hunks; otherwise leave them unstaged and explain why. Verify the task's index changes with `git diff --cached --stat` and the staged diff as needed. Report pre-existing staged work separately.
- One independently acceptable feature/fix/refactor normally fits one final commit with its tests/docs. Do not split by file count or require contributors to have only one historical commit.

Use Conventional Commit types `feat`, `fix`, `docs`, `test`, `refactor`, `build`, `ci`, or `chore`.
Substantial code changes (cross-layer features, complex fixes, significant refactors) require both
suggested and actual messages to use a scoped subject and meaningful bullet body:

```text
<type>(<scope>): <concise outcome>

- Main behavior change and purpose
- Relevant design decisions and compatibility/lifecycle/security boundaries
- Accompanying tests/docs when they help explain the change
```

Choose the primary area as scope. Describe the final result; omit file-by-file inventories,
investigation history, and abandoned approaches. Adapt bullets rather than filling a template.
Small straightforward changes may use only a subject. Omit routine `Validation:` blocks and test
inventories by default; evidence and unverified boundaries belong in PRs/completion reports.
Include a brief verification note only for a consequential limitation or explicit request. Testing
and reporting requirements still apply.

After staging/verification, inspect `git status --short` and recommend one exact Conventional Commit
command, without executing it: `git commit -m "<subject>"`. Substantial changes must include the body
with a second `-m` or a message file via `--file`.

Without explicit maintainer/user approval, do not commit, push, force-push, rewrite history/authorship/
commit dates, rebase shared branches, delete branches, delete/modify tags, create/merge PRs,
create/modify releases, change the license, or run `git reset --hard`, `git clean`,
`git checkout -- <file>`, or `git restore <file>`. Never discard unrelated changes for convenience.

### Development Tasks and Git Branches

1. **Task boundaries**
   - One independently verifiable feature, bug fix, or refactor should generally be treated as one development task.
   - Related implementation, tests, CLI help, and documentation belong to the same task.
   - Minor fixes and follow-up changes should remain in the current task when they share the same acceptance criteria.
   - Split unrelated objectives into separate tasks.

2. **Branch workflow**
   - `main` is the primary integration branch.
   - Develop independent features and fixes on short-lived branches.
   - After validation and approval, merge completed work into `main`.
   - Create subsequent feature branches from the latest `main` whenever practical.
   - Do not use permanent branches for individual GUI frameworks or features.
   - Do not automatically create, rename, switch, merge, or delete branches without explicit authorization.

3. **Branch naming**
   - `feat/<scope>-<feature>` — new functionality.
   - `fix/<scope>-<issue>` — bug fixes.
   - `refactor/<scope>-<goal>` — refactoring.
   - `test/<scope>-<goal>` — tests.
   - `docs/<scope>-<goal>` — documentation.
   - `build/<scope>-<goal>` — build and release tooling.
   - Prefer names describing a concrete deliverable, not an indefinite development area.

4. **Multiple frontends**
   - CLI, Electron GUI, and potential future GUI implementations must remain independently buildable.
   - GUI implementations should reside in separate directories.
   - Frontends should reuse the existing Go Host/Core rather than duplicate its business logic.
   - A new frontend does not require a permanent Git branch or a separate repository.
   - Future GUI frameworks must not be treated as implemented until actual code exists.

5. **Completion and integration**
   - Run relevant tests and checks before recommending integration.
   - Report changes, test results, compatibility impact, and remaining limitations.
   - Do not automatically stage, commit, push, merge, or publish without explicit user approval.

## 9. Maintaining This File

`AGENTS.md` owns long-lived governance and necessary current high-level facts, not a detailed tree,
feature inventory, diary, or roadmap. Update stable ownership/platform/document-owner facts when
implementation changes durably; detailed package information stays in the repository map.

Do not remove, weaken, bypass, or reinterpret scope, architecture, testing, docs, Git, security,
dependency, or licensing rules without explicit maintainer approval, or change rules merely to ease
a task. Temporary tasks, TODO lists, roadmap items, one-off implementation details, personal
preferences, and development diaries also require explicit approval. Report any rule/fact change
and its reason.

## 10. Completion Gate and Report

Before declaring completion, confirm correct placement/scope, intentional contracts, accurate
comments, relevant regression/lifecycle/error/concurrency tests, all documentation-impact areas,
and compatibility/platform/build/network/config/dependency/license/migration effects. Implementation,
tests, help/schemas, examples, and docs must agree; reported commands must have actually run.

Keep the final report concise; combine related items while covering:

- What changed, why those owners/files are appropriate, and all files actually modified.
- Exact formatting/test/lint/vet/build/diff commands run and PASS/FAIL results; relevant unverified hardware, OS, authentication, or service boundaries.
- Docs updated or a concrete no-update reason; `README Impact: Yes/No` followed by `Reason: ...`.
- Public-contract, platform, network/security, config, migration, dependency, or license effects.
- Task files staged, preserved unrelated changes/index state, final `git status --short` summary, and one suggested commit command (not executed).

If a required item is stale or unverified, say the task is incomplete or partially verified.
