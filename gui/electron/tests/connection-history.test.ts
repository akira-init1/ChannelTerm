import assert from "node:assert/strict";
import test from "node:test";
import {
  sharedPreset,
  connectionPreset,
  recordConnection,
  restoreConnections,
} from "../src/shared/connection-history";
import type { ConnectionRequest } from "../src/shared/connections";

const ssh: ConnectionRequest = {
  kind: "ssh",
  host: "example.invalid",
  user: "test",
  port: 22,
  label: "Board",
  identityFiles: ["private-path"],
  knownHostsFile: "trust-path",
  password: "fake-password",
  keyPassphrase: "fake-passphrase",
};

test("history projects safe fields on write and read, preserving names and usage", () => {
  const first = recordConnection([], ssh, true, 10);
  const second = recordConnection(
    first,
    { ...ssh, password: "different" },
    false,
    20,
  );
  assert.equal(second.length, 1);
  assert.equal(second[0]?.uses, 2);
  assert.equal(second[0]?.lastUsed, 20);
  assert.equal(second[0]?.favorite, true);
  assert.equal(second[0]?.request.label, "Board");
  assert.deepEqual(restoreConnections(JSON.stringify(second)), second);
  const serialized = JSON.stringify(second);
  for (const secret of [
    "fake-password",
    "fake-passphrase",
    "private-path",
    "trust-path",
  ])
    assert.ok(!serialized.includes(secret));
  assert.deepEqual(
    restoreConnections(JSON.stringify([{ ...first[0], request: ssh }])),
    first,
  );
});

test("history rejects malformed storage and bounds recent entries while retaining favorites", () => {
  for (const value of [
    "{",
    "{}",
    "x".repeat(131073),
    JSON.stringify(Array(51).fill({})),
    '[{"request":{}}]',
  ])
    assert.deepEqual(restoreConnections(value), []);
  let entries = recordConnection([], ssh, true, 1);
  for (let i = 0; i < 60; i++)
    entries = recordConnection(
      entries,
      { ...ssh, host: `host-${i}.invalid` },
      false,
      i + 2,
    );
  assert.equal(entries.length, 50);
  assert.equal(entries[0]?.request.label, "Board");
  assert.equal(entries[0]?.favorite, true);
  assert.equal(entries[1]?.lastUsed, 61);
});

test("only explicitly saved serial connections are favorites and wake intent is never restored", () => {
  const serial: ConnectionRequest = {
    kind: "serial",
    port: "COM-FAKE",
    baud: 115200,
    dataBits: 8,
    parity: "none",
    stopBits: "1",
    flowControl: "none",
    label: "Test",
    wake: true,
  };
  assert.deepEqual(connectionPreset(serial), { ...serial, wake: false });
  assert.equal(recordConnection([], serial, false)[0]?.favorite, false);
  const saved = recordConnection([], serial, true);
  assert.equal(restoreConnections(JSON.stringify(saved))[0]?.favorite, true);
});

test("first attachment retains only usable endpoint hints for a reviewed reconnect", () => {
  assert.deepEqual(
    sharedPreset({
      transport: "ssh",
      endpoint: "dev@[::1]:2222",
      label: "Board",
    }),
    {
      kind: "ssh",
      host: "::1",
      port: 2222,
      user: "dev",
      label: "Board",
      knownHostsFile: "",
      identityFiles: [],
    },
  );
  assert.equal(
    sharedPreset({ transport: "ssh", endpoint: "missing-login", label: "" }),
    undefined,
  );
  assert.equal(
    sharedPreset({ transport: "serial", endpoint: "COM8", label: "Board" })
      ?.kind,
    "serial",
  );
});
