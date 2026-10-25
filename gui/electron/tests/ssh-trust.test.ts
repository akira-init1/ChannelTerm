import test from "node:test";
import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import { HostClient } from "../src/main/host/client";
import { SSHTrustExchange } from "../src/main/host/ssh-trust";
import { startFixture } from "./fixture";

test("SSH trust requires a single decision in the opening client and persists only approval", async () => {
  for (const mode of ["accept", "reject", "disconnect"]) {
    const fixture = await startFixture();
    const client = new HostClient(fixture.endpoint, fixture.token);
    const foreign = new HostClient(fixture.endpoint, fixture.token);
    try {
      const original = await readFile(fixture.knownHosts, "utf8");
      await writeFile(fixture.knownHosts, "");
      await client.connect();
      await foreign.connect();
      const result = client
        .openConnection({
          kind: "ssh",
          host: "127.0.0.1",
          port: fixture.sshPort,
          user: "fixture-user",
          password: "fixture-password",
          identityFiles: [],
          knownHostsFile: fixture.knownHosts,
          label: "trusted",
          confirmHostKey: true,
        })
        .then(
          (value) => ({ value, error: undefined }),
          (error) => ({ value: undefined, error }),
        );
      const deadline = Date.now() + 5000;
      while (!client.sshTrust.current() && Date.now() < deadline)
        await delay(10);
      const prompt = client.sshTrust.current();
      assert.ok(prompt);
      assert.equal(prompt.algorithm, "ssh-ed25519");
      assert.match(prompt.fingerprint, /^SHA256:/);
      assert.equal(JSON.stringify(prompt).includes(fixture.knownHosts), false);
      assert.equal(foreign.sshTrust.current(), undefined);
      assert.throws(() => foreign.sshTrust.decide(prompt.id, true), {
        code: "cancelled",
      });
      assert.throws(() => client.sshTrust.decide(prompt.id, "yes"), {
        code: "invalid_input",
      });
      if (mode === "disconnect") await client.disconnect();
      else client.sshTrust.decide(prompt.id, mode === "accept");
      const finished = await result;
      assert.equal(client.sshTrust.current(), undefined);
      assert.throws(() => client.sshTrust.decide(prompt.id, true), {
        code: "cancelled",
      });
      if (mode === "accept") {
        assert.equal(finished.error, undefined);
        assert.ok(finished.value?.sessionId);
        assert.equal(await readFile(fixture.knownHosts, "utf8"), original);
      } else {
        assert.ok(finished.error);
        assert.equal(await readFile(fixture.knownHosts, "utf8"), "");
        assert.equal(
          (await foreign.list()).filter((s) => s.transport === "ssh").length,
          0,
        );
      }
    } finally {
      await client.disconnect();
      await foreign.disconnect();
      await fixture.stop();
    }
  }
});

test("SSH trust streams reject malformed identities, oversized frames and duplicate results", async () => {
  const trust = {
    event: "trust",
    challenge_id: "a".repeat(64),
    address: "example.invalid:22",
    algorithm: "ssh-ed25519",
    fingerprint: "SHA256:" + "a".repeat(43),
  };
  for (const body of [
    JSON.stringify({ ...trust, address: "bad\naddress" }) + "\n",
    JSON.stringify({ ...trust, fingerprint: "md5:wrong" }) + "\n",
    "x".repeat(16385),
    '{"event":"result"}\n{"event":"result"}\n',
    JSON.stringify({ event: "result" }),
  ]) {
    const exchange = new SSHTrustExchange();
    let calls = 0;
    await assert.rejects(
      exchange.read(
        new Response(body, {
          headers: { "content-type": "application/x-ndjson" },
        }),
        new AbortController().signal,
        async () => {
          calls++;
        },
      ),
      { code: "incompatible" },
    );
    assert.equal(exchange.current(), undefined);
    assert.equal(calls, 0);
  }
});
