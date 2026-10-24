import { spawn } from "node:child_process";
import electron from "electron";

const environment = { ...process.env };
delete environment.ELECTRON_RUN_AS_NODE;
delete environment.CHANNELTERM_GUI_DEV_URL;
const child = spawn(electron, ["."], { stdio: "inherit", env: environment });
process.once("SIGINT", () => child.kill());
process.once("SIGTERM", () => child.kill());
child.once("error", (error) => {
  console.error(error.message);
  process.exitCode = 1;
});
child.once("exit", (code) => {
  process.exitCode = code ?? 0;
});
