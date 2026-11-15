import test from "node:test";
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";
import { DesktopUBoot } from "../src/main/uboot";
import { isUBootRequest, type UBootRequest } from "../src/shared/uboot";
import {
  ubootArguments,
  ubootOutcome,
  UBootEventFeed,
  ubootHTTPCall,
} from "../src/main/host/uboot";

const request: UBootRequest = {
  mode: "wait_reset",
  rebootCommand: "",
  verifiedIdleBash: false,
  countdown: "Autoboot in ",
  prompt: "Board> ",
  bootStarted: ["Starting kernel"],
  interruptHex: "20",
  timeoutMS: 60000,
  capture: false,
};
const session = "opaque-session",
  instance = "CI-" + "a".repeat(20),
  use = "CU-" + "b".repeat(20),
  operation = "BOOT-" + "c".repeat(32);
const ready = (id: number) => ({
  id,
  session_id: session,
  type: "OPERATION_STATE",
  client: { instance_id: instance, operation_id: use },
  metadata: {
    operation: "uboot",
    status: "ready",
    operation_id: operation,
    output_cursor: 123,
  },
});
const result = {
  operation_id: operation,
  session_id: session,
  status: "entered",
  output_start: 123,
  output_end: 155,
  countdown_cursor: 123,
  prompt_cursor: 148,
  countdown_matched: true,
  prompt_matched: true,
  bytes_written: 1,
  elapsed_ms: 75,
  dropped: false,
};

test("U-Boot HTTP cancellation binding cannot change unrelated request deadlines", () => {
  const pending = {
    useId: use,
    signal: new AbortController().signal,
    timeoutMS: 120000,
  };
  const envelope = {
    method: "tools/call",
    params: {
      name: "terminal_enter_uboot",
      _meta: { "io.channelterm/instance": { use_id: use } },
    },
  };
  assert.equal(ubootHTTPCall(JSON.stringify(envelope), pending), pending);
  assert.equal(ubootHTTPCall("bad json", pending), undefined);
  assert.equal(ubootHTTPCall(JSON.stringify(envelope), undefined), undefined);
  assert.equal(
    ubootHTTPCall(
      JSON.stringify({
        ...envelope,
        params: { ...envelope.params, name: "terminal_read" },
      }),
      pending,
    ),
    undefined,
  );
  assert.equal(
    ubootHTTPCall(JSON.stringify(envelope), { ...pending, useId: "other" }),
    undefined,
  );
});

test("U-Boot window jobs fence cancellation and late readiness without leaking protocol errors", async () => {
  const jobs = new DesktopUBoot();
  let observed: AbortSignal | undefined;
  let ready: ((id: string | undefined) => void) | undefined;
  let reject: ((error: Error) => void) | undefined;
  const pending = new Promise<never>((_resolve, fail) => {
    reject = fail;
  });
  const prepared = {
    sessionId: session,
    run: (signal: AbortSignal, report: (id: string | undefined) => void) => {
      observed = signal;
      ready = report;
      return pending;
    },
  };
  const first = jobs.start("attachment-one", prepared);
  assert.equal(jobs.busy, true);
  assert.throws(() => jobs.start("attachment-two", prepared), { code: "busy" });
  ready!(operation);
  assert.equal(jobs.snapshot()!.state, "ready");
  assert.throws(() => jobs.cancel("foreign-job"), { code: "cancelled" });
  assert.equal(observed!.aborted, false);
  jobs.cancel(first.id);
  jobs.cancel(first.id);
  assert.equal(observed!.aborted, true);
  ready!(operation);
  assert.equal(jobs.snapshot()!.state, "stopping");
  assert.equal(jobs.snapshot()!.operationId, undefined);
  reject!(new Error("private-host-detail"));
  await delay(0);
  assert.equal(jobs.snapshot()!.state, "finished");
  assert.equal(jobs.busy, false);
  assert.equal(
    JSON.stringify(jobs.snapshot()).includes("private-host-detail"),
    false,
  );
  const copy = jobs.snapshot()!;
  copy.state = "ready";
  assert.equal(jobs.snapshot()!.state, "finished");
});

