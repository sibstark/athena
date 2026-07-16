import { createReadStream } from "node:fs";
import { readFile } from "node:fs/promises";
import { createInterface } from "node:readline";
import type { ChunkRecord, EvalQuery } from "./contracts.js";
import { sha256 } from "./shared/hashing.js";

interface Candidate {
  score: string;
  chunk: ChunkRecord;
}

export async function selectChunkSample(inputFile: string, sampleSize: number, seed: number): Promise<{ chunks: ChunkRecord[]; errors: string[]; total: number }> {
  const buckets = new Map<string, Candidate[]>();
  const capacities: Record<string, number> = {
    short: 20, medium: 35, long: 30, table: 10, image: 10, empty_heading: 10,
    long_breadcrumb: 10, duplicate: 10, deep: 10, all: Math.max(sampleSize * 2, 200),
  };
  const errors: string[] = [];
  let total = 0;
  const stream = createReadStream(inputFile, { encoding: "utf8" });
  const lines = createInterface({ input: stream, crlfDelay: Infinity });
  let lineNumber = 0;
  for await (const line of lines) {
    lineNumber += 1;
    if (!line.trim()) continue;
    try {
      const chunk = validateChunk(JSON.parse(line) as unknown, lineNumber);
      total += 1;
      for (const bucket of classify(chunk)) {
        addCandidate(buckets, bucket, capacities[bucket]!, chunk, seed);
      }
    } catch (error) {
      errors.push(`line ${lineNumber}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  const quotas: Array<[string, number]> = [
    ["short", 15], ["medium", 25], ["long", 20], ["table", 5], ["image", 5],
    ["empty_heading", 5], ["long_breadcrumb", 5], ["duplicate", 5], ["deep", 5],
  ];
  const selected = new Map<string, ChunkRecord>();
  for (const [bucket, quota] of quotas) {
    for (const candidate of (buckets.get(bucket) ?? []).slice(0, quota)) selected.set(candidate.chunk.chunk_id, candidate.chunk);
  }
  for (const candidate of buckets.get("all") ?? []) {
    if (selected.size >= sampleSize) break;
    selected.set(candidate.chunk.chunk_id, candidate.chunk);
  }
  return { chunks: [...selected.values()].slice(0, sampleSize), errors, total };
}

export async function readQueries(path: string): Promise<{ queries: EvalQuery[]; errors: string[] }> {
  const text = await readFile(path, "utf8");
  const queries: EvalQuery[] = [];
  const errors: string[] = [];
  for (const [index, line] of text.split(/\r?\n/u).entries()) {
    if (!line.trim()) continue;
    try {
      const value = JSON.parse(line) as unknown;
      if (!isRecord(value) || typeof value.id !== "string" || typeof value.query !== "string" || !value.query.trim()) {
        throw new Error("expected non-empty id and query");
      }
      queries.push({
        id: value.id,
        query: value.query,
        ...(stringArray(value.expected_document_ids) ? { expected_document_ids: value.expected_document_ids as string[] } : {}),
        ...(stringArray(value.expected_chunk_ids) ? { expected_chunk_ids: value.expected_chunk_ids as string[] } : {}),
      });
    } catch (error) {
      errors.push(`line ${index + 1}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return { queries, errors };
}

function validateChunk(value: unknown, line: number): ChunkRecord {
  if (!isRecord(value)) throw new Error("chunk is not an object");
  for (const field of ["chunk_id", "document_id", "content", "embedding_text", "embedding_text_hash"] as const) {
    if (typeof value[field] !== "string" || !(value[field] as string).trim()) throw new Error(`missing ${field}`);
  }
  if (!isRecord(value.metadata)) throw new Error("missing metadata");
  return value as unknown as ChunkRecord;
}

function classify(chunk: ChunkRecord): string[] {
  const tokens = typeof chunk.metadata.token_count === "number" ? chunk.metadata.token_count : Math.ceil(chunk.embedding_text.length / 4);
  const buckets = [tokens < 120 ? "short" : tokens < 320 ? "medium" : "long", "all"];
  if (chunk.metadata.contains_table) buckets.push("table");
  if (Array.isArray(chunk.metadata.assets) && chunk.metadata.assets.length > 0) buckets.push("image");
  if (!Array.isArray(chunk.metadata.heading_path) || chunk.metadata.heading_path.length === 0) buckets.push("empty_heading");
  if (Array.isArray(chunk.metadata.breadcrumbs) && chunk.metadata.breadcrumbs.join(" → ").length > 180) buckets.push("long_breadcrumb");
  if (typeof chunk.metadata.duplicate_of_chunk_id === "string") buckets.push("duplicate");
  if (typeof chunk.metadata.depth === "number" && chunk.metadata.depth >= 3) buckets.push("deep");
  return buckets;
}

function addCandidate(buckets: Map<string, Candidate[]>, name: string, capacity: number, chunk: ChunkRecord, seed: number): void {
  const candidates = buckets.get(name) ?? [];
  candidates.push({ score: sha256(`${seed}:${name}:${chunk.chunk_id}`), chunk });
  candidates.sort((a, b) => a.score.localeCompare(b.score));
  if (candidates.length > capacity) candidates.length = capacity;
  buckets.set(name, candidates);
}

function stringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
