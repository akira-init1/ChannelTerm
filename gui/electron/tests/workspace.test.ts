import assert from "node:assert/strict";
import test from "node:test";
import {
  defaultWorkbenchPreferences,
  restoreWorkbenchPreferences,
  restoreWorkspace,
  workspaceSession,
  type WorkspaceSnapshot,
} from "../src/shared/workspace";
import { defaultTerminalSettings } from "../src/shared/terminal-settings";
import type { SessionSummary } from "../src/shared/api";

const endpoint = "http://127.0.0.1:37099/mcp";
const session: SessionSummary = {
  session_id: "opaque-session",
  session_ref: "SER-1",
  transport: "serial",
  endpoint: "COM-FAKE",
  state: "open",
  label: "Board",
};
const snapshot: WorkspaceSnapshot = {
  endpoint,
  selected: 1,
  tabs: [
    { settings: { ...defaultTerminalSettings } },
    {
      target: {
        sessionId: session.session_id,
        transport: session.transport,
        endpoint: session.endpoint,
        label: session.label,
      },
      settings: { encoding: "gbk", view: "hex", description: "Diagnostic" },
    },
  ],
};

test("workspace restores tab order, selection and view settings without input, output or attachment capabilities", () => {
  const injected = {
    ...snapshot,
    input: "unsent",
    screen: "private",
    tabs: snapshot.tabs.map((tab) => ({
      ...tab,
      input: "unsent",
      attachment: { id: "stale" },
      output: "private",
    })),
  };
  assert.deepEqual(
    restoreWorkspace(JSON.stringify(injected), endpoint),
    snapshot,
  );
  assert.equal(
    restoreWorkspace(JSON.stringify(snapshot), "http://127.0.0.1:1/mcp"),
    undefined,
  );
  const duplicates = {
    ...snapshot,
    tabs: [snapshot.tabs[1], snapshot.tabs[1]],
  };
  assert.deepEqual(
    restoreWorkspace(JSON.stringify(duplicates), endpoint),
    duplicates,
  );
});

test("workspace rechecks full identifiers, endpoint, transport and live state; references never replace IDs", () => {
  const target = snapshot.tabs[1]!.target!;
  assert.equal(workspaceSession(target, [session]), session);
  for (const changed of [
    { state: "closed" },
    { session_id: "replacement" },
    { endpoint: "COM-OTHER" },
    { transport: "ssh" },
  ]) {
    assert.equal(
      workspaceSession(target, [{ ...session, ...changed }]),
      undefined,
    );
  }
  assert.equal(
    workspaceSession({ ...target, sessionId: session.session_ref }, [session]),
    undefined,
  );
  assert.equal(workspaceSession(target, []), undefined);
});

test("optional split layout restores bounded local indices and ignores invalid layout without losing tabs", () => {
  for (const axis of ["columns", "rows"] as const) {
    const split = { axis, first: 0, second: 1 };
    assert.deepEqual(
      restoreWorkspace(
        JSON.stringify({
          ...snapshot,
          split: { ...split, input: "never replay", owner: "not a capability" },
        }),
        endpoint,
      ),
      { ...snapshot, split },
    );
  }
  for (const split of [
    null,
    {},
    { axis: "diagonal", first: 0, second: 1 },
    { axis: "rows", first: 0, second: 0 },
    { axis: "rows", first: 0, second: 32 },
    { axis: "rows", first: -1, second: 1 },
    { axis: "rows", first: 0.5, second: 1 },
    { axis: "rows", first: "0", second: 1 },
  ]) {
    assert.deepEqual(
      restoreWorkspace(JSON.stringify({ ...snapshot, split }), endpoint),
      snapshot,
    );
  }
  const three = {
    ...snapshot,
    tabs: [...snapshot.tabs, snapshot.tabs[0]!],
    selected: 2,
  };
  assert.deepEqual(
    restoreWorkspace(
      JSON.stringify({
        ...three,
        split: { axis: "rows", first: 0, second: 1 },
      }),
      endpoint,
    ),
    three,
  );
});

test("corrupt, oversized or invalid workspace records fail closed", () => {
  for (const raw of [
    "{",
    "null",
    "x".repeat(131073),
    JSON.stringify({ ...snapshot, tabs: Array(33).fill(snapshot.tabs[1]) }),
    JSON.stringify({ ...snapshot, selected: 9 }),
    JSON.stringify({
      ...snapshot,
      tabs: [{ settings: { ...defaultTerminalSettings, input: "unsafe" } }],
    }),
    JSON.stringify({
      ...snapshot,
      tabs: [
        {
          settings: defaultTerminalSettings,
          target: { ...snapshot.tabs[1]!.target, sessionId: "bad\nID" },
        },
      ],
    }),
  ]) {
    assert.equal(restoreWorkspace(raw, endpoint), undefined);
  }
});

test("preferences preserve language, layout and terminal defaults; invalid data uses safe defaults", () => {
  const preferences = {
    ...defaultWorkbenchPreferences,
    language: "zh-CN",
    panelHeight: 300,
    sidebarVisible: false,
    terminal: { encoding: "gbk", view: "hex", description: "Board" },
  };
  assert.deepEqual(
    restoreWorkbenchPreferences(JSON.stringify(preferences)),
    preferences,
  );
  assert.deepEqual(
    restoreWorkbenchPreferences(
      JSON.stringify({ ...preferences, password: "fake" }),
    ),
    preferences,
  );
  for (const patch of [
    { language: "unknown" },
    { panelHeight: 0 },
    { sidebarWidth: 9999 },
    { sidebarView: ["sessions"] },
    { terminal: { ...preferences.terminal, encoding: "invalid" } },
  ]) {
    assert.deepEqual(
      restoreWorkbenchPreferences(JSON.stringify({ ...preferences, ...patch })),
      defaultWorkbenchPreferences,
    );
  }
  for (const raw of [null, "{", "[]", "x".repeat(8193)])
    assert.deepEqual(
      restoreWorkbenchPreferences(raw),
      defaultWorkbenchPreferences,
    );
});
