import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  extractPageId,
  extractFirstH1,
  normalizeDirectory,
  normalizeMarkdown,
  normalizeMarkdownFile,
  parseFrontMatter,
} from "../src/normalizer.js";

const temporaryDirectories: string[] = [];

afterEach(async () => {
  await Promise.all(
    temporaryDirectories.splice(0).map((directory) =>
      rm(directory, { recursive: true, force: true }),
    ),
  );
});

describe("Markdown normalization", () => {
  it("extracts valid YAML Front Matter and metadata", async () => {
    const file = await temporaryMarkdown(
      [
        "---",
        "id: 42",
        "collection_id: 17",
        'title: "YAML title"',
        'version: "1.0"',
        "date: 2026-07-14",
        "category: methodology",
        "status: draft",
        "language: en",
        "---",
        "# Markdown title",
        "",
        "Body",
      ].join("\n"),
    );

    const document = await normalizeMarkdownFile(file, "documents/example.md");

    expect(document.content).toBe("# Markdown title\n\nBody");
    expect(document.metadata).toMatchObject({
      title: "YAML title",
      source: "documents/example.md",
      page_id: null,
      parent_page_id: null,
      root_page_id: null,
      depth: 0,
      ancestor_ids: [],
      breadcrumbs: ["YAML title"],
      context_text: "YAML title",
      document_type: "short_note",
      indexable: true,
      version: "1.0",
      date: "2026-07-14",
      category: "methodology",
      status: "draft",
      language: "en",
    });
    expect(document.document_id).toBe(`doc_${hash("source:documents/example.md")}`);
    expect(document.parent_document_id).toBeNull();
    expect(document.content_hash).toBe(hash(document.content));
  });

  it("accepts a document without YAML and gets title from the first H1", async () => {
    const file = await temporaryMarkdown("Intro\n\n# First **heading**\n\n# Second heading");

    const document = await normalizeMarkdownFile(file, "plain.md");

    expect(document.metadata.title).toBe("First heading");
    expect(document.metadata.version).toBeNull();
    expect(document.metadata.status).toBe("active");
    expect(document.metadata.language).toBe("ru");
    expect(document.document_id).toBe(`doc_${hash("source:plain.md")}`);
  });

  it("keeps non-empty files without a title with title null", async () => {
    const file = await temporaryMarkdown("Paragraph without a heading.");
    const document = await normalizeMarkdownFile(file, "untitled.md");

    expect(document.metadata.title).toBeNull();
    expect(extractFirstH1(document.content)).toBeNull();
  });

  it("normalizes line endings, trailing spaces, and repeated blank lines", () => {
    const markdown = "  First line   \r\n\r\n \r\n\r\n\r\nSecond\t\r\n";

    expect(normalizeMarkdown(markdown)).toBe("  First line\n\n\nSecond");
  });

  it("preserves Markdown tables, lists, and links", () => {
    const markdown = [
      "| Name | Value |",
      "| --- | --- |",
      "| A | 1 |",
      "",
      "- first",
      "- [second](https://example.com)",
    ].join("\n");

    expect(normalizeMarkdown(markdown)).toBe(markdown);
  });

  it("does not alter fenced code block contents", () => {
    const markdown = [
      "Outside trailing spaces   ",
      "",
      "```ts",
      "const value = 1;   ",
      "   ",
      "",
      "",
      "console.log(value);\t",
      "```",
      "",
      "After   ",
    ].join("\n");

    const normalized = normalizeMarkdown(markdown);

    expect(normalized).toContain(
      [
        "```ts",
        "const value = 1;   ",
        "   ",
        "",
        "",
        "console.log(value);\t",
        "```",
      ].join("\n"),
    );
    expect(normalized.startsWith("Outside trailing spaces\n")).toBe(true);
    expect(normalized.endsWith("After")).toBe(true);
  });

  it("does not alter indented code block contents", () => {
    const markdown = "Text   \n\n    const value = 1;   \n    next();\t\n\nAfter   ";

    expect(normalizeMarkdown(markdown)).toContain(
      "    const value = 1;   \n    next();\t",
    );
  });

  it("reports invalid YAML without stopping other files", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "input");
    const output = join(root, "normalized", "documents.jsonl");
    await mkdir(input, { recursive: true });
    await writeFile(join(input, "broken.md"), "---\ntitle: [broken\n---\nText", "utf8");
    await writeFile(join(input, "valid.md"), "# Valid\n\nText", "utf8");

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const lines = await jsonl(output);

    expect(result.report).toMatchObject({
      found_files: 2,
      processed_documents: 1,
      errors: 1,
    });
    expect(result.report.error_files[0]).toMatchObject({ source: "broken.md" });
    expect(result.report.error_files[0]?.message).toContain("Invalid YAML Front Matter");
    expect(lines).toHaveLength(1);
    expect(lines[0]?.metadata.title).toBe("Valid");
  });

  it("skips empty files and counts files without titles", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "input");
    const output = join(root, "normalized", "documents.jsonl");
    await mkdir(input, { recursive: true });
    await writeFile(join(input, "empty.md"), "---\nstatus: active\n---\n \n\n", "utf8");
    await writeFile(join(input, "untitled.md"), "Useful text", "utf8");

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const lines = await jsonl(output);

    expect(result.report).toMatchObject({
      found_files: 2,
      processed_documents: 1,
      empty_files: 1,
      files_without_title: 2,
      errors: 0,
    });
    expect(lines[0]?.metadata.title).toBeNull();
  });

  it("detects exact duplicates after normalization", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "input");
    const output = join(root, "normalized", "documents.jsonl");
    await mkdir(input, { recursive: true });
    await writeFile(join(input, "a.md"), "# Same\n\n\n\nText   \n", "utf8");
    await writeFile(join(input, "b.md"), "# Same\r\n\r\n\r\nText\r\n", "utf8");

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const lines = await jsonl(output);

    expect(result.report.processed_documents).toBe(2);
    expect(result.report.duplicate_files).toBe(1);
    expect(lines).toHaveLength(2);
    expect(lines[0]?.metadata.source).toBe("a.md");
    expect(lines[1]?.metadata.duplicate_of).toBe(lines[0]?.document_id);
  });

  it("produces the same IDs and hashes on repeated runs and after moving a Bitrix file", async () => {
    const source = [
      "---",
      "id: 3493",
      "collection_id: 17",
      'title: "Stable"',
      "---",
      "Body",
    ].join("\n");
    const firstFile = await temporaryMarkdown(source);
    const secondFile = await temporaryMarkdown(source);

    const first = await normalizeMarkdownFile(firstFile, "old/stable--3493.md");
    const repeated = await normalizeMarkdownFile(firstFile, "old/stable--3493.md");
    const moved = await normalizeMarkdownFile(secondFile, "new/stable--3493.md");

    expect(repeated.document_id).toBe(first.document_id);
    expect(repeated.content_hash).toBe(first.content_hash);
    expect(moved.document_id).toBe(first.document_id);
    expect(first.document_id).toBe("page_3493");
    expect(moved.content_hash).toBe(first.content_hash);
  });

  it("treats malformed UTF-8 as a per-file error", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "input");
    const output = join(root, "normalized", "documents.jsonl");
    await mkdir(input, { recursive: true });
    await writeFile(join(input, "broken.md"), new Uint8Array([0xc3, 0x28]));

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });

    expect(result.report.errors).toBe(1);
    expect(result.report.error_files[0]?.message).toBe("File is not valid UTF-8.");
  });

  it("separates Front Matter from Markdown without changing the body", () => {
    expect(parseFrontMatter("---\ntitle: Test\n---\n# Body")).toEqual({
      attributes: { title: "Test" },
      markdown: "# Body",
    });
  });

  it("extracts numeric page IDs from Unicode file and directory names", () => {
    expect(extractPageId("библиотека-ресурсов--3039.md")).toBe("3039");
    expect(extractPageId("ментальная-арифметика--3499")).toBe("3499");
    expect(extractPageId("document.md")).toBeNull();
  });

  it("creates a root page node and a root document", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "content");
    const output = join(root, "normalized", "normalized.jsonl");
    await mkdir(input, { recursive: true });
    await writeFile(
      join(input, "библиотека-ресурсов--3039.md"),
      "---\ntitle: Библиотека ресурсов\n---\n# Root\n\nContent",
      "utf8",
    );

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const [document] = await jsonl(output);
    const tree = await readJson(result.treeFile);

    expect(document).toMatchObject({
      document_id: "page_3039",
      parent_document_id: null,
      metadata: {
        page_id: "3039",
        parent_page_id: null,
        root_page_id: "3039",
        depth: 0,
        ancestor_ids: [],
        breadcrumbs: ["Библиотека ресурсов"],
      },
    });
    expect(tree.nodes).toEqual([
      expect.objectContaining({
        node_id: "page_3039",
        parent_node_id: null,
        depth: 0,
        has_document: true,
      }),
    ]);
  });

  it("builds parent links, breadcrumbs, and context through four levels", async () => {
    const fixture = await nestedTreeFixture();
    const output = join(fixture.root, "normalized", "normalized.jsonl");
    const result = await normalizeDirectory({ inputDir: fixture.input, outputFile: output });
    const documents = await jsonl(output);
    const child = documents.find((document) => document.document_id === "page_3693");

    expect(child).toMatchObject({
      parent_document_id: "page_3663",
      metadata: {
        page_id: "3693",
        parent_page_id: "3663",
        root_page_id: "3035",
        depth: 3,
        ancestor_ids: ["3035", "3499", "3663"],
        breadcrumbs: [
          "Ментальная арифметика New",
          "Ментальная арифметика для 6–8 лет",
          "М.1. Простой счет",
          "МЕ.0.2. 3. Цифра 6",
        ],
        context_text:
          "Ментальная арифметика New → Ментальная арифметика для 6–8 лет → М.1. Простой счет → МЕ.0.2. 3. Цифра 6",
      },
    });
    expect(result.report.max_depth).toBe(3);
    expect(result.report.root_documents).toBe(1);
  });

  it("keeps a folder without a corresponding MD file as a virtual tree node", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "content");
    const virtual = join(input, "ментальная-арифметика-для-6-8-лет--3499");
    const output = join(root, "normalized", "normalized.jsonl");
    await mkdir(virtual, { recursive: true });
    await writeFile(join(virtual, "урок--3501.md"), "# Урок\n\nТекст", "utf8");

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const tree = await readJson(result.treeFile);
    const virtualNode = tree.nodes.find((node: any) => node.node_id === "page_3499");
    const child = (await jsonl(output))[0];

    expect(virtualNode).toMatchObject({
      parent_node_id: null,
      title: "Ментальная арифметика для 6–8 лет",
      has_document: false,
      source: null,
    });
    expect(child).toMatchObject({
      parent_document_id: "page_3499",
      metadata: { parent_page_id: "3499", root_page_id: "3499", depth: 1 },
    });
    expect(result.report.virtual_nodes).toBe(1);
  });

  it("prefers YAML titles and falls back to a cleaned node name for breadcrumbs", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "content");
    const parent = join(input, "родитель--100");
    const output = join(root, "normalized", "normalized.jsonl");
    await mkdir(parent, { recursive: true });
    await writeFile(
      join(input, "родитель--100.md"),
      "---\ntitle: Название из YAML\n---\nТекст",
      "utf8",
    );
    await writeFile(join(parent, "дочерний-документ--101.md"), "Текст", "utf8");

    await normalizeDirectory({ inputDir: input, outputFile: output });
    const child = (await jsonl(output)).find((document) => document.document_id === "page_101");

    expect(child?.metadata.title).toBeNull();
    expect(child?.metadata.breadcrumbs).toEqual([
      "Название из YAML",
      "Дочерний документ",
    ]);
  });

  it("classifies a URL-only document and decodes HTML entities in URLs", async () => {
    const file = await temporaryMarkdown("https://example.com/resource?a=1&amp;b=2");
    const document = await normalizeMarkdownFile(file, "ссылка--42.md");

    expect(document.content).toBe("https://example.com/resource?a=1&b=2");
    expect(document.metadata).toMatchObject({
      document_type: "external_link",
      indexable: false,
      external_urls: ["https://example.com/resource?a=1&b=2"],
      external_url_count: 1,
    });
  });

  it("extracts Markdown images without removing them from content", async () => {
    const markdown = "# Материал\n\n![Схема](../../assets/example.png)";
    const file = await temporaryMarkdown(markdown);
    const document = await normalizeMarkdownFile(file, "материал--43.md");

    expect(document.content).toBe(markdown);
    expect(document.metadata.assets).toEqual([
      { type: "image", path: "../../assets/example.png", alt: "Схема" },
    ]);
    expect(document.metadata.asset_count).toBe(1);
  });

  it("counts headings outside fenced code blocks only", async () => {
    const file = await temporaryMarkdown(
      "# Первый\n\n```md\n# Не заголовок   \n```\n\n## Второй",
    );
    const document = await normalizeMarkdownFile(file, "headings.md");

    expect(document.metadata.heading_count).toBe(2);
    expect(document.content).toContain("# Не заголовок   ");
  });

  it("replaces forbidden control characters and reports them", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "content");
    const output = join(root, "normalized", "normalized.jsonl");
    await mkdir(input, { recursive: true });
    await writeFile(join(input, "таблица--42.md"), "# Таблица\n\n| A\u001FB |", "utf8");

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const [normalized] = await jsonl(output);

    expect(normalized?.content).toContain("A B");
    expect(normalized?.content).not.toContain("\u001F");
    expect(result.report.control_character_replacements).toBe(1);
    expect(result.report.control_character_warnings).toEqual([
      {
        source: "таблица--42.md",
        document_id: "page_42",
        character: "U+001F",
        count: 1,
      },
    ]);
  });

  it("retains identical documents in different branches and marks duplicate_of", async () => {
    const root = await temporaryDirectory();
    const input = join(root, "content");
    const left = join(input, "левая--10");
    const right = join(input, "правая--20");
    const output = join(root, "normalized", "normalized.jsonl");
    await mkdir(left, { recursive: true });
    await mkdir(right, { recursive: true });
    await writeFile(join(left, "одинаковый--11.md"), "# Same\n\nBody", "utf8");
    await writeFile(join(right, "одинаковый--21.md"), "# Same\n\nBody", "utf8");

    const result = await normalizeDirectory({ inputDir: input, outputFile: output });
    const documents = await jsonl(output);

    expect(documents).toHaveLength(2);
    expect(documents[0]?.content_hash).toBe(documents[1]?.content_hash);
    expect(documents[1]?.metadata.duplicate_of).toBe(documents[0]?.document_id);
    expect(documents[0]?.metadata.breadcrumbs).not.toEqual(
      documents[1]?.metadata.breadcrumbs,
    );
    expect(result.report.exact_duplicates).toBe(1);
  });

  it("produces identical IDs, hashes, JSONL, and trees under different absolute roots", async () => {
    const first = await deterministicFixture();
    const second = await deterministicFixture();
    const firstOutput = join(first.root, "normalized", "normalized.jsonl");
    const secondOutput = join(second.root, "normalized", "normalized.jsonl");
    const firstResult = await normalizeDirectory({ inputDir: first.input, outputFile: firstOutput });
    const secondResult = await normalizeDirectory({ inputDir: second.input, outputFile: secondOutput });

    expect(await readFile(firstOutput, "utf8")).toBe(await readFile(secondOutput, "utf8"));
    expect(await readFile(firstResult.treeFile, "utf8")).toBe(
      await readFile(secondResult.treeFile, "utf8"),
    );
    const generated = `${await readFile(firstOutput, "utf8")}${await readFile(firstResult.treeFile, "utf8")}`;
    expect(generated).not.toContain(first.root);
    expect(generated).not.toContain(second.root);
  });

  it("ensures every parent exists and generated trees contain no cycles or self-links", async () => {
    const fixture = await nestedTreeFixture();
    const output = join(fixture.root, "normalized", "normalized.jsonl");
    const result = await normalizeDirectory({ inputDir: fixture.input, outputFile: output });
    const tree = await readJson(result.treeFile);
    const nodes = new Map(tree.nodes.map((node: any) => [node.node_id, node]));

    for (const node of tree.nodes) {
      expect(node.parent_node_id).not.toBe(node.node_id);
      if (node.parent_node_id) expect(nodes.has(node.parent_node_id)).toBe(true);
      const visited = new Set<string>();
      let current: any = node;
      while (current?.parent_node_id) {
        expect(visited.has(current.node_id)).toBe(false);
        visited.add(current.node_id);
        current = nodes.get(current.parent_node_id);
      }
    }
    expect(result.report.tree_errors).toBe(0);
  });
});

