import test from "node:test";
import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  operationSource,
  managedTransferSummaries,
} from "../src/main/host/provenance";
import { ActivityFeed } from "../src/main/host/activity";
import { TransferEvents } from "../src/main/host/transfer-events";
import { OperationSource } from "../src/renderer/components/OperationSource";
import { retainedTransfer } from "../src/shared/transfers";

const client = {
  client_id: "CL-agent",
  name: "Test agent",
  kind: "agent",
  instance_id: "CI-first",
  operation_id: "CU-original",
  entry: "mcp",
  declared_entry: "cli",
  secret: "private-secret",
  owner: "private-owner",
};

test("public source projection strips capabilities and never upgrades actor or missing identity", () => {
  const source = operationSource(client)!;
  assert.equal(source.kind, "agent");
  assert.equal(source.presence, "unknown");
  assert.equal(
    operationSource({ kind: "user", name: "Unverified" })!.kind,
    "unknown",
  );
  assert.equal(operationSource(undefined), undefined);
  assert.doesNotMatch(JSON.stringify(source), /private-/);
  for (const bad of [
    { ...client, name: "bad\nname" },
    { ...client, instance_id: "x".repeat(129) },
    { ...client, entry: 5 },
  ])
    assert.throws(() => operationSource(bad));
  const markup = renderToStaticMarkup(
    createElement(OperationSource, { source, t: (id: string) => id }),
  );
  assert.match(markup, /source.agent/);
  assert.match(markup, /cli/);
  assert.match(markup, /source.declared/);
  const unknown = renderToStaticMarkup(
    createElement(OperationSource, { t: (id: string) => id }),
  );
  assert.match(unknown, /source.unknown/);
  assert.doesNotMatch(unknown, /source.user/);
});

test("activity and transfer histories preserve initiators and distinguish checkpoint reports after loss", () => {
  const event = (
    id: number,
    type: string,
    metadata: Record<string, unknown>,
    source: unknown = client,
  ) => ({
    id,
    type,
    metadata,
    client: source,
    actor: "user",
    session_id: "board",
    timestamp: "2026-11-15T00:00:00Z",
  });
  const feed = new ActivityFeed("board");
  const first = feed.apply(undefined, {
    next: 1,
    dropped: false,
    events: [event(0, "FILE_TRANSFER_STARTED", { transfer_id: "T-1" })],
  });
  assert.equal(first.rows[0]!.source!.kind, "agent");
  first.rows[0]!.source!.name = "mutated";
  assert.equal(
    feed.apply(undefined, undefined).rows[0]!.source!.name,
    "Test agent",
  );
  const transfers = new TransferEvents("board");
  const checkpoint = "a".repeat(24);
  transfers.apply({
    next: 1,
    dropped: false,
    events: [event(0, "FILE_TRANSFER_STARTED", { transfer_id: "T-1" })],
  });
  transfers.apply({
    next: 3,
    dropped: true,
    events: [
      event(
        2,
        "FILE_TRANSFER_PROGRESS",
        {
          transfer_id: "T-1",
          checkpoint_id: checkpoint,
          checkpoint_origin: {
            ...client,
            client_id: "CL-original",
            kind: "user",
          },
          resumed: true,
          sent: 1,
          total: 2,
        },
        { ...client, instance_id: "CI-later" },
      ),
    ],
  });
  const job = transfers.snapshots()[0]!;
  assert.equal(job.initiator!.instanceId, "CI-first");
  assert.equal(job.originalSource!.clientId, "CL-original");
  assert.equal(job.initiator!.kind, "agent");
  assert.equal(job.resumed, true);
  assert.equal(job.checkpointId, checkpoint);
  assert.doesNotMatch(JSON.stringify(job), /private-/);
  job.initiator!.name = "mutated";
  assert.equal(transfers.snapshots()[0]!.initiator!.name, "Test agent");
});

test("managed summaries omit private fields, bound records and preserve independent SSH targets", () => {
  const raw = {
    transfer_id: "FT-" + "a".repeat(26),
    client,
    transport: "sftp",
    direction: "send",
    state: "running",
    phase: "copying",
    bytes: 1,
    total_bytes: 2,
    ssh_profile: "private-profile",
    source: "private-path",
    error: "private-secret",
  };
  const job = managedTransferSummaries([raw])[0]!;
  assert.equal(job.scope, "host");
  assert.equal(job.sessionId, "");
  assert.equal(job.initiator!.kind, "agent");
  assert.equal(job.released, false);
  job.initiator!.presence = "online";
  const retained = retainedTransfer(job);
  assert.equal(retained.state, "unknown");
  assert.equal(retained.initiator!.presence, "unknown");
  assert.equal(retained.initiator!.clientId, job.initiator!.clientId);
  assert.doesNotMatch(JSON.stringify(job), /private-/);
  assert.throws(() => managedTransferSummaries(Array(129).fill(raw)));
  assert.throws(() =>
    managedTransferSummaries([{ ...raw, state: "invented" }]),
  );
});
