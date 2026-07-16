import { E5_MODEL_CONTRACT } from "../model-contract.js";

export interface EmbeddingProvider {
  readonly modelId: string;
  readonly revision: string | null;
  readonly dimension: number;

  embedDocuments(texts: string[], signal?: AbortSignal): Promise<number[][]>;
  embedQueries(texts: string[], signal?: AbortSignal): Promise<number[][]>;
}

export type EmbeddingKind = "document" | "query";

export function prefixInput(text: string, kind: EmbeddingKind): string {
  const expected = kind === "document" ? E5_MODEL_CONTRACT.documentPrefix : E5_MODEL_CONTRACT.queryPrefix;
  const withoutKnownPrefix = text.replace(/^(?:passage|query):\s*/u, "");
  return `${expected}${withoutKnownPrefix}`;
}

export function l2Norm(vector: readonly number[]): number {
  return Math.sqrt(vector.reduce((sum, value) => sum + value * value, 0));
}

export function assertValidVector(vector: number[], dimension: number, checkNorm = true): void {
  if (vector.length !== dimension) {
    throw new Error(`Embedding dimension is ${vector.length}; expected ${dimension}.`);
  }
  if (vector.some((value) => !Number.isFinite(value))) {
    throw new Error("Embedding contains NaN or Infinity.");
  }
  if (checkNorm) {
    const norm = l2Norm(vector);
    if (norm < 0.99 || norm > 1.01) {
      throw new Error(`Embedding L2 norm is ${norm}; expected a normalized vector.`);
    }
  }
}
