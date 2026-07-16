import { createHash } from "node:crypto";
import {
  access,
  mkdir,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { basename, dirname, isAbsolute, join, resolve } from "node:path";
import { unified } from "unified";
import remarkParse from "remark-parse";
import type {
  DocumentTree,
  DocumentTreeNode,
  NormalizedAsset,
  NormalizedDocument,
} from "./normalizer.js";
import {
  containsForbiddenControlCharacters,
  sanitizeControlCharacters,
} from "./text.js";
import { TransformersJsE5EmbeddingProvider } from "./embeddings/transformers-e5-provider.js";
import {
  DEFAULT_DTYPE,
  DEFAULT_MODEL,
  DEFAULT_MODEL_REVISION,
} from "./indexer/config.js";

export interface TokenCounter {
  readonly name: string;
  count(value: string): number;
}

export class UnicodeHeuristicTokenCounter implements TokenCounter {
  readonly name = "unicode-heuristic-v1";

  count(value: string): number {
    let count = 0;
    for (const match of value.matchAll(/[\p{L}\p{M}\p{N}]+|[^\s]/gu)) {
      const token = match[0];
      if (/^[\p{L}\p{M}\p{N}]+$/u.test(token)) {
        count += Math.max(1, Math.ceil([...token].length / 4));
      } else {
        count += 1;
      }
    }
    return count;
  }
}

export interface ChunkMetadata {
  title: string | null;
  source: string;
  page_id: string | null;
  parent_page_id: string | null;
  root_page_id: string | null;
  depth: number;
  ancestor_ids: string[];
  breadcrumbs: string[];
  heading_path: string[];
  section_title: string;
  heading_level: number;
  section_part: number;
  section_parts_total: number;
  chunk_index: number;
  chunk_count: number;
  token_count: number;
  character_count: number;
  contains_table: boolean;
  contains_code: boolean;
  oversized: boolean;
  oversized_reason?: OversizedReason;
  table_part?: number;
  table_parts_total?: number;
  assets: NormalizedAsset[];
  external_urls: string[];
  duplicate_of_chunk_id?: string;
  language: string;
  category: string | null;
  version: string | null;
  date: string | null;
  status: string;
}

export interface ChunkRecord {
  chunk_id: string;
  document_id: string;
  parent_document_id: string | null;
  content: string;
  embedding_text: string;
  content_hash: string;
  embedding_text_hash: string;
  metadata: ChunkMetadata;
  previous_chunk_id: string | null;
  next_chunk_id: string | null;
}

export type OversizedReason =
  | "code_block"
  | "table_row"
  | "list_item"
  | "inline_atom"
  | "html_block"
  | "context_prefix";

export interface ChunkingError {
  line: number;
  document_id: string | null;
  source: string | null;
  message: string;
}

export interface ChunkingSkip {
  line: number;
  document_id: string;
  source: string;
  reason: "indexable_false" | "external_link" | "empty_content";
}

export interface ChunkControlCharacterWarning {
  document_id: string;
  character: string;
  count: number;
}

export interface OversizedChunkReport {
  chunk_id: string;
  document_id: string;
  reason: OversizedReason;
  token_count: number;
}

export interface ChunkReport {
  input_documents: number;
  processed_documents: number;
  skipped_documents: number;
  chunks_created: number;
  tokenizer: string;
  target_tokens: number;
  max_tokens: number;
  min_tokens: number;
  overlap_tokens: number;
  average_tokens: number;
  minimum_tokens: number;
  maximum_tokens: number;
  chunks_below_min_tokens: number;
  chunks_above_max_tokens: number;
  chunks_with_tables: number;
  chunks_with_code: number;
  chunks_with_images: number;
  duplicate_chunks: number;
  control_character_replacements: number;
  errors: number;
  skipped: ChunkingSkip[];
  control_character_warnings: ChunkControlCharacterWarning[];
  oversized_chunks: OversizedChunkReport[];
  error_documents: ChunkingError[];
}

export interface ChunkOptions {
  inputFile: string;
  treeFile: string;
  outputFile: string;
  reportFile?: string;
  targetTokens?: number;
  maxTokens?: number;
  minTokens?: number;
  overlapTokens?: number;
  tokenCounter?: TokenCounter;
  tokenizer?: string;
  tokenizerRevision?: string;
  tokenizerCacheDir?: string;
  offline?: boolean;
  force?: boolean;
  onProgress?: (message: string) => void;
}

export interface ChunkingResult {
  outputFile: string;
  reportFile: string;
  report: ChunkReport;
}

interface MarkdownSection {
  headingPath: string[];
  sectionTitle: string;
  headingLevel: number;
  content: string;
}

type BlockKind =
  | "heading"
  | "rule"
  | "paragraph"
  | "list_item"
  | "table"
  | "code"
  | "html";

interface MarkdownBlock {
  kind: BlockKind;
  raw: string;
}

interface ChunkUnit extends MarkdownBlock {
  forceOwn?: boolean;
  oversizedReason?: OversizedReason;
  tablePart?: number;
  tablePartsTotal?: number;
  overlapEligible?: boolean;
}

interface ChunkGroup {
  units: ChunkUnit[];
  section: MarkdownSection;
}

interface PendingChunk {
  section: MarkdownSection;
  content: string;
  embeddingText: string;
  containsTable: boolean;
  containsCode: boolean;
  oversizedReason?: OversizedReason;
  tablePart?: number;
  tablePartsTotal?: number;
  sectionPart: number;
  sectionPartsTotal: number;
}

interface SyntaxNode {
  type?: string;
  value?: string;
  alt?: string;
  children?: SyntaxNode[];
}

const DEFAULT_TARGET_TOKENS = 320;
const DEFAULT_MAX_TOKENS = 450;
const DEFAULT_MIN_TOKENS = 80;
const DEFAULT_OVERLAP_TOKENS = 50;

export async function chunkNormalizedDocuments(
  options: ChunkOptions,
): Promise<ChunkingResult> {
  const inputFile = resolve(options.inputFile);
  const treeFile = resolve(options.treeFile);
  const outputFile = resolve(options.outputFile);
  const reportFile = resolve(
    options.reportFile ?? join(dirname(outputFile), "chunk_report.json"),
  );
  const targetTokens = options.targetTokens ?? DEFAULT_TARGET_TOKENS;
  const maxTokens = options.maxTokens ?? DEFAULT_MAX_TOKENS;
  const minTokens = options.minTokens ?? DEFAULT_MIN_TOKENS;
  const overlapTokens = options.overlapTokens ?? DEFAULT_OVERLAP_TOKENS;
  const tokenCounter = options.tokenCounter ?? (await createDefaultTokenCounter(options));
  const log = options.onProgress ?? (() => undefined);

  validateOptions({ targetTokens, maxTokens, minTokens, overlapTokens });
  if (outputFile === reportFile) {
    throw new Error("The chunks JSONL and report paths must be different.");
  }
  if (new Set([inputFile, treeFile, outputFile, reportFile]).size !== 4) {
    throw new Error("Input, tree, output, and report paths must be different.");
  }
  await requireFile(inputFile, "Normalized JSONL");
  await requireFile(treeFile, "Document tree");
  await assertTargetsAvailable([outputFile, reportFile], Boolean(options.force));

  const tree = parseTree(await readFile(treeFile, "utf8"));
  const nodes = validateTree(tree);
  const input = await readFile(inputFile, "utf8");
  const lines = input.split("\n");
  const report = createReport({
    tokenizer: tokenCounter.name,
    targetTokens,
    maxTokens,
    minTokens,
    overlapTokens,
  });
  const chunks: ChunkRecord[] = [];

  for (const [lineIndex, rawLine] of lines.entries()) {
    if (!rawLine.trim()) continue;
    const lineNumber = lineIndex + 1;
    report.input_documents += 1;
    let documentId: string | null = null;
    let source: string | null = null;
    try {
      const document = parseNormalizedDocument(rawLine);
      documentId = document.document_id;
      source = document.metadata.source;
      validateDocumentTreeLink(document, nodes);

      const skipReason = documentSkipReason(document);
      if (skipReason) {
        report.skipped.push({
          line: lineNumber,
          document_id: document.document_id,
          source: document.metadata.source,
          reason: skipReason,
        });
        log(`[${report.input_documents}] skipped: ${document.document_id}: ${skipReason}`);
        continue;
      }

      const sanitized = sanitizeControlCharacters(document.content);
      for (const replacement of sanitized.replacements) {
        report.control_character_replacements += replacement.count;
        report.control_character_warnings.push({
          document_id: document.document_id,
          character: replacement.character,
          count: replacement.count,
        });
      }
      if (!sanitized.value.trim()) {
        report.skipped.push({
          line: lineNumber,
          document_id: document.document_id,
          source: document.metadata.source,
          reason: "empty_content",
        });
        continue;
      }

      const documentChunks = chunkDocument(
        { ...document, content: sanitized.value },
        {
          targetTokens,
          maxTokens,
          overlapTokens,
          tokenCounter,
        },
      );
      validateDocumentChunks(document, documentChunks, tokenCounter, nodes);
      chunks.push(...documentChunks);
      report.processed_documents += 1;
      log(
        `[${report.input_documents}] chunked: ${document.document_id}: ${documentChunks.length} chunks`,
      );
    } catch (error) {
      report.error_documents.push({
        line: lineNumber,
        document_id: documentId,
        source,
        message: error instanceof Error ? error.message : String(error),
      });
      log(
        `[${report.input_documents}] error: ${documentId ?? `line ${lineNumber}`}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  annotateChunkDuplicates(chunks, report);
  validateGlobalChunks(chunks);
  finalizeReport(report, chunks);
  await writeResults({ outputFile, reportFile, chunks, report, force: Boolean(options.force) });
  return { outputFile, reportFile, report };
}

async function createDefaultTokenCounter(options: ChunkOptions): Promise<TokenCounter> {
  if (options.tokenizer === "unicode-heuristic-v1") return new UnicodeHeuristicTokenCounter();
  const modelId = options.tokenizer ?? process.env.EMBEDDING_MODEL?.trim() ?? DEFAULT_MODEL;
  const provider = new TransformersJsE5EmbeddingProvider({
    modelId,
    revision:
      options.tokenizerRevision ??
      process.env.EMBEDDING_MODEL_REVISION?.trim() ??
      DEFAULT_MODEL_REVISION,
    dtype: process.env.EMBEDDING_DTYPE?.trim() ?? DEFAULT_DTYPE,
    cacheDir:
      options.tokenizerCacheDir ?? process.env.EMBEDDING_CACHE_DIR?.trim() ?? "var/models",
    offline: Boolean(options.offline),
  });
  return provider.createTokenCounter();
}

function chunkDocument(
  document: NormalizedDocument,
  options: {
    targetTokens: number;
    maxTokens: number;
    overlapTokens: number;
    tokenCounter: TokenCounter;
  },
): ChunkRecord[] {
  const sections = parseSections(document.content);
  const pending: PendingChunk[] = [];
  for (const section of sections) {
    const blocks = parseBlocks(section.content);
    const units = expandBlocks(document, section, blocks, options);
    const groups = packUnits(document, section, units, options);
    applyOverlap(document, groups, options);
    const total = groups.length;
    for (const [index, group] of groups.entries()) {
      const content = joinUnits(group.units);
      if (!content.trim()) continue;
      const embeddingText = buildEmbeddingText(document, section, content);
      const oversizedReason = group.units.find((unit) => unit.oversizedReason)
        ?.oversizedReason;
      const tableUnit = group.units.find((unit) => unit.tablePart !== undefined);
      pending.push({
        section,
        content,
        embeddingText,
        containsTable: group.units.some((unit) => unit.kind === "table"),
        containsCode: group.units.some((unit) => unit.kind === "code"),
        ...(oversizedReason ? { oversizedReason } : {}),
        ...(tableUnit?.tablePart !== undefined
          ? {
              tablePart: tableUnit.tablePart,
              tablePartsTotal: tableUnit.tablePartsTotal ?? 1,
            }
          : {}),
        sectionPart: index + 1,
        sectionPartsTotal: total,
      });
    }
  }

  const occurrenceByIdentity = new Map<string, number>();
  const chunks: ChunkRecord[] = pending.map((item, index): ChunkRecord => {
    const contentHash = sha256(item.content);
    const headingIdentity = normalizedHeadingIdentity(item.section.headingPath);
    const identity = `${headingIdentity}\u001e${contentHash}`;
    const occurrence = occurrenceByIdentity.get(identity) ?? 0;
    occurrenceByIdentity.set(identity, occurrence + 1);
    const chunkId = `chunk_${sha256(
      JSON.stringify([document.document_id, headingIdentity, contentHash, occurrence]),
    )}`;
    const assets = extractAssets(item.content);
    const externalUrls = extractExternalUrls(item.content);
    const tokenCount = options.tokenCounter.count(item.embeddingText);
    const metadata: ChunkMetadata = {
      title: document.metadata.title,
      source: document.metadata.source,
      page_id: document.metadata.page_id,
      parent_page_id: document.metadata.parent_page_id,
      root_page_id: document.metadata.root_page_id,
      depth: document.metadata.depth,
      ancestor_ids: [...document.metadata.ancestor_ids],
      breadcrumbs: [...document.metadata.breadcrumbs],
      heading_path: [...item.section.headingPath],
      section_title: item.section.sectionTitle,
      heading_level: item.section.headingLevel,
      section_part: item.sectionPart,
      section_parts_total: item.sectionPartsTotal,
      chunk_index: index,
      chunk_count: pending.length,
      token_count: tokenCount,
      character_count: [...item.content].length,
      contains_table: item.containsTable,
      contains_code: item.containsCode,
      oversized: tokenCount > options.maxTokens || item.oversizedReason !== undefined,
      ...(item.oversizedReason ? { oversized_reason: item.oversizedReason } : {}),
      ...(item.tablePart !== undefined
        ? { table_part: item.tablePart, table_parts_total: item.tablePartsTotal ?? 1 }
        : {}),
      assets,
      external_urls: externalUrls,
      language: document.metadata.language,
      category: document.metadata.category,
      version: document.metadata.version,
      date: document.metadata.date,
      status: document.metadata.status,
    };
    if (metadata.oversized && !metadata.oversized_reason) {
      metadata.oversized_reason = "context_prefix";
    }
    return {
      chunk_id: chunkId,
      document_id: document.document_id,
      parent_document_id: document.parent_document_id,
      content: item.content,
      embedding_text: item.embeddingText,
      content_hash: contentHash,
      embedding_text_hash: sha256(item.embeddingText),
      metadata,
      previous_chunk_id: null,
      next_chunk_id: null,
    };
  });

  for (const [index, chunk] of chunks.entries()) {
    chunk.previous_chunk_id = chunks[index - 1]?.chunk_id ?? null;
    chunk.next_chunk_id = chunks[index + 1]?.chunk_id ?? null;
  }
  return chunks;
}

function parseSections(markdown: string): MarkdownSection[] {
  const lines = markdown.split("\n");
  const sections: MarkdownSection[] = [];
  const headingStack: Array<{ level: number; title: string }> = [];
  let current: MarkdownSection = {
    headingPath: [],
    sectionTitle: "Преамбула",
    headingLevel: 0,
    content: "",
  };
  let currentLines: string[] = [];
  let fence: { marker: "`" | "~"; length: number } | null = null;

  const flush = (): void => {
    const content = trimBlankLines(currentLines.join("\n"));
    if (content) sections.push({ ...current, content });
    currentLines = [];
  };

  for (const line of lines) {
    const fenceMatch = line.match(/^ {0,3}(`{3,}|~{3,})/u);
    if (fence) {
      currentLines.push(line);
      if (
        fenceMatch &&
        fenceMatch[1]?.[0] === fence.marker &&
        fenceMatch[1].length >= fence.length &&
        /^ {0,3}(?:`{3,}|~{3,})\s*$/u.test(line)
      ) {
        fence = null;
      }
      continue;
    }
    if (fenceMatch) {
      fence = {
        marker: fenceMatch[1]?.[0] as "`" | "~",
        length: fenceMatch[1]?.length ?? 3,
      };
      currentLines.push(line);
      continue;
    }

    const heading = line.match(/^ {0,3}(#{1,6})[ \t]+(.+?)\s*#*\s*$/u);
    if (!heading) {
      currentLines.push(line);
      continue;
    }

    const preambleOnlyRules =
      current.headingLevel === 0 &&
      currentLines.every((item) => !item.trim() || isThematicBreak(item));
    const carriedLines = preambleOnlyRules ? currentLines : [];
    if (!preambleOnlyRules) flush();
    else currentLines = [];

    const level = heading[1]?.length ?? 1;
    const title = cleanHeadingText(heading[2] ?? "") || `Раздел ${sections.length + 1}`;
    while (headingStack.at(-1) && (headingStack.at(-1)?.level ?? 0) >= level) {
      headingStack.pop();
    }
    headingStack.push({ level, title });
    current = {
      headingPath: headingStack.map((item) => item.title),
      sectionTitle: title,
      headingLevel: level,
      content: "",
    };
    currentLines.push(...carriedLines, line);
  }
  if (fence) throw new Error("Markdown contains an unclosed fenced code block.");
  flush();
  return sections;
}

function parseBlocks(content: string): MarkdownBlock[] {
  const lines = content.split("\n");
  const blocks: MarkdownBlock[] = [];
  let index = 0;

  const append = (kind: BlockKind, start: number, endExclusive: number): void => {
    let end = endExclusive;
    while (end < lines.length && lines[end]?.trim() === "") end += 1;
    blocks.push({ kind, raw: lines.slice(start, end).join("\n") });
    index = end;
  };

  while (index < lines.length) {
    if (lines[index]?.trim() === "") {
      if (blocks.length > 0) {
        blocks[blocks.length - 1]!.raw += `${blocks[blocks.length - 1]!.raw ? "\n" : ""}${lines[index]}`;
      }
      index += 1;
      continue;
    }
    const start = index;
    const line = lines[index] ?? "";
    const fence = line.match(/^ {0,3}(`{3,}|~{3,})/u);
    if (fence) {
      const marker = fence[1]?.[0] ?? "`";
      const length = fence[1]?.length ?? 3;
      index += 1;
      while (index < lines.length) {
        const candidate = lines[index] ?? "";
        index += 1;
        const closing = candidate.match(/^ {0,3}(`{3,}|~{3,})\s*$/u)?.[1];
        if (closing?.[0] === marker && closing.length >= length) break;
      }
      append("code", start, index);
      continue;
    }
    if (index + 1 < lines.length && isTableRow(line) && isTableDelimiter(lines[index + 1] ?? "")) {
      index += 2;
      while (index < lines.length && isTableRow(lines[index] ?? "")) index += 1;
      append("table", start, index);
      continue;
    }
    if (/^ {0,3}#{1,6}[ \t]+/u.test(line)) {
      append("heading", start, start + 1);
      continue;
    }
    if (isThematicBreak(line)) {
      append("rule", start, start + 1);
      continue;
    }
    if (isTopLevelListItem(line)) {
      index += 1;
      while (index < lines.length) {
        const candidate = lines[index] ?? "";
        if (isTopLevelListItem(candidate) || isBlockBoundary(lines, index)) break;
        if (!candidate.trim()) {
          const next = nextNonBlankLine(lines, index + 1);
          if (next === null || (!/^\s{2,}\S/u.test(lines[next] ?? "") && !isNestedListItem(lines[next] ?? ""))) {
            break;
          }
        }
        index += 1;
      }
      append("list_item", start, index);
      continue;
    }
    if (/^ {0,3}<[/!?A-Za-z][^>]*>/u.test(line)) {
      index += 1;
      while (index < lines.length && lines[index]?.trim()) index += 1;
      append("html", start, index);
      continue;
    }

    index += 1;
    while (index < lines.length && lines[index]?.trim() && !isBlockBoundary(lines, index)) {
      index += 1;
    }
    append("paragraph", start, index);
  }
  return blocks.filter((block) => block.raw.trim());
}

function expandBlocks(
  document: NormalizedDocument,
  section: MarkdownSection,
  blocks: MarkdownBlock[],
  options: {
    targetTokens: number;
    maxTokens: number;
    tokenCounter: TokenCounter;
  },
): ChunkUnit[] {
  const units: ChunkUnit[] = [];
  for (const block of blocks) {
    const tokenCount = countCandidate(document, section, block.raw, options.tokenCounter);
    if (tokenCount <= options.maxTokens) {
      units.push({ ...block, overlapEligible: block.kind === "paragraph" });
      continue;
    }
    if (block.kind === "code") {
      units.push({ ...block, forceOwn: true, oversizedReason: "code_block" });
      continue;
    }
    if (block.kind === "table") {
      units.push(...splitTable(document, section, block, options));
      continue;
    }
    if (block.kind === "list_item") {
      units.push({ ...block, forceOwn: true, oversizedReason: "list_item" });
      continue;
    }
    if (block.kind === "html") {
      units.push({ ...block, forceOwn: true, oversizedReason: "html_block" });
      continue;
    }
    if (block.kind === "paragraph") {
      units.push(...splitParagraph(document, section, block.raw, options));
      continue;
    }
    units.push({ ...block, forceOwn: true, oversizedReason: "context_prefix" });
  }
  return units;
}

function splitTable(
  document: NormalizedDocument,
  section: MarkdownSection,
  block: MarkdownBlock,
  options: { targetTokens: number; maxTokens: number; tokenCounter: TokenCounter },
): ChunkUnit[] {
  const lines = trimBlankLines(block.raw).split("\n");
  const header = lines.slice(0, 2);
  const rows = lines.slice(2);
  if (rows.length === 0) {
    return [{ ...block, forceOwn: true, oversizedReason: "table_row" }];
  }
  const parts: Array<{ raw: string; oversized?: OversizedReason }> = [];
  let currentRows: string[] = [];
  const flush = (): void => {
    if (currentRows.length === 0) return;
    parts.push({ raw: [...header, ...currentRows].join("\n") });
    currentRows = [];
  };
  for (const row of rows) {
    const rowCandidate = [...header, row].join("\n");
    if (countCandidate(document, section, rowCandidate, options.tokenCounter) > options.maxTokens) {
      flush();
      parts.push({ raw: rowCandidate, oversized: "table_row" });
      continue;
    }
    const candidate = [...header, ...currentRows, row].join("\n");
    const candidateTokens = countCandidate(document, section, candidate, options.tokenCounter);
    if (currentRows.length > 0 && candidateTokens > options.targetTokens) flush();
    currentRows.push(row);
  }
  flush();
  return parts.map((part, index) => ({
    kind: "table",
    raw: part.raw,
    forceOwn: true,
    ...(part.oversized ? { oversizedReason: part.oversized } : {}),
    tablePart: index + 1,
    tablePartsTotal: parts.length,
  }));
}

function splitParagraph(
  document: NormalizedDocument,
  section: MarkdownSection,
  raw: string,
  options: { maxTokens: number; tokenCounter: TokenCounter },
): ChunkUnit[] {
  const segmenter = new Intl.Segmenter(document.metadata.language || "ru", {
    granularity: "sentence",
  });
  const sentences = [...segmenter.segment(trimBlankLines(raw))]
    .map((item) => item.segment.trim())
    .filter(Boolean);
  if (sentences.length <= 1) {
    return splitInlineAtoms(document, section, trimBlankLines(raw), options);
  }
  const units: ChunkUnit[] = [];
  for (const sentence of sentences) {
    if (countCandidate(document, section, sentence, options.tokenCounter) <= options.maxTokens) {
      units.push({ kind: "paragraph", raw: sentence, overlapEligible: true });
    } else {
      units.push(...splitInlineAtoms(document, section, sentence, options));
    }
  }
  return units;
}

function splitInlineAtoms(
  document: NormalizedDocument,
  section: MarkdownSection,
  raw: string,
  options: { maxTokens: number; tokenCounter: TokenCounter },
): ChunkUnit[] {
  const atoms = inlineAtoms(raw);
  const units: ChunkUnit[] = [];
  let current = "";
  const flush = (): void => {
    const value = current.trim();
    if (value) units.push({ kind: "paragraph", raw: value, overlapEligible: true });
    current = "";
  };
  for (const atom of atoms) {
    const candidate = `${current}${atom}`;
    if (current && countCandidate(document, section, candidate, options.tokenCounter) > options.maxTokens) {
      flush();
    }
    if (countCandidate(document, section, atom.trim(), options.tokenCounter) > options.maxTokens) {
      flush();
      units.push({
        kind: "paragraph",
        raw: atom.trim(),
        forceOwn: true,
        oversizedReason: "inline_atom",
      });
    } else {
      current += atom;
    }
  }
  flush();
  return units;
}

function packUnits(
  document: NormalizedDocument,
  section: MarkdownSection,
  units: ChunkUnit[],
  options: { targetTokens: number; maxTokens: number; tokenCounter: TokenCounter },
): ChunkGroup[] {
  const groups: ChunkGroup[] = [];
  let current: ChunkUnit[] = [];
  const flush = (): void => {
    if (current.length > 0) groups.push({ units: current, section });
    current = [];
  };
  for (const unit of units) {
    if (unit.kind === "rule" && current.length > 0) {
      current.push(unit);
      flush();
      continue;
    }
    if (unit.forceOwn) {
      flush();
      groups.push({ units: [unit], section });
      continue;
    }
    if (current.length === 0) {
      current.push(unit);
      continue;
    }
    const currentContent = joinUnits(current);
    const candidateContent = joinUnits([...current, unit]);
    const currentTokens = countCandidate(document, section, currentContent, options.tokenCounter);
    const candidateTokens = countCandidate(
      document,
      section,
      candidateContent,
      options.tokenCounter,
    );
    if (currentTokens >= options.targetTokens || candidateTokens > options.maxTokens) flush();
    current.push(unit);
  }
  flush();
  return groups;
}

function applyOverlap(
  document: NormalizedDocument,
  groups: ChunkGroup[],
  options: { overlapTokens: number; maxTokens: number; tokenCounter: TokenCounter },
): void {
  if (options.overlapTokens === 0 || groups.length < 2) return;
  for (let index = 1; index < groups.length; index += 1) {
    const previous = groups[index - 1];
    const current = groups[index];
    if (!previous || !current || current.units.some((unit) => unit.forceOwn)) continue;
    const suffix: ChunkUnit[] = [];
    for (let unitIndex = previous.units.length - 1; unitIndex >= 0; unitIndex -= 1) {
      const unit = previous.units[unitIndex];
      if (!unit?.overlapEligible || unit.forceOwn) break;
      const candidate = [unit, ...suffix];
      const bodyTokens = options.tokenCounter.count(embeddingBody(joinUnits(candidate), previous.section));
      if (bodyTokens > options.overlapTokens) break;
      const withCurrent = joinUnits([...candidate, ...current.units]);
      if (countCandidate(document, current.section, withCurrent, options.tokenCounter) > options.maxTokens) {
        break;
      }
      suffix.unshift({ ...unit });
    }
    if (suffix.length > 0) current.units.unshift(...suffix);
  }
}

function buildEmbeddingText(
  document: NormalizedDocument,
  section: MarkdownSection,
  content: string,
): string {
  const context = document.metadata.context_text || document.metadata.breadcrumbs.join(" → ");
  const headingPath = dedupeAdjacent(section.headingPath);
  const prefix: string[] = [];
  if (context) prefix.push(`Путь: ${context}`);
  if (document.metadata.title) prefix.push(`Документ: ${document.metadata.title}`);
  prefix.push(`Раздел: ${headingPath.length > 0 ? headingPath.join(" → ") : "Преамбула"}`);
  const body = embeddingBody(content, section);
  return `${prefix.join("\n")}\n\n${body}`.trim();
}

function embeddingBody(content: string, section: MarkdownSection): string {
  const lines = content.split("\n");
  let fence: { marker: string; length: number } | null = null;
  const output: string[] = [];
  let removedSectionHeading = false;
  for (const line of lines) {
    const fenceMatch = line.match(/^ {0,3}(`{3,}|~{3,})/u)?.[1];
    if (fence) {
      output.push(line);
      if (
        fenceMatch?.[0] === fence.marker &&
        fenceMatch.length >= fence.length &&
        /^ {0,3}(?:`{3,}|~{3,})\s*$/u.test(line)
      ) {
        fence = null;
      }
      continue;
    }
    if (fenceMatch) {
      fence = { marker: fenceMatch[0] ?? "`", length: fenceMatch.length };
      output.push(line);
      continue;
    }
    const heading = line.match(/^ {0,3}#{1,6}[ \t]+(.+?)\s*#*\s*$/u);
    if (
      !removedSectionHeading &&
      heading &&
      normalizeIdentity(cleanHeadingText(heading[1] ?? "")) ===
        normalizeIdentity(section.sectionTitle)
    ) {
      removedSectionHeading = true;
      continue;
    }
    let transformed = line.replace(
      /!\[([^\]]*)\]\((?:<([^>]+)>|([^\s)]+))(?:\s+["'][^"']*["'])?\)/gu,
      (_match, alt: string) => (alt.trim() ? `Изображение: ${alt.trim()}` : ""),
    );
    transformed = transformed.replace(
      /(?<!!)\[([^\]]*)\]\((?:<([^>]+)>|([^\s)]+))(?:\s+["'][^"']*["'])?\)/gu,
      (_match, label: string, angleUrl: string | undefined, plainUrl: string | undefined) =>
        label.trim() || angleUrl || plainUrl || "",
    );
    output.push(transformed);
  }
  return trimBlankLines(output.join("\n"));
}

function extractAssets(content: string): NormalizedAsset[] {
  const assets: NormalizedAsset[] = [];
  const seen = new Set<string>();
  for (const match of content.matchAll(
    /!\[([^\]]*)\]\((?:<([^>]+)>|([^\s)]+))(?:\s+["'][^"']*["'])?\)/gu,
  )) {
    const path = decodeHtmlEntities(match[2] ?? match[3] ?? "").replace(/\\/gu, "/");
    if (!path) continue;
    const identity = `${path}\u001e${match[1] ?? ""}`;
    if (seen.has(identity)) continue;
    seen.add(identity);
    assets.push({ type: "image", path, alt: match[1] ?? "" });
  }
  return assets;
}

function extractExternalUrls(content: string): string[] {
  const urls: string[] = [];
  const seen = new Set<string>();
  for (const match of content.matchAll(/https?:\/\/[^\s<>"'`]+/giu)) {
    let url = decodeHtmlEntities(match[0]).replace(/[.,;:!?]+$/gu, "");
    while (url.endsWith(")") && countCharacter(url, ")") > countCharacter(url, "(")) {
      url = url.slice(0, -1);
    }
    url = url.replace(/\]+$/gu, "");
    if (!seen.has(url)) {
      seen.add(url);
      urls.push(url);
    }
  }
  return urls;
}

function inlineAtoms(value: string): string[] {
  const atoms: string[] = [];
  const protectedPattern =
    /!\[[^\]]*\]\([^)]+\)|\[[^\]]+\]\([^)]+\)|`+[^`\n]+`+|<[^>\n]+>|\*\*[^*\n]+\*\*|__[^_\n]+__|\*[^*\n]+\*|_[^_\n]+_/gu;
  let cursor = 0;
  for (const match of value.matchAll(protectedPattern)) {
    const index = match.index ?? cursor;
    atoms.push(...splitPlainAtoms(value.slice(cursor, index)));
    atoms.push(match[0]);
    cursor = index + match[0].length;
  }
  atoms.push(...splitPlainAtoms(value.slice(cursor)));
  return atoms.filter(Boolean);
}

function splitPlainAtoms(value: string): string[] {
  return value.match(/\S+\s*|\s+/gu) ?? [];
}

function joinUnits(units: Array<{ raw: string }>): string {
  return trimBlankLines(
    units
      .map((unit) => unit.raw)
      .join("\n\n")
      .replace(/\n{4,}/gu, "\n\n\n"),
  );
}

function countCandidate(
  document: NormalizedDocument,
  section: MarkdownSection,
  content: string,
  counter: TokenCounter,
): number {
  return counter.count(buildEmbeddingText(document, section, content));
}

function cleanHeadingText(value: string): string {
  try {
    const tree = unified().use(remarkParse).parse(`# ${value}`) as SyntaxNode;
    return syntaxText(tree.children?.[0] ?? {}).replace(/\s+/gu, " ").trim();
  } catch {
    return value.replace(/[*_~`]/gu, "").replace(/\s+/gu, " ").trim();
  }
}

function syntaxText(node: SyntaxNode): string {
  if (typeof node.value === "string") return node.value;
  if (typeof node.alt === "string") return node.alt;
  return node.children?.map(syntaxText).join("") ?? "";
}

function normalizedHeadingIdentity(path: string[]): string {
  return path.map(normalizeIdentity).join("\u001f");
}

function normalizeIdentity(value: string): string {
  return value.normalize("NFKC").toLocaleLowerCase("ru-RU").replace(/\s+/gu, " ").trim();
}

function dedupeAdjacent(values: string[]): string[] {
  const output: string[] = [];
  for (const value of values) {
    if (normalizeIdentity(output.at(-1) ?? "") !== normalizeIdentity(value)) output.push(value);
  }
  return output;
}

function isBlockBoundary(lines: string[], index: number): boolean {
  const line = lines[index] ?? "";
  if (/^ {0,3}(?:#{1,6}[ \t]+|`{3,}|~{3,})/u.test(line)) return true;
  if (isThematicBreak(line) || isTopLevelListItem(line)) return true;
  if (index + 1 < lines.length && isTableRow(line) && isTableDelimiter(lines[index + 1] ?? "")) {
    return true;
  }
  return /^ {0,3}<[/!?A-Za-z][^>]*>/u.test(line);
}

function isThematicBreak(line: string): boolean {
  return /^ {0,3}(?:(?:\*\s*){3,}|(?:-\s*){3,}|(?:_\s*){3,})$/u.test(line);
}

function isTableRow(line: string): boolean {
  return line.includes("|") && line.trim().length > 1;
}

function isTableDelimiter(line: string): boolean {
  const trimmed = line.trim().replace(/^\||\|$/gu, "");
  const cells = trimmed.split("|");
  return cells.length > 0 && cells.every((cell) => /^\s*:?-{3,}:?\s*$/u.test(cell));
}

function isTopLevelListItem(line: string): boolean {
  return /^ {0,3}(?:[-+*]|\d+[.)])[ \t]+\S/u.test(line);
}

