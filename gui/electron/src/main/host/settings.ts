import { readFile, writeFile, rename, mkdir, rm, stat } from "node:fs/promises";
import { dirname } from "node:path";
import { randomUUID } from "node:crypto";
import { hostEndpoint } from "./config";
import { HostError } from "./errors";

/** One bounded local default. Existing windows retain their captured endpoint. */
export class HostSettings {
  private saved?: string;
  private saving = false;
  readonly locked: boolean;

  constructor(
    private readonly path: string,
    private readonly override?: string,
  ) {
    this.locked = override !== undefined;
  }

  async load(): Promise<void> {
    try {
      if ((await stat(this.path)).size > 4096) return;
      const raw = await readFile(this.path, "utf8");
      if (raw.length > 4096) return;
      const value: unknown = JSON.parse(raw);
      if (
        value &&
        typeof value === "object" &&
        "version" in value &&
        value.version === 1 &&
        "endpoint" in value &&
        typeof value.endpoint === "string" &&
        Object.keys(value).length === 2
      )
        this.saved = validateEndpoint(value.endpoint);
    } catch {
      // Invalid/unreadable saved preferences never authorize a new endpoint.
    }
  }

  endpoint(): string {
    return hostEndpoint(this.override ?? this.saved).href;
  }

  async save(value: unknown): Promise<string> {
    const endpoint = validateEndpoint(value);
    if (this.locked)
      throw new HostError(
        "configuration",
        "The environment overrides Host settings.",
      );
    if (this.saving)
      throw new HostError("busy", "A Host setting is being saved.");
    this.saving = true;
    const temporary = `${this.path}.${randomUUID()}.tmp`;
    try {
      await mkdir(dirname(this.path), { recursive: true });
      await writeFile(
        temporary,
        JSON.stringify({ version: 1, endpoint }) + "\n",
        { flag: "wx", mode: 0o600 },
      );
      await rename(temporary, this.path);
      this.saved = endpoint;
      return endpoint;
    } catch {
      throw new HostError(
        "configuration",
        "Host setting could not be saved. The connection was not changed.",
      );
    } finally {
      await rm(temporary, { force: true }).catch(() => {});
      this.saving = false;
    }
  }
}

/** Reject ambiguous URL spellings before WHATWG normalization, without echoing input. */
export function validateEndpoint(value: unknown): string {
  if (
    typeof value !== "string" ||
    value.length > 2048 ||
    /[?#]/.test(value) ||
    !/^http:\/\/(127\.0\.0\.1|\[::1\])(?::[0-9]{1,5})?(?:\/[^\s\\]*)?$/.test(
      value,
    )
  )
    throw new HostError(
      "configuration",
      "Use a numeric loopback HTTP endpoint.",
    );
  const url = hostEndpoint(value);
  if (url.port === "0")
    throw new HostError("configuration", "Use a nonzero Host port.");
  return url.href;
}