test("U-Boot refuses a Host without the explicit HTTP cancellation capability before submitting any operation", async () => {
  const fixture = await startFixture(),
    client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async (input, init) => {
    const body =
      typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    if (body?.method === "tools/list")
      return new Response(
        JSON.stringify({ jsonrpc: "2.0", id: body.id, result: { tools: [] } }),
        { headers: { "Content-Type": "application/json" } },
      );
    if (body?.params?.name === "terminal_enter_uboot") calls++;
    return original(input, init);
  };
  try {
    assert.equal((await client.connect()).ubootAvailable, false);
    assert.throws(() => client.prepareUBoot("not-an-attachment", request), {
      code: "incompatible",
    });
    assert.equal(calls, 0);
  } finally {
    await client.disconnect();
    await fixture.stop();
    globalThis.fetch = original;
  }
});

test(
  "U-Boot can wait beyond the ordinary ten-second HTTP budget",
  { timeout: 15000 },
  async () => {
    const fixture = await startFixture(),
      client = new HostClient(fixture.endpoint, fixture.token);
    try {
      await client.connect();
      const attached = await client.attach(
        (await client.list())[0]!.session_id,
      );
      const result = await client
        .prepareUBoot(attached.id, { ...request, timeoutMS: 10500 })
        .run(new AbortController().signal, () => {});
      assert.equal(result.status, "timeout");
      assert.ok(result.elapsedMS >= 10000);
      assert.equal(result.bytesWritten, 0);
      assert.equal((await client.list())[0]!.lease, undefined);
    } finally {
      await client.disconnect();
      await fixture.stop();
    }
  },
);

test("U-Boot requires exact verified board settings and explicit idle Bash for reboot", () => {
  assert.ok(isUBootRequest(request));
  assert.ok(
    isUBootRequest({
      ...request,
      mode: "reboot",
      rebootCommand: "reboot",
      verifiedIdleBash: true,
    }),
  );
  for (const change of [
    { countdown: "" },
    { prompt: "x\ny" },
    { prompt: "\uD800" },
    { countdown: "中".repeat(86) },
    { bootStarted: [] },
    { bootStarted: Array(9).fill("x") },
    { bootStarted: [null] },
    { interruptHex: "2" },
    { interruptHex: "20 20" },
    { interruptHex: "20".repeat(33) },
    { timeoutMS: 0 },
    { timeoutMS: 180001 },
    { timeoutMS: 1.1 },
    { capture: "yes" },
    { unexpected: true },
    { rebootCommand: "reboot" },
    { verifiedIdleBash: true },
    { mode: "reboot" },
    { mode: "reboot", rebootCommand: "reboot\n", verifiedIdleBash: true },
  ])
    assert.equal(
      isUBootRequest({ ...request, ...change }),
      false,
      JSON.stringify(change),
    );
  const args = ubootArguments(session, request);
  assert.equal(args.prompt_pattern, "Board> ");
  assert.equal(args.interrupt_data_hex, "20");
  assert.equal(args.diagnostics, "metadata");
  assert.equal("reboot_command" in args, false);
  assert.notEqual(args.boot_started_patterns, request.bootStarted);
});

test("U-Boot keeps partial failure evidence without treating writes as success or forwarding internals", () => {
  const success = ubootOutcome(result, session, false);
  assert.equal(success.status, "entered");
  const failure = ubootOutcome(
    {
      ...result,
      status: "timeout",
      prompt_matched: false,
      prompt_cursor: 0,
      diagnostic_id: "DIAG-" + "d".repeat(32),
      diagnostic_error: "sensitive internal detail",
      extra: "secret",
    },
    session,
    true,
  );
  assert.equal(failure.bytesWritten, 1);
  assert.equal(failure.diagnosticUnavailable, true);
  assert.equal(JSON.stringify(failure).includes("sensitive"), false);
  assert.equal("extra" in failure, false);
  for (const change of [
    { status: "invented" },
    { prompt_matched: false },
    { dropped: true },
    { output_end: 100 },
    { countdown_cursor: 122 },
    { prompt_cursor: 156 },
    { bytes_written: -1 },
    { elapsed_ms: 0.1 },
    { session_id: "other" },
    { diagnostic_id: "path/to/archive" },
  ])
    assert.throws(() => ubootOutcome({ ...result, ...change }, session, false));
  assert.throws(() => ubootOutcome(result, session, true));
  assert.throws(() =>
    ubootOutcome({ ...result, status: "timeout" }, session, false),
  );
});

