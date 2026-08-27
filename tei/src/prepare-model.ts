#!/usr/bin/env bun
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { copyFile, link, lstat, mkdir, unlink, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { E5_MODEL_CONTRACT } from "./model-contract.js";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const sourceRoot = join(E5_MODEL_CONTRACT.cacheDir, E5_MODEL_CONTRACT.modelId, E5_MODEL_CONTRACT.revision);
const snapshotRoot = join(
  repoRoot,
  "var/tei",
  `models--${E5_MODEL_CONTRACT.modelId.replace("/", "--")}`,
  "snapshots",
  E5_MODEL_CONTRACT.revision,
);
const sourceModel = join(sourceRoot, "onnx/model_quantized.onnx");
const targetModel = join(snapshotRoot, "onnx/model.onnx");

await mkdir(dirname(targetModel), { recursive: true });
await replaceWithHardlink(sourceModel, targetModel);
for (const file of ["config.json", "tokenizer.json", "tokenizer_config.json"]) {
  await copyFile(join(sourceRoot, file), join(snapshotRoot, file));
}
const refsDir = join(repoRoot, "var/tei", `models--${E5_MODEL_CONTRACT.modelId.replace("/", "--")}`, "refs");
await mkdir(refsDir, { recursive: true });
await writeFile(join(refsDir, E5_MODEL_CONTRACT.revision), E5_MODEL_CONTRACT.revision, "utf8");

const manifest = {
  model_id: E5_MODEL_CONTRACT.modelId,
  revision: E5_MODEL_CONTRACT.revision,
  source_artifact: "onnx/model_quantized.onnx",
  served_artifact: "onnx/model.onnx",
  dtype: E5_MODEL_CONTRACT.dtype,
  sha256: await fileSha256(sourceModel),
  prepared_at: new Date().toISOString(),
};
await writeFile(join(repoRoot, "var/tei/model-manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
console.log(`Prepared ${manifest.model_id}@${manifest.revision}`);
console.log(`Artifact: ${manifest.source_artifact} (${manifest.dtype}, sha256:${manifest.sha256})`);

async function replaceWithHardlink(source: string, target: string): Promise<void> {
  try {
    await lstat(target);
    await unlink(target);
  } catch (error) {
    if (!isNotFound(error)) throw error;
  }
  try {
    await link(source, target);
  } catch (error) {
    if (isCrossDevice(error)) await copyFile(source, target);
    else throw error;
  }
}

async function fileSha256(path: string): Promise<string> {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk as Buffer);
  return hash.digest("hex");
}

function isNotFound(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "ENOENT";
}

function isCrossDevice(error: unknown): boolean {
  return error instanceof Error && "code" in error && error.code === "EXDEV";
}
