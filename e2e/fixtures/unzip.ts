import { inflateRawSync } from "node:zlib";

const CENTRAL_HEADER = 0x02014b50;
const END_OF_CENTRAL = 0x06054b50;
const STORED = 0;
const LOCAL_HEADER_SIZE = 30;

/** The files of a zip by name, as text, read from its central directory; stored and deflated entries only. */
export function unzipText(zip: Buffer): Record<string, string> {
  const end = zip.lastIndexOf(Buffer.from([0x50, 0x4b, 0x05, 0x06]));
  if (end < 0 || zip.readUInt32LE(end) !== END_OF_CENTRAL) throw new Error("not a zip");
  const count = zip.readUInt16LE(end + 10);
  let at = zip.readUInt32LE(end + 16);
  const out: Record<string, string> = {};
  for (let i = 0; i < count; i++) {
    if (zip.readUInt32LE(at) !== CENTRAL_HEADER) throw new Error("a broken central directory");
    const method = zip.readUInt16LE(at + 10);
    const size = zip.readUInt32LE(at + 20);
    const nameLength = zip.readUInt16LE(at + 28);
    const extraLength = zip.readUInt16LE(at + 30);
    const commentLength = zip.readUInt16LE(at + 32);
    const local = zip.readUInt32LE(at + 42);
    const name = zip.toString("utf8", at + 46, at + 46 + nameLength);
    const start = local + LOCAL_HEADER_SIZE + zip.readUInt16LE(local + 26) + zip.readUInt16LE(local + 28);
    const data = zip.subarray(start, start + size);
    out[name] = (method === STORED ? data : inflateRawSync(data)).toString("utf8");
    at += 46 + nameLength + extraLength + commentLength;
  }
  return out;
}
