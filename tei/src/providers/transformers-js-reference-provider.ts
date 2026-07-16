import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import type { FeatureExtractionPipeline } from "@huggingface/transformers";
import { E5_MODEL_CONTRACT } from "../model-contract.js";
import { assertValidVector, prefixInput, type EmbeddingProvider, type EmbeddingKind } from "./embedding-provider.js";

export class TransformersJsE5ReferenceProvider implements EmbeddingProvider {
  readonly modelId = E5_MODEL_CONTRACT.modelId;
  readonly revision = E5_MODEL_CONTRACT.revision;
  readonly dimension = E5_MODEL_CONTRACT.dimension;
  private readonly cacheDir: string;
  private readonly offline: boolean;
  private extractor: FeatureExtractionPipeline | null = null;
  private loading: Promise<void> | null = null;

  constructor(options: { cacheDir: string; offline?: boolean }) {
    this.cacheDir = resolve(options.cacheDir);
    this.offline = Boolean(options.offline);
  }

  embedDocuments(texts: string[]): Promise<number[][]> {
    return this.embed(texts, "document");
  }

  embedQueries(texts: string[]): Promise<number[][]> {
    return this.embed(texts, "query");
  }

  private async embed(texts: string[], kind: EmbeddingKind): Promise<number[][]> {
    if (texts.length === 0) return [];
    await this.load();
    const output = await this.extractor!(texts.map((text) => prefixInput(text, kind)), {
      pooling: E5_MODEL_CONTRACT.pooling,
      normalize: E5_MODEL_CONTRACT.normalized,
    });
    const nested = output.tolist() as unknown;
    if (!Array.isArray(nested)) throw new Error("Transformers.js returned an invalid tensor.");
    const vectors = texts.length === 1 && nested.every((value) => typeof value === "number")
      ? [nested as number[]]
      : nested as number[][];
    if (vectors.length !== texts.length) throw new Error("Transformers.js returned an invalid batch size.");
    for (const vector of vectors) assertValidVector(vector, this.dimension);
    return vectors;
  }

  private async load(): Promise<void> {
    if (this.extractor) return;
    this.loading ??= this.initialize();
    await this.loading;
  }

  private async initialize(): Promise<void> {
    await mkdir(this.cacheDir, { recursive: true });
    const { pipeline } = await import("@huggingface/transformers");
    this.extractor = await pipeline("feature-extraction", this.modelId, {
      revision: this.revision,
      dtype: E5_MODEL_CONTRACT.dtype,
      cache_dir: this.cacheDir,
      local_files_only: this.offline,
    });
  }
}
