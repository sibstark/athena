#!/usr/bin/env bun
import { Command } from "commander";
import { runtimeConfig } from "./config.js";
import { MODEL_CONTRACT } from "./model-contract.js";
import { TeiEmbeddingProvider } from "./providers/tei-provider.js";
import { ReadOnlyQdrantStore } from "./qdrant/read-only-qdrant-store.js";

const program = new Command()
  .name("rag:search:tei")
  .requiredOption("--query <text>", "query text")
  .option("--collection <name>", "Qdrant collection")
  .option("--qdrant-url <url>", "Qdrant URL")
  .option("--tei-url <url>", "TEI URL")
  .option("--limit <number>", "result count", positiveInt, 10)
  .parse();

const options = program.opts<{ query: string; collection?: string; qdrantUrl?: string; teiUrl?: string; limit: number }>();
const config = runtimeConfig({
  ...(options.collection ? { collection: options.collection } : {}),
  ...(options.qdrantUrl ? { qdrantUrl: options.qdrantUrl } : {}),
  ...(options.teiUrl ? { teiUrl: options.teiUrl } : {}),
});
const tei = new TeiEmbeddingProvider({ baseUrl: config.teiUrl, timeoutMs: config.timeoutMs });
const qdrant = new ReadOnlyQdrantStore({
  url: config.qdrantUrl,
  collection: config.collection,
  ...(config.qdrantApiKey ? { apiKey: config.qdrantApiKey } : {}),
});

const snapshot = await qdrant.snapshot();
if (snapshot.dimension !== MODEL_CONTRACT.dimension || snapshot.metadata.embedding_model !== MODEL_CONTRACT.modelId || snapshot.metadata.embedding_model_revision !== MODEL_CONTRACT.revision) {
  throw new Error("Qdrant collection does not match the configured embedding contract.");
}
const [vector] = await tei.embedQueries([options.query]);
const hits = await qdrant.search(vector!, options.limit);
for (const hit of hits) {
  const payload = hit.payload;
  const preview = String(payload.content ?? "").replace(/\s+/gu, " ").slice(0, 240);
  console.log(`\n${hit.score.toFixed(6)}  ${String(payload.chunk_id ?? hit.id)}`);
  console.log(`document: ${String(payload.document_id ?? "")}`);
  console.log(`title: ${String(payload.title ?? "")}`);
  console.log(`breadcrumbs: ${Array.isArray(payload.breadcrumbs) ? payload.breadcrumbs.join(" → ") : ""}`);
  console.log(`section: ${String(payload.section_title ?? "")}`);
  console.log(`preview: ${preview}`);
}

function positiveInt(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`Invalid positive integer: ${value}`);
  return parsed;
}
