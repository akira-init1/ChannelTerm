import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import {
  loadToken,
  localClientTokenPath,
  hostIdentityPath,
} from "../src/main/host/config";

test("Main loads local management credentials without upgrading explicit or missing credentials", async () => {
  const directory = await mkdtemp(join(tmpdir(), "channelterm-credentials-"));
  const names = [
    "CHANNELTERM_HOST_CONFIG_DIR",
    "APPDATA",
    "XDG_CONFIG_HOME",
    "CHANNELTERM_HTTP_AUTH_TOKEN",
  ] as const;
  const previous = names.map((name) => process.env[name]);
  try {
    process.env.CHANNELTERM_HOST_CONFIG_DIR = directory;
    process.env.APPDATA = directory;
    process.env.XDG_CONFIG_HOME = directory;
    delete process.env.CHANNELTERM_HTTP_AUTH_TOKEN;
    const shared = hostIdentityPath(process.platform, process.env, directory);
    await mkdir(dirname(shared), { recursive: true });
    const hostID = "H-0123456789abcdef0123456789abcdef";
    await writeFile(shared, JSON.stringify({ version: 1, host_id: hostID }), {
      mode: 0o600,
    });
    // A leftover old token must not grant access or select another path.
    await writeFile(join(directory, "http-auth-token"), "retired-token\n");
    await assert.rejects(loadToken(), /management credential unavailable/);
    await writeFile(
      localClientTokenPath(shared, hostID),
      "test-local-management\n",
      { mode: 0o600 },
    );
    assert.equal(await loadToken(), "test-local-management");
    await rm(join(directory, "http-auth-token"));
    assert.equal(await loadToken(), "test-local-management");
    await writeFile(
      shared,
      JSON.stringify({ version: 1, host_id: "../escape" }),
    );
    await assert.rejects(loadToken(), /management credential unavailable/);
    process.env.CHANNELTERM_HTTP_AUTH_TOKEN = "test-explicit-read-only";
    assert.equal(await loadToken(), "test-explicit-read-only");
    process.env.CHANNELTERM_HTTP_AUTH_TOKEN = "   ";
    await assert.rejects(loadToken(), /empty or malformed/);
    process.env.CHANNELTERM_HTTP_AUTH_TOKEN = "invalid token";
    await assert.rejects(loadToken(), /empty or malformed/);
  } finally {
    names.forEach((name, index) => {
      const value = previous[index];
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    });
    await rm(directory, { recursive: true, force: true });
  }
});
