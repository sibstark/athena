#!/usr/bin/env node
import "dotenv/config";
import { Command } from "commander";
import { BitrixApiError, BitrixClient } from "./bitrix/client.js";
import { chunkNormalizedDocuments } from "./chunker.js";
import {
  parseNonNegativeInteger,
  parsePositiveInteger,
  requireWebhookUrl,
  resolveOutputDir,
} from "./config.js";
import { exportCollection } from "./exporter.js";
import { normalizeDirectory } from "./normalizer.js";

const program = new Command()
  .name("athena")
  .description("Export Bitrix24 Knowledge Base 2.0 collections to Markdown and JSON")
  .version("0.1.0");

const auth = program.command("auth").description("Check Bitrix24 authentication");
auth
  .command("check")
  .description("Validate the webhook and note permission")
  .action(async () => {
    const client = createClient();
    const collections = await client.listCollections();
    console.log(`Connected to ${client.portalOrigin}. Accessible collections: ${collections.length}.`);
  });

const collections = program.command("collections").description("Inspect accessible collections");
collections
  .command("list")
  .description("List all Knowledge Base 2.0 collections")
  .action(async () => {
    const client = createClient();
    const items = await client.listCollections();
    if (items.length === 0) {
      console.log("No accessible Knowledge Base 2.0 collections found.");
      return;
    }
    console.table(
      items.map((item) => ({
        id: item.id,
        name: item.name,
        access: item.policyLevel,
        updatedAt: item.updatedAt,
      })),
    );
  });

program
  .command("export")
  .description("Export one Knowledge Base 2.0 collection")
  .option("-c, --collection <id>", "collection ID", parsePositiveInteger, 17)
  .option("-o, --out <directory>", "output directory", "exports/mental-arithmetic")
  .option("--force", "replace an existing complete export", false)
  .action(
    async (options: { collection: number; out: string; force: boolean }) => {
      const client = createClient();
      const summary = await exportCollection(client, {
        collectionId: options.collection,
        outputDir: resolveOutputDir(options.out),
        force: options.force,
        onProgress: (message) => console.log(message),
      });

      console.log("");
      console.log(`Export complete: ${summary.collectionName} (${summary.collectionId})`);
      console.log(`Documents: ${summary.documents}`);
      console.log(
        `Assets: ${summary.assetsDownloaded} downloaded, ${summary.assetsFailed} failed`,
      );
      console.log(`Output: ${summary.outputDir}`);
      if (summary.assetsFailed > 0) {
        console.log("See report.json for resources that could not be downloaded.");
      }
    },
  );

program
  .command("normalize")
  .description("Normalize Markdown files into deterministic JSONL for RAG ingestion")
  .requiredOption("-i, --input <directory>", "directory containing Markdown files")
  .requiredOption("-o, --output <file>", "target JSONL file")
  .option("-t, --tree <file>", "document tree (defaults next to JSONL)")
  .option("-r, --report <file>", "normalization report (defaults next to JSONL)")
  .option(
    "--short-note-threshold <characters>",
    "useful-text character threshold for short notes",
    parsePositiveInteger,
    200,
  )
  .option("--force", "replace existing JSONL, tree, and report files", false)
  .action(
    async (options: {
      input: string;
      output: string;
      tree?: string;
      report?: string;
      shortNoteThreshold: number;
      force: boolean;
    }) => {
      const result = await normalizeDirectory({
        inputDir: resolveOutputDir(options.input),
        outputFile: resolveOutputDir(options.output),
        ...(options.tree ? { treeFile: resolveOutputDir(options.tree) } : {}),
        ...(options.report ? { reportFile: resolveOutputDir(options.report) } : {}),
        shortNoteThreshold: options.shortNoteThreshold,
        force: options.force,
        onProgress: (message) => console.log(message),
      });

      console.log("");
      console.log("Normalization complete");
      console.log(`Found Markdown files: ${result.report.found_files}`);
      console.log(
        `Tree nodes: ${result.report.tree_nodes} (${result.report.virtual_nodes} virtual)`,
      );
      console.log(`Documents written: ${result.report.processed_documents}`);
      console.log(`Root documents: ${result.report.root_documents}`);
      console.log(`Maximum depth: ${result.report.max_depth}`);
      console.log(`Empty files: ${result.report.empty_files}`);
      console.log(`Short notes: ${result.report.short_documents}`);
      console.log(`External-link documents: ${result.report.external_link_documents}`);
      console.log(`Images: ${result.report.images}`);
      console.log(`External links: ${result.report.external_links}`);
      console.log(`Duplicates: ${result.report.duplicate_files}`);
      console.log(`Files without page ID: ${result.report.files_without_page_id}`);
      console.log(`Files without title: ${result.report.files_without_title}`);
      console.log(`Tree errors: ${result.report.tree_errors}`);
      console.log(`Processing errors: ${result.report.processing_errors}`);
      console.log(
        `Control characters replaced: ${result.report.control_character_replacements}`,
      );
      console.log(`JSONL: ${result.outputFile}`);
      console.log(`Tree: ${result.treeFile}`);
      console.log(`Report: ${result.reportFile}`);
    },
  );

