import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { readFileSync } from "node:fs";
import { config as loadEnv } from "dotenv";
import { E5_MODEL_CONTRACT, assertExactModel } from "./model-contract.js";

const PROJECT_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = resolve(PROJECT_DIR, "..");
loadEnv({ path: join(REPO_ROOT, ".env"), quiet: true });
loadEnv({ path: join(PROJECT_DIR, ".env"), override: true, quiet: true });

export interface RuntimeConfig {
  teiUrl: string;
  qdrantUrl: string;
  qdrantApiKey?: string;
  collection: string;
  cacheDir: string;
  timeoutMs: number;
  teiImage: string;
  teiImageDigest: string | null;
  teiBackend: string;
  modelArtifact: PreparedModelArtifact | null;
}

export interface PreparedModelArtifact {
  model_id: string;
  revision: string;
  source_artifact: string;
  served_artifact: string;
  dtype: string;
  sha256: string;
}

export function runtimeConfig(overrides: Partial<RuntimeConfig> = {}): RuntimeConfig {
  const modelId = process.env.TEI_MODEL_ID?.trim() || E5_MODEL_CONTRACT.modelId;
  const revision = process.env.TEI_MODEL_REVISION?.trim() || E5_MODEL_CONTRACT.revision;
  assertExactModel(modelId, revision);
  const pooling = process.env.TEI_POOLING?.trim() || E5_MODEL_CONTRACT.pooling;
  if (pooling !== E5_MODEL_CONTRACT.pooling) throw new Error(`TEI pooling must be ${E5_MODEL_CONTRACT.pooling}.`);
  if (!/^(?:false|0|no)$/iu.test(process.env.TEI_AUTO_TRUNCATE?.trim() || "false")) {
    throw new Error("TEI_AUTO_TRUNCATE must be false.");
  }
  return {
    teiUrl: overrides.teiUrl ?? process.env.TEI_URL?.trim() ?? "http://127.0.0.1:8080",
    qdrantUrl: overrides.qdrantUrl ?? process.env.QDRANT_URL?.trim() ?? "http://127.0.0.1:6333",
    ...(overrides.qdrantApiKey ?? process.env.QDRANT_API_KEY?.trim()
      ? { qdrantApiKey: overrides.qdrantApiKey ?? process.env.QDRANT_API_KEY!.trim() }
      : {}),
    collection: overrides.collection ?? process.env.QDRANT_COLLECTION?.trim() ?? "methodology_e5_small_v1",
    cacheDir: overrides.cacheDir ?? resolve(REPO_ROOT, process.env.EMBEDDING_CACHE_DIR?.trim() || "var/models"),
    timeoutMs: overrides.timeoutMs ?? positiveInt(process.env.TEI_REQUEST_TIMEOUT_MS, 30_000),
    teiImage: overrides.teiImage ?? process.env.TEI_IMAGE?.trim() ?? "ghcr.io/huggingface/text-embeddings-inference:cpu-1.9@sha256:ad950d30878eceb72aaf32024d26fa2b1d04a75304fa0b4776b49aa1941fea07",
    teiImageDigest: overrides.teiImageDigest ?? (process.env.TEI_IMAGE_DIGEST?.trim() || "sha256:ad950d30878eceb72aaf32024d26fa2b1d04a75304fa0b4776b49aa1941fea07"),
    teiBackend: overrides.teiBackend ?? process.env.TEI_BACKEND?.trim() ?? "onnxruntime",
    modelArtifact: overrides.modelArtifact ?? readPreparedModelArtifact(),
  };
}

function readPreparedModelArtifact(): PreparedModelArtifact | null {
  try {
    const value = JSON.parse(readFileSync(join(REPO_ROOT, "var/tei/model-manifest.json"), "utf8")) as PreparedModelArtifact;
    assertExactModel(value.model_id, value.revision);
    if (value.dtype !== E5_MODEL_CONTRACT.dtype || !/^[0-9a-f]{64}$/u.test(value.sha256)) {
      throw new Error("Prepared TEI model manifest has invalid dtype or SHA-256.");
    }
    return value;
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return null;
    throw error;
  }
}

function positiveInt(value: string | undefined, fallback: number): number {
  if (!value) return fallback;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`Expected positive integer, got ${value}.`);
  return parsed;
}
