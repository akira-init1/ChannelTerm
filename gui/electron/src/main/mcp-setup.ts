import { spawn } from "node:child_process";
import { isAbsolute } from "node:path";
import type {
  MCPSetupClient,
  MCPSetupRequest,
  MCPSetupResult,
} from "../shared/mcp-setup";
import { HostError } from "./host/errors";
import { hostEndpoint } from "./host/config";

type Reply = { ok: boolean; code?: string; value?: unknown };
type Preview = {
  path: string;
  before: string;
  after: string;
  existing: boolean;
  fingerprint: string;
  official: string;
};

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new HostError(
      "incompatible",
      "Client setup companion returned an invalid result.",
    );
  return value as Record<string, unknown>;
}

function result(reply: Reply): MCPSetupResult {
  const row = reply.value ? object(reply.value) : {};
  if (!reply.ok) {
    const code: MCPSetupResult["code"] =
      reply.code === "recovery_required"
        ? "recovery"
        : reply.code === "configuration_changed"
          ? "changed"
          : reply.code === "permission_denied"
            ? "permission"
            : reply.code === "authentication_unavailable"
              ? "unavailable"
              : reply.code === "host_unreachable"
                ? "network"
                : reply.code === "authentication_failed" ||
                    reply.code === "authentication_or_host" ||
                    reply.code === "credential_unavailable"
                  ? "authentication"
                  : reply.code === "apply_failed"
                    ? "apply"
                    : "configuration";
    return { state: "failed", code };
  }
  const states = [
    "preserved",
    "applied",
    "verified_existing",
    "recovered_applied",
    "rolled_back",
    "none",
  ] as const;
  const state = states.find((state) => state === row.state);
  if (!state)
    throw new HostError("incompatible", "Unknown client setup result.");
  if (
    row.client_id !== undefined &&
    (typeof row.client_id !== "string" ||
      !/^CL-[a-f0-9]{32}$/.test(row.client_id))
  )
    throw new HostError("incompatible", "Invalid public client identity.");
  return {
    state,
    clientId: row.client_id as string | undefined,
    permissions: row.permissions === "read_only" ? "read_only" : "unknown",
  };
}

/** Main-only, window-owned local configuration workflow. No generic process IPC. */
export class DesktopMCPSetup {
  private running = false;
  get busy() {
    return this.running;
  }
  constructor(
    private readonly executable: string,
    private readonly invoke = (request: unknown) => this.command(request),
  ) {}

  private async exclusive<T>(work: () => Promise<T>): Promise<T> {
    if (this.running)
      throw new HostError("busy", "Finish the current client setup first.");
    this.running = true;
    try {
      return await work();
    } finally {
      this.running = false;
    }
  }

  async discover(): Promise<MCPSetupClient[]> {
    return this.exclusive(async () => {
      const reply = await this.invoke({
        action: "discover",
        target: { client: "codex", scope: "user" },
      });
      if (!reply.ok || !Array.isArray(reply.value) || reply.value.length !== 4)
        throw new HostError(
          "tool_error",
          "Could not discover supported MCP clients. Check the bundled CLI and local configuration overrides.",
        );
      const ids = new Set<string>();
      return reply.value.map((value): MCPSetupClient => {
        const row = object(value);
        if (
          typeof row.id !== "string" ||
          !["codex", "claude", "opencode", "zoo"].includes(row.id) ||
          ids.has(row.id) ||
          typeof row.detected !== "boolean" ||
          !["unavailable", "needs_explicit_file_fallback"].includes(
            String(row.official),
          )
        )
          throw new HostError(
            "incompatible",
            "Invalid client discovery result.",
          );
        ids.add(row.id);
        return {
          id: row.id as MCPSetupClient["id"],
          detected: row.detected,
          official: row.official as MCPSetupClient["official"],
          version:
            typeof row.version === "string" &&
            /^\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?$/.test(row.version) &&
            row.version.length < 128
              ? row.version
              : undefined,
        };
      });
    });
  }

  async recover(): Promise<MCPSetupResult> {
    return this.exclusive(async () =>
      result(
        await this.invoke({
          action: "recover",
          target: { client: "codex", scope: "user" },
        }),
      ),
    );
  }

