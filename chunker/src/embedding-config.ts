import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { config as loadEnv } from "dotenv";

const PROJECT_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const REPO_ROOT = resolve(PROJECT_DIR, "..");
loadEnv({ path: join(REPO_ROOT, ".env"), quiet: true });

export interface EmbeddingConfig {
  modelId: string;
  modelRevision: string;
  dimension: number;
  dtype: string;
  pooling: string;
  normalized: boolean;
  documentPrefix: string;
  queryPrefix: string;
  maxInputTokens: number;
  autoTruncate: boolean;
  pipelineVersion: string;
  pointNamespace: string;
  tokenizerSHA256: string;
  tokenizerConfigSHA256: string;
  cacheDir: string;
  vectorCacheSize: number;
}

export function loadEmbeddingConfig(environment: NodeJS.ProcessEnv = process.env): EmbeddingConfig {
  const cacheDir = required(environment, "EMBEDDING_CACHE_DIR");
  const config: EmbeddingConfig = {
    modelId: required(environment, "EMBEDDING_MODEL_ID"),
    modelRevision: required(environment, "EMBEDDING_MODEL_REVISION"),
    dimension: positiveInteger(environment, "EMBEDDING_DIMENSION"),
    dtype: required(environment, "EMBEDDING_DTYPE"),
    pooling: required(environment, "EMBEDDING_POOLING"),
    normalized: requiredBoolean(environment, "EMBEDDING_NORMALIZED"),
    documentPrefix: required(environment, "EMBEDDING_DOCUMENT_PREFIX", true),
    queryPrefix: required(environment, "EMBEDDING_QUERY_PREFIX", true),
    maxInputTokens: positiveInteger(environment, "EMBEDDING_MAX_INPUT_TOKENS"),
    autoTruncate: requiredBoolean(environment, "EMBEDDING_AUTO_TRUNCATE"),
    pipelineVersion: required(environment, "EMBEDDING_PIPELINE_VERSION"),
    pointNamespace: required(environment, "EMBEDDING_POINT_NAMESPACE"),
    tokenizerSHA256: required(environment, "EMBEDDING_TOKENIZER_SHA256"),
    tokenizerConfigSHA256: required(environment, "EMBEDDING_TOKENIZER_CONFIG_SHA256"),
    cacheDir: isAbsolute(cacheDir) ? cacheDir : resolve(REPO_ROOT, cacheDir),
    vectorCacheSize: positiveInteger(environment, "EMBEDDING_VECTOR_CACHE_SIZE"),
  };
  if (config.queryPrefix.trim() === "") throw new Error("EMBEDDING_QUERY_PREFIX is required.");
  if (!/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/u.test(config.pointNamespace)) {
    throw new Error("EMBEDDING_POINT_NAMESPACE must be a lowercase UUID.");
  }
  for (const [name, value] of [
    ["EMBEDDING_TOKENIZER_SHA256", config.tokenizerSHA256],
    ["EMBEDDING_TOKENIZER_CONFIG_SHA256", config.tokenizerConfigSHA256],
  ] as const) {
    if (!/^[0-9a-f]{64}$/u.test(value)) throw new Error(`${name} must be a lowercase SHA-256.`);
  }
  return config;
}

function required(environment: NodeJS.ProcessEnv, name: string, allowEmpty = false): string {
  if (!(name in environment)) throw new Error(`${name} is required.`);
  const value = environment[name] ?? "";
  if (!allowEmpty && value.trim() === "") throw new Error(`${name} is required.`);
  return allowEmpty ? value : value.trim();
}

function positiveInteger(environment: NodeJS.ProcessEnv, name: string): number {
  const value = required(environment, name);
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`${name} must be a positive integer.`);
  return parsed;
}

function requiredBoolean(environment: NodeJS.ProcessEnv, name: string): boolean {
  const value = required(environment, name);
  if (value === "true") return true;
  if (value === "false") return false;
  throw new Error(`${name} must be true or false.`);
}
