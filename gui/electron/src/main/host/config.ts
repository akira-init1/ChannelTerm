import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { HostError } from "./errors";

/** The prototype connects only to explicitly configured loopback Hosts. */
export function hostEndpoint(value = "http://127.0.0.1:37099/mcp"): URL {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new HostError("configuration", "Invalid Host endpoint.");
  }
  if (
    url.protocol !== "http:" ||
    !["127.0.0.1", "[::1]"].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  ) {
    throw new HostError(
      "configuration",
      "Use an HTTP loopback IP endpoint without credentials, query, or fragment.",
    );
  }
  return url;
}

/** Match Go os.UserConfigDir, not Electron's application-specific userData path. */
export function tokenPath(
  platform: NodeJS.Platform,
  env: NodeJS.ProcessEnv,
  home: string,
): string {
  const root =
    platform === "win32"
      ? env.APPDATA
      : platform === "darwin"
        ? join(home, "Library", "Application Support")
        : env.XDG_CONFIG_HOME || join(home, ".config");
  if (!root || !isAbsolute(root))
    throw new HostError(
      "configuration",
      "Cannot locate the Host credential directory.",
    );
  return join(root, "channelterm", "http-auth-token");
}

export async function loadToken(): Promise<string> {
  let token = process.env.CHANNELTERM_HTTP_AUTH_TOKEN?.trim();
  if (!token) {
    try {
      token = (
        await readFile(
          tokenPath(process.platform, process.env, homedir()),
          "utf8",
        )
      ).trim();
    } catch {
      throw new HostError(
        "configuration",
        "Host token unavailable. Start your Host first or set CHANNELTERM_HTTP_AUTH_TOKEN for this process.",
      );
    }
  }
  if (!token || /\s/.test(token) || token.length > 4096) {
    throw new HostError("configuration", "Host token is empty or malformed.");
  }
  return token;
}
