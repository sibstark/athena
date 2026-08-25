import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  chunkNormalizedDocuments,
  type ChunkRecord,
  type ChunkReport,
  type TokenCounter,
} from "../src/chunker.js";
import type { DocumentTreeNode, NormalizedDocument } from "../src/contracts.js";

const temporaryDirectories: string[] = [];

const wordCounter: TokenCounter = {
  name: "test-word-counter",
  count(value) {
    return value.match(/\S+/gu)?.length ?? 0;
  },
};

afterEach(async () => {
  await Promise.all(
    temporaryDirectories.splice(0).map((directory) =>
      rm(directory, { recursive: true, force: true }),
    ),
  );
});

describe("Markdown chunker", () => {
  it("keeps independent H2 sections separate", async () => {
    const result = await run([
      document("# Сценарий\n\n## Первый раздел\n\nТекст первого раздела.\n\n## Второй раздел\n\nТекст второго раздела."),
    ]);

    const first = result.chunks.find((chunk) => chunk.metadata.section_title === "Первый раздел");
    const second = result.chunks.find((chunk) => chunk.metadata.section_title === "Второй раздел");
    expect(first?.content).toContain("Текст первого");
    expect(first?.content).not.toContain("Текст второго");
    expect(second?.content).toContain("Текст второго");
  });

  it("builds nested heading paths and strips heading formatting", async () => {
    const result = await run([
      document(
        "# **Сценарий урока**\n\nВведение.\n\n## 4. Постановка рук\n\nТекст.\n\n### **Без абакуса**\n\nСовет.",
      ),
    ]);
    const child = result.chunks.find((chunk) => chunk.metadata.section_title === "Без абакуса");

    expect(child?.metadata).toMatchObject({
      heading_path: ["Сценарий урока", "4. Постановка рук", "Без абакуса"],
      heading_level: 3,
    });
    expect(child?.content).toContain("### **Без абакуса**");
  });

  it("creates a preamble section for text before the first heading", async () => {
    const result = await run([document("Важное введение.\n\n# Основной раздел\n\nТекст.")]);
    const preamble = result.chunks[0];

    expect(preamble?.metadata).toMatchObject({
      section_title: "Преамбула",
      heading_path: [],
      heading_level: 0,
    });
    expect(preamble?.content).toBe("Важное введение.");
  });

  it("keeps a small semantic section below the soft minimum", async () => {
    const result = await run([document("## Коротко\n\nДа.")], {
      minTokens: 100,
      targetTokens: 100,
    });

    expect(result.chunks).toHaveLength(1);
    expect(result.chunks[0]!.metadata.token_count).toBeLessThan(100);
    expect(result.report.chunks_below_min_tokens).toBe(1);
  });

  it("splits a large section by paragraphs and numbers section parts", async () => {
    const paragraphs = Array.from(
      { length: 12 },
      (_, index) => `Абзац ${index + 1} содержит несколько полезных слов для проверки разбиения.`,
    );
    const result = await run([document(`## Большой раздел\n\n${paragraphs.join("\n\n")}`)], {
      targetTokens: 35,
      maxTokens: 45,
      overlapTokens: 0,
    });

    expect(result.chunks.length).toBeGreaterThan(1);
    expect(result.chunks.map((chunk) => chunk.metadata.section_part)).toEqual(
      Array.from({ length: result.chunks.length }, (_, index) => index + 1),
    );
    expect(result.chunks.every((chunk) => chunk.metadata.section_parts_total === result.chunks.length)).toBe(true);
  });

  it("overlaps only complete paragraphs", async () => {
    const paragraphs = Array.from(
      { length: 8 },
      (_, index) => `Полный абзац номер ${index + 1} содержит контрольную фразу.`,
    );
    const result = await run([document(`## Раздел\n\n${paragraphs.join("\n\n")}`)], {
      targetTokens: 30,
      maxTokens: 45,
      overlapTokens: 10,
    });

    expect(result.chunks.length).toBeGreaterThan(1);
    const firstParagraphs = paragraphs.filter((paragraph) => result.chunks[0]!.content.includes(paragraph));
    const overlap = firstParagraphs.filter((paragraph) => result.chunks[1]!.content.includes(paragraph));
    expect(overlap.length).toBeGreaterThan(0);
    expect(overlap.every((paragraph) => result.chunks[1]!.content.includes(paragraph))).toBe(true);
  });

  it("splits an oversized list item without truncating its text", async () => {
    const item = `- ${Array.from({ length: 80 }, () => "слово").join(" ")}`;
    const result = await run([document(`## Список\n\n${item}\n- короткий пункт`)], {
      targetTokens: 25,
      maxTokens: 35,
      overlapTokens: 0,
    });

    expect(result.chunks.length).toBeGreaterThan(1);
    expect(result.chunks.every((chunk) => chunk.metadata.token_count <= 35)).toBe(true);
    expect(result.chunks.every((chunk) => chunk.metadata.oversized_reason !== "list_item")).toBe(true);
    expect(result.chunks.map((chunk) => chunk.content).join("\n").match(/слово/gu)).toHaveLength(80);
    expect(result.chunks.some((chunk) => chunk.content.includes("- слово"))).toBe(true);
  });

  it("splits malformed multiline list content on line boundaries", async () => {
    const lines = Array.from(
      { length: 6 },
      (_, index) => `Отзыв ${index + 1}. ${Array.from({ length: 12 }, () => "полезный текст").join(" ")}.`,
    );
    const result = await run([document(`## Отзывы\n\n- ${lines.join("\n")}`)], {
      targetTokens: 30,
      maxTokens: 45,
      overlapTokens: 0,
    });

    expect(result.chunks.length).toBeGreaterThan(1);
    expect(result.chunks.every((chunk) => chunk.metadata.token_count <= 45)).toBe(true);
    for (const line of lines) {
      expect(result.chunks.some((chunk) => chunk.content.includes(line))).toBe(true);
    }
  });

  it("keeps a fitting Markdown table in one chunk", async () => {
    const table = "| Имя | Значение |\n| --- | --- |\n| A | 1 |\n| B | 2 |";
    const result = await run([document(`## Таблица\n\n${table}`)]);

    expect(result.chunks).toHaveLength(1);
    expect(result.chunks[0]?.metadata.contains_table).toBe(true);
    expect(result.chunks[0]?.content).toContain(table);
  });

  it("splits a large table by rows and repeats its header", async () => {
    const header = "| Имя | Описание |\n| --- | --- |";
    const rows = Array.from(
      { length: 14 },
      (_, index) => `| Строка ${index} | ${Array.from({ length: 7 }, () => "данные").join(" ")} |`,
    );
    const result = await run([document(`## Таблица\n\n${header}\n${rows.join("\n")}`)], {
      targetTokens: 35,
      maxTokens: 45,
      overlapTokens: 0,
    });
    const tableChunks = result.chunks.filter((chunk) => chunk.metadata.contains_table);

    expect(tableChunks.length).toBeGreaterThan(1);
    expect(tableChunks.every((chunk) => chunk.content.includes(header))).toBe(true);
    expect(tableChunks.map((chunk) => chunk.metadata.table_part)).toEqual(
      Array.from({ length: tableChunks.length }, (_, index) => index + 1),
    );
  });

  it("hard-splits a single oversized table row within the model limit", async () => {
    const header = "| Имя | Описание |\n| --- | --- |";
    const row = `| Строка | ${Array.from({ length: 120 }, () => "данные").join(" ")} |`;
    const result = await run([document(`## Таблица\n\n${header}\n${row}`)], {
      targetTokens: 25,
      maxTokens: 35,
      overlapTokens: 0,
    });

    expect(result.chunks.length).toBeGreaterThan(1);
    expect(result.chunks.every((chunk) => chunk.metadata.token_count <= 35)).toBe(true);
    expect(result.report.chunks_above_max_tokens).toBe(0);
  });

  it("hard-splits an oversized protected inline atom within the model limit", async () => {
    const atom = `\`${Array.from({ length: 120 }, () => "value").join(" ")}\``;
    const result = await run([document(`## Значение\n\n${atom}`)], {
      targetTokens: 25,
      maxTokens: 35,
      overlapTokens: 0,
    });

    expect(result.chunks.length).toBeGreaterThan(1);
    expect(result.chunks.every((chunk) => chunk.metadata.token_count <= 35)).toBe(true);
    expect(result.report.chunks_above_max_tokens).toBe(0);
  });

  it("hard-splits oversized fenced code within the model limit", async () => {
    const code = ["```ts", ...Array.from({ length: 70 }, (_, index) => `const v${index} = ${index};`), "```"].join("\n");
    const result = await run([document(`## Код\n\n${code}`)], {
      targetTokens: 25,
      maxTokens: 35,
      overlapTokens: 0,
    });
    const codeChunks = result.chunks.filter((chunk) => chunk.metadata.contains_code);

    expect(result.chunks.length).toBeGreaterThan(1);
    expect(codeChunks.length).toBeGreaterThan(1);
    expect(result.chunks.every((chunk) => chunk.metadata.token_count <= 35)).toBe(true);
    expect(result.report.chunks_above_max_tokens).toBe(0);
    const combinedCode = codeChunks.map((chunk) => chunk.content).join(" ").replace(/\s+/gu, " ");
    for (let index = 0; index < 70; index += 1) {
      expect(combinedCode).toContain(`const v${index} = ${index};`);
    }
  });

  it("preserves image Markdown but transforms images for embedding_text", async () => {
    const content = [
      "## Изображения",
      "",
      "![](../../assets/empty.png)",
      "",
      "![Схема счёта](../../assets/scheme.png)",
    ].join("\n");
    const result = await run([document(content)]);
    const chunk = result.chunks[0]!;

    expect(chunk.content).toContain("![](../../assets/empty.png)");
    expect(chunk.embedding_text).not.toContain("empty.png");
    expect(chunk.embedding_text).toContain("Изображение: Схема счёта");
    expect(chunk.metadata.assets).toEqual([
      { type: "image", path: "../../assets/empty.png", alt: "" },
      { type: "image", path: "../../assets/scheme.png", alt: "Схема счёта" },
    ]);
  });

  it("keeps Markdown links in content and uses their labels for embeddings", async () => {
    const result = await run([
      document("## Ссылка\n\n[Инструкция по кабинету](https://example.com/instruction?a=1&amp;b=2)"),
    ]);
    const chunk = result.chunks[0]!;

    expect(chunk.content).toContain("[Инструкция по кабинету](https://example.com");
    expect(chunk.embedding_text).toContain("Инструкция по кабинету");
    expect(chunk.embedding_text).not.toContain("https://example.com");
    expect(chunk.metadata.external_urls).toEqual([
      "https://example.com/instruction?a=1&b=2",
    ]);
  });

  it("adds breadcrumbs to embedding_text and inherits tree metadata", async () => {
    const child = document("## Урок\n\nМатериал", {
      document_id: "page_2",
      parent_document_id: "page_1",
      metadata: {
        page_id: "2",
        parent_page_id: "1",
        root_page_id: "1",
        depth: 1,
        ancestor_ids: ["1"],
        breadcrumbs: ["Корень", "Дочерний урок"],
        context_text: "Корень → Дочерний урок",
      },
    });
    const result = await run([child], {}, [
      treeNode("page_1", null, 0),
      treeNode("page_2", "page_1", 1),
    ]);
    const chunk = result.chunks[0]!;

    expect(chunk.embedding_text).toContain("Путь: Корень → Дочерний урок");
    expect(chunk.parent_document_id).toBe("page_1");
    expect(chunk.metadata).toMatchObject({
      page_id: "2",
      parent_page_id: "1",
      root_page_id: "1",
      depth: 1,
      ancestor_ids: ["1"],
      breadcrumbs: ["Корень", "Дочерний урок"],
    });
  });

  it("skips non-indexable and empty documents", async () => {
    const result = await run([
      document("https://example.com", {
        document_id: "page_1",
        metadata: { indexable: false, document_type: "external_link" },
      }),
      document("", { document_id: "page_2" }),
    ]);

    expect(result.chunks).toHaveLength(0);
    expect(result.report).toMatchObject({
      input_documents: 2,
      processed_documents: 0,
      skipped_documents: 2,
    });
  });

  it("replaces U+001F and records a warning", async () => {
    const result = await run([document("## Таблица\n\n| A | B |\n| --- | --- |\n| x\u001Fy | z |")] );

    expect(result.chunks[0]?.content).toContain("x y");
    expect(result.chunks[0]?.content).not.toContain("\u001F");
    expect(result.report.control_character_replacements).toBe(1);
    expect(result.report.control_character_warnings).toEqual([
      { document_id: "page_1", character: "U+001F", count: 1 },
    ]);
  });

  it("produces stable IDs and an inserted section does not change unchanged chunk IDs", async () => {
    const original = document("## Первый\n\nОдин.\n\n## Второй\n\nДва.");
    const repeated = await run([original]);
    const secondRun = await run([original]);
    const inserted = await run([
      document("## Новый\n\nНоль.\n\n## Первый\n\nОдин.\n\n## Второй\n\nДва."),
    ]);

    expect(secondRun.chunks.map((chunk) => chunk.chunk_id)).toEqual(
      repeated.chunks.map((chunk) => chunk.chunk_id),
    );
    const stable = repeated.chunks.map((chunk) => chunk.chunk_id);
    expect(inserted.chunks.filter((chunk) => stable.includes(chunk.chunk_id))).toHaveLength(stable.length);
  });

  it("links neighboring chunks consistently", async () => {
    const result = await run([
      document("## Первый\n\nОдин.\n\n## Второй\n\nДва.\n\n## Третий\n\nТри."),
    ]);

    expect(result.chunks.length).toBeGreaterThanOrEqual(3);
    for (const [index, chunk] of result.chunks.entries()) {
      expect(chunk.previous_chunk_id).toBe(result.chunks[index - 1]?.chunk_id ?? null);
      expect(chunk.next_chunk_id).toBe(result.chunks[index + 1]?.chunk_id ?? null);
      expect(chunk.metadata.chunk_index).toBe(index);
      expect(chunk.metadata.chunk_count).toBe(result.chunks.length);
    }
  });

  it("retains duplicate chunks in different tree branches", async () => {
    const first = document("## Одинаково\n\nОбщий материал.", { document_id: "page_1" });
    const second = document("## Одинаково\n\nОбщий материал.", { document_id: "page_2" });
    const result = await run([first, second]);

    expect(result.chunks).toHaveLength(2);
    expect(result.chunks[0]?.content_hash).toBe(result.chunks[1]?.content_hash);
    expect(result.chunks[1]?.metadata.duplicate_of_chunk_id).toBe(result.chunks[0]?.chunk_id);
    expect(result.report.duplicate_chunks).toBe(1);
  });

  it("continues after a damaged JSONL document", async () => {
    const result = await runRaw(["{broken", JSON.stringify(document("## Рабочий\n\nТекст."))]);

    expect(result.chunks).toHaveLength(1);
    expect(result.report).toMatchObject({ input_documents: 2, processed_documents: 1, errors: 1 });
    expect(result.report.error_documents[0]?.message).toContain("Invalid JSON");
  });

  it("produces identical chunks under different absolute roots", async () => {
    const first = await run([document("## Стабильно\n\nМатериал.")]);
    const second = await run([document("## Стабильно\n\nМатериал.")]);

    expect(first.rawChunks).toBe(second.rawChunks);
  });
});

