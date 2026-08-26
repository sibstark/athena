import { access, mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { TokenCounter } from "../src/chunker.js";
import {
  loadChunkEmbeddingConfig,
  type ChunkEmbeddingConfig,
} from "../src/embedding-config.js";
import { createPinnedTokenCounter } from "../src/token-counter.js";
import { chunkNormalizedRun } from "../src/run.js";

const wordCounter: TokenCounter = {
  name: "test-word-counter",
  count(value) {
    return value.match(/\S+/gu)?.length ?? 0;
  },
};
const testEmbeddingConfig: ChunkEmbeddingConfig = {
  modelId: "test/model",
  revision: "test-revision",
  tokenizerSha256: "0".repeat(64),
  tokenizerConfigSha256: "1".repeat(64),
  targetTokens: 320,
  maxTokens: 450,
  minTokens: 80,
  overlapTokens: 50,
};

describe("chunk run", () => {
  it("processes every workspace, publishes failures, and replaces the old run", async () => {
    const root = await mkdtemp(join(tmpdir(), "athena-chunk-run-"));
    const input = join(root, "normalized_123");
    const output = join(root, "chunks_123");
    await mkdir(output, { recursive: true });
    await writeFile(join(output, "old.txt"), "old", "utf8");
    await writeRunFixture(input, [
      workspace(1, "complete"),
      workspace(2, "complete_with_warnings"),
      {
        ...workspace(3, "failed"),
        errors: 2,
        normalizedPath: undefined,
        treePath: undefined,
      },
    ]);
    await writeWorkspace(input, 1, [
      document("page_1", "## Раздел\n\nМатериал."),
    ]);
    await writeWorkspace(input, 2, []);

    const result = await chunkNormalizedRun({
      runDir: input,
      outputDir: output,
      tokenCounter: wordCounter,
      embeddingConfig: testEmbeddingConfig,
      targetTokens: 80,
      maxTokens: 120,
      minTokens: 1,
      overlapTokens: 10,
      now: () => new Date("2026-08-25T00:00:00.000Z"),
    });

    expect(result.report).toMatchObject({
      status: "complete_with_errors",
      workspaceCount: 3,
      successfulWorkspaces: 2,
      warningWorkspaces: 0,
      failedWorkspaces: 1,
      processedDocuments: 1,
      errors: 1,
    });
    expect(result.manifest.workspaces.map((item) => item.status)).toEqual([
      "complete",
      "complete",
      "failed",
    ]);
    expect(await exists(join(output, "old.txt"))).toBe(false);
    expect(
      await readFile(join(output, "workspace_2", "chunks.jsonl"), "utf8"),
    ).toBe("");
    expect(await exists(join(output, "workspace_3", "chunks.jsonl"))).toBe(
      false,
    );
    const failedReport = JSON.parse(
      await readFile(join(output, "workspace_3", "chunk_report.json"), "utf8"),
    ) as { errors: number };
    expect(failedReport.errors).toBe(1);
  });

  it("publishes valid documents and reports a damaged document as a warning", async () => {
    const root = await mkdtemp(join(tmpdir(), "athena-chunk-run-"));
    const input = join(root, "normalized_8");
    const output = join(root, "chunks_8");
    await writeRunFixture(input, [workspace(1, "complete")], "run_8");
    await writeWorkspace(input, 1, [
      document("page_1", "## Рабочий документ\n\nПолезный материал."),
      document("page_2", "```plaintext\nНезакрытый блок"),
    ]);

    const result = await chunkNormalizedRun({
      runDir: input,
      outputDir: output,
      tokenCounter: wordCounter,
      embeddingConfig: testEmbeddingConfig,
    });

    expect(result.report).toMatchObject({
      status: "complete_with_warnings",
      successfulWorkspaces: 1,
      warningWorkspaces: 1,
      failedWorkspaces: 0,
      processedDocuments: 1,
      errors: 1,
    });
    expect(result.manifest.formatVersion).toBe(2);
    expect(result.manifest.workspaces[0]).toMatchObject({
      status: "complete_with_warnings",
      chunksPath: "workspace_1/chunks.jsonl",
      processedDocuments: 1,
      errors: 1,
    });
    const chunks = await readFile(
      join(output, "workspace_1", "chunks.jsonl"),
      "utf8",
    );
    expect(chunks).toContain('"document_id":"page_1"');
    expect(chunks).not.toContain('"document_id":"page_2"');
    const report = JSON.parse(
      await readFile(join(output, "workspace_1", "chunk_report.json"), "utf8"),
    ) as {
      errors: number;
      error_documents: Array<{ document_id: string }>;
    };
    expect(report.errors).toBe(1);
    expect(report.error_documents).toEqual([
      expect.objectContaining({ document_id: "page_2" }),
    ]);
  });

  it("does not publish an empty partial workspace when every document is damaged", async () => {
    const root = await mkdtemp(join(tmpdir(), "athena-chunk-run-"));
    const input = join(root, "normalized_9");
    const output = join(root, "chunks_9");
    await writeRunFixture(input, [workspace(1, "complete")], "run_9");
    await mkdir(join(input, "workspace_1"), { recursive: true });
    await writeFile(
      join(input, "workspace_1", "normalized.jsonl"),
      "{broken\n",
      "utf8",
    );
    await writeFile(
      join(input, "workspace_1", "document_tree.json"),
      '{"nodes":[]}\n',
      "utf8",
    );

    const result = await chunkNormalizedRun({
      runDir: input,
      outputDir: output,
      tokenCounter: wordCounter,
      embeddingConfig: testEmbeddingConfig,
    });

    expect(result.report.status).toBe("failed");
    expect(await exists(join(output, "workspace_1", "chunks.jsonl"))).toBe(
      false,
    );
    expect(await exists(join(output, "workspace_1", "chunk_report.json"))).toBe(
      true,
    );
  });

  it("requires matching normalized, chunks, and source run suffixes", async () => {
    const root = await mkdtemp(join(tmpdir(), "athena-chunk-run-"));
    const input = join(root, "normalized_1");
    await writeRunFixture(input, [workspace(1, "complete")], "run_2");
    await expect(
      chunkNormalizedRun({
        runDir: input,
        outputDir: join(root, "chunks_1"),
        tokenCounter: wordCounter,
        embeddingConfig: testEmbeddingConfig,
      }),
    ).rejects.toThrow("belongs to run_2, expected run_1");
    await expect(
      chunkNormalizedRun({
        runDir: input,
        outputDir: join(root, "chunks_2"),
        tokenCounter: wordCounter,
        embeddingConfig: testEmbeddingConfig,
      }),
    ).rejects.toThrow("must be named chunks_1");
  });
});

it("loads the configured pinned tokenizer directly from the local cache", async () => {
  let config: ChunkEmbeddingConfig;
  try {
    config = loadChunkEmbeddingConfig();
  } catch {
    return;
  }
  const cache = resolve("../var/models");
  const tokenizerFile = join(
    cache,
    config.modelId,
    config.revision,
    "tokenizer.json",
  );
  try {
    await access(tokenizerFile);
  } catch {
    return;
  }
  const counter = await createPinnedTokenCounter({
    modelId: config.modelId,
    revision: config.revision,
    tokenizerSha256: config.tokenizerSha256,
    tokenizerConfigSha256: config.tokenizerConfigSha256,
    cacheDir: cache,
  });
  expect(counter.count("Путь: Тест\n\nРаздел: Пример")).toBeGreaterThan(0);
});

function workspace(id: number, status: string) {
  return {
    id,
    name: `Workspace ${id}`,
    path: `workspace_${id}`,
    qdrantCollection: `workspace_${id}`,
    normalizedPath: `workspace_${id}/normalized.jsonl`,
    treePath: `workspace_${id}/document_tree.json`,
    reportPath: `workspace_${id}/normalization_report.json`,
    status,
    processedDocuments: status === "failed" ? 0 : 1,
    suspiciousDocuments: 0,
    errors: 0,
    warnings: 0,
  };
}

async function writeRunFixture(
  path: string,
  workspaces: unknown[],
  sourceRun = "run_123",
): Promise<void> {
  await mkdir(path, { recursive: true });
  await writeFile(
    join(path, "normalization_manifest.json"),
    `${JSON.stringify(
      {
        formatVersion: 1,
        generatedAt: "2026-08-25T00:00:00.000Z",
        sourceRun,
        source: {
          provider: "bitrix24-note",
          portal: "https://example.com",
          url: "https://example.com/note/",
        },
        workspaces,
      },
      null,
      2,
    )}\n`,
    "utf8",
  );
}

async function writeWorkspace(
  root: string,
  id: number,
  documents: ReturnType<typeof document>[],
): Promise<void> {
  const path = join(root, `workspace_${id}`);
  await mkdir(path, { recursive: true });
  const jsonl =
    documents.length > 0
      ? `${documents.map((item) => JSON.stringify(item)).join("\n")}\n`
      : "";
  await writeFile(join(path, "normalized.jsonl"), jsonl, "utf8");
  await writeFile(
    join(path, "document_tree.json"),
    `${JSON.stringify(
      {
        nodes: documents.map((item) => ({
          node_id: item.document_id,
          parent_node_id: null,
          page_id: item.metadata.page_id,
          title: item.metadata.title,
          depth: 0,
          has_document: true,
          source: item.metadata.source,
        })),
      },
      null,
      2,
    )}\n`,
    "utf8",
  );
}

function document(id: string, content: string) {
  return {
    document_id: id,
    parent_document_id: null,
    content,
    metadata: {
      title: "Документ",
      source: `documents/${id}.md`,
      page_id: id.replace("page_", ""),
      document_url: `https://hwschool.bitrix24.ru/note/document/${id.replace("page_", "")}/`,
      parent_page_id: null,
      root_page_id: id.replace("page_", ""),
      depth: 0,
      ancestor_ids: [],
      breadcrumbs: ["Документ"],
      context_text: "Документ",
      document_type: "markdown",
      indexable: true,
      external_urls: [],
      assets: [],
      version: null,
      date: null,
      category: null,
      status: "active",
      language: "ru",
    },
    content_hash: "fixture",
  };
}

async function exists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}