  async perform(
    request: MCPSetupRequest,
    endpoint: string,
    chooseProject: () => Promise<string | undefined>,
    confirm: (preview: Preview, inspect: boolean) => Promise<boolean>,
  ): Promise<MCPSetupResult> {
    hostEndpoint(endpoint);
    return this.exclusive(async () => {
      const recovered = result(
        await this.invoke({
          action: "recover",
          target: { client: request.client, scope: "user" },
        }),
      );
      if (recovered.state !== "none") return recovered;
      const project =
        request.scope === "project" ? await chooseProject() : undefined;
      if (request.scope === "project" && !project)
        return { state: "cancelled" };
      const target = {
        client: request.client,
        scope: request.scope,
        ...(project ? { project } : {}),
      };
      const reply = await this.invoke({ action: "preview", target, endpoint });
      if (!reply.ok) return result(reply);
      const response = object(reply.value),
        plan = object(response.plan),
        client = object(response.client);
      if (
        typeof plan.path !== "string" ||
        !isAbsolute(plan.path) ||
        plan.path.length > 32768 ||
        typeof plan.before !== "string" ||
        typeof plan.after !== "string" ||
        plan.before.length > 16384 ||
        plan.after.length > 16384 ||
        typeof plan.existing !== "boolean" ||
        typeof plan.fingerprint !== "string" ||
        !/^[a-f0-9]{64}$/.test(plan.fingerprint) ||
        !["unavailable", "needs_explicit_file_fallback"].includes(
          String(client.official),
        )
      )
        throw new HostError("incompatible", "Invalid configuration preview.");
      const preview: Preview = {
        path: plan.path,
        before: plan.before,
        after: plan.after,
        existing: plan.existing,
        fingerprint: plan.fingerprint,
        official: String(client.official),
      };
      if (request.action === "verify")
        return result(
          await this.invoke({
            action: "verify",
            target,
            endpoint,
            fingerprint: preview.fingerprint,
          }),
        );
      if (!(await confirm(preview, request.action === "inspect")))
        return { state: request.action === "inspect" ? "viewed" : "cancelled" };
      if (request.action === "inspect") return { state: "viewed" };
      return result(
        await this.invoke({
          action: "apply",
          target,
          endpoint,
          fingerprint: preview.fingerprint,
          replace: request.replace,
          file_fallback: true,
        }),
      );
    });
  }

  private command(request: unknown): Promise<Reply> {
    return new Promise((resolve, reject) => {
      const env = { ...process.env };
      // Setup creates its own read-only credential via local authority. Neither it
      // nor official help subprocesses receive this window's management token.
      for (const key of Object.keys(env))
        if (key.toUpperCase() === "CHANNELTERM_HTTP_AUTH_TOKEN")
          delete env[key];
      const child = spawn(this.executable, ["init", "--mcp-desktop"], {
        env,
        windowsHide: true,
        stdio: ["pipe", "pipe", "ignore"],
      });
      let bytes = 0;
      const chunks: Buffer[] = [];
      let failed = false;
      const fail = () => {
        if (failed) return;
        failed = true;
        child.kill();
        reject(
          new HostError(
            "tool_error",
            "Client setup did not finish. Reopen setup to inspect recovery before trying another change.",
          ),
        );
      };
      const timer = setTimeout(fail, 55000);
      child.on("error", fail);
      child.stdin.on("error", fail);
      child.stdout.on("data", (chunk: Buffer) => {
        bytes += chunk.length;
        if (bytes > 65536) fail();
        else chunks.push(chunk);
      });
      child.on("close", (code) => {
        clearTimeout(timer);
        if (failed) return;
        if (code !== 0) {
          fail();
          return;
        }
        try {
          const value = object(
            JSON.parse(Buffer.concat(chunks).toString("utf8")),
          );
          if (
            typeof value.ok !== "boolean" ||
            (value.code !== undefined && typeof value.code !== "string")
          )
            throw new Error("invalid reply");
          resolve(value as Reply);
        } catch {
          fail();
        }
      });
      child.stdin.end(JSON.stringify(request));
    });
  }
}
