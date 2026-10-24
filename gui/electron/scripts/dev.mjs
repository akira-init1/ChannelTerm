import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import electron from "electron";
import { createServer } from "vite";
import { buildProcesses } from "./build.mjs";
import { prepareHost } from "./build-host.mjs";

await prepareHost();
await buildProcesses({ strictCatalogs: false });
const nonce = randomBytes(16).toString("hex");
const server = await createServer({ html: { cspNonce: nonce } });
await server.listen();
const environment = {
  ...process.env,
  CHANNELTERM_GUI_DEV_URL: "http://127.0.0.1:5173",
  CHANNELTERM_GUI_DEV_NONCE: nonce,
};
delete environment.ELECTRON_RUN_AS_NODE;
const child = spawn(electron, ["."], {
  stdio: "inherit",
  env: environment,
});
const stop = async () => {
  child.kill();
  await server.close();
};
process.once("SIGINT", stop);
process.once("SIGTERM", stop);
child.once("error", async (error) => {
  console.error(error.message);
  await stop();
  process.exitCode = 1;
});
child.once("exit", async (code) => {
  await server.close();
  process.exitCode = code ?? 0;
});
