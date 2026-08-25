import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import type { PreTrainedTokenizer } from "@huggingface/transformers";
import type { TokenCounter } from "./chunker.js";

export const DEFAULT_TOKENIZER = "Xenova/multilingual-e5-small";
export const DEFAULT_TOKENIZER_REVISION = "761b726dd34fb83930e26aab4e9ac3899aa1fa78";
export const DEFAULT_TOKENIZER_SHA256 = "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39";
export const DEFAULT_TOKENIZER_CONFIG_SHA256 = "a1d6bc8734a6f635dc158508bef000f8e2e5a759c7d92f984b2c86e5ff53425b";

export async function createE5TokenCounter(options: {
  modelId: string;
  revision: string;
  cacheDir: string;
}): Promise<TokenCounter> {
  if (options.modelId !== DEFAULT_TOKENIZER || options.revision !== DEFAULT_TOKENIZER_REVISION) {
    throw new Error(
      `Athena requires tokenizer ${DEFAULT_TOKENIZER}@${DEFAULT_TOKENIZER_REVISION}; got ${options.modelId}@${options.revision}.`,
    );
  }
  const cacheDir = resolve(options.cacheDir);
  const modelDir = join(cacheDir, options.modelId, options.revision);
  const tokenizerFile = join(modelDir, "tokenizer.json");
  const configFile = join(modelDir, "tokenizer_config.json");
  try {
    const [tokenizerRaw, configRaw] = await Promise.all([
      readFile(tokenizerFile),
      readFile(configFile),
    ]);
    assertSHA256(tokenizerRaw, DEFAULT_TOKENIZER_SHA256, tokenizerFile);
    assertSHA256(configRaw, DEFAULT_TOKENIZER_CONFIG_SHA256, configFile);
    const tokenizerJSON: unknown = JSON.parse(tokenizerRaw.toString("utf8"));
    const tokenizerConfig: unknown = JSON.parse(configRaw.toString("utf8"));
    const { XLMRobertaTokenizer } = await import("@huggingface/transformers");
    const tokenizer = new XLMRobertaTokenizer(tokenizerJSON, tokenizerConfig);
    return {
      name: `${options.modelId}@${options.revision}`,
      count: (value) => countTokens(tokenizer, value),
    };
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    throw new Error(`Cannot load pinned tokenizer from ${modelDir}: ${detail}`);
  }
}

function assertSHA256(value: Uint8Array, expected: string, path: string): void {
  const actual = createHash("sha256").update(value).digest("hex");
  if (actual !== expected) {
    throw new Error(`Tokenizer checksum mismatch for ${path}: expected ${expected}, got ${actual}.`);
  }
}

function countTokens(tokenizer: PreTrainedTokenizer, text: string): number {
  const result = tokenizer(text, {
    truncation: false,
    padding: false,
    add_special_tokens: true,
  });
  if (!result.input_ids || typeof result.input_ids.size !== "number") {
    throw new Error("Tokenizer did not return input_ids.");
  }
  return result.input_ids.size;
}