interface RunOptions {
  targetTokens?: number;
  maxTokens?: number;
  minTokens?: number;
  overlapTokens?: number;
}

async function run(
  documents: NormalizedDocument[],
  options: RunOptions = {},
  nodes?: DocumentTreeNode[],
): Promise<{ chunks: ChunkRecord[]; report: ChunkReport; rawChunks: string }> {
  return runRaw(documents.map((item) => JSON.stringify(item)), options, nodes);
}

async function runRaw(
  lines: string[],
  options: RunOptions = {},
  nodes?: DocumentTreeNode[],
): Promise<{ chunks: ChunkRecord[]; report: ChunkReport; rawChunks: string }> {
  const root = await temporaryDirectory();
  const input = join(root, "normalized.jsonl");
  const tree = join(root, "document_tree.json");
  const output = join(root, "chunks.jsonl");
  const reportFile = join(root, "chunk_report.json");
  const documentIds = lines
    .map((line) => {
      try {
        return JSON.parse(line).document_id as string | undefined;
      } catch {
        return undefined;
      }
    })
    .filter((value): value is string => Boolean(value));
  await writeFile(input, `${lines.join("\n")}\n`, "utf8");
  await writeFile(
    tree,
    `${JSON.stringify({ nodes: nodes ?? documentIds.map((id) => treeNode(id, null, 0)) }, null, 2)}\n`,
    "utf8",
  );
  const result = await chunkNormalizedDocuments({
    inputFile: input,
    treeFile: tree,
    outputFile: output,
    reportFile,
    targetTokens: options.targetTokens ?? 80,
    maxTokens: options.maxTokens ?? 120,
    minTokens: options.minTokens ?? 1,
    overlapTokens: options.overlapTokens ?? 10,
    tokenCounter: wordCounter,
  });
  const rawChunks = await readFile(output, "utf8");
  return {
    chunks: rawChunks.split("\n").filter(Boolean).map((line) => JSON.parse(line) as ChunkRecord),
    report: result.report,
    rawChunks,
  };
}