program
  .command("chunk")
  .description("Split normalized Markdown documents into deterministic RAG chunks")
  .requiredOption("-i, --input <file>", "normalized JSONL file")
  .requiredOption("-t, --tree <file>", "document tree JSON file")
  .requiredOption("-o, --output <file>", "target chunks JSONL file")
  .option("-r, --report <file>", "chunking report (defaults next to JSONL)")
  .option("--target-tokens <tokens>", "preferred embedding_text size", parsePositiveInteger, 320)
  .option("--max-tokens <tokens>", "maximum embedding_text size", parsePositiveInteger, 450)
  .option("--min-tokens <tokens>", "soft minimum embedding_text size", parsePositiveInteger, 80)
  .option(
    "--overlap-tokens <tokens>",
    "maximum complete-sentence/paragraph overlap",
    parseNonNegativeInteger,
    50,
  )
  .option("--tokenizer <model>", "Hugging Face tokenizer or unicode-heuristic-v1")
  .option("--tokenizer-revision <revision>", "pinned tokenizer revision")
  .option("--offline", "use only the local tokenizer cache", false)
  .option("--force", "replace existing chunks and report files", false)
  .action(
    async (options: {
      input: string;
      tree: string;
      output: string;
      report?: string;
      targetTokens: number;
      maxTokens: number;
      minTokens: number;
      overlapTokens: number;
      tokenizer?: string;
      tokenizerRevision?: string;
      offline: boolean;
      force: boolean;
    }) => {
      const result = await chunkNormalizedDocuments({
        inputFile: resolveOutputDir(options.input),
        treeFile: resolveOutputDir(options.tree),
        outputFile: resolveOutputDir(options.output),
        ...(options.report ? { reportFile: resolveOutputDir(options.report) } : {}),
        targetTokens: options.targetTokens,
        maxTokens: options.maxTokens,
        minTokens: options.minTokens,
        overlapTokens: options.overlapTokens,
        ...(options.tokenizer ? { tokenizer: options.tokenizer } : {}),
        ...(options.tokenizerRevision
          ? { tokenizerRevision: options.tokenizerRevision }
          : {}),
        offline: options.offline,
        force: options.force,
        onProgress: (message) => console.log(message),
      });

      console.log("");
      console.log("Chunking complete");
      console.log(`Input documents: ${result.report.input_documents}`);
      console.log(`Documents processed: ${result.report.processed_documents}`);
      console.log(`Documents skipped: ${result.report.skipped_documents}`);
      console.log(`Chunks created: ${result.report.chunks_created}`);
      console.log(`Tokenizer: ${result.report.tokenizer}`);
      console.log(`Average tokens: ${result.report.average_tokens}`);
      console.log(`Token range: ${result.report.minimum_tokens}-${result.report.maximum_tokens}`);
      console.log(`Chunks below minimum: ${result.report.chunks_below_min_tokens}`);
      console.log(`Chunks above maximum: ${result.report.chunks_above_max_tokens}`);
      console.log(`Chunks with tables: ${result.report.chunks_with_tables}`);
      console.log(`Chunks with code: ${result.report.chunks_with_code}`);
      console.log(`Chunks with images: ${result.report.chunks_with_images}`);
      console.log(`Duplicates: ${result.report.duplicate_chunks}`);
      console.log(
        `Control characters replaced: ${result.report.control_character_replacements}`,
      );
      console.log(`Errors: ${result.report.errors}`);
      console.log(`JSONL: ${result.outputFile}`);
      console.log(`Report: ${result.reportFile}`);
    },
  );

program.showHelpAfterError();

program.parseAsync(process.argv).catch((error: unknown) => {
  const message = formatError(error);
  console.error(`Error: ${message}`);
  process.exitCode = 1;
});

function createClient(): BitrixClient {
  return new BitrixClient(requireWebhookUrl());
}

function formatError(error: unknown): string {
  if (error instanceof BitrixApiError) {
    if (error.code === "insufficient_scope") {
      return `${error.message} Add the note permission to the incoming webhook.`;
    }
    if (error.code === "INVALID_CREDENTIALS" || error.code === "NO_AUTH_FOUND") {
      return `${error.message} Check BITRIX_WEBHOOK_URL in .env.`;
    }
  }
  return error instanceof Error ? error.message : String(error);
}
