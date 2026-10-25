import { createHash } from "node:crypto";
import { isDiagnosticID } from "../../shared/diagnostics";
import { incompatible, integer, text } from "./validation";
import { HostError } from "./errors";

export interface DiagnosticMetadata {
  id: string;
  size: number;
  sha256: string;
}

export function diagnosticMetadata(
  value: Record<string, unknown>,
  id: unknown,
): DiagnosticMetadata {
  const size = integer(value.size),
    sha256 = text(value.sha256);
  if (
    !isDiagnosticID(id) ||
    size < 1 ||
    size > 16 * 1024 * 1024 ||
    !/^[a-f0-9]{64}$/.test(sha256)
  )
    throw incompatible();
  return { id, size, sha256 };
}

/** Stream bounded chunks; a saved archive is successful only after full size/hash verification. */
export async function downloadDiagnostic(
  id: string,
  read: (offset: number) => Promise<Record<string, unknown>>,
  write: (data: Buffer) => Promise<void>,
  signal: AbortSignal,
  expected?: DiagnosticMetadata,
): Promise<DiagnosticMetadata> {
  const hash = createHash("sha256");
  let offset = 0,
    metadata = expected;
  for (;;) {
    signal.throwIfAborted();
    const chunk = await read(offset);
    signal.throwIfAborted();
    const current = diagnosticMetadata(chunk, id);
    metadata ??= current;
    if (current.size !== metadata.size || current.sha256 !== metadata.sha256)
      throw incompatible();
    const count = integer(chunk.bytes_read),
      next = integer(chunk.next),
      data = text(chunk.data);
    if (
      chunk.encoding !== "base64" ||
      count < 1 ||
      count > 65536 ||
      data.length > 87384 ||
      typeof chunk.eof !== "boolean" ||
      next !== offset + count ||
      next > metadata.size ||
      chunk.eof !== (next === metadata.size)
    )
      throw incompatible();
    const bytes = Buffer.from(data, "base64");
    if (bytes.length !== count || bytes.toString("base64") !== data)
      throw incompatible();
    hash.update(bytes);
    await write(bytes);
    offset = next;
    if (chunk.eof) {
      signal.throwIfAborted();
      if (offset !== metadata.size || hash.digest("hex") !== metadata.sha256)
        throw new HostError(
          "incompatible",
          "Diagnostic ZIP failed size or SHA-256 verification. Create a new snapshot.",
        );
      return metadata;
    }
  }
}
