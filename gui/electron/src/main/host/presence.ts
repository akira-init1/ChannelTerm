import { randomUUID } from "node:crypto";
import type { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { ListToolsResultSchema } from "@modelcontextprotocol/sdk/types.js";
import { HostError } from "./errors";
import { object, text, incompatible } from "./validation";

type Credential = { id: string; secret: string };

/** One Main-owned window instance. Capabilities never cross Preload or enter disk state. */
export class ClientPresence {
  private credential?: Credential;
  private timer?: ReturnType<typeof setTimeout>;
  private stopped = false;
  private failed = false;
  private uses = new Map<string, string>();

  constructor(
    private readonly client: Client,
    private readonly signal: AbortSignal,
  ) {}

  private async request(args: Record<string, unknown>, closing = false) {
    const result = await this.client.callTool(
      { name: "client_instance", arguments: args },
      undefined,
      {
        signal: closing
          ? AbortSignal.timeout(1000)
          : AbortSignal.any([this.signal, AbortSignal.timeout(5000)]),
        timeout: closing ? 1000 : 5000,
      },
    );
    if (result.isError)
      throw new HostError(
        "tool_error",
        "Client presence is unavailable. Reconnect explicitly; input was not retried.",
      );
    return object(result.structuredContent);
  }

  async start(): Promise<void> {
    // Discovery must not switch existing calls to the SDK's cached success-only
    // validator: SDK 1.32 also applies it to our structured error results. Keep
    // the existing operation-specific Main validation and structured error codes.
    const listed = await this.client.request(
      { method: "tools/list", params: {} },
      ListToolsResultSchema,
      { signal: this.signal, timeout: 5000 },
    );
    if (!listed.tools.some((tool) => tool.name === "client_instance")) return;
    const result = await this.request({
      action: "register",
      name: "Electron window",
      entry: "electron",
    });
    const credential = object(result.credential);
    const id = text(credential.id),
      secret = text(credential.secret);
    if (
      !/^CI-[A-Za-z0-9]{16,64}$/.test(id) ||
      !/^[A-Za-z0-9]{20,128}$/.test(secret)
    )
      throw incompatible();
    this.credential = { id, secret };
    if (this.stopped || this.signal.aborted) {
      await this.close();
      return;
    }
    this.schedule();
  }

  private schedule() {
    this.timer = setTimeout(() => {
      void this.request({ action: "renew", credential: this.credential }).then(
        () => {
          if (!this.stopped && !this.signal.aborted) this.schedule();
        },
        () => {
          this.failed = true;
        },
      );
    }, 20000);
    this.timer.unref();
  }

  metadata() {
    if (this.failed)
      throw new HostError(
        "disconnected",
        "Client presence expired. Reconnect explicitly; input was not retried.",
      );
    return this.credential
      ? { "io.channelterm/instance": this.credential }
      : undefined;
  }

  async use(
    key: string,
    sessionId: string,
    kind: "monitor" | "operation",
  ): Promise<void> {
    this.metadata();
    if (!this.credential) return;
    const result = await this.request({
      action: "use",
      credential: this.credential,
      key,
      session_id: sessionId,
      kind,
    });
    const id = text(object(result.use).id);
    if (!/^CU-[A-Za-z0-9]{16,64}$/.test(id)) throw incompatible();
    this.uses.set(key, id);
  }

  async unuse(key: string): Promise<void> {
    const id = this.uses.get(key);
    this.uses.delete(key);
    if (id)
      await this.request(
        { action: "unuse", credential: this.credential, use_id: id },
        true,
      ).catch(() => {});
  }

  /** Declare a local file operation; delegation links a serial CLI child to this exact target. */
  async operation(sessionId: string, delegate: boolean) {
    const key = `file:${randomUUID()}`;
    await this.use(key, sessionId, "operation");
    try {
      const id = this.uses.get(key);
      let delegation = "";
      if (id && delegate) {
        const result = await this.request({
          action: "delegate",
          credential: this.credential,
          use_id: id,
        });
        delegation = text(result.delegation);
        if (!/^[A-Za-z0-9]{20,128}$/.test(delegation)) throw incompatible();
      }
      return { delegation, done: () => this.unuse(key) };
    } catch (error) {
      await this.unuse(key);
      throw error;
    }
  }

  async close(): Promise<void> {
    this.stopped = true;
    clearTimeout(this.timer);
    const credential = this.credential;
    this.credential = undefined;
    this.uses.clear();
    if (credential)
      await this.request({ action: "end", credential }, true).catch(() => {});
  }
}
