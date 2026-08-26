import { MODEL_CONTRACT, assertExactModel } from "../model-contract.js";
import type { TeiInfo } from "../contracts.js";
import {
  assertValidVector,
  prefixInput,
  type EmbeddingKind,
  type EmbeddingProvider,
} from "./embedding-provider.js";

export interface TeiOptions {
  baseUrl: string;
  timeoutMs?: number;
  fetchImpl?: typeof fetch;
}

export class TeiEmbeddingProvider implements EmbeddingProvider {
  readonly modelId = MODEL_CONTRACT.modelId;
  readonly revision = MODEL_CONTRACT.revision;
  readonly dimension = MODEL_CONTRACT.dimension;
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly fetchImpl: typeof fetch;
  private cachedInfo: TeiInfo | null = null;

  constructor(options: TeiOptions) {
    this.baseUrl = options.baseUrl.replace(/\/+$/u, "");
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  async getInfo(signal?: AbortSignal): Promise<TeiInfo> {
    if (this.cachedInfo) return this.cachedInfo;
    const info = await this.request<TeiInfo>(
      "/info",
      { method: "GET" },
      signal,
    );
    assertExactModel(
      String(info.model_id ?? ""),
      typeof info.model_sha === "string" ? info.model_sha : null,
    );
    if (info.auto_truncate !== MODEL_CONTRACT.autoTruncate) {
      throw new Error(
        `TEI auto_truncate must be ${MODEL_CONTRACT.autoTruncate}; reported ${String(info.auto_truncate)}.`,
      );
    }
    if (info.model_dtype && info.model_dtype !== MODEL_CONTRACT.dtype) {
      throw new Error(
        `TEI dtype must be ${MODEL_CONTRACT.dtype}; reported ${info.model_dtype}.`,
      );
    }
    if (info.max_input_length !== MODEL_CONTRACT.maxInputTokens) {
      throw new Error(
        `TEI max_input_length must be ${MODEL_CONTRACT.maxInputTokens}; reported ${String(info.max_input_length)}.`,
      );
    }
    this.cachedInfo = info;
    return info;
  }

  embedDocuments(texts: string[], signal?: AbortSignal): Promise<number[][]> {
    return this.embed(texts, "document", signal);
  }

  embedQueries(texts: string[], signal?: AbortSignal): Promise<number[][]> {
    return this.embed(texts, "query", signal);
  }

  async countTokens(text: string, signal?: AbortSignal): Promise<number> {
    const response = await this.request<unknown>(
      "/tokenize",
      jsonRequest({ inputs: text, add_special_tokens: true }),
      signal,
    );
    return tokenCountFromResponse(response);
  }

  private async embed(
    texts: string[],
    kind: EmbeddingKind,
    signal?: AbortSignal,
  ): Promise<number[][]> {
    if (texts.length === 0) return [];
    const info = await this.getInfo(signal);
    const inputs = texts.map((text) => prefixInput(text, kind));
    const limit = info.max_input_length;
    if (typeof limit !== "number")
      throw new Error("TEI did not report max_input_length.");
    for (let index = 0; index < inputs.length; index += 1) {
      const count = await this.countTokens(inputs[index]!, signal);
      if (count > limit) {
        throw new Error(
          `Input ${index} has ${count} tokens; TEI maximum is ${limit}. Truncation is forbidden.`,
        );
      }
    }
    const maxBatch = Math.max(
      1,
      Math.min(
        typeof info.max_batch_requests === "number"
          ? info.max_batch_requests
          : 32,
        typeof info.max_client_batch_size === "number"
          ? info.max_client_batch_size
          : 32,
      ),
    );
    const output: number[][] = [];
    for (let offset = 0; offset < inputs.length; offset += maxBatch) {
      const batch = inputs.slice(offset, offset + maxBatch);
      const vectors = await this.request<unknown>(
        "/embed",
        jsonRequest({
          inputs: batch,
          normalize: MODEL_CONTRACT.normalized,
          truncate: MODEL_CONTRACT.autoTruncate,
        }),
        signal,
      );
      if (!Array.isArray(vectors) || vectors.length !== batch.length) {
        throw new Error(
          `TEI returned an invalid embedding batch for ${batch.length} inputs.`,
        );
      }
      for (const vector of vectors) {
        if (
          !Array.isArray(vector) ||
          !vector.every((value) => typeof value === "number")
        ) {
          throw new Error("TEI returned a non-numeric embedding.");
        }
        const typed = vector as number[];
        assertValidVector(typed, this.dimension, MODEL_CONTRACT.normalized);
        output.push(typed);
      }
    }
    return output;
  }

  private async request<T>(
    path: string,
    init: RequestInit,
    externalSignal?: AbortSignal,
  ): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(
      () =>
        controller.abort(
          new Error(`TEI request timed out after ${this.timeoutMs}ms.`),
        ),
      this.timeoutMs,
    );
    const abort = () => controller.abort(externalSignal?.reason);
    externalSignal?.addEventListener("abort", abort, { once: true });
    try {
      const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
        ...init,
        signal: controller.signal,
      });
      if (!response.ok) {
        const body = (await response.text()).slice(0, 500);
        throw new Error(
          `TEI ${path} failed with HTTP ${response.status}: ${body}`,
        );
      }
      return (await response.json()) as T;
    } finally {
      clearTimeout(timer);
      externalSignal?.removeEventListener("abort", abort);
    }
  }
}

function jsonRequest(body: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  };
}

function tokenCountFromResponse(value: unknown): number {
  if (!Array.isArray(value))
    throw new Error("TEI /tokenize returned an invalid response.");
  if (value.length === 0) return 0;
  if (Array.isArray(value[0])) return (value[0] as unknown[]).length;
  if (typeof value[0] === "object") return value.length;
  if (typeof value[0] === "number") return value.length;
  throw new Error("TEI /tokenize returned an unsupported response.");
}
