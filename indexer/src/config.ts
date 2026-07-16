import { dirname, join, resolve } from "node:path";

export const DEFAULT_MODEL = "Xenova/multilingual-e5-small";
export const DEFAULT_MODEL_REVISION = "761b726dd34fb83930e26aab4e9ac3899aa1fa78";
export const DEFAULT_DTYPE = "q8";
export const DEFAULT_DIMENSION = 384;
export const DEFAULT_PIPELINE_VERSION = "1";
export const DEFAULT_COLLECTION = "methodology_e5_small_v1";

export type LogLevel = "error" | "warn" | "info" | "debug";

export interface IndexerConfig {
  inputFile: string;
  reportFile: string;
  collection: string;
  qdrantUrl: string;
  qdrantApiKey?: string;
  batchSize: number;
  modelId: string;
  modelRevision: string;
  dtype: string;
  dimension: number;
  pipelineVersion: string;
  cacheDir: string;
  vectorCacheSize: number;
  storeEmbeddingText: boolean;
  dryRun: boolean;
  forceReembed: boolean;
  prune: boolean;
  offline: boolean;
  continueOnError: boolean;
  logLevel: LogLevel;
}

export interface CliConfigInput {
  input: string;
  report?: string;
  collection?: string;
  qdrantUrl?: string;
  batchSize?: number;
  dryRun?: boolean;
  forceReembed?: boolean;
  prune?: boolean;
  offline?: boolean;
  continueOnError?: boolean;
  logLevel?: LogLevel;
}

export function resolveIndexerConfig(
  input: CliConfigInput,
  env: NodeJS.ProcessEnv = process.env,
): IndexerConfig {
  const modelId = env.EMBEDDING_MODEL?.trim() || DEFAULT_MODEL;
  const modelRevision = env.EMBEDDING_MODEL_REVISION?.trim() || DEFAULT_MODEL_REVISION;
  assertPinnedModel(modelId, modelRevision);
  const collection = input.collection ?? env.QDRANT_COLLECTION?.trim() ?? DEFAULT_COLLECTION;
  const inputFile = resolve(input.input);
  const reportFile = resolve(
    input.report ?? join(dirname(inputFile), `index-report.${safeCollectionName(collection)}.json`),
  );
  return {
    inputFile,
    reportFile,
    collection,
    qdrantUrl: input.qdrantUrl ?? env.QDRANT_URL?.trim() ?? "http://127.0.0.1:6333",
    ...(env.QDRANT_API_KEY?.trim() ? { qdrantApiKey: env.QDRANT_API_KEY.trim() } : {}),
    batchSize: input.batchSize ?? readPositiveInteger(env.INDEXER_BATCH_SIZE, 16),
    modelId,
    modelRevision,
    dtype: env.EMBEDDING_DTYPE?.trim() || DEFAULT_DTYPE,
    dimension: DEFAULT_DIMENSION,
    pipelineVersion: env.EMBEDDING_PIPELINE_VERSION?.trim() || DEFAULT_PIPELINE_VERSION,
    cacheDir: resolve(env.EMBEDDING_CACHE_DIR?.trim() || "../var/models"),
    vectorCacheSize: readPositiveInteger(env.EMBEDDING_VECTOR_CACHE_SIZE, 256),
    storeEmbeddingText: readBoolean(env.STORE_EMBEDDING_TEXT, false),
    dryRun: Boolean(input.dryRun),
    forceReembed: Boolean(input.forceReembed),
    prune: Boolean(input.prune),
    offline: Boolean(input.offline),
    continueOnError: Boolean(input.continueOnError),
    logLevel: input.logLevel ?? parseLogLevel(env.LOG_LEVEL),
  };
}

function assertPinnedModel(modelId: string, revision: string): void {
  if (modelId !== DEFAULT_MODEL || revision !== DEFAULT_MODEL_REVISION) {
    throw new Error(
      `Athena requires ${DEFAULT_MODEL}@${DEFAULT_MODEL_REVISION}; got ${modelId}@${revision}. Use a separate collection and explicit migration for another model.`,
    );
  }
}

function safeCollectionName(value: string): string {
  return value.replace(/[^A-Za-z0-9_.-]+/gu, "_");
}

function readPositiveInteger(value: string | undefined, fallback: number): number {
  if (!value) return fallback;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`Expected a positive integer, got ${value}.`);
  return parsed;
}

function readBoolean(value: string | undefined, fallback: boolean): boolean {
  if (!value) return fallback;
  if (/^(?:1|true|yes)$/iu.test(value)) return true;
  if (/^(?:0|false|no)$/iu.test(value)) return false;
  throw new Error(`Expected a boolean, got ${value}.`);
}

function parseLogLevel(value: string | undefined): LogLevel {
  if (!value) return "info";
  if (["error", "warn", "info", "debug"].includes(value)) return value as LogLevel;
  throw new Error(`Unknown LOG_LEVEL: ${value}.`);
}
