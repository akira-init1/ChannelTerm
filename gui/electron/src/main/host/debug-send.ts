import {
  debugByteLimit,
  isDebugPayload,
  type DebugPayload,
} from "../../shared/debug-send";
import { TerminalCodec } from "./terminal-codec";
import { HostError } from "./errors";

/** Encode once before confirmation, preserving exact bytes through publication. */
export function debugBytes(
  payload: DebugPayload,
  codec: TerminalCodec,
): Buffer {
  if (!isDebugPayload(payload))
    throw new HostError("invalid_input", "Invalid debug input.");
  let bytes: Buffer;
  if (payload.mode === "hex") {
    if (!/^[0-9a-fA-F \t\r\n]+$/.test(payload.data))
      throw new HostError(
        "invalid_input",
        "HEX requires complete hexadecimal byte pairs.",
      );
    const compact = payload.data.replace(/[ \t\r\n]/g, "");
    if (!compact || compact.length % 2)
      throw new HostError(
        "invalid_input",
        "HEX requires complete hexadecimal byte pairs.",
      );
    bytes = Buffer.from(compact, "hex");
  } else bytes = codec.encode(payload.data);
  const ending = { none: "", lf: "\n", cr: "\r", crlf: "\r\n" }[payload.ending];
  const result = Buffer.concat([bytes, Buffer.from(ending, "ascii")]);
  if (result.length === 0 || result.length > debugByteLimit)
    throw new HostError(
      "invalid_input",
      "Debug input including line ending must be 1–4096 bytes.",
    );
  return result;
}
