import { readFile, stat } from "node:fs/promises";
import { isAbsolute } from "node:path";
import { hostEndpoint } from "./config";
import { HostError, publicError } from "./errors";
import { HostClient } from "./client";
import type { PeerSessions } from "../../shared/api";

import { createHash } from "node:crypto";

export interface Peer {
  id: string;
  label: string;
  endpoint: string;
  tokenFile: string;
}

/** Only operator-provisioned loopback forwards and their matching credential files
 * are eligible. Discovery cannot select a URL, start a Host, or use the local token. */
export async function configuredPeers(
  path = process.env.CHANNELTERM_GUI_PEERS,
): Promise<Peer[]> {
  if (!path) return [];
  try {
    if (!isAbsolute(path) || (await stat(path)).size > 32768) throw Error();
    const rows: unknown = JSON.parse(await readFile(path, "utf8"));
    if (!Array.isArray(rows) || rows.length > 8) throw Error();
    const ids = new Set<string>(),
      endpoints = new Set<string>();
    return rows.map((row) => {
      if (
        !row ||
        typeof row !== "object" ||
        typeof row.id !== "string" ||
        !/^[a-zA-Z0-9_-]{1,64}$/.test(row.id) ||
        typeof row.label !== "string" ||
        !row.label.trim() ||
        row.label.length > 128 ||
        /[\x00-\x1f\x7f]/.test(row.label) ||
        typeof row.tokenFile !== "string" ||
        !isAbsolute(row.tokenFile) ||
        typeof row.endpoint !== "string"
      )
        throw Error();
      const endpoint = hostEndpoint(row.endpoint);
      if (
        endpoint.pathname !== "/mcp" ||
        !endpoint.port ||
        endpoint.port === "37099" ||
        ids.has(row.id) ||
        endpoints.has(endpoint.href)
      )
        throw Error();
      ids.add(row.id);
      endpoints.add(endpoint.href);
      return {
        id: createHash("sha256")
          .update(JSON.stringify([row.id, endpoint.href, row.tokenFile]))
          .digest("hex"),
        label: row.label,
        endpoint: endpoint.href,
        tokenFile: row.tokenFile,
      };
    });
  } catch {
    throw new HostError("configuration", "Invalid trusted Host configuration.");
  }
}

export async function peerToken(peer: Peer): Promise<string> {
  try {
    if ((await stat(peer.tokenFile)).size > 4096) throw Error();
    const token = (await readFile(peer.tokenFile, "utf8")).trim();
    if (!token || /\s/.test(token)) throw Error();
    return token;
  } catch {
    throw new HostError(
      "unauthorized",
      "Cannot load the configured Host credential.",
    );
  }
}

export async function listPeers(): Promise<PeerSessions[]> {
  return Promise.all(
    (await configuredPeers()).map(async (peer) => {
      let client: HostClient | undefined;
      try {
        client = new HostClient(peer.endpoint, await peerToken(peer));
        await client.connect();
        return {
          id: peer.id,
          label: peer.label,
          sessions: await client.list(),
        };
      } catch (e) {
        return {
          id: peer.id,
          label: peer.label,
          sessions: [],
          error: publicError(e).code,
        };
      } finally {
        await client?.disconnect();
      }
    }),
  );
}