test("U-Boot readiness is fenced by fresh cursor, Session, authenticated instance and use", () => {
  const feed = new UBootEventFeed(session, instance, use, 10);
  const other = ready(10);
  other.client.operation_id = "CU-" + "z".repeat(20);
  assert.equal(
    feed.apply({ events: [other], next: 11, dropped: false }).ready,
    false,
  );
  const foreign = ready(11);
  foreign.client.instance_id = "CI-" + "z".repeat(20);
  assert.equal(
    feed.apply({ events: [foreign], next: 12, dropped: false }).ready,
    false,
  );
  assert.equal(
    feed.apply({ events: [ready(12)], next: 13, dropped: false }).ready,
    true,
  );
  const end = ready(13);
  end.metadata.status = "cancelled";
  assert.deepEqual(feed.apply({ events: [end], next: 14, dropped: false }), {
    ready: false,
    operationId: operation,
    finished: true,
  });
  assert.throws(() =>
    feed.apply({ events: [ready(14)], next: 15, dropped: false }),
  );
  assert.equal(feed.parameters().cursor, 14);
});

test("U-Boot rejects dropped, oversized and malformed pages atomically instead of inviting reset", () => {
  const feed = new UBootEventFeed(session, instance, use, 10);
  for (const page of [
    { events: [ready(9)], next: 10, dropped: false },
    { events: [ready(11)], next: 12, dropped: true },
    { events: [ready(10)], next: 12, dropped: false },
    {
      events: [ready(10), { ...ready(11), session_id: "other" }],
      next: 12,
      dropped: false,
    },
    {
      events: Array.from({ length: 33 }, (_, i) => ready(10 + i)),
      next: 43,
      dropped: false,
    },
  ]) {
    assert.throws(() => feed.apply(page));
    assert.equal(feed.parameters().cursor, 10);
  }
  const end = ready(10);
  end.metadata.status = "input_pending";
  assert.equal(
    feed.apply({ events: [end], next: 11, dropped: false }).ready,
    false,
  );
});

async function until(check: () => Promise<boolean>) {
  for (const deadline = Date.now() + 2000; Date.now() < deadline;) {
    if (await check()) return;
    await delay(20);
  }
  assert.fail("Expected isolated Host state within two seconds");
}

test(
  "U-Boot real Host confirms fresh evidence, exact interrupt once and diagnostic receipt",
  { timeout: 10000 },
  async () => {
    const fixture = await startFixture();
    const client = new HostClient(fixture.endpoint, fixture.token),
      observer = new HostClient(fixture.endpoint, fixture.token);
    const controller = new AbortController();
    let pending: Promise<unknown> | undefined;
    try {
      await client.connect();
      await observer.connect();
      const id = (await client.list())[0]!.session_id;
      const attached = await client.attach(id),
        observed = await observer.attach(id);
      await fixture.command("output", "Autoboot in 0\r\nBoard> ");
      await observer.read(observed.id);
      let readyId: string | undefined;
      const prepared = client.prepareUBoot(attached.id, {
        ...request,
        interruptHex: "21",
        timeoutMS: 3000,
        capture: true,
      });
      const result = prepared.run(controller.signal, (id) => {
        readyId = id;
      });
      pending = result;
      void result.catch(() => {});
      await until(async () => !!readyId);
      assert.match(readyId!, /^BOOT-/);
      await assert.rejects(
        prepared.run(controller.signal, () => {}),
        { code: "cancelled" },
      );
      await assert.rejects(
        client
          .prepareUBoot(attached.id, request)
          .run(controller.signal, () => {}),
        { code: "busy" },
      );
      await fixture.command("output", "\r\nAutoboot in 3\r\n");
      let received = "";
      await until(async () => {
        received += Buffer.from(
          (await observer.read(observed.id)).data,
        ).toString("utf8");
        return received.includes("!");
      });
      await fixture.command("output", "\r\nBoard> ");
      const outcome = await result;
      assert.equal(outcome.status, "entered");
      assert.equal(outcome.bytesWritten, 1);
      assert.equal(outcome.promptMatched, true);
      assert.match(outcome.diagnosticId!, /^DIAG-/);
      assert.equal(readyId, undefined);
      assert.equal((await observer.list())[0]!.state, "open");
    } finally {
      controller.abort();
      await pending?.catch(() => {});
      await client.disconnect();
      await observer.disconnect();
      await fixture.stop();
    }
  },
);

