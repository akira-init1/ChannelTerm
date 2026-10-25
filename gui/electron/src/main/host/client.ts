import { ActivityFeed } from "./activity";
import type { ActivitySnapshot } from "../../shared/activity";
import { SSHTrustExchange } from "./ssh-trust";
import {
  isSerialProfileChange,
  isSerialProfiles,
  type SerialProfiles,
} from "../../shared/serial-profiles";
import { TransferEvents } from "./transfer-events";
import { TerminalCodec } from "./terminal-codec";
import {
  defaultTerminalSettings,
  isTerminalSettings,
  type TerminalSettings,
} from "../../shared/terminal-settings";
import { randomUUID } from "node:crypto";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type {
  Attachment,
  HostInfo,
  Output,
  SessionSummary,
} from "../../shared/api";
import { HostError } from "./errors";
import { hostEndpoint } from "./config";
import {
  isConnectionRequest,
  type OpenedConnection,
} from "../../shared/connections";
import {
  identifier,
  incompatible,
  input,
  integer,
  object,
  sessions,
  text,
} from "./validation";

type ActiveAttachment = Attachment & {
  feed: ActivityFeed;
  settings: TerminalSettings;
  codec: TerminalCodec;
  cursor?: number;
  controller: AbortController;
  reading: boolean;
  queuedBytes: number;
  writeVersion: number;
  writes: Promise<void>;
};

class ToolFailure extends Error {
  constructor(readonly code: string) {
    super("Host tool failed");
  }
}

/** An MCP client only: owns cursors/cancellation, never Session or lease authority. */
export class HostClient {
  private client?: Client;
  private lifetime: HostInfo["lifetime"] = "unknown";
  private connection = new AbortController();
  private attachments = new Map<string, ActiveAttachment>();
  private changing = false;
  private profilesBusy = false;
  private activityReading = false;
  private closing = new Set<string>();
  readonly sshTrust = new SSHTrustExchange();
  private sshTrustAvailable = false;
  private sshAvailable = false;
  private sshAdvanced = false;
  private transferEvents = new Map<string, TransferEvents>();
  private eventReads = new Map<string, Promise<TransferEvents>>();

  private async events(
    sessionId: string,
    fresh = false,
  ): Promise<TransferEvents> {
    const existing = this.eventReads.get(sessionId);
    if (existing) {
      if (!fresh) return existing;
      await existing;
      return this.events(sessionId, true);
    }
    let events = this.transferEvents.get(sessionId);
    if (!events) {
      if (this.transferEvents.size >= 64)
        throw new HostError(
          "busy",
          "Too many observed Sessions. Reconnect to reset observation.",
        );
      events = new TransferEvents(sessionId);
      this.transferEvents.set(sessionId, events);
    }
    const state = events;
    const pending = (async () => {
      // Read after raw output: the Host publishes the lease boundary before protocol writes.
      for (let page = 0; page < 4; page++) {
        let result: Record<string, unknown>;
        try {
          result = await this.call(
            "terminal_session_events",
            {
              session_id: sessionId,
              cursor: state.cursor,
              max_events: 1024,
              timeout_ms: 1,
            },
            this.connection.signal,
          );
        } catch (error) {
          if (error instanceof ToolFailure && error.code === "timeout")
            return state;
          throw error;
        }
        state.apply(result);
        if ((result.events as unknown[]).length < 1024) return state;
      }
      throw new HostError(
        "busy",
        "Session events are changing too quickly; output is held for safety.",
      );
    })();
    this.eventReads.set(sessionId, pending);
    try {
      return await pending;
    } finally {
      this.eventReads.delete(sessionId);
    }
  }

  async transfers(sessionId: string) {
    identifier(sessionId);
    return (await this.events(sessionId)).snapshots();
  }

  async validateSerial(sessionId: string): Promise<void> {
    identifier(sessionId);
    const entry = (await this.list()).find(
      (entry) => entry.session_id === sessionId,
    );
    if (!entry || entry.state !== "open" || entry.transport !== "serial")
      throw new HostError("closed", "An open serial Session is required.");
    if (entry.lease)
      throw new HostError(
        "busy",
        "Session is busy. Wait for the current operation.",
      );
  }

