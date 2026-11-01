import { HostError } from "./errors";
import { object } from "./validation";

/** Main-only, per-attachment capability. One bounded request, never a resize queue. */
export class RemoteSize {
  private pending?: Promise<boolean>;
  private disposed = false;

  constructor(
    private readonly endpoint: URL,
    private readonly token: string,
    private readonly sessionId: string,
    private owner: string,
    private readonly connection: AbortSignal,
  ) {}

  static validOwner(owner: unknown): owner is string {
    return typeof owner === "string" && /^[a-f0-9]{64}$/.test(owner);
  }

  get owned(): boolean {
    return this.owner !== "";
  }

  async request(
    action: "status" | "take" | "release" | "resize",
    columns?: number,
    rows?: number,
  ): Promise<boolean> {
    if (this.disposed)
      throw new HostError("cancelled", "This size controller has ended.");
    if (this.pending)
      throw new HostError("busy", "A size request is in progress.");
    if (action === "resize" && !this.owner) return false;
    const pending = this.send(action, columns, rows);
    this.pending = pending;
    try {
      return await pending;
    } finally {
      this.pending = undefined;
    }
  }

  private async send(
    action: string,
    columns?: number,
    rows?: number,
    cleanup = false,
  ): Promise<boolean> {
    let response: Response;
    try {
      response = await fetch(new URL("/gui/v1/resize", this.endpoint), {
        method: "POST",
        redirect: "error",
        headers: {
          Authorization: `Bearer ${this.token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          action,
          session_id: this.sessionId,
          owner: this.owner,
          columns,
          rows,
        }),
        signal: AbortSignal.any([
          ...(cleanup ? [] : [this.connection]),
          AbortSignal.timeout(5000),
        ]),
      });
    } catch {
      // An uncertain resize/take is not replayed. Explicit takeover can recover
      // an orphaned owner after a lost response; guessing cannot grant authority.
      throw new HostError(
        "tool_error",
        "Remote size result is uncertain; the request was not replayed.",
      );
    }
    if (response.status === 401 || response.status === 403) {
      await response.body?.cancel();
      throw new HostError("unauthorized", "Host rejected remote size control.");
    }
    const payload = object(await response.json());
    if (response.status === 409 && payload.error === "resize_not_owned") {
      this.owner = "";
      return false;
    }
    if (!response.ok)
      throw new HostError(
        payload.error === "busy" ? "busy" : "tool_error",
        "Host could not change remote size control.",
      );
    if (typeof payload.owned !== "boolean")
      throw new HostError("incompatible", "Invalid remote size response.");
    if (action === "take") {
      if (!RemoteSize.validOwner(payload.owner) || !payload.owned)
        throw new HostError("incompatible", "Invalid remote size capability.");
      this.owner = payload.owner;
    } else if (!payload.owned) this.owner = "";
    return payload.owned;
  }

  /** Cleanup releases only this exact capability, including a late take response. */
  async dispose(): Promise<void> {
    if (this.disposed) return;
    this.disposed = true;
    await this.pending?.catch(() => {});
    if (this.owner)
      await this.send("release", undefined, undefined, true).catch(() => {});
    this.owner = "";
  }
}
