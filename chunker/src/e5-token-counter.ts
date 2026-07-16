import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import type { PreTrainedTokenizer } from "@huggingface/transformers";
import type { TokenCounter } from "./chunker.js";

export const DEFAULT_TOKENIZER = "Xenova/multilingual-e5-small";
export const DEFAULT_TOKENIZER_REVISION = "761b726dd34fb83930e26aab4e9ac3899aa1fa78";

export async function createE5TokenCounter(options: {
  modelId: string;
  revision: string;
  cacheDir: string;
  offline: boolean;
}): Promise<TokenCounter> {
  if (options.modelId !== DEFAULT_TOKENIZER || options.revision !== DEFAULT_TOKENIZER_REVISION) {
    throw new Error(
      `Athena requires tokenizer ${DEFAULT_TOKENIZER}@${DEFAULT_TOKENIZER_REVISION}; got ${options.modelId}@${options.revision}.`,
    );
  }
  const cacheDir = resolve(options.cacheDir);
  await mkdir(cacheDir, { recursive: true });
  try {
    const { AutoTokenizer } = await import("@huggingface/transformers");
    const tokenizer = await AutoTokenizer.from_pretrained(options.modelId, {
      revision: options.revision,
      cache_dir: cacheDir,
      local_files_only: options.offline,
    });
    return {
      name: `${options.modelId}@${options.revision}`,
      count: (value) => countTokens(tokenizer, value),
    };
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    const mode = options.offline ? " from the local cache" : "";
    throw new Error(`Cannot load tokenizer${mode} at ${cacheDir}: ${detail}`);
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