  /** File selection is bound to a live Session; dedicated SSH copies need no lease. */
  async validateFileSession(sessionId: string): Promise<SessionSummary> {
    identifier(sessionId);
    const entry = (await this.list()).find(
      (entry) => entry.session_id === sessionId,
    );
    if (
      !entry ||
      entry.state !== "open" ||
      !["serial", "ssh"].includes(entry.transport)
    )
      throw new HostError(
        "closed",
        "An open serial or SSH Session is required.",
      );
    if (entry.transport === "serial" && entry.lease)
      throw new HostError(
        "busy",
        "Session is busy. Wait for the current operation.",
      );
    return entry;
  }

  async beginTransferCancel(
    sessionId: string,
    transferId: string,
  ): Promise<string> {
    identifier(sessionId);
    identifier(transferId);
    const jobs = await this.transfers(sessionId);
    if (!jobs.some((job) => job.id === transferId && !job.released))
      throw new HostError("closed", "Transfer is no longer active.");
    const result = await this.call("terminal_begin_file_transfer_cancel", {
      session_id: sessionId,
    });
    if (result.active !== true)
      throw new HostError("closed", "Transfer is no longer active.");
    const requestId = text(result.request_id);
    try {
      if ((await this.events(sessionId, true)).activeTransferId !== transferId)
        throw new HostError("closed", "Transfer changed before confirmation.");
    } catch (error) {
      await this.resolveTransferCancel(sessionId, requestId, false).catch(
        () => {},
      );
      throw error;
    }
    // The Host binds request_id to the exact lease, so late confirmation cannot cancel its replacement.
    return text(result.request_id);
  }

  async resolveTransferCancel(
    sessionId: string,
    requestId: string,
    confirm: boolean,
  ): Promise<void> {
    identifier(sessionId);
    identifier(requestId);
    await this.call("terminal_resolve_file_transfer_cancel", {
      session_id: sessionId,
      request_id: requestId,
      cancel: confirm,
    });
  }

  private readonly endpoint: URL;

  constructor(
    endpoint: string,
    private readonly token: string,
  ) {
    this.endpoint = hostEndpoint(endpoint);
  }

  info(): HostInfo {
    return {
      endpoint: this.endpoint.href,
      lifetime: this.lifetime,
      sshAvailable: this.sshAvailable,
      sshAdvanced: this.sshAdvanced,
    };
  }

  async connect(): Promise<HostInfo> {
    if (this.client || this.changing)
      throw new HostError(
        "invalid_input",
        "Disconnect before connecting again.",
      );
    this.changing = true;
    this.connection = new AbortController();
    const signal = this.connection.signal;
    const client = new Client({
      name: "channelterm-electron",
      version: "0.1.0",
    });
    this.client = client;
    const transport = new StreamableHTTPClientTransport(this.endpoint, {
      // Never follow redirects with a credential or accept a renderer-supplied URL.
      fetch: async (url, init) => {
        const requested = new URL(url.toString());
        if (requested.href !== this.endpoint.href)
          throw new HostError(
            "unauthorized",
            "Host attempted to redirect the client.",
          );
        const response = await fetch(url, {
          ...init,
          redirect: "error",
          headers: {
            ...Object.fromEntries(new Headers(init?.headers)),
            Authorization: `Bearer ${this.token}`,
          },
          signal: AbortSignal.any([
            signal,
            AbortSignal.timeout(10000),
            ...(init?.signal ? [init.signal] : []),
          ]),
        });
        if (response.status === 401 || response.status === 403) {
          await response.body?.cancel();
          throw new HostError(
            "unauthorized",
            "Host rejected authentication. Check the Main process credential and endpoint.",
          );
        }
        this.lifetime =
          response.headers.get("X-ChannelTerm-Host-Lifetime") === "attachment"
            ? "attachment"
            : "persistent";
        return response;
      },
    });
    try {
      await client.connect(transport);
      if (signal.aborted)
        throw new HostError("cancelled", "Connection cancelled.");
      if (client.getServerVersion()?.name !== "channelterm")
        throw incompatible();
      await this.list();
      // Older/foreground Hosts may expose MCP only. Probe a fixed same-origin
      // route; never derive a URL or credential from Renderer input.
      try {
        const response = await this.sshRequest("GET");
        const capabilities = response.ok ? object(await response.json()) : {};
        this.sshTrustAvailable = capabilities.trust_confirmation === true;
        this.sshAvailable = capabilities.available === true;
        this.sshAdvanced = this.sshAvailable && capabilities.advanced === true;
      } catch {
        this.sshTrustAvailable = false;
        this.sshAvailable = false;
        this.sshAdvanced = false;
      }
      if (signal.aborted)
        throw new HostError("cancelled", "Connection cancelled.");
      return this.info();
    } catch (error) {
      await this.disconnect();
      throw error;
    } finally {
      this.changing = false;
    }
  }