test(
  "U-Boot cancellation and stale attachment discard do not send recovery keys or close shared Session",
  { timeout: 10000 },
  async () => {
    const fixture = await startFixture();
    const client = new HostClient(fixture.endpoint, fixture.token),
      observer = new HostClient(fixture.endpoint, fixture.token);
    const controller = new AbortController();
    let pending: Promise<unknown> | undefined;
    try {
      await client.connect();
      await observer.connect();
      const id = (await client.list())[0]!.session_id;
      const attached = await client.attach(id),
        observed = await observer.attach(id);
      const old = client.prepareUBoot(attached.id, request);
      await client.detach(attached.id);
      await assert.rejects(
        old.run(controller.signal, () => {}),
        { code: "cancelled" },
      );
      const fresh = await client.attach(id);
      await observer.read(observed.id);
      let ready = false;
      const result = client
        .prepareUBoot(fresh.id, request)
        .run(controller.signal, (id) => {
          ready = !!id;
        });
      pending = result;
      void result.catch(() => {});
      await until(async () => ready);
      controller.abort();
      await assert.rejects(result, { code: "cancelled" });
      await until(async () => !(await observer.list())[0]!.lease);
      assert.equal(ready, false);
      assert.deepEqual((await observer.read(observed.id)).data, []);
      assert.equal((await observer.list())[0]!.state, "open");
    } finally {
      controller.abort();
      await pending?.catch(() => {});
      await client.disconnect();
      await observer.disconnect();
      await fixture.stop();
    }
  },
);

test(
  "U-Boot reports timeout and boot-start failure as partial Host results",
  { timeout: 10000 },
  async () => {
    const fixture = await startFixture();
    const client = new HostClient(fixture.endpoint, fixture.token);
    const controller = new AbortController();
    let pending: Promise<unknown> | undefined;
    try {
      await client.connect();
      const id = (await client.list())[0]!.session_id,
        attached = await client.attach(id);
      const timeout = await client
        .prepareUBoot(attached.id, { ...request, timeoutMS: 150 })
        .run(controller.signal, () => {});
      assert.equal(timeout.status, "timeout");
      assert.equal(timeout.bytesWritten, 0);
      await client.list();
      let ready = false;
      const result = client
        .prepareUBoot(attached.id, { ...request, timeoutMS: 3000 })
        .run(controller.signal, (id) => {
          ready = !!id;
        });
      pending = result;
      void result.catch(() => {});
      await until(async () => ready);
      await fixture.command(
        "output",
        "Autoboot in 0\r\nStarting kernel\r\nBoard> ",
      );
      const failed = await result;
      assert.equal(failed.status, "boot_started");
      assert.equal(failed.bytesWritten, 0);
    } finally {
      controller.abort();
      await pending?.catch(() => {});
      await client.disconnect();
      await fixture.stop();
    }
  },
);

