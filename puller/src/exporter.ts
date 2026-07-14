import { access, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { AssetStore, type AssetRecord } from "./assets.js";
import { BitrixClient } from "./bitrix/client.js";
import type {
  DocumentTreeNode,
  KnowledgeCollection,
  KnowledgeDocument,
} from "./bitrix/types.js";
import {
  assertPathWithin,
  mapLimit,
  sanitizeSegment,
  toPosixPath,
  yamlString,
} from "./utils.js";

export interface ExportOptions {
  collectionId: number;
  outputDir: string;
  force?: boolean;
  documentConcurrency?: number;
  assetConcurrency?: number;
  fetch?: typeof fetch;
  now?: () => Date;
  onProgress?: (message: string) => void;
}

export interface ExportSummary {
  collectionId: number;
  collectionName: string;
  outputDir: string;
  documents: number;
  assetsDownloaded: number;
  assetsFailed: number;
}

interface PlannedDocument {
  node: DocumentTreeNode;
  localPath: string;
  rawPath: string;
}

interface ManifestDocument {
  id: number;
  parentId: number | null;
  title: string;
  position: number;
  localPath: string;
  rawPath: string;
  children: ManifestDocument[];
}

interface ExportManifest {
  formatVersion: 1;
  exportedAt: string;
  source: {
    provider: "bitrix24-note";
    portal: string;
    workspaceUrl: string;
  };
  collection: KnowledgeCollection;
  documents: ManifestDocument[];
}

interface ExportReport {
  status: "complete";
  exportedAt: string;
  collectionId: number;
  documentCount: number;
  assets: {
    downloaded: number;
    failed: number;
    items: AssetRecord[];
  };
}

export async function exportCollection(
  client: BitrixClient,
  options: ExportOptions,
): Promise<ExportSummary> {
  const outputDir = options.outputDir;
  const parentDir = dirname(outputDir);
  const temporaryDir = join(
    parentDir,
    `.${basename(outputDir)}.tmp-${process.pid}-${Date.now()}`,
  );
  const log = options.onProgress ?? (() => undefined);
  const now = options.now ?? (() => new Date());

  if ((await pathExists(outputDir)) && !options.force) {
    throw new Error(`Output directory already exists: ${outputDir}. Use --force to replace it.`);
  }

  await mkdir(parentDir, { recursive: true });
  await rm(temporaryDir, { recursive: true, force: true });
  await mkdir(temporaryDir, { recursive: true });

  try {
    log("Checking collection access...");
    const collections = await client.listCollections();
    const collection = collections.find((item) => Number(item.id) === options.collectionId);
    if (!collection) {
      throw new Error(
        `Collection ${options.collectionId} is not available to this webhook user. ` +
          "Check the user permissions and the note scope.",
      );
    }

    log(`Loading document tree for ${collection.name}...`);
    const tree = await client.getDocumentTree(options.collectionId);
    if (tree.truncated) {
      throw new Error(
        `Bitrix truncated collection ${options.collectionId} at its internal tree limit; ` +
          "the export was stopped to avoid publishing an incomplete snapshot.",
      );
    }

    const { plans, manifestTree } = planDocuments(tree.items);
    const workspaceUrl = `${client.portalOrigin}/note/workspace/${options.collectionId}/`;
    const assetStore = new AssetStore({
      outputRoot: temporaryDir,
      portalOrigin: client.portalOrigin,
      ...(options.fetch ? { fetch: options.fetch } : {}),
      redact: (value) => client.redact(value),
      concurrency: options.assetConcurrency ?? 4,
    });

    log(`Downloading ${plans.length} documents...`);
    await mapLimit(plans, options.documentConcurrency ?? 4, async (plan, index) => {
      const document = await client.getDocument(plan.node.id);
      validateDocument(document, plan.node, options.collectionId);
      const markdownPath = join(temporaryDir, plan.localPath);
      const rawPath = join(temporaryDir, plan.rawPath);
      assertPathWithin(temporaryDir, markdownPath);
      assertPathWithin(temporaryDir, rawPath);
      await mkdir(dirname(markdownPath), { recursive: true });
      await mkdir(dirname(rawPath), { recursive: true });

      const rewritten = await assetStore.processMarkdown(document.markdown, markdownPath);
      await writeFile(
        markdownPath,
        renderMarkdown(document, workspaceUrl, rewritten),
        "utf8",
      );
      await writeJson(rawPath, document);
      log(`[${index + 1}/${plans.length}] ${document.title}`);
    });

    const exportedAt = now().toISOString();
    const manifest: ExportManifest = {
      formatVersion: 1,
      exportedAt,
      source: {
        provider: "bitrix24-note",
        portal: client.portalOrigin,
        workspaceUrl,
      },
      collection,
      documents: manifestTree,
    };
    await writeJson(join(temporaryDir, "manifest.json"), manifest);

    const assetRecords = assetStore.getRecords();
    const downloaded = assetRecords.filter((record) => record.status === "downloaded").length;
    const failed = assetRecords.filter((record) => record.status === "failed").length;
    const report: ExportReport = {
      status: "complete",
      exportedAt,
      collectionId: options.collectionId,
      documentCount: plans.length,
      assets: { downloaded, failed, items: assetRecords },
    };
    await writeJson(join(temporaryDir, "report.json"), report);

    await publishDirectory(temporaryDir, outputDir, Boolean(options.force));
    return {
      collectionId: options.collectionId,
      collectionName: collection.name,
      outputDir,
      documents: plans.length,
      assetsDownloaded: downloaded,
      assetsFailed: failed,
    };
  } catch (error) {
    await rm(temporaryDir, { recursive: true, force: true });
    throw error;
  }
}

function planDocuments(nodes: DocumentTreeNode[]): {
  plans: PlannedDocument[];
  manifestTree: ManifestDocument[];
} {
  const plans: PlannedDocument[] = [];

  function walk(items: DocumentTreeNode[], ancestors: string[]): ManifestDocument[] {
    return [...items]
      .sort((left, right) => left.position - right.position || left.id - right.id)
      .map((node) => {
        const segment = `${sanitizeSegment(node.title)}--${node.id}`;
        const localPath = toPosixPath(join("content", ...ancestors, `${segment}.md`));
        const rawPath = toPosixPath(join("raw", `${node.id}.json`));
        plans.push({ node, localPath, rawPath });
        return {
          id: node.id,
          parentId: node.parentId,
          title: node.title,
          position: node.position,
          localPath,
          rawPath,
          children: walk(node.children ?? [], [...ancestors, segment]),
        };
      });
  }

  return { plans, manifestTree: walk(nodes, []) };
}

function validateDocument(
  document: KnowledgeDocument,
  node: DocumentTreeNode,
  collectionId: number,
): void {
  if (document.id !== node.id) {
    throw new Error(`Bitrix returned document ${document.id} while ${node.id} was requested.`);
  }
  if (document.collectionId !== null && Number(document.collectionId) !== collectionId) {
    throw new Error(`Document ${document.id} belongs to collection ${document.collectionId}.`);
  }
  if (typeof document.markdown !== "string") {
    throw new Error(`Document ${document.id} has no Markdown content.`);
  }
}

function renderMarkdown(
  document: KnowledgeDocument,
  workspaceUrl: string,
  markdown: string,
): string {
  const frontMatter = [
    "---",
    `id: ${document.id}`,
    `collection_id: ${document.collectionId ?? "null"}`,
    `parent_id: ${document.parentId ?? "null"}`,
    `title: ${yamlString(document.title)}`,
    `position: ${document.position}`,
    `created_by: ${document.createdBy}`,
    `updated_by: ${document.updatedBy}`,
    `created_at: ${yamlString(document.createdAt)}`,
    `updated_at: ${yamlString(document.updatedAt)}`,
    `workspace_url: ${yamlString(workspaceUrl)}`,
    "---",
    "",
  ].join("\n");
  return `${frontMatter}${markdown.trimEnd()}\n`;
}

async function publishDirectory(
  temporaryDir: string,
  outputDir: string,
  force: boolean,
): Promise<void> {
  if (!(await pathExists(outputDir))) {
    await rename(temporaryDir, outputDir);
    return;
  }
  if (!force) throw new Error(`Output directory already exists: ${outputDir}`);

  const backupDir = `${outputDir}.backup-${process.pid}-${Date.now()}`;
  await rename(outputDir, backupDir);
  try {
    await rename(temporaryDir, outputDir);
    await rm(backupDir, { recursive: true, force: true });
  } catch (error) {
    if (!(await pathExists(outputDir)) && (await pathExists(backupDir))) {
      await rename(backupDir, outputDir);
    }
    throw error;
  }
}

async function writeJson(path: string, value: unknown): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function pathExists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

// Kept exported for black-box validation without exposing implementation details in the CLI.
export async function readManifest(outputDir: string): Promise<unknown> {
  return JSON.parse(await readFile(join(outputDir, "manifest.json"), "utf8")) as unknown;
}
