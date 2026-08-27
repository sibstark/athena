import { join } from "node:path";
import { readFileSync } from "node:fs";
import { E5_MODEL_CONTRACT, REPO_ROOT, assertExactModel } from "./model-contract.js";

export interface RuntimeConfig {
  teiUrl: string;
  qdrantUrl: string;
  qdrantApiKey?: string;
  collection: string;
  cacheDir: string;
  timeoutMs: number;
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
  return {
    teiUrl: overrides.teiUrl ?? process.env.TEI_URL?.trim() ?? "http://127.0.0.1:8080",
    qdrantUrl: overrides.qdrantUrl ?? process.env.QDRANT_URL?.trim() ?? "http://127.0.0.1:6333",
    ...(overrides.qdrantApiKey ?? process.env.QDRANT_API_KEY?.trim()
      ? { qdrantApiKey: overrides.qdrantApiKey ?? process.env.QDRANT_API_KEY!.trim() }
      : {}),
    collection: overrides.collection ?? required("QDRANT_COLLECTION"),
    cacheDir: overrides.cacheDir ?? E5_MODEL_CONTRACT.cacheDir,
    timeoutMs: overrides.timeoutMs ?? positiveInt(process.env.TEI_REQUEST_TIMEOUT_MS, 30_000),
    modelArtifact: overrides.modelArtifact ?? readPreparedModelArtifact(),
  };
}

function required(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required.`);
  return value;
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
