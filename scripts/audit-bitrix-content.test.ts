import { describe, expect, test } from "bun:test";

import { analyzeMarkdown, buildApiConfig } from "./audit-bitrix-content.mjs";

describe("Bitrix content audit", () => {
  test("marks missing sections from a same-document table of contents", () => {
    const markdown = [
      "1. [Первый](/note/document/351/#first)",
      "2. [Второй](https://portal.bitrix24.ru/note/document/351/#second)",
      "",
      "## Первый",
    ].join("\n");

    expect(analyzeMarkdown(markdown, { documentId: 351, isLeaf: true })).toMatchObject({
      suspicious: true,
      reasons: ["missing_toc_sections", "ends_with_heading"],
      missingTocSections: ["Второй"],
    });
  });

  test("marks a dangling list item", () => {
    const result = analyzeMarkdown("## Раздел\n\nТекст\n\n- ", {
      documentId: 353,
      isLeaf: true,
    });

    expect(result.suspicious).toBe(true);
    expect(result.reasons).toContain("ends_with_empty_list_item");
  });

  test("does not mark ordinary long Markdown", () => {
    const markdown = `## Раздел\n\n${"Полный текст документа. ".repeat(20)}`;
    expect(analyzeMarkdown(markdown, { documentId: 311, isLeaf: true })).toMatchObject({
      suspicious: false,
      reasons: [],
    });
  });

  test("builds a REST 3.0 URL without exposing it in output data", () => {
    const config = buildApiConfig("https://portal.bitrix24.ru/rest/42/secret/");
    expect(config.apiBase.toString()).toBe("https://portal.bitrix24.ru/rest/api/42/secret/");
    expect(config.portal).toBe("https://portal.bitrix24.ru");
  });
});
