import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { selectChunkSample } from "../src/io.js";

describe("deterministic sample", () => {
  it("is stable for one seed and reports broken lines", async () => {
    const dir = await mkdtemp(join(tmpdir(), "athena-tei-"));
    const path = join(dir, "chunks.jsonl");
    const lines = Array.from({ length: 20 }, (_, index) => JSON.stringify({
      chunk_id: `chunk_${index}`,
      document_id: `page_${index}`,
      parent_document_id: null,
      content: "text",
      embedding_text: `text ${index}`,
      embedding_text_hash: `hash_${index}`,
      metadata: { token_count: index * 30, heading_path: [] },
    }));
    await writeFile(path, `${lines.join("\n")}\n{broken\n`, "utf8");
    const first = await selectChunkSample(path, 10, 42);
    const second = await selectChunkSample(path, 10, 42);
    expect(first.chunks.map((item) => item.chunk_id)).toEqual(second.chunks.map((item) => item.chunk_id));
    expect(first.errors).toHaveLength(1);
  });
});
