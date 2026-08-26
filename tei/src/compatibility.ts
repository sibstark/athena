import type {
  ChunkRecord,
  CollectionSnapshot,
  EvalQuery,
  SearchHit,
  TeiInfo,
} from "./contracts.js";
import {
  distribution,
  compareVectors,
  jaccard,
  overlap,
  recallAt,
  spearmanForCommon,
} from "./metrics.js";
import { MODEL_CONTRACT } from "./model-contract.js";
import type { EmbeddingProvider } from "./providers/embedding-provider.js";
import type { TeiEmbeddingProvider } from "./providers/tei-provider.js";
import type { ReadOnlyQdrantStore } from "./qdrant/read-only-qdrant-store.js";
import { stableJson } from "./shared/stable-json.js";

export type Verdict = "PASS_STRICT" | "PASS" | "WARN" | "FAIL";

export interface CompatibilityThresholds {
  pairedCosineMean: number;
  pairedCosineP05: number;
  pairedCosineMinimum: number;
  top1Agreement: number;
  top5Overlap: number;
  top10Overlap: number;
  maxRecall5Drop: number;
}

export const DEFAULT_THRESHOLDS: CompatibilityThresholds = {
  pairedCosineMean: 0.995,
  pairedCosineP05: 0.99,
  pairedCosineMinimum: 0.98,
  top1Agreement: 0.9,
  top5Overlap: 0.8,
  top10Overlap: 0.85,
  maxRecall5Drop: 0.05,
};

export interface CompatibilityReport {
  verdict: Verdict;
  generated_at: string;
  read_only: true;
  tei: Record<string, unknown>;
  qdrant: Record<string, unknown>;
  pipeline_comparison: Record<string, unknown>;
  document_vector_comparison: Record<string, unknown>;
  query_vector_comparison: Record<string, unknown>;
  retrieval_comparison: Record<string, unknown>;
  thresholds: CompatibilityThresholds;
  warnings: string[];
  errors: string[];
  recommendation: string;
  collection_unchanged: boolean;
}

export async function runCompatibilityTest(options: {
  chunks: ChunkRecord[];
  queries: EvalQuery[];
  qdrant: ReadOnlyQdrantStore;
  tei: TeiEmbeddingProvider;
  reference: EmbeddingProvider;
  thresholds?: CompatibilityThresholds;
  topK: number;
  batchSize?: number;
  dryRun?: boolean;
  inputErrors?: string[];
}): Promise<CompatibilityReport> {
  const thresholds = options.thresholds ?? DEFAULT_THRESHOLDS;
  const warnings: string[] = [];
  const errors = [...(options.inputErrors ?? [])];
  const before = await options.qdrant.snapshot();
  let info: TeiInfo;
  try {
    info = await options.tei.getInfo();
  } catch (error) {
    errors.push(message(error));
    return finish(
      "FAIL",
      {},
      before,
      before,
      {},
      {},
      {},
      thresholds,
      warnings,
      errors,
      options,
    );
  }
  const pipelineComparison = comparePipeline(before, info);
  if (pipelineComparison.compatible !== true)
    errors.push(...(pipelineComparison.critical_mismatches as string[]));
  warnings.push(
    ...(pipelineComparison.expected_runtime_differences as string[]),
  );

  if (options.dryRun) {
    const after = await options.qdrant.snapshot();
    warnings.push(
      "Dry run: vector and retrieval comparisons were not executed.",
    );
    return finish(
      errors.length ? "FAIL" : "WARN",
      info,
      before,
      after,
      pipelineComparison,
      {},
      {},
      thresholds,
      warnings,
      errors,
      options,
    );
  }

  const documentResult = await compareDocuments(
    options.chunks,
    options.qdrant,
    options.tei,
    options.batchSize ?? 16,
  );
  errors.push(...documentResult.errors);
  const queryResult = await compareQueries(
    options.queries,
    options.qdrant,
    options.reference,
    options.tei,
    options.topK,
  );
  errors.push(...queryResult.errors);
  const after = await options.qdrant.snapshot();
  const unchanged = stableJson(before) === stableJson(after);
  if (!unchanged)
    errors.push(
      "Qdrant collection changed during the read-only compatibility test.",
    );

  const numericalPass = passesNumerical(documentResult.summary, thresholds);
  const retrievalPass = passesRetrieval(queryResult.retrieval, thresholds);
  let verdict: Verdict;
  if (errors.length > 0 || !numericalPass || !retrievalPass || !unchanged)
    verdict = "FAIL";
  else if (
    info.model_sha === MODEL_CONTRACT.revision &&
    pipelineComparison.exact_match === true
  )
    verdict = "PASS_STRICT";
  else if (queryResult.completed) verdict = "PASS";
  else verdict = "WARN";

  return finish(
    verdict,
    info,
    before,
    after,
    pipelineComparison,
    documentResult.summary,
    queryResult.queryVectors,
    thresholds,
    warnings,
    errors,
    options,
    queryResult.retrieval,
  );
}

