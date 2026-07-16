import { createHash } from "node:crypto";

export const POINT_ID_NAMESPACE = "a1f0e7c2-7a6b-5c74-9f3e-6c8d91b3a204";

export function uuidV5(name: string, namespace = POINT_ID_NAMESPACE): string {
  const namespaceBytes = uuidToBytes(namespace);
  const digest = createHash("sha1")
    .update(namespaceBytes)
    .update(name, "utf8")
    .digest()
    .subarray(0, 16);
  digest[6] = (digest[6]! & 0x0f) | 0x50;
  digest[8] = (digest[8]! & 0x3f) | 0x80;
  const hex = digest.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function uuidToBytes(value: string): Buffer {
  const hex = value.replaceAll("-", "");
  if (!/^[0-9a-f]{32}$/iu.test(hex)) throw new Error(`Invalid UUID namespace: ${value}`);
  return Buffer.from(hex, "hex");
}
