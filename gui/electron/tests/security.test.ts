import test from "node:test";
import assert from "node:assert/strict";
import type { IpcMainInvokeEvent, WebContents } from "electron";
import { hostEndpoint, tokenPath } from "../src/main/host/config";
import {
  identifier,
  input,
  integer,
  sessions,
} from "../src/main/host/validation";
import {
  productionURL,
  trustedURL,
  validateSender,
} from "../src/main/security";

test("endpoint and IPC validation reject broad or malformed capabilities", () => {
  for (const value of [
    "https://example.com/mcp",
    "http://localhost/mcp",
    "http://127.0.0.1/mcp?token=x",
    "http://secret@127.0.0.1/mcp",
    "file:///tmp/mcp",
  ])
    assert.throws(() => hostEndpoint(value));
  assert.equal(hostEndpoint().href, "http://127.0.0.1:37099/mcp");
  assert.equal(hostEndpoint("http://[::1]:123/mcp").hostname, "[::1]");
  for (const id of ["", null, {}, "a\n", "a".repeat(513)])
    assert.throws(() => identifier(id));
  for (const data of ["", null, {}, "a".repeat(65537)])
    assert.throws(() => input(data));
  input("\x03\r\n");
  assert.throws(() => integer(Number.MAX_SAFE_INTEGER + 1));
  assert.throws(() => sessions({ sessions: [{}] }));
});

test("only the exact trusted main frame may invoke privileged IPC", () => {
  const mainFrame = { url: productionURL };
  const contents = { mainFrame } as WebContents;
  const event = {
    sender: contents,
    senderFrame: mainFrame,
  } as IpcMainInvokeEvent;
  validateSender(event, contents, false);
  assert.throws(() =>
    validateSender(
      { ...event, sender: {} } as IpcMainInvokeEvent,
      contents,
      false,
    ),
  );
  assert.throws(() =>
    validateSender(
      { ...event, senderFrame: { url: productionURL } } as IpcMainInvokeEvent,
      contents,
      false,
    ),
  );
  for (const url of [
    "https://evil.invalid/",
    productionURL + "?x",
    "channelterm://app/other.html",
  ])
    assert.equal(trustedURL(url, false), false);
});

test("credential paths follow the Host configuration root, not GUI userData", () => {
  // Path separators follow the current test OS; all three selection rules are checked.
  const home = process.cwd();
  assert.ok(
    tokenPath("win32", { APPDATA: home }, home).endsWith("http-auth-token"),
  );
  assert.ok(tokenPath("darwin", {}, home).includes("Application Support"));
  assert.ok(
    tokenPath("linux", { XDG_CONFIG_HOME: home }, home).startsWith(home),
  );
  assert.throws(() => tokenPath("win32", {}, home));
  assert.throws(() =>
    tokenPath("linux", { XDG_CONFIG_HOME: "relative" }, home),
  );
});
