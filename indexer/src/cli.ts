#!/usr/bin/env node
import "dotenv/config";
import { Command } from "commander";
import { resolveIndexerConfig, type LogLevel } from "./config.js";
import { indexChunks } from "./indexer.js";

const program = new Command()
  .name("indexer")
  .description("Create local E5 embeddings and incrementally index chunks in Qdrant")
  .requiredOption("-i, --input <path>", "chunks JSONL file")
  .option("-c, --collection <name>", "Qdrant collection")
  .option("--qdrant-url <url>", "Qdrant REST URL")
  .option("--batch-size <number>", "embedding and Qdrant batch size", parsePositiveInteger)
  .option("-r, --report <path>", "JSON report path")
  .option("--dry-run", "validate and calculate changes without writes or model loading", false)
  .option("--force-reembed", "recalculate embeddings for all valid chunks", false)
  .option("--prune", "delete managed points absent from the input", false)
  .option("--offline", "use only the local Transformers.js cache", false)
  .option("--continue-on-error", "continue after model or Qdrant batch failures", false)
  .option("--log-level <level>", "error, warn, info, or debug", parseLogLevel)
  .showHelpAfterError();

program.action(async (options: {
  input: string;
  report?: string;
  collection?: string;
  qdrantUrl?: string;
  batchSize?: number;
  dryRun: boolean;
  forceReembed: boolean;
  prune: boolean;
  offline: boolean;
  continueOnError: boolean;
  logLevel?: LogLevel;
}) => {
  const config = resolveIndexerConfig(options);
  const report = await indexChunks(config);
  console.log(`Report: ${config.reportFile}`);
  if (report.failed_chunks > 0 || report.errors.length > 0) process.exitCode = 1;
});

program.parseAsync(process.argv).catch((error: unknown) => {
  console.error(`Error: ${error instanceof Error ? error.message : String(error)}`);
  process.exitCode = 1;
});

function parseLogLevel(value: string): LogLevel {
  if (["error", "warn", "info", "debug"].includes(value)) return value as LogLevel;
  throw new Error(`Unknown log level: ${value}.`);
}

function parsePositiveInteger(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`Expected a positive integer, received: ${value}`);
  }
  return parsed;
}