async function compareDocuments(
  chunks: ChunkRecord[],
  qdrant: ReadOnlyQdrantStore,
  tei: TeiEmbeddingProvider,
  batchSize: number,
) {
  const points = await qdrant.getPointsByChunkIds(
    chunks.map((chunk) => chunk.chunk_id),
  );
  const metrics: Array<
    ReturnType<typeof compareVectors> & {
      chunk_id: string;
      document_id: string;
    }
  > = [];
  const errors: string[] = [];
  for (let offset = 0; offset < chunks.length; offset += batchSize) {
    const batch = chunks
      .slice(offset, offset + batchSize)
      .filter((chunk) => points.has(chunk.chunk_id));
    try {
      const vectors = await tei.embedDocuments(
        batch.map((chunk) => chunk.embedding_text),
      );
      for (let index = 0; index < batch.length; index += 1) {
        const chunk = batch[index]!;
        const stored = points.get(chunk.chunk_id)!;
        metrics.push({
          chunk_id: chunk.chunk_id,
          document_id: chunk.document_id,
          ...compareVectors(stored.vector, vectors[index]!),
        });
      }
    } catch (error) {
      for (const chunk of batch) {
        try {
          const [vector] = await tei.embedDocuments([chunk.embedding_text]);
          metrics.push({
            chunk_id: chunk.chunk_id,
            document_id: chunk.document_id,
            ...compareVectors(points.get(chunk.chunk_id)!.vector, vector!),
          });
        } catch (itemError) {
          errors.push(`${chunk.chunk_id}: ${message(itemError)}`);
        }
      }
    }
  }
  for (const chunk of chunks)
    if (!points.has(chunk.chunk_id))
      errors.push(`${chunk.chunk_id}: point not found in Qdrant`);
  const cosine = distribution(metrics.map((item) => item.cosine));
  const differences = metrics.map((item) => item.mean_absolute_difference);
  const worst = [...metrics]
    .sort((a, b) => a.cosine - b.cosine)
    .slice(0, 20)
    .map(
      ({
        chunk_id,
        document_id,
        cosine,
        max_absolute_difference,
        mean_absolute_difference,
      }) => ({
        chunk_id,
        document_id,
        cosine,
        max_absolute_difference,
        mean_absolute_difference,
      }),
    );
  return {
    errors,
    summary: {
      sample_size: chunks.length,
      compared: metrics.length,
      dimension_matches: metrics.filter(
        (item) => item.dimension === MODEL_CONTRACT.dimension,
      ).length,
      finite_vectors: metrics.length,
      cosine,
      absolute_difference: {
        mean: differences.length
          ? differences.reduce((a, b) => a + b, 0) / differences.length
          : 0,
        maximum: Math.max(
          0,
          ...metrics.map((item) => item.max_absolute_difference),
        ),
      },
      worst,
    },
  };
}

