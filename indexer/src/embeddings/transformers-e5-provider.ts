import { resolve } from "node:path";
import { mkdir } from "node:fs/promises";
import type {
  FeatureExtractionPipeline,
  PreTrainedTokenizer,
} from "@huggingface/transformers";
import { assertValidVector, type EmbeddingProvider } from "./embedding-provider.js";

export interface TransformersE5Options {
  modelId: string;
  revision: string;
  dtype: string;
  cacheDir: string;
  offline?: boolean;
  dimension?: number;
}

export class TransformersJsE5EmbeddingProvider implements EmbeddingProvider {
  readonly modelId: string;
  readonly revision: string;
  readonly dtype: string;
  readonly dimension: number;
  readonly cacheDir: string;
  readonly offline: boolean;
  private tokenizer: PreTrainedTokenizer | null = null;
  private extractor: FeatureExtractionPipeline | null = null;
  private tokenizerPromise: Promise<void> | null = null;
  private loadPromise: Promise<void> | null = null;

  constructor(options: TransformersE5Options) {
    this.modelId = options.modelId;
    this.revision = options.revision;
    this.dtype = options.dtype;
    this.dimension = options.dimension ?? 384;
    this.cacheDir = resolve(options.cacheDir);
    this.offline = Boolean(options.offline);
  }

  get loaded(): boolean {
    return this.extractor !== null;
  }

  async loadTokenizer(): Promise<void> {
    if (this.tokenizer) return;
    this.tokenizerPromise ??= this.initializeTokenizer();
    await this.tokenizerPromise;
  }

  async load(): Promise<void> {
    if (this.extractor) return;
    this.loadPromise ??= this.initializeModel();
    await this.loadPromise;
  }

  async countTokens(text: string): Promise<number> {
    await this.loadTokenizer();
    return tokenCount(this.requireTokenizer(), text);
  }

  async embedDocuments(texts: string[]): Promise<number[][]> {
    if (texts.length === 0) return [];
    await this.load();
    const output = await this.requireExtractor()(texts, {
      pooling: "mean",
      normalize: true,
    });
    const nested = output.tolist() as unknown;
    const vectors = normalizeTensorOutput(nested, texts.length);
    for (const vector of vectors) assertValidVector(vector, this.dimension);
    return vectors;
  }

  private async initializeTokenizer(): Promise<void> {
    try {
      await mkdir(this.cacheDir, { recursive: true });
      const { AutoTokenizer } = await import("@huggingface/transformers");
      this.tokenizer = await AutoTokenizer.from_pretrained(this.modelId, {
        revision: this.revision,
        cache_dir: this.cacheDir,
        local_files_only: this.offline,
      });
    } catch (error) {
      this.tokenizerPromise = null;
      throw modelLoadError("tokenizer", this.offline, this.cacheDir, error);
    }
  }

  private async initializeModel(): Promise<void> {
    try {
      await mkdir(this.cacheDir, { recursive: true });
      await this.loadTokenizer();
      const { pipeline } = await import("@huggingface/transformers");
      this.extractor = await pipeline("feature-extraction", this.modelId, {
        revision: this.revision,
        dtype: this.dtype as "q8",
        cache_dir: this.cacheDir,
        local_files_only: this.offline,
      });
    } catch (error) {
      this.loadPromise = null;
      throw modelLoadError("model", this.offline, this.cacheDir, error);
    }
  }

  private requireTokenizer(): PreTrainedTokenizer {
    if (!this.tokenizer) throw new Error("Tokenizer is not loaded.");
    return this.tokenizer;
  }

  private requireExtractor(): FeatureExtractionPipeline {
    if (!this.extractor) throw new Error("Embedding model is not loaded.");
    return this.extractor;
  }
}

function tokenCount(tokenizer: PreTrainedTokenizer, text: string): number {
  const result = tokenizer(text, {
    truncation: false,
    padding: false,
    add_special_tokens: true,
  });
  const inputIds = result.input_ids;
  if (!inputIds || typeof inputIds.size !== "number") {
    throw new Error("Tokenizer did not return input_ids.");
  }
  return inputIds.size;
}

function normalizeTensorOutput(value: unknown, expected: number): number[][] {
  if (!Array.isArray(value)) throw new Error("Embedding model returned an invalid tensor.");
  if (expected === 1 && value.every((item) => typeof item === "number")) {
    return [value as number[]];
  }
  if (!value.every((item) => Array.isArray(item))) {
    throw new Error("Embedding model returned an invalid batch tensor.");
  }
  const vectors = value as number[][];
  if (vectors.length !== expected) {
    throw new Error(`Embedding model returned ${vectors.length} vectors for ${expected} inputs.`);
  }
  return vectors;
}

function modelLoadError(
  kind: "tokenizer" | "model",
  offline: boolean,
  cacheDir: string,
  error: unknown,
): Error {
  const detail = error instanceof Error ? error.message : String(error);
  if (offline) {
    return new Error(`Cannot load ${kind} from local cache ${cacheDir} in offline mode: ${detail}`);
  }
  return new Error(
    `Cannot load embedding ${kind}. Check access to huggingface.co and the pinned revision, then retry: ${detail}`,
  );
}
