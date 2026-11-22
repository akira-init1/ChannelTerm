import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { HostClient } from "../src/main/host/client.ts";
import { startFixture } from "../tests/fixture.ts";

/** Real isolated Hosts deliberately publish the same Session ID and short reference. */
export async function checkDirectory(environment) {
  const local = await startFixture(),
    peer = await startFixture();
  const profile = await mkdtemp(resolve("test-results/directory-"));
  const tokenFile = resolve(profile, "peer.token"),
    file = resolve(profile, "peers.json"),
    preload = resolve(profile, "profile.cjs");
  let desktop;
  const peerObserver = new HostClient(peer.endpoint, peer.token);
  const localObserver = new HostClient(local.endpoint, local.token);
  const agent = new Client({ name: "waiting-agent", version: "test" });
  try {
    await writeFile(tokenFile, peer.token);
    await writeFile(
      file,
      JSON.stringify([
        { id: "lab", label: "Lab", endpoint: peer.endpoint, tokenFile },
      ]),
    );
    await writeFile(
      preload,
      "const {app}=require('electron');app.setPath('userData'," +
        JSON.stringify(profile) +
        ");app.setPath('sessionData'," +
        JSON.stringify(profile) +
        ")",
    );
    const env = {
      ...environment,
      CHANNELTERM_GUI_ENDPOINT: local.endpoint,
      CHANNELTERM_HTTP_AUTH_TOKEN: local.token,
      CHANNELTERM_GUI_PEERS: file,
    };
    delete env.ELECTRON_RUN_AS_NODE;
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    const page = await desktop.firstWindow();
    await page.getByRole("button", { name: /Connect to Host/ }).click();
    await chooseSession(page, undefined, "electron-fixture");
    await expect(page.locator(".xterm-screen")).toBeVisible();
    const native = async (id) => {
      await page.bringToFront();
      await desktop.evaluate(
        ({ Menu }, id) => Menu.getApplicationMenu().getMenuItemById(id).click(),
        id,
      );
    };
    // Use the existing connection menu, save fake COM8, then share the selected Session.
    await native("connection.new");
    const dialog = page.locator(".new-connection-dialog");
    await dialog.locator('[name="port"]').fill("COM8");
    await dialog.locator('[name="label"]').fill("Board");
    await dialog.getByRole("checkbox", { name: "Save connection" }).check();
    await dialog.locator('button[type="submit"]').click();
    await expect(dialog).toHaveCount(0);
    const board = page
      .locator(".terminal-tab")
      .filter({ hasText: "Board" })
      .first();
    await board.getByRole("tab").dblclick();
    const rename = page.getByRole("textbox", { name: "Rename Tab…" });
    await rename.fill("启动日志");
    await rename.press("Enter");
    await native("connection.new");
    await dialog
      .locator("summary")
      .filter({ hasText: "View an existing Session" })
      .click();
    await dialog.getByRole("button", { name: /Board.*COM8/ }).click();
    await expect(page.locator(".terminal-tab")).toHaveCount(3);
    const card = page.locator(".connection-card").filter({ hasText: "COM8" });
    await expect(card).toHaveCount(1);
    await card.locator(".session").click();
    await expect(card.getByRole("button", { name: /启动日志/ })).toBeVisible();
    await card
      .getByRole("button", { name: "Connection settings", exact: true })
      .click();
    await card
      .getByRole("button", { name: "Connection settings", exact: true })
      .last()
      .click();
    const settings = page.getByRole("dialog").filter({
      has: page.getByRole("heading", { name: "Connection settings" }),
    });
    await settings.getByRole("textbox").fill("Development board");
    await settings.getByRole("button", { name: "Save connection" }).click();
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Development board",
    );
    await expect(
      page.locator(".terminal-tab").filter({ hasText: "启动日志" }),
    ).toHaveCount(1);
    const shared = page.locator(".terminal-tab.active");
    await shared.getByRole("tab").dblclick();
    await rename.fill("手动输入");
    await rename.press("Enter");
    await card.getByRole("checkbox").click();
    await expect(card.getByRole("checkbox")).toBeChecked();
    await card.getByRole("checkbox").click();
    await expect(card.getByRole("checkbox")).not.toBeChecked();

    await agent.connect(
      new StreamableHTTPClientTransport(new URL(local.endpoint), {
        requestInit: {
          headers: { Authorization: "Bearer " + local.agentToken },
        },
      }),
    );
    const register = await agent.callTool({
      name: "client_instance",
      arguments: { action: "register", name: "waiting-agent", entry: "test" },
    });
    const meta = {
      "io.channelterm/instance": register.structuredContent.credential,
    };
    const sessions = await agent.callTool({
      name: "terminal_list_sessions",
      arguments: {},
      _meta: meta,
    });
    const target = sessions.structuredContent.sessions.find(
      (s) => s.endpoint === "COM8",
    );
    const call = (args) =>
      agent.callTool({
        name: "terminal_control",
        arguments: { session_id: target.session_id, ...args },
        _meta: meta,
      });
    const started = await call({
      action: "begin",
      version: target.control.version,
    });
    const version = started.structuredContent.control.version,
      operation_id = started.structuredContent.control.operation_id;
    await expect(page.locator(".control-overlay")).toBeVisible();
    await page
      .locator(".control-overlay")
      .getByTestId("control-takeover")
      .click();
    await page
      .getByTestId("control-confirm")
      .getByRole("button", { name: "Take control", exact: true })
      .click();
    const waiting = call({ action: "wait", version, operation_id });
    await page
      .getByRole("button", { name: "Continue AI task", exact: true })
      .click();
    assert.equal((await waiting).structuredContent.wait.status, "allowed");

    const cards = page.locator(".connection-card");
    const status = page.getByRole("combobox", { name: "Connection status" });
    await expect(card.getByRole("img", { name: "Connected" })).toBeVisible();
    await status.selectOption("offline");
    await expect(cards).toHaveCount(0);
    await status.selectOption("online");
    await expect(cards).toHaveCount(2);
    await status.selectOption("all");
    await expect(cards.first()).toContainText("COM8");
    const row = card.locator("button.session");
    await row.dragTo(cards.nth(1).locator("button.session"));
    await expect(cards.last()).toContainText("COM8");
    await row.focus();
    await row.press("Alt+ArrowUp");
    await expect(cards.first()).toContainText("COM8");
    await row.press("Alt+ArrowDown");
    await expect(cards.last()).toContainText("COM8");
    const openedWindow = desktop.waitForEvent("window");
    await native("window.new");
    const second = await openedWindow;
    const sameConnection = second
      .locator(".connection-card")
      .filter({ hasText: "COM8" });
    await expect(sameConnection.locator(".session")).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    await expect(second.locator(".connection-card").last()).toContainText(
      "COM8",
    );
    await sameConnection.locator(".session").click();
    await sameConnection.getByRole("button", { name: /启动日志/ }).click();
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "启动日志",
    );
    await expect(page.locator(".terminal-tab")).toHaveCount(3);
    const localTabId = await second.evaluate(async () => {
      const result = await window.channelterm.openedTabs();
      return result.value.find((row) => row.label === "启动日志").id;
    });
    await second.close();
    await native("language.zh-CN");
    await expect(page.locator(".terminal-directory:visible h2")).toHaveText(
      "会话",
    );
    await page.screenshot({ path: "test-results/session-rail-zh.png" });
    await native("language.en");
    await expect(
      page.locator(".terminal-directory").filter({ hasText: "Lab" }),
    ).not.toBeVisible();
    await page.getByRole("button", { name: "LAN", exact: true }).click();
    await expect(card).not.toBeVisible();
    const group = page
      .locator(".terminal-directory details")
      .filter({ has: page.locator("summary").filter({ hasText: /^Lab$/ }) });
    await expect(group).toContainText("SER-1");
    const lanSearch = page.getByRole("searchbox");
    await lanSearch.fill("unknown-host-x");
    await expect(group).toHaveCount(0);
    await lanSearch.fill("lab");
    await expect(group).toContainText("SER-1");
    await lanSearch.fill("SER-1");
    await expect(group.getByRole("button", { name: /SER-1/ })).toBeVisible();
    await lanSearch.fill("");
    await status.selectOption("offline");
    await expect(group).toHaveCount(0);
    await status.selectOption("all");
    await native("language.zh-CN");
    await expect(page.locator(".terminal-directory:visible h2")).toHaveText(
      "局域网",
    );
    await page.screenshot({ path: "test-results/lan-rail-zh.png" });
    await native("language.en");
    await peerObserver.connect();
    const ssh = await peerObserver.openConnection({
      kind: "ssh",
      host: "127.0.0.1",
      user: "fixture-user",
      port: peer.sshPort,
      label: "Remote SSH",
      knownHostsFile: peer.knownHosts,
      identityFiles: [],
      password: "fixture-password",
    });
    await page.getByRole("button", { name: "LAN · Refresh Sessions" }).click();
    const sshRow = group.getByRole("button", { name: /Remote SSH/ });
    await expect(
      sshRow.getByRole("img", { name: "SSH", exact: true }),
    ).toBeVisible();
    await group.screenshot({
      path: "test-results/directory-lan-transports.png",
    });
    const sshWindow = desktop.waitForEvent("window");
    await sshRow.dblclick();
    const remoteShell = await sshWindow;
    await expect(remoteShell.locator(".xterm-rows:visible")).toContainText(
      "SSH fixture ready",
    );
    assert.equal(
      (await peerObserver.list()).filter((s) => s.transport === "ssh")[0]
        .session_id,
      ssh.sessionId,
    );
    await remoteShell.close();
    await page.bringToFront();
    const serialRow = group.getByRole("button", { name: /SER-1/ });
    await serialRow.click();
    await expect(serialRow).toHaveAttribute("aria-pressed", "true");
    assert.equal(desktop.windows().length, 1);
    await expect(
      serialRow.getByRole("img", { name: "Serial", exact: true }),
    ).toBeVisible();
    const newWindow = desktop.waitForEvent("window");
    await serialRow.dblclick();
    const remote = await newWindow;
    await expect(remote.locator(".xterm-screen")).toBeVisible();
    await peer.command("output", "\r\nremote-only\r\n");
    await expect(remote.locator(".xterm-rows:visible")).toContainText(
      "remote-only",
    );
    await expect(page.locator(".xterm-rows:visible")).not.toContainText(
      "remote-only",
    );
    await expect(
      remote.locator(".terminal-directory:visible"),
    ).not.toContainText("COM8");
    const denied = await remote.evaluate(
      (id) => window.channelterm.focusTab(id),
      localTabId,
    );
    assert.equal(denied.ok, false);
    assert.equal(denied.error.code, "closed");
    await page.bringToFront();
    await writeFile(tokenFile, "invalid-disposable-token");
    await page.getByRole("button", { name: "LAN · Refresh Sessions" }).click();
    await expect(group.getByRole("status")).toBeVisible();
    await status.selectOption("offline");
    await expect(
      group.getByRole("img", { name: "Disconnected" }),
    ).toBeVisible();
    await expect(group.getByRole("button")).toHaveCount(0);
    await status.selectOption("all");
    await writeFile(tokenFile, peer.token);
    await page.getByRole("button", { name: "LAN · Refresh Sessions" }).click();
    await expect(group.getByRole("button", { name: /SER-1/ })).toBeEnabled();
    await peer.command("close");
    await page.getByRole("button", { name: "LAN · Refresh Sessions" }).click();
    await status.selectOption("offline");
    const closedPeer = group.getByRole("button", { name: /SER-1/ });
    await expect(closedPeer).toBeDisabled();
    await expect(
      closedPeer.getByRole("img", { name: "Disconnected" }),
    ).toBeVisible();
    await status.selectOption("all");
    await page.getByRole("button", { name: "Toggle Session sidebar" }).click();
    await peer.stop();
    await page.waitForTimeout(5500); // A full polling period with LAN hidden.
    await expect(group.locator('[role="status"]')).toHaveCount(0);
    await page.getByRole("button", { name: "LAN", exact: true }).click();
    await expect(group.getByRole("status")).toBeVisible({ timeout: 20000 });
    await page.getByRole("button", { name: "Toggle Session sidebar" }).click();
    await page.getByRole("searchbox").fill("COM8");
    await expect(card).toHaveCount(1);
    await expect(page.locator(".connection-card")).toHaveCount(1);
    await page.getByRole("searchbox").fill("");
    await expect(cards.last()).toContainText("★");
    await local.command("close");
    await status.selectOption("offline");
    await expect(cards).toHaveCount(1);
    await expect(
      cards.first().getByRole("img", { name: "Disconnected" }),
    ).toBeVisible();
    await status.selectOption("online");
    await expect(cards).toHaveCount(1);
    await expect(card).toBeVisible();
    await status.selectOption("all");
    await native("language.zh-CN");
    const directory = page.locator(".terminal-directory:visible");
    await expect(directory.getByRole("img", { name: "已断开" })).toBeVisible();
    await directory.screenshot({
      path: "test-results/session-order-status-zh.png",
    });
    await native("language.en");
    const boardTabs = page
      .locator(".terminal-tab")
      .filter({ hasText: /启动日志|手动输入/ });
    for (let i = 0; i < 2; i++)
      await boardTabs
        .first()
        .getByRole("button", { name: "Close tab (detach)" })
        .click();
    await expect(boardTabs).toHaveCount(0);
    await localObserver.connect();
    const oldBoard = (await localObserver.list()).find(
      (s) => s.endpoint === "COM8" && s.state === "open",
    );
    const currentRows = page.locator(".terminal-tab");
    const previousCount = await currentRows.count();
    await card.locator(".session").click();
    await expect(dialog).toHaveCount(0);
    await expect(currentRows).toHaveCount(previousCount);
    await card.locator(".session").dblclick();
    await expect(page.locator(".terminal-tab.active")).toContainText("COM8");
    await expect(currentRows).toHaveCount(previousCount + 1);
    await card.locator(".session").dblclick();
    await expect(currentRows).toHaveCount(previousCount + 1);
    const handle = await localObserver.attach(oldBoard.session_id);
    await localObserver.closeSession(handle.id);
    await expect(card.getByRole("img", { name: "Disconnected" })).toBeVisible();
    await native("host.disconnect");
    await status.selectOption("offline");
    await expect(card.getByRole("img", { name: "Disconnected" })).toBeVisible();
    await status.selectOption("online");
    await expect(cards).toHaveCount(0);
    await status.selectOption("all");
    await card.locator(".session").dblclick();
    await expect(page.locator(".terminal-tab.active")).toContainText("COM8");
    await expect(card.getByRole("img", { name: "Connected" })).toBeVisible();
    await expect(
      card.getByRole("img", { name: "Serial", exact: true }),
    ).toBeVisible();
    await expect(currentRows).toHaveCount(1);
    await expect(dialog).toHaveCount(0);
    const reopened = (await localObserver.list()).filter(
      (s) => s.endpoint === "COM8" && s.state === "open",
    );
    assert.equal(reopened.length, 1);
    assert.notEqual(reopened[0].session_id, oldBoard.session_id);
    await desktop.close();
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    const restarted = await desktop.firstWindow();
    await expect(restarted.locator(".connection-card")).toHaveCount(2);
    await expect(restarted.locator(".connection-card").last()).toContainText(
      "COM8",
    );
    const connectHost = restarted.getByRole("button", {
      name: "Connect to Host...",
    });
    if (await connectHost.isVisible()) await connectHost.click();
    const localShell = await localObserver.openConnection({
      kind: "ssh",
      host: "127.0.0.1",
      port: local.sshPort,
      user: "fixture-user",
      label: "Build SSH",
      identityFiles: [],
      knownHostsFile: local.knownHosts,
      password: "fixture-password",
    });
    await chooseSession(restarted, undefined, localShell.sessionId);
    const shellCard = restarted
      .locator(".connection-card")
      .filter({ hasText: "Build SSH" });
    await expect(
      shellCard.getByRole("img", { name: "SSH", exact: true }),
    ).toBeVisible();
    await restarted
      .locator(".terminal-tab.active")
      .getByRole("button", { name: "Close tab (detach)" })
      .click();
    await shellCard.locator("button.session").dblclick();
    await expect(restarted.locator(".terminal-tab.active")).toContainText(
      "Build SSH",
    );
    await expect(restarted.locator(".xterm-rows:visible")).toContainText(
      "SSH fixture ready",
    );
    assert.deepEqual(
      (await localObserver.list())
        .filter((s) => s.transport === "ssh")
        .map((s) => s.session_id),
      [localShell.sessionId],
    );
    await restarted
      .locator(".terminal-directory:visible")
      .screenshot({ path: "test-results/directory-session-transports.png" });
  } finally {
    await peerObserver.disconnect();
    await localObserver.disconnect();
    await agent.close();
    await desktop?.close();
    await local.stop();
    await peer.stop();
  }
}
