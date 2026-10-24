# Electron GUI Engineering Rules

These rules apply to `gui/electron/**`, including its build scripts and tests. They supplement
the [root AGENTS.md](../../AGENTS.md); global architecture, compatibility, security, dependency,
documentation, testing, and Git requirements remain in force. Do not stage, commit, switch
branches, or merge without explicit user authorization. A request for code changes is not Git
authorization.

Read the [GUI architecture](../../docs/architecture/electron-gui.md) and
[repository map](../../docs/architecture/repository-map.md#electron-source-ownership) before
choosing an owner. The architecture document owns cross-frontend boundaries; this file owns
Electron-specific review rules. Do not duplicate the source tree or Go/MCP contracts here.

## 1. Processes and permitted dependencies

- Main owns window lifecycle, trusted Host Client composition, credentials, and authorized
  system capabilities. Keep protocol adaptation in Host Client, separate from window and IPC
  wiring. Host Client must not import React or renderer modules.
- Preload exposes individual, narrowly scoped, typed methods through `contextBridge`. It only
  adapts those methods to fixed IPC channels; it does not own Host requests or business policy.
  Never expose `ipcRenderer`, a generic `send`/`invoke`, arbitrary channel selection, or arbitrary
  tool, command, URL, file, or process execution.
- Renderer uses React for interaction and presentation, and the renderer bridge adapter for Host
  operations. It must not import Main/Preload implementations, the MCP SDK, Electron, Node APIs,
  filesystem/process APIs, or credentials. React components must not issue MCP HTTP requests,
  implement authentication, or grant Lease authorization.
- `shared` contains serializable request/response/error contracts and inert constants. It must
  not import React, Electron, Node runtime APIs, or any process-specific implementation. Do not
  expose runtime-bound types such as Electron events, Node buffers, or xterm objects in IPC types.
- Main, Preload, and Renderer may depend on `shared`; `shared` never depends on them. A bridge
  method call is IPC, not permission to import another process's implementation. Avoid circular
  imports and global mutable service registries.

## 2. Host authority and state

- Go Host is authoritative for Application use cases, Session lifecycle, Transport connections,
  Leases, and device operations. Reuse its public interfaces; do not copy Go Core or parse
  human-readable CLI output as a business protocol. Report a missing capability before making
  a minimal, explicitly scoped interface change; never bypass Application or silently change MCP.
- Host Client owns this client's connection, cancellation, attachment handles, and output
  cursors. These are client resources, not a second Session Manager or writer-ownership system.
  Every shared write must still pass through Host Application authorization.
- Renderer owns view selection, progress, errors, and display snapshots of Host state. A cached
  Lease or disabled input control is a usability guard, never authority to allow a write. Do not
  introduce a client-side arbiter that independently decides shared write permission.
- Closing, reloading, detaching, or disconnecting a GUI releases only its client resources. Do
  not close a shared Session, stop an independently owned Host, or release another client's
  Lease as attachment cleanup. Stale asynchronous completions must not revive old attachments.

## 3. Renderer features and component responsibilities

- Organize implemented features by responsibility, currently `terminal` and `sessions`. Add
  `devices`, an MCP-related view, or another feature only when it has actual supported behavior;
  a renderer MCP view still accesses Host capabilities through the bridge, not the MCP SDK.
- Keep feature components, hooks, state, and tests together when useful. Split files for distinct
  responsibilities or test seams, not to meet an arbitrary size quota. Do not create empty feature
  directories or move working code just to impose a speculative layout.
- `components` holds the application shell and reusable cross-feature UI. The shell may compose
  feature entry points; reusable controls must not reach into feature state or protocol clients.
  Features should not directly import another feature's internal hooks, stores, or implementation;
  use explicit props/callbacks or a small consumer-needed contract at the composition boundary.
- Keep state with its owner and lift it only when coordination requires it. Do not require Redux,
  a DI container, Repository layers, a plugin system, or a common GUI SDK without a concrete need.

## 4. Terminal throughput and lifecycle

- xterm.js renders terminal bytes. Do not render logs character by character in React or route
  high-frequency raw output through global React state. React may hold low-frequency status and
  notices; terminal buffers and render scheduling belong to the terminal view/controller.
- Batch output according to measured throughput and use bounded byte queues, bounded request
  concurrency, and explicit backpressure. Document bounds and overflow behavior when changing
  the pipeline; a chain of unresolved promises is also a queue. Do not accumulate unlimited
  output, input, retries, or callbacks while the Host or renderer is slow.
- Preserve ordering and byte boundaries across chunks, including split UTF-8 and ANSI sequences.
  Handle cursors, `dropped`, disconnect, Session close, and reattachment explicitly. Show data
  loss and reset invalid parser state; never silently concatenate across a gap or describe finite
  retained output as a complete terminal-state snapshot or complete history.
- Avoid unnecessary high-frequency redraws for hidden/background terminals. Any visibility-aware
  suspension must still bound retention and handle cursor loss and resumption explicitly; it must
  not change Host ownership or buffer semantics. Measure before adding scheduling abstractions.
- Effects and controllers must release subscriptions, timers, observers, listeners, xterm/addons,
  and pending work on replacement and unmount. Cleanup must tolerate duplicate calls, React
  StrictMode effect replay, late callbacks, and a window closing during a request.
- Never block Renderer or Main on device waits, synchronous IPC, busy loops, or synchronous
  process execution. Use asynchronous, cancellable Host requests with bounded waits. GUI detach
  must release this client's pending work without closing the shared device connection.

## 5. Security at every boundary

- Keep `contextIsolation` and renderer sandboxing enabled, `nodeIntegration` disabled, and
  `webSecurity` enabled. Preserve restricted content loading, navigation, popup, permission, and
  CSP policies in production and development. Do not weaken them to fix an integration problem.
- Main validates the owning WebContents, main frame, and trusted page identity on every IPC call,
  plus argument count, shape, size, identifiers, and operation-specific capability/attachment
  ownership. A trusted origin alone is insufficient. Treat Renderer requests as untrusted.
- Main's trusted adapter manages Host authentication. Never expose tokens, keys, credential-file
  paths, or other sensitive system paths through IPC, renderer state, logs, URLs, build-time
  `VITE_` variables, or errors. Return only the minimum sanitized data needed for the UI.
- Do not broaden Host authentication, Origin/CORS rules, network listeners, redirect policy, or
  endpoint acceptance to make GUI development easier. Preserve existing safe defaults.
- A system/device operation requires an explicit supported capability and the relevant user
  authorization, not a renderer-provided shell command. Destructive device actions must not run
  without user authorization. A future capability needs boundary review and negative tests before
  exposure; the current prototype does not provide arbitrary device or file operations.

## 6. Types, validation, and errors

- Keep TypeScript `strict` checks enabled. Define explicit requests, responses, and discriminated
  error results at process boundaries. Do not use `any`, unchecked casts, or suppression comments
  to evade core contract checks; narrow external values from `unknown` with runtime validation.
- Validate external Host responses as well as IPC requests. TypeScript types do not validate
  runtime data, cursor ranges, payload lengths, or identifier ownership.
- Preserve available structured Host error codes and safe semantic details in the adapter; map
  them deliberately into the typed UI error contract. Do not flatten every failure to a string,
  infer status by parsing message text, invent missing error metadata, or forward raw diagnostic
  bodies containing secrets. Keep cancellation distinguishable from failure where supported.
- A timeout/disconnect after a device write can mean delivery is uncertain. Never automatically
  retry such writes. Lease busy or a rejected write must not enqueue input for replay after
  unlocking; discard affected queued input and require an explicit new user action.

## 7. Verification and supported environments

Follow the commands and evidence boundaries in the
[GUI development guide](../../docs/development/electron-gui.md#checks-and-isolated-host) and
[repository test guide](../../docs/development/building-and-testing.md). Before handing off a
change for commit, run the available TypeScript, formatting/lint, relevant tests, and build checks.
Currently `typecheck`, `format:check`, `test`, and `build` are available; there is no dedicated
Lint command. Prettier is a formatting check, not a substitute for a semantic linter. Report that
gap rather than claiming Lint passed or adding an unrelated dependency. Run native desktop tests
when process, IPC, terminal, or lifecycle behavior changes, including development-mode checks
when its loading/security path is affected. Documentation-only work still follows the root Go
test/vet requirement.

Required coverage for affected behavior (not a claim that every scenario is already covered):

| Area | Review and test expectations |
| --- | --- |
| Host Client | Simulated/local isolated Host; protocol responses, malformed data, authentication, cancellation/timeouts, busy/closed states, disconnect, and no write replay or credential leaks. Use the existing real Go Host with fake I/O for integration authority checks. |
| IPC | Wrong WebContents/frame/origin, malformed and oversized arguments, stale or foreign attachment handles, unauthorized capabilities, and safe exception results. |
| Terminal | Sustained output with bounded queues, ANSI and Unicode split across chunks, overwrite/loss notices, disconnect/reconnect, and disposal during pending reads/writes/renders. |
| GUI lifecycle | Repeated mount/unmount and attach/detach, native window close, and another client's continued access to the same live Host Session. |

Use fake I/O and disposable loopback fixtures; never make routine tests open real hardware or use
personal credentials. Keep native Windows/Linux/macOS and x64/arm64 path/process behavior explicit:
use platform APIs, avoid hardcoded home/config paths, shells, executable suffixes, and quoting
assumptions. Compilation is not native runtime verification. Report skipped/unavailable checks and
remaining hardware/platform boundaries honestly.

The GUI must build independently without copying Go Core sources; the Go CLI must build without
Node/Electron. GUI test fixtures may compose the existing Go packages, but must not become a second
business implementation or production Host startup path.

## 8. Evolution and review completion

Deliver the current independently verifiable capability with the smallest coherent change. If a
boundary cannot support it, explain the limitation before proposing a minimal adjustment; do not
use future extensibility as the sole reason for abstractions or broad refactors. Update the
canonical architecture and development docs when their responsibilities or procedures change.

Electron and a possible future Qt frontend remain independent clients of public Go Host abilities.
There is no Qt project, file-management GUI, or built-in Agent implied by this policy. If Qt is
implemented later, its technical rules belong in `gui/qt/AGENTS.md`, not in this Electron scope.

Before completion, review dependency direction, state/Lease authority, IPC input and permission
checks, secret exposure, bounded I/O, cleanup/error paths, test evidence, and documentation impact,
in addition to the root completion gate. These rules guide review; they do not assert automated
import-boundary enforcement or coverage that the current checks do not provide.

## 9. Menus and localization

- Keep the six menu groups Connection, Session, Terminal, View, Settings, Help and no more than
  three levels. The [menu reference](../../docs/reference/gui-menus.md) owns the implemented
  action inventory and explicitly unimplemented planning boundaries; planning never grants a capability.
- Use a finite, typed action set and a shared menu description. Menu, toolbar, and keyboard
  activation must reuse the existing action dispatcher; do not duplicate Host/business handlers.
  Shared menu builders, validators, and message definitions must remain pure and process-independent.
- Keep close-tab/detach, GUI disconnect, shared-Session close, and app exit distinct. Bind context
  actions to the clicked opaque Session/attachment and revalidate before execution. Stale callbacks
  must never fall back to the selected Session or a replacement attachment.
- Visibility, disabled state, and checkmarks are display hints only. Preserve Main caller/input
  validation and Host/Application write and Lease authority. Never replay rejected/uncertain writes.
  Local display cleanup must not issue remote commands or alter Host buffers.
- Menu labels/help use Lingui with complete catalogs for every supported locale in the menu reference.
  Keep native menus, renderer menus, open context menus, and checked language state synchronized.
  Follow the [translation workflow](../../docs/development/electron-gui.md#menu-translations-lingui-590):
  define stable IDs and English defaults with Lingui's annotated message descriptors, run official
  extraction, and maintain translations only in per-locale PO files. Do not assemble catalogs with
  language tuples, array indices, or handwritten compiled messages.
- Keep the CLI, config, PO formatter, and Core on the pinned compatible Lingui version. Run
  `i18n:extract` after message changes, `i18n:check` before handoff, and the extraction regression
  test when changing tooling. Do not conceal untranslated target entries with explicit fallback
  locales; strict builds must fail. Non-strict development uses the English source fallback.
- Compiled `messages.ts` files are ignored build output, never hand-edited, formatted, or committed.
  Compile before both Main/esbuild and Renderer/Vite resolve catalogs; Vite configuration alone
  cannot transform Main. Each process owns its Lingui instance and bundles the same generated data.
  Catalogs never expose executable expressions or arbitrary menu/IPC capabilities. Restart development
  after PO changes so Main and Renderer load the same revision; preserve window-scoped locale behavior.
- Keep keyboard navigation scoped to focused menus and ignore IME composition. Do not add native
  accelerators, default roles, or global key handlers that consume terminal/editor input. Any later
  shortcut needs an explicit focus/composition policy and regression coverage.
- Connection forms send bounded typed intent through Main. Serial setup uses existing Host tools;
  SSH setup uses the authenticated [desktop Host API](../../docs/reference/gui-host-api.md).
  Never expose an arbitrary URL/tool/command, bypass host-key checks, persist credentials, or retry
  uncertain setup automatically. The Host's Application remains the sole creation authority.
- Test offline/empty/closed states, non-current and expired context targets, detach/exit without
  shared-Session closure, local-only clear, translation sync, and terminal/IME input boundaries.