function isNestedListItem(line: string): boolean {
  return /^\s{2,}(?:[-+*]|\d+[.)])[ \t]+\S/u.test(line);
}

function nextNonBlankLine(lines: string[], start: number): number | null {
  for (let index = start; index < lines.length; index += 1) {
    if (lines[index]?.trim()) return index;
  }
  return null;
}

function trimBlankLines(value: string): string {
  return value.replace(/^(?:[ \t]*\n)+/u, "").replace(/(?:\n[ \t]*)+$/u, "");
}

function documentSkipReason(
  document: NormalizedDocument,
): ChunkingSkip["reason"] | null {
  if (document.metadata.document_type === "external_link") return "external_link";
  if (!document.metadata.indexable) return "indexable_false";
  if (!document.content.trim()) return "empty_content";
  return null;
}

function parseNormalizedDocument(line: string): NormalizedDocument {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch (error) {
    throw new Error(`Invalid JSON: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isRecord(value)) throw new Error("A JSONL line must contain an object.");
  if (typeof value.document_id !== "string" || !value.document_id) {
    throw new Error("document_id must be a non-empty string.");
  }
  if (value.parent_document_id !== null && typeof value.parent_document_id !== "string") {
    throw new Error("parent_document_id must be a string or null.");
  }
  if (typeof value.content !== "string") throw new Error("content must be a string.");
  if (!isRecord(value.metadata)) throw new Error("metadata must be an object.");
  const metadata = value.metadata;
  if (typeof metadata.source !== "string" || !metadata.source) {
    throw new Error("metadata.source must be a non-empty string.");
  }
  if (typeof metadata.indexable !== "boolean") {
    throw new Error("metadata.indexable must be a boolean.");
  }
  if (!Array.isArray(metadata.ancestor_ids) || !metadata.ancestor_ids.every(isString)) {
    throw new Error("metadata.ancestor_ids must be a string array.");
  }
  if (!Array.isArray(metadata.breadcrumbs) || !metadata.breadcrumbs.every(isString)) {
    throw new Error("metadata.breadcrumbs must be a string array.");
  }
  for (const field of ["depth"] as const) {
    if (!Number.isInteger(metadata[field]) || (metadata[field] as number) < 0) {
      throw new Error(`metadata.${field} must be a non-negative integer.`);
    }
  }
  for (const field of ["language", "status", "document_type"] as const) {
    if (typeof metadata[field] !== "string") throw new Error(`metadata.${field} must be a string.`);
  }
  if (typeof metadata.context_text !== "string") {
    throw new Error("metadata.context_text must be a string.");
  }
  return value as unknown as NormalizedDocument;
}

function parseTree(value: string): DocumentTree {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch (error) {
    throw new Error(`Invalid document tree JSON: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isRecord(parsed) || !Array.isArray(parsed.nodes)) {
    throw new Error("Document tree must contain a nodes array.");
  }
  return parsed as unknown as DocumentTree;
}

function validateTree(tree: DocumentTree): Map<string, DocumentTreeNode> {
  const nodes = new Map<string, DocumentTreeNode>();
  for (const node of tree.nodes) {
    if (!isRecord(node) || typeof node.node_id !== "string" || !node.node_id) {
      throw new Error("Every tree node must have a non-empty node_id.");
    }
    if (nodes.has(node.node_id)) throw new Error(`Duplicate tree node '${node.node_id}'.`);
    if (node.parent_node_id !== null && typeof node.parent_node_id !== "string") {
      throw new Error(`Tree node '${node.node_id}' has an invalid parent_node_id.`);
    }
    if (!Number.isInteger(node.depth) || node.depth < 0) {
      throw new Error(`Tree node '${node.node_id}' has an invalid depth.`);
    }
    nodes.set(node.node_id, node as DocumentTreeNode);
  }
  for (const node of nodes.values()) {
    if (node.parent_node_id === node.node_id) {
      throw new Error(`Tree node '${node.node_id}' cannot be its own parent.`);
    }
    if (node.parent_node_id) {
      const parent = nodes.get(node.parent_node_id);
      if (!parent) throw new Error(`Tree parent '${node.parent_node_id}' does not exist.`);
      if (node.depth !== parent.depth + 1) {
        throw new Error(`Tree node '${node.node_id}' has an invalid depth.`);
      }
    } else if (node.depth !== 0) {
      throw new Error(`Root tree node '${node.node_id}' must have depth 0.`);
    }
    const visited = new Set<string>();
    let current: DocumentTreeNode | undefined = node;
    while (current?.parent_node_id) {
      if (visited.has(current.node_id)) throw new Error("A cycle was detected in the tree.");
      visited.add(current.node_id);
      current = nodes.get(current.parent_node_id);
    }
  }
  return nodes;
}

function validateDocumentTreeLink(
  document: NormalizedDocument,
  nodes: Map<string, DocumentTreeNode>,
): void {
  const node = nodes.get(document.document_id);
  if (!node) throw new Error(`Document node '${document.document_id}' is missing from the tree.`);
  if (node.parent_node_id !== document.parent_document_id) {
    throw new Error(
      `Document parent '${document.parent_document_id ?? "null"}' does not match tree parent '${node.parent_node_id ?? "null"}'.`,
    );
  }
  if (document.metadata.depth !== node.depth) {
    throw new Error("Document depth does not match the tree depth.");
  }
  assertRelativeLocalPath(document.metadata.source, "Document source");
}

function validateDocumentChunks(
  document: NormalizedDocument,
  chunks: ChunkRecord[],
  tokenCounter: TokenCounter,
  nodes: Map<string, DocumentTreeNode>,
): void {
  if (chunks.length === 0) throw new Error("No non-empty chunks were produced.");
  if (!nodes.has(document.document_id)) throw new Error("Document tree node is missing.");
  for (const [index, chunk] of chunks.entries()) {
    if (!chunk.content.trim()) throw new Error("A chunk has empty content.");
    if (!chunk.embedding_text.trim()) throw new Error("A chunk has empty embedding_text.");
    if (containsForbiddenControlCharacters(chunk.content + chunk.embedding_text)) {
      throw new Error("A chunk contains a forbidden control character.");
    }
    if (chunk.metadata.token_count !== tokenCounter.count(chunk.embedding_text)) {
      throw new Error("A chunk token_count does not match the tokenizer.");
    }
    if (chunk.metadata.chunk_index !== index || chunk.metadata.chunk_count !== chunks.length) {
      throw new Error("Chunk indexes are inconsistent.");
    }
    if (chunk.previous_chunk_id !== (chunks[index - 1]?.chunk_id ?? null)) {
      throw new Error("previous_chunk_id is inconsistent.");
    }
    if (chunk.next_chunk_id !== (chunks[index + 1]?.chunk_id ?? null)) {
      throw new Error("next_chunk_id is inconsistent.");
    }
    if (chunk.metadata.breadcrumbs.join("\u001e") !== document.metadata.breadcrumbs.join("\u001e")) {
      throw new Error("Chunk breadcrumbs do not match the document.");
    }
    if (chunk.metadata.ancestor_ids.join("\u001e") !== document.metadata.ancestor_ids.join("\u001e")) {
      throw new Error("Chunk ancestor_ids do not match the document.");
    }
    for (const asset of chunk.metadata.assets) {
      assertRelativeLocalPath(asset.path, "Asset path", true);
    }
  }
}

function validateGlobalChunks(chunks: ChunkRecord[]): void {
  const ids = new Set<string>();
  for (const chunk of chunks) {
    if (ids.has(chunk.chunk_id)) throw new Error(`Duplicate chunk_id '${chunk.chunk_id}'.`);
    ids.add(chunk.chunk_id);
  }
  for (const chunk of chunks) {
    if (chunk.previous_chunk_id && !ids.has(chunk.previous_chunk_id)) {
      throw new Error(`Missing previous chunk '${chunk.previous_chunk_id}'.`);
    }
    if (chunk.next_chunk_id && !ids.has(chunk.next_chunk_id)) {
      throw new Error(`Missing next chunk '${chunk.next_chunk_id}'.`);
    }
  }
}

function annotateChunkDuplicates(chunks: ChunkRecord[], report: ChunkReport): void {
  const firstByHash = new Map<string, string>();
  for (const chunk of chunks) {
    const first = firstByHash.get(chunk.content_hash);
    if (first) {
      chunk.metadata.duplicate_of_chunk_id = first;
      report.duplicate_chunks += 1;
    } else {
      firstByHash.set(chunk.content_hash, chunk.chunk_id);
    }
  }
}

function finalizeReport(report: ChunkReport, chunks: ChunkRecord[]): void {
  report.skipped_documents = report.skipped.length;
  report.chunks_created = chunks.length;
  report.errors = report.error_documents.length;
  const tokens = chunks.map((chunk) => chunk.metadata.token_count);
  const total = tokens.reduce((sum, value) => sum + value, 0);
  report.average_tokens = tokens.length > 0 ? Number((total / tokens.length).toFixed(2)) : 0;
  report.minimum_tokens = tokens.length > 0 ? Math.min(...tokens) : 0;
  report.maximum_tokens = tokens.length > 0 ? Math.max(...tokens) : 0;
  report.chunks_below_min_tokens = chunks.filter(
    (chunk) => chunk.metadata.token_count < report.min_tokens,
  ).length;
  report.chunks_above_max_tokens = chunks.filter(
    (chunk) => chunk.metadata.token_count > report.max_tokens,
  ).length;
  report.chunks_with_tables = chunks.filter((chunk) => chunk.metadata.contains_table).length;
  report.chunks_with_code = chunks.filter((chunk) => chunk.metadata.contains_code).length;
  report.chunks_with_images = chunks.filter((chunk) => chunk.metadata.assets.length > 0).length;
  report.oversized_chunks = chunks
    .filter((chunk) => chunk.metadata.oversized)
    .map((chunk) => ({
      chunk_id: chunk.chunk_id,
      document_id: chunk.document_id,
      reason: chunk.metadata.oversized_reason ?? "context_prefix",
      token_count: chunk.metadata.token_count,
    }));
}

function createReport(options: {
  tokenizer: string;
  targetTokens: number;
  maxTokens: number;
  minTokens: number;
  overlapTokens: number;
}): ChunkReport {
  return {
    input_documents: 0,
    processed_documents: 0,
    skipped_documents: 0,
    chunks_created: 0,
    tokenizer: options.tokenizer,
    target_tokens: options.targetTokens,
    max_tokens: options.maxTokens,
    min_tokens: options.minTokens,
    overlap_tokens: options.overlapTokens,
    average_tokens: 0,
    minimum_tokens: 0,
    maximum_tokens: 0,
    chunks_below_min_tokens: 0,
    chunks_above_max_tokens: 0,
    chunks_with_tables: 0,
    chunks_with_code: 0,
    chunks_with_images: 0,
    duplicate_chunks: 0,
    control_character_replacements: 0,
    errors: 0,
    skipped: [],
    control_character_warnings: [],
    oversized_chunks: [],
    error_documents: [],
  };
}

function validateOptions(options: {
  targetTokens: number;
  maxTokens: number;
  minTokens: number;
  overlapTokens: number;
}): void {
  for (const [name, value] of Object.entries(options)) {
    if (!Number.isInteger(value) || value < (name === "overlapTokens" ? 0 : 1)) {
      throw new Error(`${name} must be ${name === "overlapTokens" ? "a non-negative" : "a positive"} integer.`);
    }
  }
  if (options.minTokens > options.targetTokens || options.targetTokens > options.maxTokens) {
    throw new Error("Token limits must satisfy minTokens <= targetTokens <= maxTokens.");
  }
  if (options.overlapTokens >= options.targetTokens) {
    throw new Error("overlapTokens must be smaller than targetTokens.");
  }
}

async function writeResults(options: {
  outputFile: string;
  reportFile: string;
  chunks: ChunkRecord[];
  report: ChunkReport;
  force: boolean;
}): Promise<void> {
  const outputTemp = temporarySibling(options.outputFile);
  const reportTemp = temporarySibling(options.reportFile);
  await Promise.all([
    mkdir(dirname(options.outputFile), { recursive: true }),
    mkdir(dirname(options.reportFile), { recursive: true }),
    rm(outputTemp, { force: true }),
    rm(reportTemp, { force: true }),
  ]);
  try {
    const jsonl = options.chunks.length
      ? `${options.chunks.map((chunk) => JSON.stringify(chunk)).join("\n")}\n`
      : "";
    await Promise.all([
      writeFile(outputTemp, jsonl, "utf8"),
      writeFile(reportTemp, `${JSON.stringify(options.report, null, 2)}\n`, "utf8"),
    ]);
    await publishFiles(
      [
        { temporary: outputTemp, target: options.outputFile },
        { temporary: reportTemp, target: options.reportFile },
      ],
      options.force,
    );
  } catch (error) {
    await Promise.all([rm(outputTemp, { force: true }), rm(reportTemp, { force: true })]);
    throw error;
  }
}

async function requireFile(path: string, label: string): Promise<void> {
  const fileStats = await stat(path).catch(() => null);
  if (!fileStats?.isFile()) throw new Error(`${label} does not exist or is not a file: ${path}`);
}

async function assertTargetsAvailable(targets: string[], force: boolean): Promise<void> {
  if (force) return;
  for (const target of targets) {
    if (await pathExists(target)) {
      throw new Error(`Output already exists: ${target}. Use --force to replace it.`);
    }
  }
}

async function publishFiles(
  files: Array<{ temporary: string; target: string }>,
  force: boolean,
): Promise<void> {
  const backups: Array<{ backup: string; target: string }> = [];
  const published: string[] = [];
  try {
    if (force) {
      for (const file of files) {
        if (!(await pathExists(file.target))) continue;
        const backup = `${file.target}.backup-${process.pid}-${Date.now()}`;
        await rename(file.target, backup);
        backups.push({ backup, target: file.target });
      }
    }
    for (const file of files) {
      await rename(file.temporary, file.target);
      published.push(file.target);
    }
    await Promise.all(backups.map(({ backup }) => rm(backup, { force: true })));
  } catch (error) {
    await Promise.all(published.map((target) => rm(target, { force: true })));
    for (const { backup, target } of backups.reverse()) {
      if (await pathExists(backup)) await rename(backup, target);
    }
    throw error;
  }
}

function temporarySibling(target: string): string {
  return join(dirname(target), `.${basename(target)}.tmp-${process.pid}-${Date.now()}`);
}

async function pathExists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

function assertRelativeLocalPath(
  value: string,
  label: string,
  allowParentTraversal = false,
): void {
  const normalized = value.replace(/\\/gu, "/");
  if (
    !normalized ||
    isAbsolute(normalized) ||
    /^[a-z]:\//iu.test(normalized) ||
    normalized.startsWith("file://") ||
    (!allowParentTraversal && (normalized === ".." || normalized.startsWith("../")))
  ) {
    throw new Error(`${label} must be a relative path: ${value}`);
  }
}

function decodeHtmlEntities(value: string): string {
  return value.replace(/&amp;/giu, "&").replace(/&quot;/giu, '"').replace(/&apos;/giu, "'");
}

function countCharacter(value: string, character: string): number {
  return [...value].filter((item) => item === character).length;
}

function sha256(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}