async function compareQueries(
  queries: EvalQuery[],
  qdrant: ReadOnlyQdrantStore,
  reference: EmbeddingProvider,
  tei: TeiEmbeddingProvider,
  topK: number,
) {
  const errors: string[] = [];
  const queryMetrics: ReturnType<typeof compareVectors>[] = [];
  const retrievalRows: Array<Record<string, unknown>> = [];
  let oldVectors: number[][];
  let newVectors: number[][];
  try {
    [oldVectors, newVectors] = await Promise.all([
      reference.embedQueries(queries.map((item) => item.query)),
      tei.embedQueries(queries.map((item) => item.query)),
    ]);
  } catch (error) {
    errors.push(`query embeddings: ${message(error)}`);
    return {
      errors,
      completed: false,
      queryVectors: { status: "not_run" },
      retrieval: { status: "not_run" },
    };
  }
  for (let index = 0; index < queries.length; index += 1) {
    const query = queries[index]!;
    const oldVector = oldVectors[index]!;
    const newVector = newVectors[index]!;
    queryMetrics.push(compareVectors(oldVector, newVector));
    const [oldHits, newHits] = await Promise.all([
      qdrant.search(oldVector, topK),
      qdrant.search(newVector, topK),
    ]);
    retrievalRows.push(retrievalRow(query, oldHits, newHits, topK));
  }
  const average = (field: string) =>
    mean(retrievalRows.map((row) => Number(row[field])));
  const oldRecall5 = nullableMean(
    retrievalRows.map((row) => numberOrNull(row.old_recall_5)),
  );
  const newRecall5 = nullableMean(
    retrievalRows.map((row) => numberOrNull(row.new_recall_5)),
  );
  return {
    errors,
    completed: true,
    queryVectors: {
      compared: queryMetrics.length,
      cosine: distribution(queryMetrics.map((item) => item.cosine)),
    },
    retrieval: {
      queries: queries.length,
      top1_agreement: average("top1_agreement"),
      average_top3_overlap: average("top3_overlap"),
      average_top5_overlap: average("top5_overlap"),
      average_top10_overlap: average("top10_overlap"),
      average_top_k_jaccard: average("top_k_jaccard"),
      old_recall_5: oldRecall5,
      new_recall_5: newRecall5,
      recall_5_drop:
        oldRecall5 === null || newRecall5 === null
          ? null
          : oldRecall5 - newRecall5,
      details: retrievalRows,
    },
  };
}

function retrievalRow(
  query: EvalQuery,
  oldHits: SearchHit[],
  newHits: SearchHit[],
  topK: number,
): Record<string, unknown> {
  const chunkIds = (hits: SearchHit[]) =>
    hits.map((hit) => String(hit.payload.chunk_id ?? hit.id));
  const documentIds = (hits: SearchHit[]) =>
    hits.map((hit) => String(hit.payload.document_id ?? ""));
  const oldChunks = chunkIds(oldHits);
  const newChunks = chunkIds(newHits);
  const expectedDocs = query.expected_document_ids ?? [];
  const expectedChunks = query.expected_chunk_ids ?? [];
  const oldExpected = expectedChunks.length ? oldChunks : documentIds(oldHits);
  const newExpected = expectedChunks.length ? newChunks : documentIds(newHits);
  const expected = expectedChunks.length ? expectedChunks : expectedDocs;
  return {
    query_id: query.id,
    top1_agreement: oldChunks[0] === newChunks[0] ? 1 : 0,
    top3_overlap: overlap(oldChunks, newChunks, Math.min(3, topK)),
    top5_overlap: overlap(oldChunks, newChunks, Math.min(5, topK)),
    top10_overlap: overlap(oldChunks, newChunks, Math.min(10, topK)),
    top_k_jaccard: jaccard(oldChunks, newChunks, topK),
    rank_correlation: spearmanForCommon(oldChunks, newChunks),
    old_recall_1: recallAt(oldExpected, expected, 1),
    old_recall_5: recallAt(oldExpected, expected, 5),
    old_recall_10: recallAt(oldExpected, expected, 10),
    new_recall_1: recallAt(newExpected, expected, 1),
    new_recall_5: recallAt(newExpected, expected, 5),
    new_recall_10: recallAt(newExpected, expected, 10),
    old_results: oldHits.map(compactHit),
    new_results: newHits.map(compactHit),
  };
}

function compactHit(hit: SearchHit) {
  return {
    score: hit.score,
    chunk_id: hit.payload.chunk_id ?? hit.id,
    document_id: hit.payload.document_id ?? null,
  };
}

