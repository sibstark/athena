export const E5_MODEL_CONTRACT = {
  modelId: "Xenova/multilingual-e5-small",
  revision: "761b726dd34fb83930e26aab4e9ac3899aa1fa78",
  dimension: 384,
  dtype: "q8",
  pooling: "mean",
  normalized: true,
  documentPrefix: "passage: ",
  queryPrefix: "query: ",
  pipelineVersion: "1",
  pointNamespace: "a1f0e7c2-7a6b-5c74-9f3e-6c8d91b3a204",
} as const;

export function assertExactModel(modelId: string, revision: string | null): void {
  if (modelId !== E5_MODEL_CONTRACT.modelId || revision !== E5_MODEL_CONTRACT.revision) {
    throw new Error(
      `Expected ${E5_MODEL_CONTRACT.modelId}@${E5_MODEL_CONTRACT.revision}, got ${modelId}@${revision ?? "unknown"}. Model fallback is forbidden.`,
    );
  }
}