  private async call(
    name: string,
    args: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<Record<string, unknown>> {
    const client = this.client;
    if (!client || this.connection.signal.aborted)
      throw new HostError("disconnected", "Connect to a Host first.");
    const result = await client.callTool({ name, arguments: args }, undefined, {
      signal,
      timeout: 10000,
    });
    if (result.isError) {
      const details = object(object(result.structuredContent).error);
      throw new ToolFailure(text(details.code));
    }
    return object(result.structuredContent);
  }

  async list(): Promise<SessionSummary[]> {
    return sessions(await this.call("terminal_list_sessions", {}));
  }

  private sshRequest(
    method: "GET" | "POST",
    body?: string,
    timeout = 20000,
    signal?: AbortSignal,
  ): Promise<Response> {
    return fetch(new URL("/gui/v1/ssh", this.endpoint), {
      method,
      body,
      redirect: "error",
      headers: {
        Authorization: `Bearer ${this.token}`,
        "Content-Type": "application/json",
      },
      signal: AbortSignal.any([
        this.connection.signal,
        AbortSignal.timeout(method === "GET" ? 2000 : timeout),
        ...(signal ? [signal] : []),
      ]),
    });
  }

  private async profilesRequest(request?: unknown): Promise<unknown> {
    if (!this.client || this.connection.signal.aborted)
      throw new HostError("disconnected", "Connect to a Host first.");
    if (this.profilesBusy)
      throw new HostError("busy", "A profile operation is in progress.");
    this.profilesBusy = true;
    try {
      const response = await fetch(
        new URL("/gui/v1/serial-profiles", this.endpoint),
        {
          method: request === undefined ? "GET" : "POST",
          body: request === undefined ? undefined : JSON.stringify(request),
          redirect: "error",
          headers: {
            Authorization: `Bearer ${this.token}`,
            "Content-Type": "application/json",
          },
          signal: AbortSignal.any([
            this.connection.signal,
            AbortSignal.timeout(8000),
          ]),
        },
      );
      if (!response.ok) await response.body?.cancel();
      if (response.status === 401 || response.status === 403)
        throw new HostError("unauthorized", "Host rejected profile access.");
      if (response.status === 404 && request === undefined)
        throw new HostError(
          "incompatible",
          "This Host does not provide serial profiles. Update the standalone Host.",
        );
      if (response.status === 409)
        throw new HostError(
          "busy",
          "Profile already exists. Enable replacement explicitly to overwrite it.",
        );
      if (!response.ok)
        throw new HostError(
          "tool_error",
          "Profile operation failed. Refresh profiles and check Host configuration before retrying.",
        );
      // A trusted endpoint still supplies untrusted data. Bound streamed bytes,
      // including chunked responses without Content-Length.
      const reader = response.body?.getReader();
      if (!reader) throw incompatible();
      const chunks: Uint8Array[] = [];
      let size = 0;
      try {
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          size += value.length;
          if (size > 524288) throw incompatible();
          chunks.push(value);
        }
      } finally {
        await reader.cancel().catch(() => {});
        reader.releaseLock();
      }
      return JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
    } catch (error) {
      if (error instanceof HostError) throw error;
      throw new HostError(
        this.connection.signal.aborted ? "cancelled" : "tool_error",
        "Profile result is uncertain. Refresh profiles before retrying; the request was not replayed.",
      );
    } finally {
      this.profilesBusy = false;
    }
  }

  async serialProfiles(): Promise<SerialProfiles> {
    const result = await this.profilesRequest();
    if (!isSerialProfiles(result)) throw incompatible();
    return result;
  }

