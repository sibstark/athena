import { access, mkdtemp, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import {
  chunkNormalizedDocuments,
  type ChunkReport,
  type TokenCounter,
} from "./chunker.js";
import {
  createE5TokenCounter,
} from "./e5-token-counter.js";
import { loadEmbeddingConfig, type EmbeddingConfig } from "./embedding-config.js";

export const CHUNKS_MANIFEST_FORMAT_VERSION = 2;

export interface ChunkRunOptions {
  runDir: string;
  outputDir: string;
  targetTokens?: number;
  maxTokens?: number;
  minTokens?: number;
  overlapTokens?: number;
  tokenizerCacheDir?: string;
  tokenCounter?: TokenCounter;
  embeddingConfig?: EmbeddingConfig;
  now?: () => Date;
  onProgress?: (message: string) => void;
}

export interface ChunkWorkspaceManifest {
  id: number;
  name: string;
  path: string;
  qdrantCollection: string;
  sourceStatus: string;
  chunksPath?: string;
  reportPath: string;
  status: "complete" | "complete_with_warnings" | "failed";
  processedDocuments: number;
  chunksCreated: number;
  errors: number;
  error?: string;
}

export interface ChunksManifest {
  formatVersion: number;
  generatedAt: string;
  sourceRun: string;
  sourceNormalization: string;
  tokenizer: { model: string; revision: string; sha256: string; configSha256: string };
  limits: { targetTokens: number; maxTokens: number; minTokens: number; overlapTokens: number };
  workspaces: ChunkWorkspaceManifest[];
}

export interface ChunkRunReport {
  formatVersion: number;
  status: "complete" | "complete_with_warnings" | "complete_with_errors" | "failed";
  sourceRun: string;
  sourceNormalization: string;
  startedAt: string;
  finishedAt: string;
  durationMs: number;
  workspaceCount: number;
  successfulWorkspaces: number;
  warningWorkspaces: number;
  failedWorkspaces: number;
  processedDocuments: number;
  chunksCreated: number;
  errors: number;
  workspaces: ChunkWorkspaceManifest[];
}

export interface ChunkRunResult {
  outputDir: string;
  manifestFile: string;
  reportFile: string;
  manifest: ChunksManifest;
  report: ChunkRunReport;
}

interface NormalizationManifest {
  formatVersion: number;
  sourceRun: string;
  workspaces: NormalizationWorkspace[];
}

interface NormalizationWorkspace {
  id: number;
  name: string;
  path: string;
  qdrantCollection: string;
  normalizedPath?: string;
  treePath?: string;
  status: string;
  errors: number;
}

const DEFAULT_TARGET_TOKENS = 320;
const DEFAULT_MAX_TOKENS = 450;
const DEFAULT_MIN_TOKENS = 80;
const DEFAULT_OVERLAP_TOKENS = 50;

export async function chunkNormalizedRun(options: ChunkRunOptions): Promise<ChunkRunResult> {
  const started = (options.now ?? (() => new Date()))();
  const runDir = resolve(options.runDir);
  const outputDir = resolve(options.outputDir);
  const log = options.onProgress ?? (() => undefined);
  const embedding = options.embeddingConfig ?? loadEmbeddingConfig();
  validateSeparatedPaths(runDir, outputDir);

  const sourceNormalization = basename(runDir);
  const inputMatch = /^normalized_(.+)$/u.exec(sourceNormalization);
  if (!inputMatch?.[1]) {
    throw new Error(`Normalized input directory must be named normalized_<id>: ${runDir}`);
  }
  const suffix = inputMatch[1];
  if (basename(outputDir) !== `chunks_${suffix}`) {
    throw new Error(`Chunks output directory must be named chunks_${suffix}: ${outputDir}`);
  }

  const manifest = await readNormalizationManifest(runDir);
  if (manifest.sourceRun !== `run_${suffix}`) {
    throw new Error(`Normalization manifest belongs to ${manifest.sourceRun}, expected run_${suffix}.`);
  }
  validateWorkspaces(manifest.workspaces);

  const limits = {
    targetTokens: options.targetTokens ?? DEFAULT_TARGET_TOKENS,
    maxTokens: options.maxTokens ?? DEFAULT_MAX_TOKENS,
    minTokens: options.minTokens ?? DEFAULT_MIN_TOKENS,
    overlapTokens: options.overlapTokens ?? DEFAULT_OVERLAP_TOKENS,
  };
  validateLimits(limits);
  const tokenCounter = options.tokenCounter ?? await createE5TokenCounter({
    modelId: embedding.modelId,
    revision: embedding.modelRevision,
    cacheDir: options.tokenizerCacheDir ?? embedding.cacheDir,
    tokenizerSHA256: embedding.tokenizerSHA256,
    tokenizerConfigSHA256: embedding.tokenizerConfigSHA256,
  });

  await mkdir(dirname(outputDir), { recursive: true });
  const staging = await mkdtemp(join(dirname(outputDir), `.${basename(outputDir)}.tmp-`));
  let published = false;
  try {
    const workspaceResults: ChunkWorkspaceManifest[] = [];
    for (const [index, workspace] of manifest.workspaces.entries()) {
      log(`[${index + 1}/${manifest.workspaces.length}] Chunking workspace ${workspace.name} (${workspace.id})...`);
      const result = await processWorkspace({
        runDir, staging, workspace, tokenCounter, limits,
        onProgress: (message) => log(`  ${message}`),
      });
      workspaceResults.push(result);
    }

    const warning = workspaceResults.filter((item) => item.status === "complete_with_warnings").length;
    const successful = workspaceResults.filter((item) => item.status !== "failed").length;
    const failed = workspaceResults.length - successful;
    const status = failed > 0
      ? successful === 0 ? "failed" : "complete_with_errors"
      : warning > 0 ? "complete_with_warnings" : "complete";
    const finished = (options.now ?? (() => new Date()))();
    const chunksManifest: ChunksManifest = {
      formatVersion: CHUNKS_MANIFEST_FORMAT_VERSION,
      generatedAt: finished.toISOString(),
      sourceRun: manifest.sourceRun,
      sourceNormalization,
      tokenizer: {
        model: embedding.modelId,
        revision: embedding.modelRevision,
        sha256: embedding.tokenizerSHA256,
        configSha256: embedding.tokenizerConfigSHA256,
      },
      limits,
      workspaces: workspaceResults,
    };
    const report: ChunkRunReport = {
      formatVersion: CHUNKS_MANIFEST_FORMAT_VERSION,
      status,
      sourceRun: manifest.sourceRun,
      sourceNormalization,
      startedAt: started.toISOString(),
      finishedAt: finished.toISOString(),
      durationMs: Math.max(0, finished.getTime() - started.getTime()),
      workspaceCount: workspaceResults.length,
      successfulWorkspaces: successful,
      warningWorkspaces: warning,
      failedWorkspaces: failed,
      processedDocuments: sum(workspaceResults, "processedDocuments"),
      chunksCreated: sum(workspaceResults, "chunksCreated"),
      errors: sum(workspaceResults, "errors"),
      workspaces: workspaceResults,
    };
    const manifestFile = join(staging, "chunks_manifest.json");
    const reportFile = join(staging, "chunks_report.json");
    await Promise.all([writeJSON(manifestFile, chunksManifest), writeJSON(reportFile, report)]);
    await publishDirectory(staging, outputDir);
    published = true;
    return {
      outputDir,
      manifestFile: join(outputDir, "chunks_manifest.json"),
      reportFile: join(outputDir, "chunks_report.json"),
      manifest: chunksManifest,
      report,
    };
  } finally {
    if (!published) await rm(staging, { recursive: true, force: true });
  }
}

async function processWorkspace(options: {
  runDir: string;
  staging: string;
  workspace: NormalizationWorkspace;
  tokenCounter: TokenCounter;
  limits: { targetTokens: number; maxTokens: number; minTokens: number; overlapTokens: number };
  onProgress: (message: string) => void;
}): Promise<ChunkWorkspaceManifest> {
  const { workspace } = options;
  const outputPath = workspace.path;
  const outputDir = join(options.staging, outputPath);
  const chunksRelative = portableJoin(outputPath, "chunks.jsonl");
  const reportRelative = portableJoin(outputPath, "chunk_report.json");
  const chunksFile = join(options.staging, chunksRelative);
  const reportFile = join(options.staging, reportRelative);
  await mkdir(outputDir, { recursive: true });

  const base: Omit<ChunkWorkspaceManifest, "status" | "processedDocuments" | "chunksCreated" | "errors"> = {
    id: workspace.id,
    name: workspace.name,
    path: outputPath,
    qdrantCollection: workspace.qdrantCollection,
    sourceStatus: workspace.status,
    reportPath: reportRelative,
  };
  if (workspace.status === "failed" || workspace.errors > 0) {
    const message = workspace.errors > 0
      ? `Upstream normalization contains ${workspace.errors} error(s).`
      : "Upstream normalization status is failed.";
    await writeJSON(reportFile, failureReport(options.tokenCounter.name, options.limits, message));
    return { ...base, status: "failed", processedDocuments: 0, chunksCreated: 0, errors: 1, error: message };
  }

  try {
    if (!workspace.normalizedPath || !workspace.treePath) {
      throw new Error("Normalization manifest does not declare normalizedPath and treePath.");
    }
    const inputFile = resolveWithin(options.runDir, workspace.normalizedPath);
    const treeFile = resolveWithin(options.runDir, workspace.treePath);
    const result = await chunkNormalizedDocuments({
      inputFile,
      treeFile,
      outputFile: chunksFile,
      reportFile,
      targetTokens: options.limits.targetTokens,
      maxTokens: options.limits.maxTokens,
      minTokens: options.limits.minTokens,
      overlapTokens: options.limits.overlapTokens,
      tokenCounter: options.tokenCounter,
      onProgress: options.onProgress,
    });
    if (result.report.errors > 0) {
      const message = `Chunking contains ${result.report.errors} document error(s).`;
      if (result.report.processed_documents === 0) {
        await rm(chunksFile, { force: true });
        return {
          ...base, status: "failed", processedDocuments: 0,
          chunksCreated: 0, errors: result.report.errors, error: message,
        };
      }
      return {
        ...base, chunksPath: chunksRelative, status: "complete_with_warnings",
        processedDocuments: result.report.processed_documents,
        chunksCreated: result.report.chunks_created, errors: result.report.errors, error: message,
      };
    }
    return {
      ...base, chunksPath: chunksRelative, status: "complete",
      processedDocuments: result.report.processed_documents,
      chunksCreated: result.report.chunks_created, errors: 0,
    };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await rm(chunksFile, { force: true });
    await writeJSON(reportFile, failureReport(options.tokenCounter.name, options.limits, message));
    return { ...base, status: "failed", processedDocuments: 0, chunksCreated: 0, errors: 1, error: message };
  }
}

async function readNormalizationManifest(runDir: string): Promise<NormalizationManifest> {
  const path = join(runDir, "normalization_manifest.json");
  let value: unknown;
  try {
    value = JSON.parse(await readFile(path, "utf8"));
  } catch (error) {
    throw new Error(`Cannot read normalization manifest ${path}: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isRecord(value) || value.formatVersion !== 1 || typeof value.sourceRun !== "string" || !Array.isArray(value.workspaces)) {
    throw new Error(`Invalid normalization manifest: ${path}`);
  }
  return value as unknown as NormalizationManifest;
}

function validateWorkspaces(workspaces: NormalizationWorkspace[]): void {
  if (workspaces.length === 0) throw new Error("Normalization manifest contains no workspaces.");
  const ids = new Set<number>();
  const paths = new Set<string>();
  const collections = new Set<string>();
  for (const [index, workspace] of workspaces.entries()) {
    if (!isRecord(workspace) || !Number.isInteger(workspace.id) || workspace.id <= 0 ||
      typeof workspace.name !== "string" || !workspace.name.trim() ||
      typeof workspace.path !== "string" || workspace.path !== `workspace_${workspace.id}` ||
      typeof workspace.qdrantCollection !== "string" || !workspace.qdrantCollection.trim() ||
      !["complete", "complete_with_warnings", "failed"].includes(workspace.status) ||
      !Number.isInteger(workspace.errors) || workspace.errors < 0) {
      throw new Error(`Normalization manifest workspace ${index + 1} is invalid.`);
    }
    assertPortableRelative(workspace.path, `workspace ${workspace.id} path`);
    if (workspace.normalizedPath !== undefined) {
      assertPortableRelative(workspace.normalizedPath, `workspace ${workspace.id} normalizedPath`);
      if (workspace.normalizedPath !== portableJoin(workspace.path, "normalized.jsonl")) {
        throw new Error(`Workspace ${workspace.id} normalizedPath must point to its normalized.jsonl.`);
      }
    }
    if (workspace.treePath !== undefined) {
      assertPortableRelative(workspace.treePath, `workspace ${workspace.id} treePath`);
      if (workspace.treePath !== portableJoin(workspace.path, "document_tree.json")) {
        throw new Error(`Workspace ${workspace.id} treePath must point to its document_tree.json.`);
      }
    }
    if (ids.has(workspace.id)) throw new Error(`Duplicate workspace id: ${workspace.id}.`);
    if (paths.has(workspace.path)) throw new Error(`Duplicate workspace path: ${workspace.path}.`);
    if (collections.has(workspace.qdrantCollection)) throw new Error(`Duplicate qdrantCollection: ${workspace.qdrantCollection}.`);
    ids.add(workspace.id);
    paths.add(workspace.path);
    collections.add(workspace.qdrantCollection);
  }
}

function failureReport(tokenizer: string, limits: { targetTokens: number; maxTokens: number; minTokens: number; overlapTokens: number }, message: string): ChunkReport {
  return {
    input_documents: 0, processed_documents: 0, skipped_documents: 0, chunks_created: 0,
    tokenizer, target_tokens: limits.targetTokens, max_tokens: limits.maxTokens,
    min_tokens: limits.minTokens, overlap_tokens: limits.overlapTokens,
    average_tokens: 0, minimum_tokens: 0, maximum_tokens: 0,
    chunks_below_min_tokens: 0, chunks_above_max_tokens: 0, chunks_with_tables: 0,
    chunks_with_code: 0, chunks_with_images: 0, duplicate_chunks: 0,
    control_character_replacements: 0, errors: 1, skipped: [],
    control_character_warnings: [], oversized_chunks: [],
    error_documents: [{ line: 0, document_id: null, source: null, message }],
  };
}

function validateLimits(limits: { targetTokens: number; maxTokens: number; minTokens: number; overlapTokens: number }): void {
  if (![limits.targetTokens, limits.maxTokens, limits.minTokens].every((value) => Number.isInteger(value) && value > 0) ||
    !Number.isInteger(limits.overlapTokens) || limits.overlapTokens < 0) {
    throw new Error("Token limits must be integers and overlap must be non-negative.");
  }
  if (limits.minTokens > limits.targetTokens || limits.targetTokens > limits.maxTokens) {
    throw new Error("Token limits must satisfy minTokens <= targetTokens <= maxTokens.");
  }
  if (limits.overlapTokens >= limits.targetTokens) throw new Error("overlapTokens must be smaller than targetTokens.");
}

function validateSeparatedPaths(runDir: string, outputDir: string): void {
  if (runDir === outputDir || isWithin(runDir, outputDir) || isWithin(outputDir, runDir)) {
    throw new Error("Normalized input and chunks output directories must not contain one another.");
  }
}

function isWithin(parent: string, child: string): boolean {
  const value = relative(parent, child);
  return value !== "" && value !== ".." && !value.startsWith(`..${sep}`) && !isAbsolute(value);
}

function resolveWithin(root: string, value: string): string {
  assertPortableRelative(value, "Manifest path");
  const result = resolve(root, value);
  if (!isWithin(root, result)) throw new Error(`Manifest path must point inside the normalized run: ${value}`);
  return result;
}

function assertPortableRelative(value: string, label: string): void {
  if (!value || isAbsolute(value) || /^[A-Za-z]:[\\/]/u.test(value) || value.includes("\\") ||
    value === ".." || value.startsWith("../") || value.split("/").includes("..")) {
    throw new Error(`${label} must be a portable relative path: ${value}`);
  }
}

async function publishDirectory(staging: string, target: string): Promise<void> {
  const backup = join(dirname(target), `.${basename(target)}.backup-${process.pid}-${Date.now()}`);
  const exists = await pathExists(target);
  try {
    if (exists) await rename(target, backup);
    await rename(staging, target);
    if (exists) await rm(backup, { recursive: true, force: true });
  } catch (error) {
    await rm(target, { recursive: true, force: true });
    if (exists && await pathExists(backup)) await rename(backup, target);
    throw error;
  }
}

async function writeJSON(path: string, value: unknown): Promise<void> {
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

function portableJoin(...parts: string[]): string {
  return parts.join("/");
}

function sum(items: ChunkWorkspaceManifest[], key: "processedDocuments" | "chunksCreated" | "errors"): number {
  return items.reduce((total, item) => total + item[key], 0);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
