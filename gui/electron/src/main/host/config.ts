import { readFile, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { HostError } from "./errors";

/** Host access is limited to loopback; only the default endpoint permits startup. */
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
export function hostIdentityPath(
  platform: NodeJS.Platform,
  env: NodeJS.ProcessEnv,
  home: string,
): string {
  if (env.CHANNELTERM_HOST_CONFIG_DIR) {
    if (!isAbsolute(env.CHANNELTERM_HOST_CONFIG_DIR))
      throw new HostError(
        "configuration",
        "Host configuration directory must be absolute.",
      );
    return join(env.CHANNELTERM_HOST_CONFIG_DIR, "http-host.json");
  }
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
  return join(root, "channelterm", "http-host.json");
}

export async function loadToken(): Promise<string> {
  if (
    process.env.CHANNELTERM_HTTP_AUTH_TOKEN &&
    !process.env.CHANNELTERM_HTTP_AUTH_TOKEN.trim()
  )
    throw new HostError("configuration", "Host token is empty or malformed.");
  let token = process.env.CHANNELTERM_HTTP_AUTH_TOKEN?.trim();
  if (!token) {
    const manifest = hostIdentityPath(process.platform, process.env, homedir());
    let raw: string;
    try {
      raw = await readFile(manifest, "utf8");
    } catch (error) {
      if (!missingFile(error)) throw unreadableCredential();
      // Presence is only an upgrade hint, never permission to read/use the old token.
      let legacy = false;
      try {
        legacy = (await stat(join(manifest, "..", "http-auth-token"))).isFile();
      } catch (error) {
        if (!missingFile(error)) throw unreadableCredential();
      }
      throw new HostError(
        legacy ? "host_legacy_credentials" : "host_identity_missing",
        legacy
          ? "Legacy Host configuration detected; new credentials are missing. The running Host may need upgrading. Finish shared Session work before stopping it; rebuild/update the Host and restart the GUI. Keep the old files for migration."
          : "Host identity is missing. Check that GUI and Host use the same account and CHANNELTERM_HOST_CONFIG_DIR; start the matching updated Host, then retry.",
      );
    }
    let credentialPath: string;
    try {
      if (Buffer.byteLength(raw) > 4096) throw new Error("oversized identity");
      const identity: unknown = JSON.parse(raw);
      if (
        !identity ||
        typeof identity !== "object" ||
        !("version" in identity) ||
        identity.version !== 1 ||
        !("host_id" in identity) ||
        typeof identity.host_id !== "string" ||
        Object.keys(identity).some(
          (key) => key !== "version" && key !== "host_id",
        )
      )
        throw new Error("invalid identity");
      credentialPath = localClientTokenPath(manifest, identity.host_id);
    } catch {
      throw invalidCredential();
    }
    try {
      token = (await readFile(credentialPath, "utf8")).trim();
    } catch (error) {
      if (!missingFile(error)) throw unreadableCredential();
      throw new HostError(
        "host_credential_missing",
        "Local management credential is missing. Check the Host account and configuration directory, then restart the matching Host after finishing shared Session work. Do not delete the Host identity or reuse a shared token.",
      );
    }
    if (!token || /\s/.test(token) || token.length > 4096)
      throw invalidCredential();
  }
  if (!token || /\s/.test(token) || token.length > 4096) {
    throw new HostError("configuration", "Host token is empty or malformed.");
  }
  return token;
}

function missingFile(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "ENOENT";
}
function unreadableCredential(): HostError {
  return new HostError(
    "host_credential_unreadable",
    "Host credentials could not be read. Check file permissions and that GUI and Host use the same account and configuration directory. Credentials were not changed.",
  );
}
function invalidCredential(): HostError {
  return new HostError(
    "host_credential_invalid",
    "Host identity or local credential is invalid. Restore the matching configuration from a trusted backup or diagnose it with the Host administrator. Do not delete or regenerate credentials blindly.",
  );
}

/** Match Core config's Host-domain path; this capability stays in Main. */
export function localClientTokenPath(
  manifestPath: string,
  hostID: string,
): string {
  if (!/^H-[0-9a-f]{32}$/.test(hostID))
    throw new HostError("configuration", "Invalid Host identity.");
  return join(manifestPath, "..", `http-clients-${hostID}.json.local-token`);
}
