export interface EmbeddingProvider {
  readonly modelId: string;
  readonly revision: string;
  readonly dimension: number;
  readonly dtype: string;
  readonly loaded: boolean;

  loadTokenizer(): Promise<void>;
  load(): Promise<void>;
  countTokens(text: string): Promise<number>;
  embedDocuments(texts: string[]): Promise<number[][]>;
}

export function assertValidVector(vector: number[], dimension: number): void {
  if (vector.length !== dimension) {
    throw new Error(`Embedding dimension is ${vector.length}; expected ${dimension}.`);
  }
  if (vector.some((value) => !Number.isFinite(value))) {
    throw new Error("Embedding contains NaN or Infinity.");
  }
}