  async changeSerialProfile(request: unknown): Promise<void> {
    if (!isSerialProfileChange(request))
      throw new HostError("invalid_input", "Invalid serial profile settings.");
    if (object(await this.profilesRequest(request)).saved !== true)
      throw incompatible();
  }

  async serialPorts(): Promise<string[]> {
    const result = await this.call("terminal_list_serial_ports", {});
    if (!Array.isArray(result.ports) || result.ports.length > 10000)
      throw incompatible();
    return result.ports.map((port) => {
      const name = text(object(port).name);
      identifier(name);
      return name;
    });
  }

  /** Create through the authoritative Host once. An uncertain result is never replayed. */
  async openConnection(request: unknown): Promise<OpenedConnection> {
    if (!isConnectionRequest(request))
      throw new HostError("invalid_input", "Invalid connection settings.");
    if (!this.client || this.connection.signal.aborted)
      throw new HostError("disconnected", "Connect to a Host first.");
    if (this.attachments.size >= 32)
      throw new HostError(
        "busy",
        "Close a terminal tab before opening a connection.",
      );
    if (this.changing)
      throw new HostError(
        "busy",
        "A connection change is already in progress.",
      );
    if (request.kind === "ssh" && !this.sshAvailable)
      throw new HostError(
        "incompatible",
        "This Host does not provide SSH creation. Start an updated standalone Host.",
      );
    if (request.kind === "ssh" && request.advanced && !this.sshAdvanced)
      throw new HostError(
        "incompatible",
        "This Host does not support advanced SSH settings. Update the Host or use default settings.",
      );
    this.changing = true;
    const setup = new AbortController();
    const signal = AbortSignal.any([this.connection.signal, setup.signal]);
    try {
      let result: Record<string, unknown>;
      if (request.kind === "serial") {
        result = await this.call(
          "terminal_open_serial",
          {
            port: request.port,
            baud: request.baud,
            data_bits: request.dataBits,
            parity: request.parity,
            stop_bits: request.stopBits,
            flow_control: request.flowControl,
            label: request.label,
            wake: request.wake ?? false,
          },
          signal,
        );
      } else {
        const trust = !!request.confirmHostKey && this.sshTrustAvailable;
        const response = await this.sshRequest(
          "POST",
          JSON.stringify({
            confirm_host_key: trust || undefined,
            host: request.host,
            user: request.user,
            port: request.port,
            label: request.label,
            identity_files: request.identityFiles,
            known_hosts_file: request.knownHostsFile,
            password: request.password,
            key_passphrase: request.keyPassphrase,
            advanced: request.advanced && {
              timeout_seconds: request.advanced.timeoutSeconds,
              keepalive_seconds: request.advanced.keepAliveSeconds,
              keepalive_count: request.advanced.keepAliveCount,
              write_timeout_seconds: request.advanced.writeTimeoutSeconds,
              term: request.advanced.term,
              columns: request.advanced.columns,
              rows: request.advanced.rows,
            },
          }),
          (request.advanced?.timeoutSeconds ?? 10) * 1000 +
            10000 +
            (trust ? 120000 : 0),
          signal,
        );
        if (response.status === 401 || response.status === 403)
          throw new HostError("unauthorized", "Host rejected SSH creation.");
        const payload =
          trust && response.ok
            ? await this.sshTrust.read(
                response,
                signal,
                async (challenge, accept) => {
                  const decision = await fetch(
                    new URL("/gui/v1/ssh/confirm", this.endpoint),
                    {
                      method: "POST",
                      redirect: "error",
                      headers: {
                        Authorization: `Bearer ${this.token}`,
                        "Content-Type": "application/json",
                      },
                      body: JSON.stringify({ challenge_id: challenge, accept }),
                      signal: AbortSignal.any([
                        signal,
                        AbortSignal.timeout(5000),
                      ]),
                    },
                  );
                  await decision.body?.cancel();
                  if (!decision.ok)
                    throw new HostError(
                      "cancelled",
                      "SSH confirmation has expired.",
                    );
                },
              )
            : object(await response.json());
        if (!response.ok || payload.event === "error") {
          const code = payload.error;
          const message =
            code === "host_key_rejected"
              ? "Host key rejected. Verify and install the server key in the Host known_hosts file before connecting."
              : code === "authentication_failed"
                ? "SSH authentication failed. Check the selected identity, password or passphrase."
                : "SSH creation failed. Check Host settings and refresh Sessions before trying again.";
          throw new HostError("tool_error", message);
        }
        result = payload;
      }
      if (signal.aborted)
        throw new HostError(
          "cancelled",
          "Connection request cancelled; refresh Sessions to check its result.",
        );
      identifier(result.session_id);
      if (typeof result.reused !== "boolean") throw incompatible();
      return { sessionId: result.session_id, reused: result.reused };
    } catch (error) {
      if (error instanceof HostError) throw error;
      throw new HostError(
        "tool_error",
        error instanceof ToolFailure
          ? "Host could not open this connection. Check settings and refresh Sessions before trying again."
          : "Connection result is uncertain. Refresh Sessions before trying again; the request was not replayed.",
      );
    } finally {
      setup.abort();
      this.changing = false;
    }
  }

