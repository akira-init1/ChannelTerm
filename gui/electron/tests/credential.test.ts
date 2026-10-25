import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import {
  loadToken,
  localClientTokenPath,
  tokenPath,
} from "../src/main/host/config";

test(
  "Main loads local management credentials without upgrading explicit or missing credentials",
  {
    skip:
      process.platform === "darwin"
        ? "Default macOS config requires a separate test account"
        : false,
  },
  async () => {
    const directory = await mkdtemp(join(tmpdir(), "channelterm-credentials-"));
    const names = [
      "APPDATA",
      "XDG_CONFIG_HOME",
      "CHANNELTERM_HTTP_AUTH_TOKEN",
    ] as const;
    const previous = names.map((name) => process.env[name]);
    try {
      process.env.APPDATA = directory;
      process.env.XDG_CONFIG_HOME = directory;
      delete process.env.CHANNELTERM_HTTP_AUTH_TOKEN;
      const shared = tokenPath(process.platform, process.env, directory);
      await mkdir(dirname(shared), { recursive: true });
      await writeFile(shared, "test-shared-read-only\n", { mode: 0o600 });
      await assert.rejects(loadToken(), /management credential unavailable/);
      await writeFile(
        localClientTokenPath(shared, "test-shared-read-only"),
        "test-local-management\n",
        { mode: 0o600 },
      );
      assert.equal(await loadToken(), "test-local-management");
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
  },
);
