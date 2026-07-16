#!/usr/bin/env node
import "dotenv/config";
import { resolve } from "node:path";
import { Command } from "commander";
import { chunkNormalizedDocuments } from "./chunker.js";

const program = new Command()
  .name("chunker")
  .description("Split normalized Markdown documents into deterministic RAG chunks")
  .requiredOption("-i, --input <file>", "normalized JSONL file")
  .requiredOption("-t, --tree <file>", "document tree JSON file")
  .requiredOption("-o, --output <file>", "target chunks JSONL file")
  .option("-r, --report <file>", "chunking report (defaults next to JSONL)")
  .option("--target-tokens <tokens>", "preferred embedding_text size", positiveInteger, 320)
  .option("--max-tokens <tokens>", "maximum embedding_text size", positiveInteger, 450)
  .option("--min-tokens <tokens>", "soft minimum embedding_text size", positiveInteger, 80)
  .option("--overlap-tokens <tokens>", "complete-block overlap", nonNegativeInteger, 50)
  .option("--tokenizer <model>", "Hugging Face tokenizer or unicode-heuristic-v1")
  .option("--tokenizer-revision <revision>", "pinned tokenizer revision")
  .option("--offline", "use only the local tokenizer cache", false)
  .option("--force", "replace existing output files", false)
  .showHelpAfterError();

program.action(async (options: {
  input: string; tree: string; output: string; report?: string;
  targetTokens: number; maxTokens: number; minTokens: number; overlapTokens: number;
  tokenizer?: string; tokenizerRevision?: string; offline: boolean; force: boolean;
}) => {
  const result = await chunkNormalizedDocuments({
    inputFile: resolve(options.input),
    treeFile: resolve(options.tree),
    outputFile: resolve(options.output),
    ...(options.report ? { reportFile: resolve(options.report) } : {}),
    targetTokens: options.targetTokens,
    maxTokens: options.maxTokens,
    minTokens: options.minTokens,
    overlapTokens: options.overlapTokens,
    ...(options.tokenizer ? { tokenizer: options.tokenizer } : {}),
    ...(options.tokenizerRevision ? { tokenizerRevision: options.tokenizerRevision } : {}),
    offline: options.offline,
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