  async attach(
    sessionId: string,
    settings: unknown = defaultTerminalSettings,
  ): Promise<Attachment> {
    identifier(sessionId);
    if (!isTerminalSettings(settings))
      throw new HostError("invalid_input", "Invalid terminal settings.");
    if (this.attachments.size >= 32)
      throw new HostError(
        "busy",
        "Close a terminal tab before attaching another.",
      );
    const preferences = { ...settings };
    if (this.changing)
      throw new HostError(
        "invalid_input",
        "An attachment change is already in progress.",
      );
    this.changing = true;
    const connection = this.connection;
    try {
      const entry = (await this.list()).find(
        (entry) => entry.session_id === sessionId,
      );
      if (!entry || entry.state !== "open")
        throw new HostError(
          "closed",
          "Session is no longer open. Refresh the Session list.",
        );
      if (connection.signal.aborted)
        throw new HostError("cancelled", "Attachment cancelled.");
      const attached = await this.call("terminal_session_attach", {
        session_id: sessionId,
        actor: "user",
      });
      if (attached.attached !== true) throw incompatible();
      if (connection.signal.aborted)
        throw new HostError("cancelled", "Attachment cancelled.");
      const active: ActiveAttachment = {
        settings: preferences,
        codec: new TerminalCodec(preferences.encoding),
        id: randomUUID(),
        sessionId,
        feed: new ActivityFeed(sessionId),
        controller: new AbortController(),
        reading: false,
        queuedBytes: 0,
        writeVersion: 0,
        writes: Promise.resolve(),
      };
      this.attachments.set(active.id, active);
      return { id: active.id, sessionId };
    } finally {
      this.changing = false;
    }
  }

  private attachment(id: string): ActiveAttachment {
    identifier(id);
    const active = this.attachments.get(id);
    if (!active || active.controller.signal.aborted) {
      throw new HostError("cancelled", "This attachment has ended.");
    }
    return active;
  }

  /** Bind local clipboard operations to this client's still-live attachment. */
  validateAttachment(id: string): void {
    this.attachment(id);
  }

  /** Main-only transfer description; the destination must attach through the Host again. */
  describeAttachment(id: string): {
    attachment: Attachment;
    settings: TerminalSettings;
  } {
    const active = this.attachment(id);
    return {
      attachment: { id: active.id, sessionId: active.sessionId },
      settings: { ...active.settings },
    };
  }

  /** Drain already accepted writes before handing a view to another window. No replay. */
  async settleWrites(id: string): Promise<void> {
    const active = this.attachment(id);
    await active.writes;
    this.attachment(id);
  }

  async detach(id?: string): Promise<void> {
    if (id === undefined) {
      await Promise.all(
        [...this.attachments.keys()].map((key) => this.detach(key)),
      );
      return;
    }
    identifier(id);
    const active = this.attachments.get(id);
    this.attachments.delete(id);
    if (!active) return;
    active.controller.abort();
    // This records a lifecycle event only. No terminal_close, lease release, or Host shutdown.
    try {
      await this.call(
        "terminal_session_detach",
        { session_id: active.sessionId, actor: "user" },
        AbortSignal.timeout(1000),
      );
    } catch {
      /* Best effort when the Host or Session has already gone away. */
    }
  }

