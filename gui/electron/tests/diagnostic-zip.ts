import assert from "node:assert/strict";
import { inflateRawSync } from "node:zlib";

/** Inspect the fixture's small ZIP through its central directory; never extract paths. */
export function diagnosticZipFiles(zip: Buffer): Map<string, Buffer> {
  const end = zip.lastIndexOf(Buffer.from([0x50, 0x4b, 0x05, 0x06]));
  assert.ok(end >= 0);
  const count = zip.readUInt16LE(end + 10),
    files = new Map<string, Buffer>();
  let cursor = zip.readUInt32LE(end + 16);
  for (let index = 0; index < count; index++) {
    assert.equal(zip.readUInt32LE(cursor), 0x02014b50);
    const method = zip.readUInt16LE(cursor + 10),
      size = zip.readUInt32LE(cursor + 20);
    const nameLength = zip.readUInt16LE(cursor + 28),
      extra = zip.readUInt16LE(cursor + 30),
      comment = zip.readUInt16LE(cursor + 32);
    const local = zip.readUInt32LE(cursor + 42),
      name = zip.subarray(cursor + 46, cursor + 46 + nameLength).toString();
    assert.equal(zip.readUInt32LE(local), 0x04034b50);
    const start =
      local + 30 + zip.readUInt16LE(local + 26) + zip.readUInt16LE(local + 28);
    const compressed = zip.subarray(start, start + size);
    assert.ok(method === 0 || method === 8);
    files.set(name, method === 8 ? inflateRawSync(compressed) : compressed);
    cursor += 46 + nameLength + extra + comment;
  }
  return files;
}
