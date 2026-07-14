import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { rm } from "node:fs/promises";
import { BitrixClient } from "../src/bitrix/client.js";
import { exportCollection } from "../src/exporter.js";

const temporaryDirectories: string[] = [];

afterEach(async () => {
  await Promise.all(
    temporaryDirectories.splice(0).map((directory) =>
      rm(directory, { recursive: true, force: true }),
    ),
  );
});

describe("exportCollection", () => {
  it("exports a complete tree, raw JSON, Markdown, and assets", async () => {
    const root = await mkdtemp(join(tmpdir(), "athena-export-"));
    temporaryDirectories.push(root);
    const outputDir = join(root, "mental-arithmetic");
    const fetchMock = createApiFetch(false);
    const client = new BitrixClient("https://hwschool.bitrix24.ru/rest/1/secret/", {
      fetch: fetchMock,
      sleep: async () => undefined,
    });

    const summary = await exportCollection(client, {
      collectionId: 17,
      outputDir,
      fetch: fetchMock,
      now: () => new Date("2026-07-13T10:00:00.000Z"),
    });

    expect(summary).toMatchObject({
      collectionId: 17,
      documents: 2,
      assetsDownloaded: 2,
      assetsFailed: 0,
    });

    const manifest = JSON.parse(await readFile(join(outputDir, "manifest.json"), "utf8")) as {
      collection: { name: string };
      documents: Array<{ children: unknown[]; localPath: string }>;
    };
    expect(manifest.collection.name).toBe("[M] Ментальная арифметика");
    expect(manifest.documents[0]?.children).toHaveLength(1);

    const rootMarkdownPath = manifest.documents[0]?.localPath;
    expect(rootMarkdownPath).toBeTruthy();
    const markdown = await readFile(join(outputDir, String(rootMarkdownPath)), "utf8");
    expect(markdown).toContain("title: \"Общая информация\"");
    expect(markdown).toMatch(/\.\.\/assets\/[a-f0-9]{12}-picture\.png/);
    expect(markdown).toMatch(/\.\.\/assets\/[a-f0-9]{12}-guide\.pdf/);
    expect(markdown).toContain("https://scratch.mit.edu/");
    await expect(readFile(join(outputDir, "raw", "101.json"), "utf8")).resolves.toContain(
      "Общая информация",
    );
  });

  it("does not publish a truncated tree", async () => {
    const root = await mkdtemp(join(tmpdir(), "athena-export-"));
    temporaryDirectories.push(root);
    const outputDir = join(root, "mental-arithmetic");
    const fetchMock = createApiFetch(true);
    const client = new BitrixClient("https://hwschool.bitrix24.ru/rest/1/secret/", {
      fetch: fetchMock,
    });

    await expect(
      exportCollection(client, { collectionId: 17, outputDir, fetch: fetchMock }),
    ).rejects.toThrow("truncated");
    await expect(readFile(join(outputDir, "manifest.json"), "utf8")).rejects.toThrow();
  });
});

function createApiFetch(truncated: boolean): typeof fetch {
  return async (input, init) => {
    const url = new URL(typeof input === "string" ? input : input instanceof URL ? input : input.url);
    if (url.hostname === "cdn.example.com") {
      if (url.pathname.endsWith("picture.png")) {
        return new Response(new Uint8Array([137, 80, 78, 71]), {
          status: 200,
          headers: { "content-type": "image/png" },
        });
      }
      if (url.pathname.endsWith("guide.pdf")) {
        return new Response(new TextEncoder().encode("%PDF-test"), {
          status: 200,
          headers: { "content-type": "application/pdf" },
        });
      }
    }

    const method = url.pathname.split("/").at(-1)?.replace(/\.json$/, "");
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};
    if (method === "note.collection.list") {
      return jsonResponse({
        result: {
          items: [
            {
              id: 17,
              name: "[M] Ментальная арифметика",
              position: 1,
              policyLevel: "view",
              createdBy: 1,
              updatedBy: 2,
              createdAt: "2026-01-01T00:00:00Z",
              updatedAt: "2026-07-01T00:00:00Z",
            },
          ],
          nextCursor: null,
        },
      });
    }
    if (method === "note.document.tree.list") {
      return jsonResponse({
        result: {
          truncated,
          items: [
            {
              id: 101,
              collectionId: 17,
              parentId: null,
              title: "Общая информация",
              position: 1,
              children: [
                {
                  id: 102,
                  collectionId: 17,
                  parentId: 101,
                  title: "Помощь друга +1",
                  position: 1,
                  children: [],
                },
              ],
            },
          ],
        },
      });
    }
    if (method === "note.document.get") {
      const id = Number(body.id);
      return jsonResponse({
        result: {
          item: {
            id,
            collectionId: 17,
            parentId: id === 101 ? null : 101,
            title: id === 101 ? "Общая информация" : "Помощь друга +1",
            markdown:
              id === 101
                ? "# Intro\n\n![Picture](https://cdn.example.com/picture.png)\n\n[Guide](https://cdn.example.com/guide.pdf)\n\n[Scratch](https://scratch.mit.edu/)"
                : "## Lesson\n\nMethodical content.",
            position: 1,
            createdBy: 1,
            updatedBy: 2,
            createdAt: "2026-01-01T00:00:00Z",
            updatedAt: "2026-07-01T00:00:00Z",
          },
        },
      });
    }
    return jsonResponse({ error: "NOT_FOUND", error_description: `Unknown method ${method}` }, 404);
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}