  /** Close exactly the attached shared Session once; never retry an uncertain close. */
  async closeSession(id: string): Promise<void> {
    const active = this.attachment(id);
    if (this.closing.has(active.sessionId))
      throw new HostError("busy", "Session closure is already pending.");
    this.closing.add(active.sessionId);
    // Discard this client's queued input; Host lifecycle remains authoritative for in-flight writes.
    for (const attached of this.attachments.values())
      if (attached.sessionId === active.sessionId) attached.writeVersion++;
    try {
      const entry = (await this.list()).find(
        (entry) => entry.session_id === active.sessionId,
      );
      this.attachment(id);
      if (!entry || entry.state !== "open")
        throw new HostError(
          "closed",
          "Session is already closed. Refresh Sessions.",
        );
      const result = await this.call("terminal_close", {
        session_id: active.sessionId,
      });
      if (result.closed !== true || result.session_id !== active.sessionId)
        throw incompatible();
    } catch (error) {
      if (!(error instanceof ToolFailure)) throw error;
      if (error.code === "session_not_found" || error.code === "session_closed")
        throw new HostError(
          "closed",
          "Session is already closed. Refresh Sessions.",
        );
      if (error.code === "session_busy")
        throw new HostError(
          "busy",
          "Host refused closure while an operation is active.",
        );
      throw new HostError(
        "tool_error",
        "Host did not confirm closure. Refresh Sessions before trying again.",
      );
    } finally {
      this.closing.delete(active.sessionId);
    }
  }

  async activity(id: string): Promise<ActivitySnapshot> {
    const active = this.attachment(id);
    if (this.activityReading)
      throw new HostError("busy", "An activity read is already pending.");
    this.activityReading = true;
    try {
      const read = async (name: string, kind: "activity" | "events") => {
        try {
          return await this.call(
            name,
            active.feed.parameters(kind),
            active.controller.signal,
          );
        } catch (error) {
          if (error instanceof ToolFailure && error.code === "timeout")
            return undefined;
          throw error;
        }
      };
      // Join both bounded reads before freeing the window's single observation slot.
      const results = await Promise.allSettled([
        read("terminal_read_activity", "activity"),
        read("terminal_session_events", "events"),
      ]);
      this.attachment(id);
      for (const result of results)
        if (result.status === "rejected") throw result.reason;
      const first = results[0],
        second = results[1];
      if (first?.status !== "fulfilled" || second?.status !== "fulfilled")
        throw incompatible();
      return active.feed.apply(first.value, second.value);
    } catch (error) {
      if (error instanceof HostError) throw error;
      throw new HostError(
        "tool_error",
        "Session activity is unavailable. Check the Host and Session state.",
      );
    } finally {
      this.activityReading = false;
    }
  }

  async read(id: string): Promise<Output> {
    const active = this.attachment(id);
    if (active.reading)
      throw new HostError(
        "invalid_input",
        "Only one output read may be outstanding.",
      );
    active.reading = true;
    try {
      const result = await this.call(
        "terminal_read",
        {
          session_id: active.sessionId,
          encoding: "base64",
          max_bytes: 65536,
          ...(active.cursor === undefined
            ? {}
            : { cursor: active.cursor, timeout_ms: 1000 }),
        },
        active.controller.signal,
      );
      this.attachment(id);
      const next = integer(result.next);
      const count = integer(result.bytes_read);
      const encoded = text(result.data);
      if (
        result.encoding !== "base64" ||
        typeof result.dropped !== "boolean" ||
        count > 65536 ||
        encoded.length > 87384
      )
        throw incompatible();
      const bytes = Buffer.from(encoded, "base64");
      if (
        bytes.length !== count ||
        bytes.toString("base64") !== encoded ||
        next < count ||
        (active.cursor !== undefined && next < active.cursor)
      )
        throw incompatible();
      const filtered = (await this.events(active.sessionId, true)).filter(
        bytes,
        next,
      );
      this.attachment(id);
      const replay =
        active.cursor === undefined || result.dropped || filtered.dropped;
      active.cursor = next;
      const dropped = result.dropped || filtered.dropped;
      if (dropped) active.codec.reset();
      return {
        data: filtered.data,
        replay,
        dropped,
        ...(active.settings.view === "text" &&
        active.settings.encoding !== "utf-8"
          ? { text: active.codec.decode(filtered.data) }
          : {}),
      };
    } catch (error) {
      if (error instanceof ToolFailure && error.code === "timeout")
        return { data: [], dropped: false };
      throw await this.classify(error, active);
    } finally {
      active.reading = false;
    }
  }

