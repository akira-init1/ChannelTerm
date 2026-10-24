import type { DesktopAPI, Result } from "../shared/api";

declare global {
  interface Window {
    channelterm: DesktopAPI;
  }
}

/** Renderer-side adapter unwraps the narrow bridge without knowing MCP or credentials. */
export async function unwrap<T>(result: Promise<Result<T>>): Promise<T> {
  const value = await result;
  if (!value.ok) throw value.error;
  return value.value;
}

export const host = window.channelterm;
