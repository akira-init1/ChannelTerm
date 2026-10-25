import { setupI18n } from "@lingui/core";
import type { SessionSummary } from "../shared/api";
import type { MenuLocale } from "../shared/menus";
import { menuCatalogs } from "../locales/catalogs";
import { hostEndpoint } from "./host/config";
import { HostError } from "./host/errors";
import type { HostClient } from "./host/client";
import { isMenuLocale } from "../shared/menus";

function field(value: string, empty = false): string {
  if (
    (!empty && !value.trim()) ||
    new TextEncoder().encode(value).length > 4096 ||
    /[\x00-\x1f\x7f-\x9f\u2028\u2029\u202a-\u202e\u2066-\u2069]/.test(value)
  )
    throw new HostError(
      "invalid_input",
      "Target metadata cannot be copied as bounded single-line text.",
    );
  return value;
}

/** Format only fresh Host metadata; no terminal output, credentials or operation authorization. */
export function targetInstructions(
  target: SessionSummary,
  endpoint: string,
  locale: MenuLocale,
): string {
  const i18n = setupI18n({ locale, messages: menuCatalogs });
  return i18n._("target.instructions", {
    name: field(target.label, true) || field(target.endpoint),
    host: field(hostEndpoint(endpoint).href),
    sessionId: field(target.session_id),
    reference: field(target.session_ref),
    transport: field(target.transport),
    endpoint: field(target.endpoint),
  });
}

/** Revalidate this client's captured handle after fetching metadata and before writing the clipboard. */
export async function copyAITarget(
  client: HostClient,
  attachmentId: string,
  locale: unknown,
  writeText: (text: string) => void | Promise<void>,
): Promise<void> {
  if (!isMenuLocale(locale))
    throw new HostError("invalid_input", "Invalid copy language.");
  const { attachment } = client.describeAttachment(attachmentId);
  const target = (await client.list()).find(
    (entry) => entry.session_id === attachment.sessionId,
  );
  client.validateAttachment(attachmentId);
  if (!target || target.state !== "open")
    throw new HostError(
      "closed",
      "Select an open Session before copying its target.",
    );
  await writeText(targetInstructions(target, client.info().endpoint, locale));
}
