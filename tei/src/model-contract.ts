import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { config as loadEnv } from "dotenv";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
loadEnv({ path: join(repoRoot, ".env"), quiet: true });

export interface EmbeddingModelContract {
  modelId: string;
  revision: string;
  dimension: number;
  dtype: string;
  pooling: "cls" | "mean" | "last_token";
  normalized: boolean;
  documentPrefix: string;
  queryPrefix: string;
  maxInputTokens: number;
  autoTruncate: boolean;
  pipelineVersion: string;
  pointNamespace: string;
}

export const MODEL_CONTRACT: EmbeddingModelContract = loadModelContract();

export function loadModelContract(): EmbeddingModelContract {
  return {
    modelId: required("EMBEDDING_MODEL_ID"),
    revision: required("EMBEDDING_MODEL_REVISION"),
    dimension: positiveInteger("EMBEDDING_DIMENSION"),
    dtype: required("EMBEDDING_DTYPE"),
    pooling: pooling("EMBEDDING_POOLING"),
    normalized: bool("EMBEDDING_NORMALIZED"),
    documentPrefix: present("EMBEDDING_DOCUMENT_PREFIX"),
    queryPrefix: required("EMBEDDING_QUERY_PREFIX"),
    maxInputTokens: positiveInteger("EMBEDDING_MAX_INPUT_TOKENS"),
    autoTruncate: bool("EMBEDDING_AUTO_TRUNCATE"),
    pipelineVersion: required("EMBEDDING_PIPELINE_VERSION"),
    pointNamespace: uuid("EMBEDDING_POINT_NAMESPACE"),
  };
}

export function assertExactModel(
  modelId: string,
  revision: string | null,
): void {
  if (
    modelId !== MODEL_CONTRACT.modelId ||
    revision !== MODEL_CONTRACT.revision
  ) {
    throw new Error(
      `Expected ${MODEL_CONTRACT.modelId}@${MODEL_CONTRACT.revision}, got ${modelId}@${revision ?? "unknown"}. Model fallback is forbidden.`,
    );
  }
}

function present(name: string): string {
  if (!Object.hasOwn(process.env, name))
    throw new Error(`${name} is required in the root .env.`);
  return process.env[name] ?? "";
}
function required(name: string): string {
  const value = present(name);
  if (!value.trim()) throw new Error(`${name} must not be empty.`);
  return value;
}
function positiveInteger(name: string): number {
  const value = Number(required(name));
  if (!Number.isInteger(value) || value <= 0)
    throw new Error(`${name} must be a positive integer.`);
  return value;
}
function bool(name: string): boolean {
  const value = required(name);
  if (value !== "true" && value !== "false")
    throw new Error(`${name} must be true or false.`);
  return value === "true";
}
function pooling(name: string): EmbeddingModelContract["pooling"] {
  const value = required(name);
  if (value !== "cls" && value !== "mean" && value !== "last_token")
    throw new Error(`${name} is unsupported: ${value}.`);
  return value;
}
function uuid(name: string): string {
  const value = required(name);
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iu.test(
      value,
    )
  )
    throw new Error(`${name} must be a UUID.`);
  return value;
}
