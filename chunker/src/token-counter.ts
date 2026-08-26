import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import type { PreTrainedTokenizer } from "@huggingface/transformers";
import type { TokenCounter } from "./chunker.js";

export async function createPinnedTokenCounter(options: {
  modelId: string;
  revision: string;
  tokenizerSha256: string;
  tokenizerConfigSha256: string;
  cacheDir: string;
}): Promise<TokenCounter> {
  const modelDir = join(
    resolve(options.cacheDir),
    options.modelId,
    options.revision,
  );
  const tokenizerFile = join(modelDir, "tokenizer.json");
  const configFile = join(modelDir, "tokenizer_config.json");
  try {
    const [tokenizerRaw, configRaw] = await Promise.all([
      readFile(tokenizerFile),
      readFile(configFile),
    ]);
    assertSHA256(tokenizerRaw, options.tokenizerSha256, tokenizerFile);
    assertSHA256(configRaw, options.tokenizerConfigSha256, configFile);
    const { XLMRobertaTokenizer } = await import("@huggingface/transformers");
    const tokenizer = new XLMRobertaTokenizer(
      JSON.parse(tokenizerRaw.toString("utf8")) as unknown,
      JSON.parse(configRaw.toString("utf8")) as unknown,
    );
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
  if (actual !== expected)
    throw new Error(
      `Tokenizer checksum mismatch for ${path}: expected ${expected}, got ${actual}.`,
    );
}

function countTokens(tokenizer: PreTrainedTokenizer, text: string): number {
  const result = tokenizer(text, {
    truncation: false,
    padding: false,
    add_special_tokens: true,
  });
  if (!result.input_ids || typeof result.input_ids.size !== "number")
    throw new Error("Tokenizer did not return input_ids.");
  return result.input_ids.size;
}
