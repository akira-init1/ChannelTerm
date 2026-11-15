import test from "node:test";
import assert from "node:assert/strict";
import {
  mkdtemp,
  writeFile,
  readFile,
  mkdir,
  readdir,
  rm,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { HostSettings, validateEndpoint } from "../src/main/host/settings";
import { companionVersion, displayVersion } from "../src/main/host/information";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";

test("Host endpoint rejects ambiguous, remote, credential-bearing and malformed input", () => {
  for (const value of [
    null,
    {},
    "",
    "http://localhost/mcp",
    "https://127.0.0.1/mcp",
    "http://127.1/mcp",
    "http://2130706433/mcp",
    "http://0x7f000001/mcp",
    "http://127.0.0.1:0/mcp",
    "http://127.0.0.1:99999/mcp",
    "http://127.0.0.1/mcp?",
    "http://127.0.0.1/mcp#",
    "http://secret@127.0.0.1/mcp",
    "http://127.0.0.1\\@remote/mcp",
    "http://192.0.2.1/mcp",
    "http://127.0.0.1/" + "x".repeat(2048),
  ]) {
    assert.throws(() => validateEndpoint(value), { code: "configuration" });
  }
  assert.equal(
    validateEndpoint("http://[::1]:8080/mcp"),
    "http://[::1]:8080/mcp",
  );
  assert.equal(
    validateEndpoint("http://127.0.0.1:80/mcp"),
    "http://127.0.0.1/mcp",
  );
});

test("Host settings preserve override precedence and atomic saved defaults across restart", async () => {
  const root = await mkdtemp(join(tmpdir(), "ct-host-settings-"));
  try {
    const path = join(root, "settings.json");
    const first = new HostSettings(path);
    await first.load();
    assert.equal(first.endpoint(), "http://127.0.0.1:37099/mcp");
    const saving = first.save("http://127.0.0.1:45001/mcp");
    await assert.rejects(first.save("http://127.0.0.1:45002/mcp"), {
      code: "busy",
    });
    await saving;
    const bytes = await readFile(path, "utf8");
    const next = new HostSettings(path);
    await next.load();
    assert.equal(next.endpoint(), first.endpoint());
    const override = new HostSettings(path, "http://127.0.0.1:45003/mcp");
    await override.load();
    assert.equal(override.endpoint(), "http://127.0.0.1:45003/mcp");
    await assert.rejects(override.save(first.endpoint()), {
      code: "configuration",
    });
    await assert.rejects(first.save("http://secret@127.0.0.1"), {
      code: "configuration",
    });
    assert.equal(await readFile(path, "utf8"), bytes);
    await writeFile(path, "{broken");
    const broken = new HostSettings(path);
    await broken.load();
    assert.equal(broken.endpoint(), "http://127.0.0.1:37099/mcp");
    // A directory at the final destination deterministically simulates failed replacement.
    const blocked = join(root, "blocked");
    await mkdir(blocked);
    const failure = new HostSettings(blocked);
    await assert.rejects(failure.save(first.endpoint()), {
      code: "configuration",
    });
    assert.equal(failure.endpoint(), "http://127.0.0.1:37099/mcp");
    assert.deepEqual((await readdir(root)).sort(), [
      "blocked",
      "settings.json",
    ]);
    const invalidOverride = new HostSettings(path, "http://secret@remote/mcp");
    assert.throws(() => invalidOverride.endpoint(), { code: "configuration" });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("version information stays unknown for missing, malformed or mismatched companion metadata", async () => {
  const root = await mkdtemp(join(tmpdir(), "ct-host-version-"));
  try {
    assert.equal(await companionVersion(root), undefined);
    assert.equal(displayVersion("secret\nvalue"), undefined);
    assert.equal(displayVersion("v1.2.3-dev+abc"), "v1.2.3-dev+abc");
    const name = `files/channelterm${process.platform === "win32" ? ".exe" : ""}`;
    await mkdir(join(root, "files"));
    await writeFile(join(root, name), "fake fixture bytes");
    await writeFile(
      join(root, "package-manifest.json"),
      JSON.stringify({
        version: "1.2.3",
        companions: {
          [name]: createHash("sha256")
            .update("fake fixture bytes")
            .digest("hex"),
        },
      }),
    );
    assert.equal(await companionVersion(root), "1.2.3");
    await writeFile(join(root, name), "changed");
    assert.equal(await companionVersion(root), undefined);
    await writeFile(join(root, "package-manifest.json"), "malformed");
    assert.equal(await companionVersion(root), undefined);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("Host version and capabilities are observed only while connected; detach preserves another client", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const observer = new HostClient(fixture.endpoint, fixture.token);
  try {
    assert.equal(client.info().hostVersion, undefined);
    const info = await client.connect();
    assert.ok(info.hostVersion);
    assert.equal(info.sshAvailable, true);
    assert.equal(info.remoteSize, true);
    await observer.connect();
    const id = (await client.list())[0]!.session_id;
    const old = await client.attach(id);
    const other = await observer.attach(id);
    const queued = client.write(old.id, "queued-input-must-be-discarded");
    client.discardQueuedInput();
    await assert.rejects(queued, { code: "cancelled" });
    const pending = client.read(old.id).catch(() => undefined);
    await client.disconnect();
    await pending;
    assert.equal(client.info().hostVersion, undefined);
    await assert.rejects(client.write(old.id, "must not replay"));
    assert.ok((await observer.list()).some((s) => s.session_id === id));
    await observer.write(other.id, "still available");
  } finally {
    await client.disconnect();
    await observer.disconnect();
    await fixture.stop();
  }
});

test("an incompatible MCP identity exposes no connected version or capabilities", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let changed = false;
  try {
    globalThis.fetch = async (input, init) => {
      const response = await original(input, init);
      if (
        typeof init?.body !== "string" ||
        JSON.parse(init.body).method !== "initialize"
      )
        return response;
      const body = await response.text();
      const replaced = body.replace(
        /"name"\s*:\s*"channelterm"/,
        '"name":"incompatible-fixture"',
      );
      changed = replaced !== body;
      const headers = new Headers(response.headers);
      headers.delete("content-length");
      return new Response(replaced, { status: response.status, headers });
    };
    await assert.rejects(client.connect(), { code: "incompatible" });
    assert.equal(changed, true);
    assert.equal(client.info().hostVersion, undefined);
    assert.equal(client.info().sshAvailable, false);
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});
