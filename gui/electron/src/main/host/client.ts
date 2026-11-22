import type { SavedPolicyTarget } from "../../shared/api";
import { debugBytes } from "./debug-send";
import {
  isUBootRequest,
  type UBootRequest,
  type UBootOutcome,
} from "../../shared/uboot";
import {
  ubootArguments,
  ubootOutcome,
  UBootEventFeed,
  ubootHTTPCall,
  type UBootHTTPCall,
} from "./uboot";
import {
  isDeviceSelection,
  type DeviceSelection,
  type DeviceDecision,
  type DiscoverySnapshot,
  type SerialPort,
} from "../../shared/devices";
import { discoverySnapshot, deviceDecision } from "./devices";
import type { DebugPayload } from "../../shared/debug-send";
import { ClientPresence } from "./presence";
import { idlePolicy } from "./idle";
import type { ControlSnapshot, ControlReply } from "../../shared/control";
import { controlSnapshot, controlRequest, controlOutcome } from "./control";
import { managedTransferSummaries } from "./provenance";
import type { TransferStatus } from "../../shared/transfers";
import { debugOverview } from "./debug";
import { RemoteSize } from "./resize";
import { ActivityFeed } from "./activity";
import { diagnosticMetadata, downloadDiagnostic } from "./diagnostics";
import {
  isDiagnosticRequest,
  type DiagnosticRequest,
} from "../../shared/diagnostics";
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
import { displayVersion } from "./information";
import { capturedOutput, type OutputCapture } from "./capture";
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
  sizeControl?: RemoteSize;
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
  private captures = new Map<string, AbortController>();
  private hostVersion?: string;
  private controls = new Map<string, ControlSnapshot>();
  private client?: Client;
  private presence?: ClientPresence;
  private lifetime: HostInfo["lifetime"] = "unknown";
  private idle?: HostInfo["idlePolicy"];
  private connection = new AbortController();
  private attachments = new Map<string, ActiveAttachment>();
  private changing = false;
  private profilesBusy = false;
  private devicesReading?: Promise<DiscoverySnapshot>;
  private deviceDecisionReading = false;
  private activityReading = false;
  private ubootRunning = false;
  private ubootHTTP?: UBootHTTPCall;
  private closing = new Set<string>();
  readonly sshTrust = new SSHTrustExchange();
  private sshTrustAvailable = false;
  private sshAvailable = false;
  private independentSSH = false;
  private sshAdvanced = false;
  private resizeAvailable = false;
  private pendingSizes = new Map<string, RemoteSize>();
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
    const jobs = (await this.events(sessionId)).snapshots();
    return this.decorateTransfers(jobs);
  }

  async managedTransfers(): Promise<TransferStatus[]> {
    if (!this.presence?.managedTransfersAvailable) return [];
    const result = await this.call("file_transfer_list", {});
    return this.decorateTransfers(managedTransferSummaries(result.transfers));
  }

  private interruptingTransfer = false;
  async interruptSSHTransfer(id: unknown): Promise<void> {
    if (typeof id !== "string" || !/^FT-[A-Za-z0-9]{16,64}$/.test(id))
      throw incompatible();
    if (this.interruptingTransfer)
      throw new HostError("busy", "A transfer interruption is pending.");
    this.interruptingTransfer = true;
    try {
      const result = await this.call("file_transfer", {
        action: "interrupt",
        transfer_id: id,
      });
      const row = managedTransferSummaries([result.transfer])[0];
      if (row?.managedId !== id) throw incompatible();
    } catch (error) {
      if (error instanceof ToolFailure)
        throw new HostError(
          "tool_error",
          "Transfer interruption was not confirmed. Refresh the original task; no request was retried.",
        );
      throw error;
    } finally {
      this.interruptingTransfer = false;
    }
  }

  async decorateTransfers(jobs: TransferStatus[]): Promise<TransferStatus[]> {
    const decorate = await this.presence?.sources();
    return jobs.map((job) => ({
      ...job,
      initiator: decorate?.(job.initiator) ?? job.initiator,
      originalSource: decorate?.(job.originalSource) ?? job.originalSource,
    }));
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
      hostVersion: this.hostVersion,
      remoteSize: this.resizeAvailable,
      managedTransfers: this.presence?.managedTransfersAvailable,
      ubootAvailable: this.presence?.ubootAvailable,
      lifetime: this.lifetime,
      sshAvailable: this.sshAvailable,
      sshAdvanced: this.sshAdvanced,
      ...(this.idle ? { idlePolicy: this.idle } : {}),
    };
  }

  async refreshIdlePolicy(): Promise<void> {
    if (!this.presence?.idlePolicyAvailable) return;
    const signal = this.connection.signal;
    const result = await this.call("host_idle_status", {}, signal);
    signal.throwIfAborted();
    this.idle = idlePolicy(result.policy);
  }

  async setBackgroundMCP(enabled: boolean): Promise<HostInfo> {
    if (!this.client || !this.idle?.backgroundSwitch)
      throw new HostError(
        "invalid_input",
        "Background MCP requires a connected desktop Host.",
      );
    const response = await fetch(
      new URL("/gui/v1/background-mcp", this.endpoint),
      {
        method: enabled ? "PUT" : "DELETE",
        headers: { Authorization: `Bearer ${this.token}` },
        redirect: "error",
        signal: AbortSignal.any([
          this.connection.signal,
          AbortSignal.timeout(5000),
        ]),
      },
    );
    await response.body?.cancel();
    if (response.status !== 204)
      throw new HostError(
        response.status === 403 ? "unauthorized" : "tool_error",
        "The Host rejected the background MCP setting.",
      );
    await this.refreshIdlePolicy();
    return this.info();
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
        const bootCall = ubootHTTPCall(init?.body, this.ubootHTTP);
        const response = await fetch(url, {
          ...init,
          redirect: "error",
          headers: {
            ...Object.fromEntries(new Headers(init?.headers)),
            Authorization: `Bearer ${this.token}`,
          },
          signal: AbortSignal.any([
            signal,
            AbortSignal.timeout(bootCall?.timeoutMS ?? 10000),
            ...(bootCall ? [bootCall.signal] : []),
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
      this.hostVersion = displayVersion(client.getServerVersion()?.version);
      this.presence = new ClientPresence(client, signal);
      await this.presence.start();
      await this.refreshIdlePolicy();
      await this.list();
      // Older/foreground Hosts may expose MCP only. Probe a fixed same-origin
      // route; never derive a URL or credential from Renderer input.
      try {
        const response = await this.sshRequest("GET");
        const capabilities = response.ok ? object(await response.json()) : {};
        this.sshTrustAvailable = capabilities.trust_confirmation === true;
        this.sshAvailable = capabilities.available === true;
        this.independentSSH = capabilities.independent_shell === true;
        this.sshAdvanced = this.sshAvailable && capabilities.advanced === true;
      } catch {
        this.sshTrustAvailable = false;
        this.sshAvailable = false;
        this.sshAdvanced = false;
      }
      if (signal.aborted)
        throw new HostError("cancelled", "Connection cancelled.");
      try {
        const response = await fetch(new URL("/gui/v1/resize", this.endpoint), {
          redirect: "error",
          headers: { Authorization: `Bearer ${this.token}` },
          signal: AbortSignal.any([signal, AbortSignal.timeout(2000)]),
        });
        this.resizeAvailable =
          response.ok && object(await response.json()).available === true;
        if (!response.ok) await response.body?.cancel();
      } catch {
        this.resizeAvailable = false;
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

  private resizeMetadata(sessionId: string): Record<string, unknown> {
    const control = this.controls.get(sessionId);
    return {
      instance: this.presence?.metadata()?.["io.channelterm/instance"],
      control: control
        ? { version: control.version, operation_id: control.operation_id }
        : undefined,
    };
  }

  private async call(
    name: string,
    args: Record<string, unknown>,
    signal?: AbortSignal,
    capturedControl?: ControlSnapshot,
  ): Promise<Record<string, unknown>> {
    const client = this.client;
    if (!client || this.connection.signal.aborted)
      throw new HostError("disconnected", "Connect to a Host first.");
    const control =
      capturedControl ??
      (typeof args.session_id === "string"
        ? this.controls.get(args.session_id)
        : undefined);
    const result = await client.callTool(
      {
        name,
        arguments: args,
        _meta: {
          ...this.presence?.metadata(),
          ...(control
            ? {
                "io.channelterm/control": {
                  version: control.version,
                  operation_id: control.operation_id,
                },
              }
            : {}),
        },
      },
      undefined,
      {
        signal,
        timeout: 10000,
      },
    );
    if (result.isError) {
      const details = object(object(result.structuredContent).error);
      if (details.code === "permission_denied")
        throw new HostError(
          "unauthorized",
          "Host permission denied. Ask the local operator to grant this client access. Input was not retried.",
        );
      throw new ToolFailure(text(details.code));
    }
    return object(result.structuredContent);
  }

  async connectionPolicies(raw: unknown): Promise<SavedPolicyTarget[]> {
    let args: Record<string, unknown> = { action: "policies" };
    if (raw !== undefined) {
      const q = object(raw);
      if (
        Object.keys(q).length !== 3 ||
        typeof q.enabled !== "boolean" ||
        !["serial", "ssh"].includes(String(q.transport)) ||
        typeof q.endpoint !== "string" ||
        !q.endpoint ||
        q.endpoint.length > 1024 ||
        /[\x00-\x1f\x7f]/.test(q.endpoint)
      )
        throw incompatible();
      args = {
        action: q.enabled ? "protect_saved" : "unprotect_saved",
        transport: q.transport,
        endpoint: q.endpoint,
      };
    }
    const response = await this.call(
      "terminal_control",
      args,
      this.connection.signal,
    );
    if (
      !Array.isArray(response.connections) ||
      response.connections.length > 256
    )
      throw incompatible();
    return response.connections.map((row) => {
      const r = object(row);
      if (
        (r.transport !== "ssh" && r.transport !== "serial") ||
        typeof r.endpoint !== "string" ||
        r.endpoint.length > 1024
      )
        throw incompatible();
      return { transport: r.transport, endpoint: r.endpoint };
    });
  }
  private controlPending = false;
  async control(value: unknown): Promise<ControlReply> {
    const request = controlRequest(value);
    if (this.controlPending)
      throw new HostError("busy", "A control request is pending.");
    this.controlPending = true;
    const connection = this.connection;
    // Disable queued input immediately; only a fresh Host snapshot can enable it.
    if (request.action === "takeover") {
      const old = this.controls.get(request.session_id);
      if (old)
        this.controls.set(request.session_id, { ...old, writable: false });
      for (const active of this.attachments.values())
        if (active.sessionId === request.session_id) active.writeVersion++;
    }
    try {
      const response = await this.call(
        "terminal_control",
        { ...request },
        connection.signal,
      );
      if (connection.signal.aborted || connection !== this.connection)
        throw new HostError(
          "cancelled",
          "Control observation ended; query the original result.",
        );
      if (
        request.action === "allow" ||
        request.action === "protect" ||
        request.action === "unprotect"
      ) {
        const control = controlSnapshot(response.control);
        if (control.session_id !== request.session_id) throw incompatible();
        this.controls.set(control.session_id, control);
        return { control };
      }
      const result = controlOutcome(response.result);
      if (
        result.session_id !== request.session_id ||
        result.version !== request.version ||
        result.operation_id !== request.operation_id
      )
        throw incompatible();
      return { result };
    } catch (error) {
      if (error instanceof ToolFailure) {
        const code = error.code;
        if (code === "control_stale")
          throw new HostError(
            "control_stale",
            "The captured operation changed. Nothing was retried.",
          );
        if (code === "control_busy" || code === "ai_paused")
          throw new HostError(
            "busy",
            "Control is unavailable; input was not retried.",
          );
        throw new HostError(
          "tool_error",
          "Control result is unavailable. Query the original operation after reconnecting.",
        );
      }
      throw error;
    } finally {
      this.controlPending = false;
    }
  }

  private debugPending = false;
  async debugOverview() {
    if (this.debugPending)
      throw new HostError("busy", "Debug observation is in progress.");
    this.debugPending = true;
    const connection = this.connection;
    try {
      const result = await this.call("debug_overview", {}, connection.signal);
      if (connection.signal.aborted || connection !== this.connection)
        throw new HostError("cancelled", "Debug observation ended.");
      return debugOverview(result);
    } finally {
      this.debugPending = false;
    }
  }

  async list(): Promise<SessionSummary[]> {
    const entries = sessions(
      await this.call("terminal_list_sessions", {}),
      controlSnapshot,
    );
    this.controls.clear();
    for (const entry of entries)
      if (entry.control) this.controls.set(entry.session_id, entry.control);
    for (const active of this.attachments.values()) {
      const control = this.controls.get(active.sessionId);
      if (control && !control.writable) active.writeVersion++;
    }
    return entries;
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
          "profile_exists",
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

  async serialPorts(): Promise<SerialPort[]> {
    const result = await this.call("terminal_list_serial_ports", {});
    if (!Array.isArray(result.ports) || result.ports.length > 10000)
      throw incompatible();
    return result.ports.map((port) => {
      const entry = object(port);
      const name = text(entry.name);
      identifier(name);
      // Missing or unusable display metadata must not hide a valid endpoint.
      const product = entry.product;
      return {
        name,
        product:
          typeof product === "string" &&
          product.length <= 512 &&
          !/[\x00-\x1f\x7f]/.test(product)
            ? product.trim()
            : "",
      };
    });
  }

  /** Share one bounded read per window; the Registry owns scanning and identity. */
  devices(): Promise<DiscoverySnapshot> {
    if (this.devicesReading) return this.devicesReading;
    const connection = this.connection;
    const pending = this.call("terminal_list_devices", {}, connection.signal)
      .then((result) => {
        if (connection !== this.connection || connection.signal.aborted)
          throw new HostError(
            "cancelled",
            "Device discovery belongs to an expired Host connection.",
          );
        return discoverySnapshot(result);
      })
      .catch((error: unknown) => {
        if (error instanceof HostError) throw error;
        throw new HostError(
          "tool_error",
          "Device discovery is unavailable. Manual port entry remains available.",
          "discovery_unavailable",
        );
      })
      .finally(() => {
        if (this.devicesReading === pending) this.devicesReading = undefined;
      });
    this.devicesReading = pending;
    return pending;
  }

  /** Selection is a snapshot fence, not proof that hardware cannot change after the check. */
  private async verifyDeviceSelection(
    selected: DeviceSelection,
  ): Promise<void> {
    const connection = this.connection;
    // A snapshot already in flight may predate this explicit selection/open.
    if (this.devicesReading) await this.devicesReading;
    if (connection !== this.connection || connection.signal.aborted)
      throw new HostError(
        "cancelled",
        "Device selection belongs to an expired Host connection.",
      );
    const snapshot = await this.devices();
    if (snapshot.state !== "current")
      throw new HostError(
        "tool_error",
        "Device discovery is not current. Refresh before selecting a device.",
        "discovery_stale",
      );
    const devices = snapshot.devices;
    if (
      !devices.some(
        (device) =>
          device.id === selected.deviceId &&
          device.endpoint === selected.endpoint &&
          device.transport === "serial",
      )
    )
      throw new HostError(
        "invalid_input",
        "The selected device disappeared or changed endpoints. Select it again.",
        "device_changed",
      );
  }

  /** Return policy and Host occupancy without opening, probing or automatically connecting. */
  async deviceDecision(value: unknown): Promise<DeviceDecision> {
    if (!isDeviceSelection(value))
      throw new HostError("invalid_input", "Invalid device selection.");
    if (this.deviceDecisionReading)
      throw new HostError("busy", "A device decision is already pending.");
    this.deviceDecisionReading = true;
    const connection = this.connection;
    try {
      await this.verifyDeviceSelection(value);
      const result = await this.call(
        "terminal_get_connection_decision",
        {
          transport: "serial",
          endpoint: value.endpoint,
        },
        connection.signal,
      );
      if (connection !== this.connection || connection.signal.aborted)
        throw new HostError(
          "cancelled",
          "Device decision belongs to an expired Host connection.",
        );
      return deviceDecision(result, value);
    } catch (error) {
      if (error instanceof HostError) throw error;
      throw new HostError(
        "tool_error",
        "The device decision is unavailable; no connection was attempted.",
        "discovery_unavailable",
      );
    } finally {
      this.deviceDecisionReading = false;
    }
  }

  /** Create through the authoritative Host once. An uncertain result is never replayed. */
  async openConnection(request: unknown): Promise<OpenedConnection> {
    if (!isConnectionRequest(request))
      throw new HostError("invalid_input", "Invalid connection settings.");
    if (!this.client || this.connection.signal.aborted)
      throw new HostError("disconnected", "Connect to a Host first.");
    if (this.attachments.size + this.pendingSizes.size >= 32)
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
    if (request.kind === "ssh" && !this.independentSSH)
      throw new HostError(
        "incompatible",
        "Update the Host to create an independent SSH terminal.",
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
        if (request.discovery)
          await this.verifyDeviceSelection(request.discovery);
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
            independent: true,
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
          throw new HostError(
            "tool_error",
            message,
            code === "host_key_rejected"
              ? "ssh_host_key"
              : code === "authentication_failed"
                ? "ssh_authentication"
                : undefined,
          );
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
      if (request.kind === "ssh" && this.resizeAvailable && !result.reused) {
        if (
          result.resize_owner !== "" &&
          !RemoteSize.validOwner(result.resize_owner)
        )
          throw incompatible();
        this.pendingSizes.set(
          result.session_id,
          new RemoteSize(
            this.endpoint,
            this.token,
            result.session_id,
            result.resize_owner,
            this.connection.signal,
            () => this.resizeMetadata(text(result.session_id)),
          ),
        );
      }
      return { sessionId: result.session_id, reused: result.reused };
    } catch (error) {
      if (error instanceof HostError) throw error;
      if (
        request.kind === "serial" &&
        error instanceof ToolFailure &&
        (error.code === "serial_not_found" ||
          error.code === "serial_permission_denied" ||
          error.code === "serial_busy" ||
          error.code === "serial_busy_or_denied" ||
          error.code === "serial_open_failed")
      )
        throw new HostError(
          "tool_error",
          "The explicit serial open failed. Refresh discovery before retrying.",
          error.code,
        );
      throw new HostError(
        "tool_error",
        error instanceof ToolFailure
          ? "Host could not open this connection. Check settings and refresh Sessions before trying again."
          : "Connection result is uncertain. Refresh Sessions before trying again; the request was not replayed.",
        error instanceof ToolFailure
          ? "connection_failed"
          : "connection_uncertain",
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
    const presence = this.presence;
    try {
      const entry = (await this.list()).find(
        (entry) => entry.session_id === sessionId,
      );
      if (!entry || entry.state !== "open")
        throw new HostError(
          "closed",
          "Session is no longer open. Refresh the Session list.",
        );
      if (connection.signal.aborted || !this.client)
        throw new HostError("cancelled", "Attachment cancelled.");
      const attached = await this.call("terminal_session_attach", {
        session_id: sessionId,
        actor: "user",
      });
      if (attached.attached !== true) throw incompatible();
      if (connection.signal.aborted || !this.client)
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
      if (entry.transport === "ssh" && this.resizeAvailable) {
        active.sizeControl =
          this.pendingSizes.get(sessionId) ??
          new RemoteSize(
            this.endpoint,
            this.token,
            sessionId,
            "",
            connection.signal,
            () => this.resizeMetadata(sessionId),
          );
        this.pendingSizes.delete(sessionId);
      }
      await presence?.use(active.id, sessionId, "monitor");
      if (
        connection.signal.aborted ||
        this.connection !== connection ||
        !this.client
      ) {
        await presence?.unuse(active.id);
        throw new HostError("cancelled", "Attachment cancelled.");
      }
      this.attachments.set(active.id, active);
      return {
        id: active.id,
        sessionId,
        remoteSize: !!active.sizeControl,
        ownsRemoteSize: active.sizeControl?.owned,
      };
    } finally {
      this.changing = false;
    }
  }

  async resize(id: string, size: unknown): Promise<boolean> {
    const active = this.attachment(id);
    const value = object(size);
    if (
      Object.keys(value).length !== 2 ||
      !Number.isInteger(value.columns) ||
      !Number.isInteger(value.rows) ||
      typeof value.columns !== "number" ||
      typeof value.rows !== "number" ||
      value.columns < 1 ||
      value.columns > 1000 ||
      value.rows < 1 ||
      value.rows > 1000
    )
      throw new HostError("invalid_input", "Invalid terminal dimensions.");
    return active.sizeControl
      ? active.sizeControl.request("resize", value.columns, value.rows)
      : false;
  }

  async resizeControl(id: string, action: unknown): Promise<boolean> {
    const active = this.attachment(id);
    if (action !== "status" && action !== "take" && action !== "release")
      throw new HostError("invalid_input", "Invalid size action.");
    if (!active.sizeControl)
      throw new HostError(
        "incompatible",
        "Remote size control is unavailable.",
      );
    return active.sizeControl.request(action);
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
    await active.sizeControl?.dispose();
    await this.presence?.unuse(active.id);
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
      const snapshot = active.feed.apply(first.value, second.value);
      const decorate = await this.presence?.sources();
      this.attachment(id);
      return {
        ...snapshot,
        rows: snapshot.rows.map((row) => ({
          ...row,
          source: decorate?.(row.source) ?? row.source,
        })),
      };
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

  async downloadDiagnostic(
    request: DiagnosticRequest,
    signal: AbortSignal,
    write: (data: Buffer) => Promise<void>,
  ) {
    if (!isDiagnosticRequest(request))
      throw new HostError("invalid_input", "Invalid diagnostic request.");
    const active =
      request.kind === "snapshot"
        ? this.attachment(request.attachmentId)
        : undefined;
    const connection = this.connection;
    const abort = AbortSignal.any([
      signal,
      connection.signal,
      ...(active ? [active.controller.signal] : []),
    ]);
    const validate = () => {
      if (abort.aborted || connection !== this.connection)
        throw new HostError("cancelled", "Diagnostic save cancelled.");
      if (active) this.attachment(active.id);
    };
    try {
      validate();
      const exported =
        request.kind === "snapshot"
          ? await this.call(
              "terminal_export_diagnostics",
              {
                session_id: active!.sessionId,
                include_raw: request.includeRaw,
              },
              abort,
            )
          : undefined;
      validate();
      const expected = exported
        ? diagnosticMetadata(exported, exported.diagnostic_id)
        : undefined;
      const id =
        request.kind === "existing" ? request.diagnosticId : expected!.id;
      const result = await downloadDiagnostic(
        id,
        async (offset) => {
          validate();
          const chunk = await this.call(
            "terminal_read_diagnostic",
            { diagnostic_id: id, offset, max_bytes: 65536 },
            abort,
          );
          validate();
          return chunk;
        },
        write,
        abort,
        expected,
      );
      validate();
      return result;
    } catch (error) {
      validate();
      if (error instanceof HostError) throw error;
      throw new HostError(
        "tool_error",
        "Diagnostic archive is unavailable or the Host request failed. It may have expired or been evicted; create a new snapshot.",
        "diagnostic_unavailable",
      );
    }
  }

  /** Start at the current output tail, registering real background use without PTY size authority. */
  async captureOutput(id: string, signal: AbortSignal): Promise<OutputCapture> {
    const sessionId = this.attachment(id).sessionId;
    if (this.captures.size >= 4)
      throw new HostError("busy", "Stop a recording before starting another.");
    const key = randomUUID();
    const controller = new AbortController();
    const connection = this.connection;
    const presence = this.presence;
    const combined = AbortSignal.any([
      signal,
      controller.signal,
      connection.signal,
    ]);
    this.captures.set(key, controller);
    let attached = false,
      closed = false,
      reading = false;
    const close = async () => {
      if (closed) return;
      closed = true;
      controller.abort();
      this.captures.delete(key);
      await presence?.unuse(key);
      if (
        attached &&
        this.connection === connection &&
        !connection.signal.aborted
      ) {
        await this.call(
          "terminal_session_detach",
          { session_id: sessionId, actor: "user" },
          AbortSignal.timeout(500),
        ).catch(() => {});
      }
    };
    try {
      await presence?.use(key, sessionId, "monitor");
      combined.throwIfAborted();
      const result = await this.call(
        "terminal_session_attach",
        { session_id: sessionId, actor: "user" },
        combined,
      );
      if (result.attached !== true) throw incompatible();
      attached = true;
      // Capture is explicitly forward-only. This one-byte snapshot establishes the tail;
      // it is discarded, never described as recording pre-existing history.
      const tail = capturedOutput(
        await this.call(
          "terminal_read",
          { session_id: sessionId, encoding: "base64", max_bytes: 1 },
          combined,
        ),
      );
      combined.throwIfAborted();
      let cursor = tail.next;
      return {
        sessionId,
        start: cursor,
        close,
        read: async () => {
          if (reading)
            throw new HostError("busy", "A recording read is already pending.");
          combined.throwIfAborted();
          if (connection !== this.connection)
            throw new HostError("cancelled", "Recording connection changed.");
          reading = true;
          try {
            const result = await this.call(
              "terminal_read",
              {
                session_id: sessionId,
                encoding: "base64",
                max_bytes: 65536,
                cursor,
                timeout_ms: 1000,
              },
              combined,
            );
            combined.throwIfAborted();
            const output = capturedOutput(result, cursor);
            cursor = output.next;
            return output;
          } catch (error) {
            if (error instanceof ToolFailure && error.code === "timeout")
              return undefined;
            throw error;
          } finally {
            reading = false;
          }
        },
      };
    } catch (error) {
      await close();
      throw error;
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

  /** Fence queued input before local endpoint persistence; accepted writes are never replayed. */
  discardQueuedInput(): void {
    for (const active of this.attachments.values()) active.writeVersion++;
  }

  /** Capture exact input and original attachment/control before native confirmation. */
  prepareUBoot(id: string, request: UBootRequest) {
    if (!isUBootRequest(request))
      throw new HostError(
        "invalid_input",
        "Provide verified board settings for one U-Boot attempt.",
      );
    if (!this.presence?.ubootAvailable)
      throw new HostError(
        "incompatible",
        "Update the Host for cancellable U-Boot operations.",
      );
    const active = this.attachment(id),
      control = this.controls.get(active.sessionId);
    const version = active.writeVersion;
    const captured = { ...request, bootStarted: [...request.bootStarted] };
    let consumed = false;
    return {
      sessionId: active.sessionId,
      request: captured,
      run: async (
        signal: AbortSignal,
        ready: (operationId: string | undefined) => void,
      ): Promise<UBootOutcome> => {
        if (consumed)
          throw new HostError("cancelled", "U-Boot attempt already submitted.");
        consumed = true;
        if (this.ubootRunning)
          throw new HostError("busy", "A U-Boot attempt is already pending.");
        const validate = () => {
          if (
            this.attachment(id) !== active ||
            active.writeVersion !== version ||
            this.controls.get(active.sessionId)?.version !== control?.version
          )
            throw new HostError(
              "cancelled",
              "U-Boot target or control changed; attempt discarded.",
            );
          signal.throwIfAborted();
        };
        validate();
        const client = this.client,
          presence = this.presence;
        if (!client || !presence)
          throw new HostError("disconnected", "Connect to a Host first.");
        this.ubootRunning = true;
        // One bounded observer and one managed call; no competing raw-output consumer.
        const observeController = new AbortController();
        const callController = new AbortController();
        const combined = AbortSignal.any([
          signal,
          this.connection.signal,
          active.controller.signal,
          callController.signal,
        ]);
        const observeSignal = AbortSignal.any([
          combined,
          observeController.signal,
        ]);
        let observing: Promise<void> | undefined;
        let observationError: unknown;
        let operation:
          Awaited<ReturnType<ClientPresence["operation"]>> | undefined;
        let observedId: string | undefined;
        try {
          const sessions = await this.list();
          validate();
          const session = sessions.find(
            (row) => row.session_id === active.sessionId,
          );
          if (
            !session ||
            session.transport !== "serial" ||
            session.state !== "open"
          )
            throw new HostError(
              "invalid_input",
              "U-Boot requires an open serial Session.",
            );
          if (
            session.lease ||
            session.control?.writable === false ||
            active.queuedBytes
          )
            throw new HostError(
              "busy",
              "Finish current input or managed work before U-Boot.",
            );
          operation = await presence.operation(active.sessionId, false);
          validate();
          const source = operation.source;
          if (!source?.instanceId || !source.operationId)
            throw new HostError(
              "incompatible",
              "U-Boot readiness requires registered operation provenance.",
            );
          const meta = {
            ...presence.metadata(source.operationId),
            ...(control
              ? {
                  "io.channelterm/control": {
                    version: control.version,
                    operation_id: control.operation_id,
                  },
                }
              : {}),
          };
          const tail = await this.call(
            "terminal_session_events",
            { session_id: active.sessionId, max_events: 1 },
            combined,
          );
          if (
            !Array.isArray(tail.events) ||
            tail.events.length > 1 ||
            typeof tail.dropped !== "boolean"
          )
            throw incompatible();
          const feed = new UBootEventFeed(
            active.sessionId,
            source.instanceId,
            source.operationId,
            integer(tail.next),
          );
          validate();
          observing = (async () => {
            while (!observeSignal.aborted) {
              try {
                const page = await this.call(
                  "terminal_session_events",
                  feed.parameters(),
                  observeSignal,
                );
                if (observeSignal.aborted) return;
                const state = feed.apply(page);
                observedId = state.operationId;
                ready(state.ready ? state.operationId : undefined);
                if (state.finished) return;
              } catch (error) {
                if (observeSignal.aborted) return;
                if (error instanceof ToolFailure && error.code === "timeout")
                  continue;
                throw error;
              }
            }
          })().catch((error) => {
            observationError = error;
            callController.abort();
          });
          this.ubootHTTP = {
            useId: source.operationId,
            signal: combined,
            timeoutMS: captured.timeoutMS + 5000,
          };
          const response = await client.callTool(
            {
              name: "terminal_enter_uboot",
              arguments: ubootArguments(active.sessionId, captured),
              _meta: meta,
            },
            undefined,
            { signal: combined, timeout: captured.timeoutMS + 5000 },
          );
          if (observationError) throw observationError;
          const data = object(response.structuredContent),
            failed = response.isError === true;
          const raw = failed
            ? data.result === undefined
              ? {}
              : object(data.result)
            : data;
          // Admission failures have no operation evidence; never manufacture an outcome.
          if (failed && !raw.operation_id) {
            const code = text(object(data.error).code);
            throw new HostError(
              code === "permission_denied"
                ? "unauthorized"
                : code === "session_busy" || code === "control_busy"
                  ? "busy"
                  : "tool_error",
              "Host rejected the U-Boot attempt; no automatic retry.",
            );
          }
          const result = ubootOutcome(raw, active.sessionId, failed);
          if (observedId && result.operationId !== observedId)
            throw incompatible();
          return result;
        } catch (error) {
          if (observationError instanceof HostError) throw observationError;
          if (error instanceof HostError) throw error;
          throw new HostError(
            combined.aborted ? "cancelled" : "tool_error",
            "U-Boot outcome was not confirmed. Do not repeat a reset automatically.",
          );
        } finally {
          this.ubootHTTP = undefined;
          observeController.abort();
          await observing;
          ready(undefined);
          await operation?.done();
          this.ubootRunning = false;
        }
      },
    };
  }

  /** Capture exact input and original attachment/control before native confirmation. */
  prepareDebugWrite(id: string, payload: DebugPayload) {
    const active = this.attachment(id);
    const control = this.controls.get(active.sessionId);
    const version = active.writeVersion;
    const encoded = debugBytes(payload, active.codec);
    let consumed = false;
    const validate = () => {
      if (
        this.attachment(id) !== active ||
        active.writeVersion !== version ||
        this.controls.get(active.sessionId)?.version !== control?.version
      )
        throw new HostError(
          "cancelled",
          "Debug input target or control changed; input discarded.",
        );
    };
    return {
      sessionId: active.sessionId,
      encoding: payload.mode === "hex" ? "hex" : active.settings.encoding,
      hex: encoded.toString("hex").match(/.{2}/g)!.join(" "),
      bytes: encoded.length,
      send: async () => {
        if (consumed)
          throw new HostError("cancelled", "Debug input already consumed.");
        consumed = true;
        validate();
        await this.writePayload(id, encoded, undefined, validate);
        return encoded.length;
      },
    };
  }

  async write(id: string, data: string): Promise<void> {
    input(data);
    const active = this.attachment(id);
    if (active.settings.view === "hex")
      throw new HostError("invalid_input", "Hex view is read-only.");
    return this.writePayload(
      id,
      active.codec.encode(data),
      active.settings.encoding === "utf-8" ? data : undefined,
    );
  }

  /** All input shares one bounded queue and Host authority, including explicit bytes. */
  private async writePayload(
    id: string,
    encoded: Buffer,
    utf8?: string,
    validate?: () => void,
  ): Promise<void> {
    const active = this.attachment(id);
    const control = this.controls.get(active.sessionId);
    if (control && !control.writable)
      throw new HostError(
        "busy",
        "Host control blocks input; input was discarded.",
      );
    if (this.closing.has(active.sessionId))
      throw new HostError(
        "busy",
        "Session closure is pending; input was discarded.",
      );
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
        validate?.();
        if (version !== active.writeVersion)
          throw new HostError(
            "cancelled",
            "Queued input discarded after a failed write.",
          );
        const result = await this.call(
          "terminal_write",
          {
            session_id: active.sessionId,
            data: utf8 ?? encoded.toString("base64"),
            encoding: utf8 === undefined ? "base64" : "utf8",
            actor: "user",
          },
          active.controller.signal,
          control,
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

  /** Private Main-to-companion provenance, never a Renderer capability. */
  async fileOperation(sessionId: string, delegate = true) {
    return (
      this.presence?.operation(sessionId, delegate) ?? {
        delegation: "",
        source: undefined,
        done: async () => {},
      }
    );
  }

  async disconnect(): Promise<void> {
    for (const controller of this.captures.values()) controller.abort();
    this.captures.clear();
    this.controls.clear();
    // Abort immediately, including an in-progress handshake. Detach is advisory;
    // a disconnected MCP client never owns the underlying Session lifetime.
    const attachments = [...this.attachments.values()];
    this.attachments.clear();
    for (const active of attachments) active.controller.abort();
    const sizes = [
      ...this.pendingSizes.values(),
      ...attachments.flatMap((active) =>
        active.sizeControl ? [active.sizeControl] : [],
      ),
    ];
    this.pendingSizes.clear();
    const client = this.client;
    const presence = this.presence;
    this.presence = undefined;
    this.client = undefined;
    if (client && !this.connection.signal.aborted) {
      await Promise.all(
        attachments.map(async (active) => {
          try {
            await client.callTool(
              {
                name: "terminal_session_detach",
                _meta: presence?.metadata(),
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
    await presence?.close();
    this.connection.abort();
    // A setup may have returned during the bounded detach notifications. Revoke
    // those pending capabilities too, after disabling new attachment work.
    sizes.push(...this.pendingSizes.values());
    this.pendingSizes.clear();
    await Promise.all(sizes.map((size) => size.dispose()));
    await client?.close().catch(() => {});
    this.lifetime = "unknown";
    this.hostVersion = undefined;
    this.idle = undefined;
    this.sshAvailable = false;
    this.sshAdvanced = false;
    this.resizeAvailable = false;
    this.transferEvents.clear();
    this.eventReads.clear();
  }
}