async function temporaryDirectory(): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "athena-normalizer-"));
  temporaryDirectories.push(directory);
  return directory;
}

async function temporaryMarkdown(content: string): Promise<string> {
  const directory = await temporaryDirectory();
  const file = join(directory, "document.md");
  await writeFile(file, content, "utf8");
  return file;
}

async function jsonl(path: string): Promise<Array<Record<string, any>>> {
  const content = await readFile(path, "utf8");
  return content
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line) as Record<string, any>);
}

async function readJson(path: string): Promise<any> {
  return JSON.parse(await readFile(path, "utf8"));
}

async function nestedTreeFixture(): Promise<{ root: string; input: string }> {
  const root = await temporaryDirectory();
  const input = join(root, "content");
  const rootDirectory = join(input, "ментальная-арифметика-new--3035");
  const ageDirectory = join(rootDirectory, "ментальная-арифметика-для-6-8-лет--3499");
  const sectionDirectory = join(ageDirectory, "m.1.-простой-счет--3663");
  await mkdir(sectionDirectory, { recursive: true });
  await writeFile(
    join(input, "ментальная-арифметика-new--3035.md"),
    "---\ntitle: Ментальная арифметика New\n---\nКорень",
    "utf8",
  );
  await writeFile(
    join(rootDirectory, "ментальная-арифметика-для-6-8-лет--3499.md"),
    "---\ntitle: Ментальная арифметика для 6–8 лет\nparent_id: 3035\n---\nВозраст",
    "utf8",
  );
  await writeFile(
    join(ageDirectory, "m.1.-простой-счет--3663.md"),
    "---\ntitle: М.1. Простой счет\nparent_id: 3499\n---\nРаздел",
    "utf8",
  );
  await writeFile(
    join(sectionDirectory, "ме.0.2.-3.цифра-6--3693.md"),
    "---\ntitle: МЕ.0.2. 3. Цифра 6\nparent_id: 3663\n---\n# Сценарий урока\n\nМатериал",
    "utf8",
  );
  return { root, input };
}

async function deterministicFixture(): Promise<{ root: string; input: string }> {
  const root = await temporaryDirectory();
  const input = join(root, "content");
  const parent = join(input, "root--1");
  await mkdir(parent, { recursive: true });
  await writeFile(join(input, "root--1.md"), "# Root\n\nText", "utf8");
  await writeFile(join(parent, "child--2.md"), "# Child\n\nText", "utf8");
  return { root, input };
}

function hash(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}
