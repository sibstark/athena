import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import type { FeatureExtractionPipeline } from "@huggingface/transformers";
import { MODEL_CONTRACT } from "../model-contract.js";
import { assertValidVector, prefixInput, type EmbeddingProvider, type EmbeddingKind } from "./embedding-provider.js";

export class TransformersJsReferenceProvider implements EmbeddingProvider {
  readonly modelId = MODEL_CONTRACT.modelId;
  readonly revision = MODEL_CONTRACT.revision;
  readonly dimension = MODEL_CONTRACT.dimension;
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
      pooling: MODEL_CONTRACT.pooling,
      normalize: MODEL_CONTRACT.normalized,
    });
    const nested = output.tolist() as unknown;
    if (!Array.isArray(nested)) throw new Error("Transformers.js returned an invalid tensor.");
    const vectors = texts.length === 1 && nested.every((value) => typeof value === "number")
      ? [nested as number[]]
      : nested as number[][];
    if (vectors.length !== texts.length) throw new Error("Transformers.js returned an invalid batch size.");
    for (const vector of vectors) assertValidVector(vector, this.dimension, MODEL_CONTRACT.normalized);
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
      dtype: transformersDType(MODEL_CONTRACT.dtype),
      cache_dir: this.cacheDir,
      local_files_only: this.offline,
    });
  }
}

function transformersDType(value: string): "fp32" | "fp16" | "q8" | "int8" | "uint8" | "q4" | "bnb4" | "q4f16" {
  if (value === "float32") return "fp32";
  if (value === "float16") return "fp16";
  if (["q8", "int8", "uint8", "q4", "bnb4", "q4f16"].includes(value)) return value as "q8" | "int8" | "uint8" | "q4" | "bnb4" | "q4f16";
  throw new Error(`Unsupported Transformers.js dtype: ${value}.`);
}
