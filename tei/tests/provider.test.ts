import { describe, expect, it, vi } from "vitest";
import { E5_MODEL_CONTRACT } from "../src/model-contract.js";
import { assertValidVector, prefixInput } from "../src/providers/embedding-provider.js";
import { TeiE5EmbeddingProvider } from "../src/providers/tei-e5-provider.js";

const unit = () => [1, ...Array.from({ length: 383 }, () => 0)];

describe("TeiE5EmbeddingProvider", () => {
  it("adds exactly one query prefix and sends normalize=true/truncate=false", async () => {
    const bodies: unknown[] = [];
    const provider = new TeiE5EmbeddingProvider({
      baseUrl: "http://tei",
      fetchImpl: mockFetch((path, body) => {
        bodies.push(body);
        if (path === "/info") return info();
        if (path === "/tokenize") return [{ id: 1 }, { id: 2 }];
        return [unit()];
      }),
    });
    await provider.embedQueries(["query: вопрос"]);
    const embed = bodies.at(-1) as { inputs: string[]; normalize: boolean; truncate: boolean };
    expect(embed.inputs).toEqual(["query: вопрос"]);
    expect(embed.normalize).toBe(true);
    expect(embed.truncate).toBe(false);
  });

  it("adds exactly one passage prefix", async () => {
    const inputs: string[] = [];
    const provider = new TeiE5EmbeddingProvider({
      baseUrl: "http://tei",
      fetchImpl: mockFetch((path, body) => {
        if (path === "/info") return info();
        if (path === "/tokenize") return [{ id: 1 }];
        inputs.push(...(body as { inputs: string[] }).inputs);
        return [unit()];
      }),
    });
    await provider.embedDocuments(["passage: документ"]);
    expect(inputs).toEqual(["passage: документ"]);
    expect(prefixInput("query: текст", "document")).toBe("passage: текст");
  });

  it("rejects another model or revision", async () => {
    const provider = new TeiE5EmbeddingProvider({
      baseUrl: "http://tei",
      fetchImpl: mockFetch(() => ({ ...info(), model_id: "intfloat/multilingual-e5-small" })),
    });
    await expect(provider.getInfo()).rejects.toThrow("Model fallback is forbidden");
  });

  it("rejects wrong dimensions, NaN, Infinity, and unnormalized vectors", () => {
    expect(() => assertValidVector([1], 384)).toThrow("dimension");
    expect(() => assertValidVector([NaN, ...Array(383).fill(0)], 384)).toThrow("NaN");
    expect(() => assertValidVector([Infinity, ...Array(383).fill(0)], 384)).toThrow("NaN");
    expect(() => assertValidVector(Array(384).fill(0), 384)).toThrow("L2 norm");
  });

  it("rejects an input above max tokens before embed", async () => {
    const embed = vi.fn();
    const provider = new TeiE5EmbeddingProvider({
      baseUrl: "http://tei",
      fetchImpl: mockFetch((path) => {
        if (path === "/info") return { ...info(), max_input_length: 1 };
        if (path === "/tokenize") return [{ id: 1 }, { id: 2 }];
        embed();
        return [unit()];
      }),
    });
    await expect(provider.embedQueries(["test"])).rejects.toThrow("Truncation is forbidden");
    expect(embed).not.toHaveBeenCalled();
  });

  it("times out and respects AbortSignal", async () => {
    const waitingFetch = vi.fn((_url: string | URL | Request, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(init.signal?.reason), { once: true });
    })) as typeof fetch;
    const timeoutProvider = new TeiE5EmbeddingProvider({ baseUrl: "http://tei", timeoutMs: 5, fetchImpl: waitingFetch });
    await expect(timeoutProvider.getInfo()).rejects.toThrow("timed out");

    const abortProvider = new TeiE5EmbeddingProvider({ baseUrl: "http://tei", timeoutMs: 1000, fetchImpl: waitingFetch });
    const controller = new AbortController();
    const request = abortProvider.getInfo(controller.signal);
    controller.abort(new Error("cancelled"));
    await expect(request).rejects.toThrow("cancelled");
  });
});

function info() {
  return {
    model_id: E5_MODEL_CONTRACT.modelId,
    model_sha: E5_MODEL_CONTRACT.revision,
    model_dtype: "int8",
    max_input_length: 512,
    auto_truncate: false,
    version: "1.9.3",
    backend: "ort",
  };
}

function mockFetch(handler: (path: string, body: unknown) => unknown): typeof fetch {
  return vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const url = new URL(String(input));
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    return new Response(JSON.stringify(handler(url.pathname, body)), { status: 200, headers: { "content-type": "application/json" } });
  }) as unknown as typeof fetch;
}
