import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { config as loadEnv } from "dotenv";
import { MODEL_CONTRACT } from "./model-contract.js";

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
}

export function runtimeConfig(overrides: Partial<RuntimeConfig> = {}): RuntimeConfig {
  return {
    teiUrl: overrides.teiUrl ?? process.env.TEI_URL?.trim() ?? "http://127.0.0.1:8080",
    qdrantUrl: overrides.qdrantUrl ?? process.env.QDRANT_URL?.trim() ?? "http://127.0.0.1:6333",
    ...(overrides.qdrantApiKey ?? process.env.QDRANT_API_KEY?.trim()
      ? { qdrantApiKey: overrides.qdrantApiKey ?? process.env.QDRANT_API_KEY!.trim() }
      : {}),
    collection: overrides.collection ?? process.env.QDRANT_COLLECTION?.trim() ?? "methodology_e5_small_v1",
    cacheDir: overrides.cacheDir ?? resolve(REPO_ROOT, process.env.EMBEDDING_CACHE_DIR?.trim() || "var/models"),
    timeoutMs: overrides.timeoutMs ?? positiveInt(process.env.TEI_REQUEST_TIMEOUT_MS, 30_000),
  };
}

function positiveInt(value: string | undefined, fallback: number): number {
  if (!value) return fallback;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`Expected positive integer, got ${value}.`);
  return parsed;
}
