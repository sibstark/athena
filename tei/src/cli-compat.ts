#!/usr/bin/env bun
import { resolve } from "node:path";
import { Command } from "commander";
import { runCompatibilityTest } from "./compatibility.js";
import { runtimeConfig } from "./config.js";
import { readQueries, selectChunkSample } from "./io.js";
import { TeiE5EmbeddingProvider } from "./providers/tei-e5-provider.js";
import { TransformersJsE5ReferenceProvider } from "./providers/transformers-js-reference-provider.js";
import { ReadOnlyQdrantStore } from "./qdrant/read-only-qdrant-store.js";
import { writeCompatibilityReports } from "./report.js";

const program = new Command()
  .name("rag:compat:tei")
  .requiredOption("--chunks <path>", "chunks.jsonl")
  .requiredOption("--queries <path>", "queries.jsonl")
  .option("--collection <name>", "Qdrant collection")
  .option("--qdrant-url <url>", "Qdrant URL")
  .option("--tei-url <url>", "TEI URL")
  .option("--sample-size <number>", "document vector sample", positiveInt, 100)
  .option("--seed <number>", "deterministic sample seed", integer, 42)
  .option("--top-k <number>", "retrieval result count", positiveInt, 10)
  .option("--batch-size <number>", "TEI document batch size", positiveInt, 8)
  .option("--report <path>", "JSON report", "reports/tei-compatibility.json")
  .option("--dry-run", "check services and configuration without embeddings", false)
  .option("--offline", "use only local Transformers.js model cache", false)
  .parse();

const options = program.opts<{
  chunks: string; queries: string; collection?: string; qdrantUrl?: string; teiUrl?: string;
  sampleSize: number; seed: number; topK: number; batchSize: number; report: string; dryRun: boolean; offline: boolean;
}>();
const config = runtimeConfig({
  ...(options.collection ? { collection: options.collection } : {}),
  ...(options.qdrantUrl ? { qdrantUrl: options.qdrantUrl } : {}),
  ...(options.teiUrl ? { teiUrl: options.teiUrl } : {}),
});

const sample = await selectChunkSample(resolve(options.chunks), options.sampleSize, options.seed);
const queryInput = await readQueries(resolve(options.queries));
const tei = new TeiE5EmbeddingProvider({ baseUrl: config.teiUrl, timeoutMs: config.timeoutMs });
const reference = new TransformersJsE5ReferenceProvider({ cacheDir: config.cacheDir, offline: options.offline });
const qdrant = new ReadOnlyQdrantStore({
  url: config.qdrantUrl,
  collection: config.collection,
  ...(config.qdrantApiKey ? { apiKey: config.qdrantApiKey } : {}),
});

const report = await runCompatibilityTest({
  chunks: sample.chunks,
  queries: queryInput.queries,
  qdrant,
  tei,
  reference,
  topK: options.topK,
  batchSize: options.batchSize,
  dryRun: options.dryRun,
  modelArtifact: config.modelArtifact,
  inputErrors: [...sample.errors, ...queryInput.errors],
});
const paths = await writeCompatibilityReports(resolve(options.report), report);
console.log(`Verdict: ${report.verdict}`);
console.log(`Model: ${String(report.tei.model_id)}@${String(report.tei.model_revision)}`);
console.log(`Sample: ${sample.chunks.length}/${sample.total}`);
console.log(`Collection unchanged: ${report.collection_unchanged}`);
console.log(`Reports: ${paths.json}, ${paths.markdown}`);
if (report.errors.length) console.error(`Errors: ${report.errors.length}`);
process.exitCode = report.verdict === "WARN" ? 2 : report.verdict === "FAIL" ? 1 : 0;

function positiveInt(value: string): number { const parsed = integer(value); if (parsed <= 0) throw new Error(`Expected positive integer: ${value}`); return parsed; }
function integer(value: string): number { const parsed = Number(value); if (!Number.isInteger(parsed)) throw new Error(`Expected integer: ${value}`); return parsed; }
