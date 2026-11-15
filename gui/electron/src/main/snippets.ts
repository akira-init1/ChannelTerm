import { readFile, writeFile, mkdir, rename, rm, stat } from "node:fs/promises";
import { dirname } from "node:path";
import { randomUUID } from "node:crypto";
import {
  isCommandSnippet,
  snippetLimit,
  type CommandSnippet,
} from "../shared/debug-send";
import { HostError } from "./host/errors";

/** One Main-owned plaintext library. Explicit saves only; drafts never reach disk. */
export class CommandSnippets {
  private saving = false;
  constructor(private readonly path: string) {}

  async list(): Promise<CommandSnippet[]> {
    try {
      if ((await stat(this.path)).size > 4 * 1024 * 1024)
        throw new Error("size");
      const raw = await readFile(this.path, "utf8");
      if (raw.length > 4 * 1024 * 1024) throw new Error("size");
      const parsed: unknown = JSON.parse(raw);
      if (
        !Array.isArray(parsed) ||
        parsed.length > snippetLimit ||
        !parsed.every(isCommandSnippet) ||
        new Set(parsed.map((row) => row.name)).size !== parsed.length
      )
        throw new Error("invalid");
      return parsed;
    } catch (error) {
      if (
        error &&
        typeof error === "object" &&
        "code" in error &&
        error.code === "ENOENT"
      )
        return [];
      throw new HostError(
        "configuration",
        "Saved snippets could not be read; existing file preserved.",
      );
    }
  }

  async change(value: unknown): Promise<CommandSnippet[]> {
    if (!value || typeof value !== "object" || Array.isArray(value))
      throw new HostError("invalid_input", "Invalid snippet change.");
    const row = value as Record<string, unknown>;
    const save =
      row.action === "save" &&
      Object.keys(row).length === 3 &&
      row.confirmPlaintext === true &&
      isCommandSnippet(row.snippet);
    const remove =
      row.action === "remove" &&
      Object.keys(row).length === 2 &&
      typeof row.name === "string" &&
      row.name.length <= 64;
    if (!save && !remove)
      throw new HostError(
        "invalid_input",
        "Explicit snippet confirmation is required.",
      );
    if (this.saving)
      throw new HostError("busy", "A snippet change is in progress.");
    this.saving = true;
    const temporary = `${this.path}.${randomUUID()}.tmp`;
    try {
      const entries = await this.list();
      const snippet = save ? (row.snippet as CommandSnippet) : undefined;
      const name = snippet?.name ?? row.name;
      const next = entries.filter((entry) => entry.name !== name);
      if (snippet) next.push(snippet);
      if (next.length > snippetLimit)
        throw new HostError(
          "invalid_input",
          "Remove a snippet before saving another.",
        );
      await mkdir(dirname(this.path), { recursive: true });
      await writeFile(temporary, JSON.stringify(next) + "\n", {
        flag: "wx",
        mode: 0o600,
      });
      await rename(temporary, this.path);
      return next;
    } catch (error) {
      if (error instanceof HostError) throw error;
      throw new HostError(
        "configuration",
        "Snippet change was not saved; previous library preserved.",
      );
    } finally {
      await rm(temporary, { force: true }).catch(() => {});
      this.saving = false;
    }
  }
}
