#!/usr/bin/env bun

import { createHash } from "node:crypto";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadChunkEmbeddingConfig } from "./embedding-config.js";

const config = loadChunkEmbeddingConfig();
const projectDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const cacheDir = resolve(
  process.env.EMBEDDING_CACHE_DIR?.trim() || join(projectDir, "../var/models"),
);
const targetDir = join(cacheDir, config.modelId, config.revision);

await mkdir(targetDir, { recursive: true });
await download("tokenizer.json", config.tokenizerSha256);
await download("tokenizer_config.json", config.tokenizerConfigSha256);
console.log(`Tokenizer cached at ${targetDir}`);

async function download(name: string, expectedSHA256: string): Promise<void> {
  const url = `https://huggingface.co/${config.modelId}/resolve/${encodeURIComponent(config.revision)}/${name}`;
  const response = await fetch(url, { redirect: "follow" });
  if (!response.ok) throw new Error(`${url} returned HTTP ${response.status}.`);
  const bytes = new Uint8Array(await response.arrayBuffer());
  const actual = createHash("sha256").update(bytes).digest("hex");
  if (actual !== expectedSHA256) {
    throw new Error(
      `${name} checksum mismatch: expected ${expectedSHA256}, got ${actual}.`,
    );
  }
  const target = join(targetDir, name);
  const temporary = `${target}.tmp-${process.pid}`;
  await writeFile(temporary, bytes, { mode: 0o644 });
  await rename(temporary, target);
}
