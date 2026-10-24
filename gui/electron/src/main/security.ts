import type { IpcMainInvokeEvent, WebContents } from "electron";
import { HostError } from "./host/errors";

export const productionURL = "channelterm://app/index.html";
export const developmentURL = "http://127.0.0.1:5173/";

export function trustedURL(url: string, development: boolean): boolean {
  return url === (development ? developmentURL : productionURL);
}

/** A trusted URL alone is insufficient: reject subframes and other WebContents. */
export function validateSender(
  event: IpcMainInvokeEvent,
  contents: WebContents | undefined,
  development: boolean,
): void {
  if (
    !contents ||
    event.sender !== contents ||
    event.senderFrame !== contents.mainFrame ||
    !event.senderFrame ||
    !trustedURL(event.senderFrame.url, development)
  ) {
    throw new HostError("unauthorized", "Untrusted IPC caller.");
  }
}

export function contentPolicy(development: boolean, nonce?: string): string {
  const scriptNonce =
    development && nonce && /^[a-f0-9]{32}$/.test(nonce)
      ? ` 'nonce-${nonce}'`
      : "";
  return (
    `default-src 'none'; script-src 'self'${scriptNonce}; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; ` +
    (development
      ? "connect-src 'self' ws://127.0.0.1:5173; "
      : "connect-src 'none'; ") +
    "object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'"
  );
}
