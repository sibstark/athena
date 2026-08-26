import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { config as loadEnv } from "dotenv";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
loadEnv({ path: join(repoRoot, ".env"), quiet: true });

export interface ChunkEmbeddingConfig {
  modelId: string;
  revision: string;
  tokenizerSha256: string;
  tokenizerConfigSha256: string;
  targetTokens: number;
  maxTokens: number;
  minTokens: number;
  overlapTokens: number;
}

export function loadChunkEmbeddingConfig(): ChunkEmbeddingConfig {
  const config = {
    modelId: required("EMBEDDING_MODEL_ID"),
    revision: required("EMBEDDING_MODEL_REVISION"),
    tokenizerSha256: sha256("EMBEDDING_TOKENIZER_SHA256"),
    tokenizerConfigSha256: sha256("EMBEDDING_TOKENIZER_CONFIG_SHA256"),
    targetTokens: positiveInteger("CHUNK_TARGET_TOKENS"),
    maxTokens: positiveInteger("CHUNK_MAX_TOKENS"),
    minTokens: positiveInteger("CHUNK_MIN_TOKENS"),
    overlapTokens: nonNegativeInteger("CHUNK_OVERLAP_TOKENS"),
  };
  if (config.targetTokens > config.maxTokens)
    throw new Error("CHUNK_TARGET_TOKENS must not exceed CHUNK_MAX_TOKENS.");
  if (config.overlapTokens >= config.targetTokens)
    throw new Error(
      "CHUNK_OVERLAP_TOKENS must be smaller than CHUNK_TARGET_TOKENS.",
    );
  return config;
}

function required(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required in the root .env.`);
  return value;
}

function positiveInteger(name: string): number {
  const value = Number(required(name));
  if (!Number.isInteger(value) || value <= 0)
    throw new Error(`${name} must be a positive integer.`);
  return value;
}

function nonNegativeInteger(name: string): number {
  const value = Number(required(name));
  if (!Number.isInteger(value) || value < 0)
    throw new Error(`${name} must be a non-negative integer.`);
  return value;
}

function sha256(name: string): string {
  const value = required(name);
  if (!/^[0-9a-f]{64}$/u.test(value))
    throw new Error(`${name} must be a lowercase SHA-256.`);
  return value;
}