  async write(id: string, data: string): Promise<void> {
    input(data);
    const active = this.attachment(id);
    if (this.closing.has(active.sessionId))
      throw new HostError(
        "busy",
        "Session closure is pending; input was discarded.",
      );
    if (active.settings.view === "hex")
      throw new HostError("invalid_input", "Hex view is read-only.");
    const legacy = active.settings.encoding !== "utf-8";
    const encoded = active.codec.encode(data);
    const bytes = encoded.length;
    if (active.queuedBytes + bytes > 65536)
      throw new HostError(
        "invalid_input",
        "Input queue is full; wait before sending more.",
      );
    active.queuedBytes += bytes;
    const version = active.writeVersion;
    const write = active.writes
      .then(async () => {
        this.attachment(id);
        if (version !== active.writeVersion)
          throw new HostError(
            "cancelled",
            "Queued input discarded after a failed write.",
          );
        const result = await this.call(
          "terminal_write",
          {
            session_id: active.sessionId,
            data: legacy ? encoded.toString("base64") : data,
            encoding: legacy ? "base64" : "utf8",
            actor: "user",
          },
          active.controller.signal,
        );
        if (integer(result.bytes_written) !== bytes)
          throw new HostError(
            "tool_error",
            "Incomplete write. Input was not retried; check the remote terminal.",
          );
      })
      .catch(async (error: unknown) => {
        active.writeVersion++;
        throw await this.classify(error, active);
      })
      .finally(() => {
        active.queuedBytes -= bytes;
      });
    active.writes = write.catch(() => {});
    return write;
  }

  private async classify(
    error: unknown,
    active: ActiveAttachment,
  ): Promise<unknown> {
    if (active.controller.signal.aborted || this.connection.signal.aborted)
      return new HostError("cancelled", "Attachment cancelled.");
    if (!(error instanceof ToolFailure)) return error;
    if (error.code === "session_closed" || error.code === "session_not_found")
      return new HostError(
        "closed",
        "Session closed. Refresh and select an open Session.",
      );
    if (error.code === "cancelled")
      return new HostError("cancelled", "Host cancelled this request.");
    // Ordinary terminal_write currently returns tool_error for contention. Query
    // authoritative structured state instead of parsing human-readable error text.
    const entry = (await this.list()).find(
      (entry) => entry.session_id === active.sessionId,
    );
    if (!entry || entry.state !== "open")
      return new HostError(
        "closed",
        "Session closed. Refresh and select an open Session.",
      );
    if (entry.lease || error.code === "session_busy")
      return new HostError(
        "busy",
        "Host reports an active writer lease. Input was discarded; reading remains available.",
      );
    return new HostError(
      "tool_error",
      "Host rejected the operation. Input was not retried; check Session state before continuing.",
    );
  }

  async disconnect(): Promise<void> {
    // Abort immediately, including an in-progress handshake. Detach is advisory;
    // a disconnected MCP client never owns the underlying Session lifetime.
    const attachments = [...this.attachments.values()];
    this.attachments.clear();
    for (const active of attachments) active.controller.abort();
    const client = this.client;
    this.client = undefined;
    if (client && !this.connection.signal.aborted) {
      await Promise.all(
        attachments.map(async (active) => {
          try {
            await client.callTool(
              {
                name: "terminal_session_detach",
                arguments: { session_id: active.sessionId, actor: "user" },
              },
              undefined,
              { signal: AbortSignal.timeout(500), timeout: 500 },
            );
          } catch {
            /* Host may be unreachable. */
          }
        }),
      );
    }
    this.connection.abort();
    await client?.close().catch(() => {});
    this.lifetime = "unknown";
    this.sshAvailable = false;
    this.sshAdvanced = false;
    this.transferEvents.clear();
    this.eventReads.clear();
  }
}