function comparePipeline(
  snapshot: CollectionSnapshot,
  info: TeiInfo,
): Record<string, unknown> {
  const existing = snapshot.metadata;
  const criticalChecks: Array<[string, unknown, unknown]> = [
    ["model", existing.embedding_model, info.model_id],
    ["revision", existing.embedding_model_revision, info.model_sha],
    ["dimension", snapshot.dimension, MODEL_CONTRACT.dimension],
    ["pooling", existing.embedding_pooling, MODEL_CONTRACT.pooling],
    ["normalize", existing.embedding_normalized, MODEL_CONTRACT.normalized],
    [
      "document_prefix",
      existing.document_prefix,
      MODEL_CONTRACT.documentPrefix,
    ],
    ["query_prefix", existing.query_prefix, MODEL_CONTRACT.queryPrefix],
    [
      "max_input_tokens",
      existing.embedding_max_input_tokens,
      MODEL_CONTRACT.maxInputTokens,
    ],
    [
      "auto_truncate",
      existing.embedding_auto_truncate,
      MODEL_CONTRACT.autoTruncate,
    ],
  ];
  const informativeChecks: Array<[string, unknown, unknown]> = [
    ["dtype", existing.embedding_dtype, info.model_dtype],
  ];
  const criticalMismatches = criticalChecks
    .filter(([, left, right]) => left !== right)
    .map(
      ([name, left, right]) =>
        `${name}: Qdrant=${String(left)}, TEI=${String(right)}`,
    );
  const differences = informativeChecks
    .filter(([, left, right]) => left !== right)
    .map(
      ([name, left, right]) =>
        `${name}: Qdrant=${String(left)}, TEI=${String(right)}`,
    );
  const checks = [...criticalChecks, ...informativeChecks];
  return {
    compatible: criticalMismatches.length === 0,
    exact_match: criticalMismatches.length === 0 && differences.length === 0,
    existing_qdrant: Object.fromEntries(
      checks.map(([name, left]) => [name, left]),
    ),
    tei: Object.fromEntries(checks.map(([name, , right]) => [name, right])),
    critical_mismatches: criticalMismatches,
    expected_runtime_differences: differences,
  };
}

function passesNumerical(
  summary: Record<string, unknown>,
  thresholds: CompatibilityThresholds,
): boolean {
  const cosine = summary.cosine as Record<string, number> | undefined;
  return Boolean(
    cosine &&
    typeof cosine.mean === "number" &&
    typeof cosine.p05 === "number" &&
    typeof cosine.minimum === "number" &&
    cosine.mean >= thresholds.pairedCosineMean &&
    cosine.p05 >= thresholds.pairedCosineP05 &&
    cosine.minimum >= thresholds.pairedCosineMinimum,
  );
}

function passesRetrieval(
  summary: Record<string, unknown>,
  thresholds: CompatibilityThresholds,
): boolean {
  if (summary.status === "not_run") return false;
  const drop = numberOrNull(summary.recall_5_drop);
  return (
    Number(summary.top1_agreement) >= thresholds.top1Agreement &&
    Number(summary.average_top5_overlap) >= thresholds.top5Overlap &&
    Number(summary.average_top10_overlap) >= thresholds.top10Overlap &&
    (drop === null || drop <= thresholds.maxRecall5Drop)
  );
}

function finish(
  verdict: Verdict,
  info: TeiInfo,
  before: CollectionSnapshot,
  after: CollectionSnapshot,
  pipeline: Record<string, unknown>,
  documents: Record<string, unknown>,
  queries: Record<string, unknown>,
  thresholds: CompatibilityThresholds,
  warnings: string[],
  errors: string[],
  _options: object,
  retrieval: Record<string, unknown> = {},
): CompatibilityReport {
  const unchanged = stableJson(before) === stableJson(after);
  return {
    verdict,
    generated_at: new Date().toISOString(),
    read_only: true,
    tei: {
      version: info.version ?? null,
      architecture: process.arch,
      backend: info.backend ?? null,
      model_id: info.model_id ?? null,
      model_revision: info.model_sha ?? null,
      model_dtype_reported: info.model_dtype ?? null,
      auto_truncate: info.auto_truncate ?? null,
    },
    qdrant: {
      collection: before.name,
      points_count: before.pointsCount,
      dimension: before.dimension,
      distance: before.distance,
      status: before.status,
    },
    pipeline_comparison: pipeline,
    document_vector_comparison: documents,
    query_vector_comparison: queries,
    retrieval_comparison: retrieval,
    thresholds,
    warnings,
    errors,
    recommendation: recommendation(verdict),
    collection_unchanged: unchanged,
  };
}

function recommendation(verdict: Verdict): string {
  if (verdict === "PASS_STRICT" || verdict === "PASS")
    return `Keep the existing collection and use TEI with ${MODEL_CONTRACT.modelId} for query embeddings.`;
  if (verdict === "WARN")
    return "Keep the existing collection unchanged and review the reported differences manually.";
  return "Keep the existing collection unchanged. Fix exact TEI model compatibility or rebuild the collection.";
}

function mean(values: number[]): number {
  return values.length ? values.reduce((a, b) => a + b, 0) / values.length : 0;
}
function nullableMean(values: Array<number | null>): number | null {
  const present = values.filter((value): value is number => value !== null);
  return present.length ? mean(present) : null;
}
function numberOrNull(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}
function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
