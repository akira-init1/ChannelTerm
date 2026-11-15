async function verifyStatusTraces(traces) {
  const assert = (value, message) => {
    if (!value) throw Error(message);
  };
  const results = [];
  for (const [name, steps] of Object.entries(traces)) {
    const terminal = new Terminal({
      cols: 40,
      rows: 6,
      allowProposedApi: true,
      scrollback: 100,
    });
    terminal.open(document.getElementById("terminal"));
    const write = (data) =>
      new Promise((resolve) => terminal.write(data, resolve));
    const line = (n) =>
      terminal.buffer.active
        .getLine(terminal.buffer.active.baseY + n)
        ?.translateToString(true) || "";
    const cursor = () =>
      [terminal.buffer.active.cursorX, terminal.buffer.active.cursorY].join(
        ",",
      );
    for (let i = 0; i < steps.length; i++) {
      const step = steps[i],
        before = cursor();
      if (step.resize) terminal.resize(60, 10);
      await write(step.data);
      if (step.update) {
        assert(cursor() === before, name + ": status moved typing cursor");
        assert(
          line(5).startsWith("board | user"),
          name + ": footer overwritten",
        );
      }
      if (i === steps.length - 2) {
        if (name === "line") {
          assert(
            line(4) === "final> 中文",
            "line wrap/Unicode mismatch: " + line(4),
          );
          assert(line(5).startsWith("board | user"), "scroll overwrote footer");
        }
        if (name === "wrap") {
          assert(
            line(4) === "x".repeat(40),
            "long command corrupted: " + line(4),
          );
          assert(line(5) === "Y", "wrap-pending cursor changed: " + line(5));
        }
        if (name === "clear")
          assert(line(0) === "clear-ok", "clear-screen bytes were changed");
        if (name === "absolute")
          assert(line(5) === "absolute-ok", "absolute cursor still restricted");
      }
      if (name === "alternate" && step.data.includes("alternate-ok")) {
        assert(
          terminal.buffer.active.type === "alternate",
          "alternate screen unavailable",
        );
        assert(line(0) === "alternate-ok", "alternate payload changed");
      }
    }
    // Full-screen scrolling must work again after cleanup, including repeated exit.
    await write("\u001b[6;1H\r\nRESTORED");
    assert(
      terminal.buffer.active.type === "normal",
      name + ": left alternate screen",
    );
    if (name !== "resize")
      assert(line(5) === "RESTORED", name + ": scrolling region not restored");
    results.push(name + " PASS");
    terminal.dispose();
    document.getElementById("terminal").replaceChildren();
  }
  return results;
}

// Optional native xterm display verification; normal Go/CLI builds do not need Electron.
const { app, BrowserWindow } = require("electron");
const { readFileSync } = require("node:fs");
const path = require("node:path");
app
  .whenReady()
  .then(async () => {
    const win = new BrowserWindow({
      show: false,
      webPreferences: {
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
      },
    });
    try {
      await win.loadURL('data:text/html,<div id="terminal"></div>');
      await win.webContents.executeJavaScript(
        readFileSync(
          path.resolve(
            __dirname,
            "../gui/electron/node_modules/@xterm/xterm/lib/xterm.js",
          ),
          "utf8",
        ),
      );
      const traces = JSON.parse(readFileSync(process.argv[2], "utf8"));
      const result = await win.webContents.executeJavaScript(
        `(${verifyStatusTraces.toString()})(${JSON.stringify(traces)}).catch(error => ({error: String(error.stack)}))`,
      );
      if (!Array.isArray(result)) throw Error(JSON.stringify(result));
      console.log(result.join("\n"));
    } finally {
      win.destroy();
    }
    app.quit();
  })
  .catch((error) => {
    console.error(error);
    app.exit(1);
  });
