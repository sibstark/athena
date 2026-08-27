#!/usr/bin/env node
import { resolve } from "node:path";
import { Command } from "commander";
import { chunkNormalizedDocuments } from "./chunker.js";
import { chunkNormalizedRun } from "./run.js";

const program = new Command()
  .name("chunker")
  .description("Split normalized Markdown documents into deterministic RAG chunks")
  .option("--run <directory>", "normalized_<id> run directory")
  .option("--out <directory>", "chunks_<id> output directory")
  .option("-i, --input <file>", "normalized JSONL file")
  .option("-t, --tree <file>", "document tree JSON file")
  .option("-o, --output <file>", "target chunks JSONL file")
  .option("-r, --report <file>", "chunking report (defaults next to JSONL)")
  .option("--target-tokens <tokens>", "preferred embedding_text size", positiveInteger, 320)
  .option("--max-tokens <tokens>", "maximum embedding_text size", positiveInteger, 450)
  .option("--min-tokens <tokens>", "soft minimum embedding_text size", positiveInteger, 80)
  .option("--overlap-tokens <tokens>", "complete-block overlap", nonNegativeInteger, 50)
  .option("--force", "replace existing output files", false)
  .showHelpAfterError();

program.action(async (options: {
  run?: string; out?: string; input?: string; tree?: string; output?: string; report?: string;
  targetTokens: number; maxTokens: number; minTokens: number; overlapTokens: number;
  force: boolean;
}) => {
  if (options.run || options.out) {
    if (!options.run || !options.out) throw new Error("Run mode requires both --run and --out.");
    if (options.input || options.tree || options.output || options.report) {
      throw new Error("Run mode cannot be combined with --input, --tree, --output, or --report.");
    }
    if (options.force) {
      throw new Error("Run mode atomically replaces its output; omit --force.");
    }
    const result = await chunkNormalizedRun({
      runDir: resolve(options.run),
      outputDir: resolve(options.out),
      targetTokens: options.targetTokens,
      maxTokens: options.maxTokens,
      minTokens: options.minTokens,
      overlapTokens: options.overlapTokens,
      onProgress: console.log,
    });
    console.log(`Status: ${result.report.status}`);
    console.log(`Workspaces: ${result.report.successfulWorkspaces} successful (${result.report.warningWorkspaces} with warnings), ${result.report.failedWorkspaces} failed`);
    console.log(`Documents: ${result.report.processedDocuments}; chunks: ${result.report.chunksCreated}; errors: ${result.report.errors}`);
    console.log(`Output: ${result.outputDir}`);
    console.log(`Manifest: ${result.manifestFile}`);
    console.log(`Report: ${result.reportFile}`);
    if (result.report.status !== "complete" && result.report.status !== "complete_with_warnings") {
      throw new Error(`Chunk run finished with status ${result.report.status}; see ${result.reportFile}`);
    }
    return;
  }
  if (!options.input || !options.tree || !options.output) {
    throw new Error("Single-workspace mode requires --input, --tree, and --output.");
  }
  const result = await chunkNormalizedDocuments({
    inputFile: resolve(options.input),
    treeFile: resolve(options.tree),
    outputFile: resolve(options.output),
    ...(options.report ? { reportFile: resolve(options.report) } : {}),
    targetTokens: options.targetTokens,
    maxTokens: options.maxTokens,
    minTokens: options.minTokens,
    overlapTokens: options.overlapTokens,
    force: options.force,
    onProgress: console.log,
  });
  console.log(`Documents: ${result.report.processed_documents}; chunks: ${result.report.chunks_created}; errors: ${result.report.errors}`);
  console.log(`Tokenizer: ${result.report.tokenizer}`);
  console.log(`JSONL: ${result.outputFile}`);
  console.log(`Report: ${result.reportFile}`);
});

program.parseAsync(process.argv).catch(fail);

function positiveInteger(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`Expected a positive integer: ${value}`);
  return parsed;
}

function nonNegativeInteger(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 0) throw new Error(`Expected a non-negative integer: ${value}`);
  return parsed;
}

function fail(error: unknown): void {
  console.error(`Error: ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
}
