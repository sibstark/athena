#!/usr/bin/env node
import { resolve } from "node:path";
import { Command } from "commander";
import { normalizeDirectory } from "./normalizer.js";

const program = new Command()
  .name("normalizer")
  .description("Normalize exported Markdown into deterministic JSONL and a document tree")
  .requiredOption("-i, --input <directory>", "directory containing Markdown files")
  .requiredOption("-o, --output <file>", "target normalized JSONL file")
  .option("-t, --tree <file>", "document tree (defaults next to JSONL)")
  .option("-r, --report <file>", "normalization report (defaults next to JSONL)")
  .option("--short-note-threshold <characters>", "short-note threshold", positiveInteger, 200)
  .option("--force", "replace existing output files", false)
  .showHelpAfterError();

program.action(async (options: {
  input: string;
  output: string;
  tree?: string;
  report?: string;
  shortNoteThreshold: number;
  force: boolean;
}) => {
  const result = await normalizeDirectory({
    inputDir: resolve(options.input),
    outputFile: resolve(options.output),
    ...(options.tree ? { treeFile: resolve(options.tree) } : {}),
    ...(options.report ? { reportFile: resolve(options.report) } : {}),
    shortNoteThreshold: options.shortNoteThreshold,
    force: options.force,
    onProgress: console.log,
  });
  console.log(`Documents: ${result.report.processed_documents}; tree nodes: ${result.report.tree_nodes}; errors: ${result.report.errors}`);
  console.log(`JSONL: ${result.outputFile}`);
  console.log(`Tree: ${result.treeFile}`);
  console.log(`Report: ${result.reportFile}`);
});

program.parseAsync(process.argv).catch(fail);

function positiveInteger(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`Expected a positive integer: ${value}`);
  return parsed;
}

function fail(error: unknown): void {
  console.error(`Error: ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
}
