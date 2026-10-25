import test from "node:test";
import assert from "node:assert/strict";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";
import {
  isSerialProfileChange,
  isSerialProfiles,
} from "../src/shared/serial-profiles";

const profile = {
  port: "COM-FIXTURE",
  baud: 9600,
  dataBits: 8,
  parity: "none",
  stopBits: "1",
  flowControl: "none",
  wake: true,
};

test("serial profiles persist in Host config without opening or waking hardware", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const observer = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    await observer.connect();
    const sessions = await client.list();
    assert.deepEqual(await client.serialProfiles(), {
      default: "",
      profiles: [],
    });
    await client.changeSerialProfile({
      action: "save",
      name: "one",
      profile,
      replace: false,
    });
    await assert.rejects(
      client.changeSerialProfile({
        action: "save",
        name: "one",
        profile,
        replace: false,
      }),
      { code: "busy" },
    );
    await observer.changeSerialProfile({
      action: "save",
      name: "two",
      profile: { ...profile, baud: 123456 },
      replace: false,
    });
    await client.changeSerialProfile({ action: "default", name: "two" });
    assert.equal((await observer.serialProfiles()).default, "two");
    await assert.rejects(
      client.changeSerialProfile({ action: "default", name: "missing" }),
      { code: "tool_error" },
    );
    await client.changeSerialProfile({
      action: "save",
      name: "one",
      profile: { ...profile, baud: 19200 },
      replace: true,
    });
    const saved = await observer.serialProfiles();
    assert.equal(saved.profiles[0]?.profile.baud, 19200);
    assert.equal(saved.profiles[1]?.profile.baud, 123456);
    assert.deepEqual(await client.list(), sessions);
    await assert.rejects(
      client.changeSerialProfile({
        action: "save",
        name: "bad",
        profile: { ...profile, baud: -1 },
        replace: false,
      }),
      { code: "invalid_input" },
    );
  } finally {
    await client.disconnect();
    await observer.disconnect();
    await fixture.stop();
  }
});

test("serial profile boundaries reject extra paths, malformed settings, duplicate names and uncertain writes", async () => {
  assert.equal(
    isSerialProfileChange({ action: "default", name: "one", path: "private" }),
    false,
  );
  assert.equal(
    isSerialProfileChange({
      action: "save",
      name: "one",
      profile: { ...profile, wake: "true" },
      replace: false,
    }),
    false,
  );
  assert.equal(
    isSerialProfileChange({ action: "default", name: "名".repeat(100) }),
    false,
  );
  assert.equal(
    isSerialProfiles({
      default: "one",
      profiles: [
        { name: "one", profile },
        { name: "one", profile },
      ],
    }),
    false,
  );
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let calls = 0;
  try {
    await client.connect();
    globalThis.fetch = async () => {
      calls++;
      throw new Error("private-path-secret");
    };
    await assert.rejects(
      client.changeSerialProfile({ action: "default", name: "one" }),
      (error) =>
        error instanceof Error &&
        !error.message.includes("private-path-secret") &&
        error.message.includes("not replayed"),
    );
    assert.equal(calls, 1);
    globalThis.fetch = async () =>
      new Response(
        JSON.stringify({
          default: "",
          profiles: [{ name: "bad", profile: { ...profile, baud: -1 } }],
        }),
      );
    await assert.rejects(client.serialProfiles(), { code: "incompatible" });
    globalThis.fetch = async () => new Response("missing", { status: 404 });
    await assert.rejects(client.serialProfiles(), { code: "incompatible" });
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});