test(
  "U-Boot detach and disconnect cancel pending owned work and permit explicit subsequent use",
  { timeout: 15000 },
  async () => {
    for (const action of ["detach", "disconnect"] as const) {
      const fixture = await startFixture();
      const client = new HostClient(fixture.endpoint, fixture.token),
        observer = new HostClient(fixture.endpoint, fixture.token);
      const controller = new AbortController();
      let pending: Promise<unknown> | undefined;
      try {
        await client.connect();
        await observer.connect();
        const id = (await client.list())[0]!.session_id;
        const attached = await client.attach(id),
          watched = await observer.attach(id);
        await observer.read(watched.id);
        let armed = false;
        pending = client
          .prepareUBoot(attached.id, request)
          .run(controller.signal, (id) => {
            armed = !!id;
          });
        void pending.catch(() => {});
        await until(async () => armed);
        if (action === "detach") await client.detach(attached.id);
        else await client.disconnect();
        await assert.rejects(pending, { code: "cancelled" });
        await until(async () => !(await observer.list())[0]!.lease);
        assert.equal(armed, false);
        assert.deepEqual((await observer.read(watched.id)).data, []);
        assert.equal((await observer.list())[0]!.state, "open");
        if (action === "disconnect") await client.connect();
        const fresh = await client.attach(id);
        const next = await client
          .prepareUBoot(fresh.id, { ...request, timeoutMS: 100 })
          .run(controller.signal, () => {});
        assert.equal(next.status, "timeout");
        assert.equal(next.bytesWritten, 0);
      } finally {
        controller.abort();
        await pending?.catch(() => {});
        await client.disconnect();
        await observer.disconnect();
        await fixture.stop();
      }
    }
  },
);

test(
  "U-Boot human takeover reports conservative Session closure instead of claiming restored shell control",
  { timeout: 10000 },
  async () => {
    const fixture = await startFixture();
    const owner = new HostClient(fixture.endpoint, fixture.token),
      human = new HostClient(fixture.endpoint, fixture.token);
    const controller = new AbortController();
    let pending: Promise<unknown> | undefined;
    try {
      await owner.connect();
      await human.connect();
      const id = (await owner.list())[0]!.session_id;
      const attachment = await owner.attach(id);
      let ready: string | undefined;
      const attempt = owner
        .prepareUBoot(attachment.id, { ...request, timeoutMS: 5000 })
        .run(controller.signal, (id) => {
          ready = id;
        });
      // Closing the captured Session can end its event observer before the tool receipt arrives.
      const receipt = attempt.then(
        (outcome) => ({ outcome }),
        (error: unknown) => ({ error }),
      );
      pending = receipt;
      await until(async () => !!ready);
      const control = (await human.list())[0]!.control!;
      const key = {
        session_id: id,
        version: control.version,
        operation_id: control.operation_id,
      };
      let takeover = await human.control({ ...key, action: "takeover" });
      await until(async () => {
        if ("result" in takeover && takeover.result.status !== "stopping")
          return true;
        takeover = await human.control({ ...key, action: "result" });
        return false;
      });
      assert.ok("result" in takeover);
      assert.equal(takeover.result.status, "failed");
      assert.equal(takeover.result.recovery, "session_closed");
      assert.equal(takeover.result.retry_allowed, false);
      const completed = await receipt;
      if ("outcome" in completed) {
        assert.ok(
          ["failed", "session_closed", "cancelled"].includes(
            completed.outcome.status,
          ),
        );
        assert.equal(completed.outcome.bytesWritten, 0);
      } else {
        assert.ok(
          completed.error instanceof Error && "code" in completed.error,
        );
        assert.ok(
          typeof completed.error.code === "string" &&
            ["cancelled", "closed", "tool_error"].includes(
              completed.error.code,
            ),
        );
      }
      assert.equal(ready, undefined);
      assert.notEqual(
        (await human.list()).find((row) => row.session_id === id)?.state,
        "open",
      );
      const original = await human.control({ ...key, action: "result" });
      assert.ok("result" in original);
      assert.equal(original.result.version, key.version);
      assert.equal(original.result.recovery, "session_closed");
    } finally {
      controller.abort();
      await pending?.catch(() => {});
      await owner.disconnect();
      await human.disconnect();
      await fixture.stop();
    }
  },
);
