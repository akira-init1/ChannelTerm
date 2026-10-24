import test from "node:test";
import assert from "node:assert/strict";
import {
  LocalHostStartup,
  companionPath,
  defaultHostEndpoint,
} from "../src/main/host/startup";
import { HostError } from "../src/main/host/errors";

test("startup reuses occupied listeners and never starts for custom endpoints", async () => {
  let probes = 0;
  const startup = new LocalHostStartup({
    listening: async () => {
      probes++;
      return true;
    },
    launch: async () => {
      assert.fail("must not start a second Host");
    },
    pause: async () => {
      assert.fail("must not wait");
    },
  });
  const signal = new AbortController().signal;
  await startup.ensure(defaultHostEndpoint, signal);
  for (const endpoint of [
    "http://127.0.0.1:37100/mcp",
    "http://127.0.0.1:37099/other",
    "http://[::1]:37099/mcp",
  ])
    await startup.ensure(endpoint, signal);
  assert.equal(probes, 1);
  assert.match(
    companionPath(process.cwd(), "win32"),
    /host[\\/]channelterm.exe$/,
  );
  assert.match(companionPath(process.cwd(), "linux"), /host[\\/]channelterm$/);
});

test("startup launches once, waits for readiness, and cancellation cannot spawn", async () => {
  let probes = 0,
    launches = 0;
  const startup = new LocalHostStartup({
    listening: async () => ++probes >= 3,
    launch: async () => {
      launches++;
    },
    pause: async () => {},
  });
  const controller = new AbortController();
  await startup.ensure(defaultHostEndpoint, controller.signal);
  await startup.ensure(defaultHostEndpoint, controller.signal);
  assert.equal(launches, 1);
  controller.abort();
  await assert.rejects(startup.ensure(defaultHostEndpoint, controller.signal));
  assert.equal(launches, 1);
});

test("listener failures fail closed; startup timeouts do not replay process creation", async () => {
  let launches = 0;
  const controller = new AbortController();
  const blocked = new LocalHostStartup({
    listening: async () => {
      throw new HostError("disconnected", "timeout");
    },
    launch: async () => {
      launches++;
    },
    pause: async () => {},
  });
  await assert.rejects(blocked.ensure(defaultHostEndpoint, controller.signal));
  assert.equal(launches, 0);
  const slow = new LocalHostStartup({
    listening: async () => false,
    launch: async () => {
      launches++;
    },
    pause: async () => {},
  });
  for (let i = 0; i < 2; i++)
    await assert.rejects(
      slow.ensure(defaultHostEndpoint, controller.signal),
      /did not become ready/,
    );
  assert.equal(launches, 1);
});

test("known process exit and launch failure allow an explicit retry", async () => {
  let launches = 0;
  const startup = new LocalHostStartup({
    listening: async () => false,
    launch: async (onExit) => {
      launches++;
      onExit();
    },
    pause: async () => {},
  });
  for (let i = 0; i < 2; i++)
    await assert.rejects(
      startup.ensure(defaultHostEndpoint, new AbortController().signal),
      /exited during startup/,
    );
  assert.equal(launches, 2);
  const missing = new LocalHostStartup({
    listening: async () => false,
    launch: async () => {
      launches++;
      throw new HostError("configuration", "missing executable");
    },
    pause: async () => {},
  });
  for (let i = 0; i < 2; i++)
    await assert.rejects(
      missing.ensure(defaultHostEndpoint, new AbortController().signal),
      /missing executable/,
    );
  assert.equal(launches, 4);
});

test("cancellation during probing or readiness waiting never launches another Host", async () => {
  for (const duringProbe of [true, false]) {
    const controller = new AbortController();
    let launches = 0;
    const startup = new LocalHostStartup({
      listening: async () => {
        if (duringProbe) controller.abort();
        return false;
      },
      launch: async () => {
        launches++;
      },
      pause: async () => {
        controller.abort();
      },
    });
    await assert.rejects(
      startup.ensure(defaultHostEndpoint, controller.signal),
    );
    assert.equal(launches, duringProbe ? 0 : 1);
  }
});
