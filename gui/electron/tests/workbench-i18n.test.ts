import test from "node:test";
import assert from "node:assert/strict";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs";
import { locales } from "../src/shared/menus";
import { menuMessages } from "../src/shared/menu-messages";
import {
  clientErrorMessageId,
  hostRecoveryMessage,
  recoveryMessages,
  sessionStateMessage,
} from "../src/shared/host-recovery";
import { HostError, publicError } from "../src/main/host/errors";
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";

test("all workbench states and errors render in nine languages, without diagnostic text", () => {
  const error = {
    code: "disconnected" as const,
    message: "SECRET: arbitrary remote diagnostic",
  };
  const stored = clientErrorMessageId(error);
  for (const locale of locales) {
    const i18n = setupI18n({ locale, messages: menuCatalogs });
    const t = (id: string) => i18n._(id);
    for (const id of [
      ...Object.values(recoveryMessages),
      "recovery.ssh_host_key",
      "recovery.ssh_authentication",
      "recovery.connection_failed",
      "recovery.connection_uncertain",
      "recovery.diagnostic_unavailable",
      "recovery.profile_exists",
    ]) {
      assert.notEqual(t(id), id);
      assert.ok(t(id).length > 0);
    }
    assert.equal(hostRecoveryMessage(error, t), t(stored));
    assert.doesNotMatch(hostRecoveryMessage(error, t), /SECRET|arbitrary/);
    assert.equal(
      sessionStateMessage("future-state", t),
      t("workbench.unknown"),
    );
    for (const state of [
      "new",
      "connecting",
      "open",
      "closing",
      "closed",
      "failed",
    ]) {
      assert.notEqual(sessionStateMessage(state, t), t("workbench.unknown"));
    }
    for (const count of [0, 1, 2, 5, 21]) {
      const message = i18n._("workbench.sessionCount", { count });
      assert.match(message, new RegExp(String(count)));
      assert.doesNotMatch(message, /plural|undefined/);
    }
  }
  const i18n = setupI18n({ locale: "en", messages: menuCatalogs });
  const english = i18n._(stored);
  i18n.activate("zh-CN");
  assert.notEqual(
    i18n._(stored),
    english,
    "an existing error follows locale changes",
  );
  assert.equal(
    clientErrorMessageId({ message: "unauthorized" }),
    "recovery.tool_error",
  );
  assert.equal(
    clientErrorMessageId({ code: "__proto__" }),
    "recovery.tool_error",
  );
  assert.equal(clientErrorMessageId(null), "recovery.tool_error");
});

test("only explicit safe SSH reasons select security recovery instructions", () => {
  for (const reason of [
    "ssh_host_key",
    "ssh_authentication",
    "connection_failed",
    "connection_uncertain",
    "diagnostic_unavailable",
  ] as const) {
    const error = publicError(
      new HostError("tool_error", "local diagnostic", reason),
    );
    assert.equal(error.reason, reason);
    assert.equal(clientErrorMessageId(error), `recovery.${reason}`);
  }
  assert.equal(
    clientErrorMessageId(
      publicError(new HostError("busy", "local", "profile_exists")),
    ),
    "recovery.profile_exists",
  );
  assert.equal(
    clientErrorMessageId({ code: "tool_error", reason: "untrusted" }),
    "recovery.tool_error",
  );
  assert.equal(
    clientErrorMessageId({ code: "busy", reason: "ssh_host_key" }),
    "recovery.busy",
  );
});

test("renderer literal translation IDs exist and errors are not rendered from raw messages", async () => {
  const ids = new Set<string>(menuMessages.map((message) => message.id));
  async function inspect(directory: string): Promise<void> {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) await inspect(path);
      else if (entry.name.endsWith(".tsx")) {
        const source = await readFile(path, "utf8");
        for (const match of source.matchAll(/\bt\("([^"\n]+)"\)/g)) {
          assert.ok(
            match[1] && ids.has(match[1]),
            `${path}: missing message ${match[1]}`,
          );
        }
        assert.doesNotMatch(
          source,
          /set(?:Error|Notice|Failure|FileError)\([^;\n]*\.message\)/,
          path,
        );
      }
    }
  }
  await inspect("src/renderer");
});
