import assert from "node:assert/strict";
import { test } from "node:test";
import { idlePolicy } from "../src/main/host/idle";

test("effective idle policy validates modes, bounds and returns only display fields", () => {
  const valid = {
    mode: "release",
    origin: "desktop",
    session_timeout_ms: 30000,
    exit_timeout_ms: 0,
    credential: "fake-private",
  };
  assert.deepEqual(idlePolicy(valid), {
    mode: "release",
    origin: "desktop",
    sessionTimeoutMs: 30000,
    exitTimeoutMs: 0,
  });
  for (const patch of [
    { mode: "unknown" },
    { origin: "agent" },
    { session_timeout_ms: -1 },
    { session_timeout_ms: 0 },
    { session_timeout_ms: 86400001 },
    { exit_timeout_ms: 1 },
    { mode: "exit" },
    { mode: "keep" },
  ])
    assert.throws(() => idlePolicy({ ...valid, ...patch }));
  assert.equal(
    idlePolicy({ ...valid, mode: "keep", session_timeout_ms: 0 }).mode,
    "keep",
  );
  assert.equal(
    idlePolicy({ ...valid, mode: "exit", exit_timeout_ms: 30000 }).mode,
    "exit",
  );
});