function document(
  content: string,
  overrides: {
    document_id?: string;
    parent_document_id?: string | null;
    metadata?: Partial<NormalizedDocument["metadata"]>;
  } = {},
): NormalizedDocument {
  const id = overrides.document_id ?? "page_1";
  return {
    document_id: id,
    parent_document_id: overrides.parent_document_id ?? null,
    content,
    metadata: {
      title: "Тестовый документ",
      source: `documents/${id}.md`,
      page_id: id.replace(/^page_/u, ""),
      parent_page_id: null,
      root_page_id: id.replace(/^page_/u, ""),
      depth: 0,
      ancestor_ids: [],
      breadcrumbs: ["Тестовый документ"],
      context_text: "Тестовый документ",
      document_type: "markdown",
      indexable: true,
      external_urls: [],
      assets: [],
      character_count: [...content].length,
      word_count: content.match(/[\p{L}\p{N}]+/gu)?.length ?? 0,
      heading_count: content.match(/^#{1,6}\s/gmu)?.length ?? 0,
      external_url_count: 0,
      asset_count: 0,
      version: null,
      date: null,
      category: "methodology",
      status: "active",
      language: "ru",
      ...overrides.metadata,
    },
    content_hash: "fixture-hash",
  };
}

function treeNode(
  nodeId: string,
  parentNodeId: string | null,
  depth: number,
): DocumentTreeNode {
  return {
    node_id: nodeId,
    parent_node_id: parentNodeId,
    page_id: nodeId.replace(/^page_/u, ""),
    title: nodeId,
    depth,
    has_document: true,
    source: `documents/${nodeId}.md`,
  };
}

async function temporaryDirectory(): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "athena-chunker-"));
  temporaryDirectories.push(directory);
  return directory;
}
