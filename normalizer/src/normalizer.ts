import { createHash } from "node:crypto";
import {
  access,
  mkdir,
  readFile,
  readdir,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import {
  basename,
  dirname,
  extname,
  isAbsolute,
  join,
  relative,
  resolve,
} from "node:path";
import { unified } from "unified";
import remarkParse from "remark-parse";
import { visit } from "unist-util-visit";
import { parse as parseYaml } from "yaml";
import {
  sanitizeControlCharacters,
  type ControlCharacterReplacement,
} from "./text.js";
import { toPosixPath } from "./path.js";

export type DocumentType = "markdown" | "external_link" | "short_note" | "empty";

export interface NormalizedAsset {
  type: "image";
  path: string;
  alt: string;
}

export interface NormalizedMetadata {
  title: string | null;
  source: string;
  page_id: string | null;
  parent_page_id: string | null;
  root_page_id: string | null;
  depth: number;
  ancestor_ids: string[];
  breadcrumbs: string[];
  context_text: string;
  document_type: DocumentType;
  indexable: boolean;
  external_urls: string[];
  assets: NormalizedAsset[];
  character_count: number;
  word_count: number;
  heading_count: number;
  external_url_count: number;
  asset_count: number;
  duplicate_of?: string;
  version: string | null;
  date: string | null;
  category: string | null;
  status: string;
  language: string;
}

export interface NormalizedDocument {
  document_id: string;
  parent_document_id: string | null;
  content: string;
  metadata: NormalizedMetadata;
  content_hash: string;
}

export interface DocumentTreeNode {
  node_id: string;
  parent_node_id: string | null;
  page_id: string | null;
  title: string;
  depth: number;
  has_document: boolean;
  source: string | null;
}

export interface DocumentTree {
  nodes: DocumentTreeNode[];
}

export interface NormalizationError {
  source: string;
  message: string;
}

export interface NormalizationControlCharacterWarning {
  source: string;
  document_id: string;
  character: string;
  count: number;
}

export interface NormalizationReport {
  found_files: number;
  tree_nodes: number;
  virtual_nodes: number;
  processed_documents: number;
  root_documents: number;
  max_depth: number;
  empty_files: number;
  short_documents: number;
  external_link_documents: number;
  images: number;
  external_links: number;
  duplicate_files: number;
  exact_duplicates: number;
  files_without_page_id: number;
  files_without_title: number;
  tree_errors: number;
  processing_errors: number;
  errors: number;
  control_character_replacements: number;
  control_character_warnings: NormalizationControlCharacterWarning[];
  tree_error_files: NormalizationError[];
  error_files: NormalizationError[];
}

export interface NormalizeDirectoryOptions {
  inputDir: string;
  outputFile: string;
  treeFile?: string;
  reportFile?: string;
  shortNoteThreshold?: number;
  force?: boolean;
  onProgress?: (message: string) => void;
}

export interface NormalizationResult {
  outputFile: string;
  treeFile: string;
  reportFile: string;
  report: NormalizationReport;
}

interface ParsedFrontMatter {
  attributes: Record<string, unknown>;
  markdown: string;
}

interface SyntaxNode {
  type?: string;
  depth?: number;
  value?: string;
  alt?: string;
  url?: string;
  children?: SyntaxNode[];
  position?: {
    start: { line: number };
    end: { line: number };
  };
}

interface MarkdownAnalysis {
  externalUrls: string[];
  assets: NormalizedAsset[];
  semanticText: string;
  wordCount: number;
  headingCount: number;
}

interface ParsedMarkdownDocument {
  attributes: Record<string, unknown>;
  content: string;
  title: string | null;
  version: string | null;
  date: string | null;
  category: string | null;
  status: string;
  language: string;
  contentHash: string;
  documentType: DocumentType;
  indexable: boolean;
  analysis: MarkdownAnalysis;
  controlReplacements: ControlCharacterReplacement[];
}

interface DiscoveredFile {
  absolutePath: string;
  source: string;
  pageId: string | null;
  nodeId: string;
  conflict?: string;
}

interface MutableTreeNode {
  nodeId: string;
  parentNodeId: string | null;
  pageId: string | null;
  title: string;
  fallbackTitle: string;
  structuralSource: string;
  hasDocument: boolean;
  source: string | null;
  depth: number;
  ancestorNodeIds: string[];
  ancestorIds: string[];
  breadcrumbs: string[];
  rootPageId: string | null;
}

interface DiscoveryResult {
  files: DiscoveredFile[];
  nodes: Map<string, MutableTreeNode>;
  treeErrors: NormalizationError[];
}

interface PendingDocument {
  file: DiscoveredFile;
  parsed: ParsedMarkdownDocument;
}

const PAGE_ID_PATTERN = /--(\d+)(?:\.md)?$/iu;
const DEFAULT_SHORT_NOTE_THRESHOLD = 200;

export async function normalizeDirectory(
  options: NormalizeDirectoryOptions,
): Promise<NormalizationResult> {
  const inputDir = resolve(options.inputDir);
  const outputFile = resolve(options.outputFile);
  const treeFile = resolve(
    options.treeFile ?? join(dirname(outputFile), "document_tree.json"),
  );
  const reportFile = resolve(
    options.reportFile ?? join(dirname(outputFile), "report.json"),
  );
  const shortNoteThreshold = options.shortNoteThreshold ?? DEFAULT_SHORT_NOTE_THRESHOLD;
  const log = options.onProgress ?? (() => undefined);

  if (!Number.isInteger(shortNoteThreshold) || shortNoteThreshold < 1) {
    throw new Error("The short note threshold must be a positive integer.");
  }
  if (new Set([outputFile, treeFile, reportFile]).size !== 3) {
    throw new Error("The JSONL, tree, and report paths must be different.");
  }
  const inputStats = await stat(inputDir).catch(() => null);
  if (!inputStats?.isDirectory()) {
    throw new Error(`Input directory does not exist or is not a directory: ${inputDir}`);
  }
  await assertTargetsAvailable([outputFile, treeFile, reportFile], Boolean(options.force));

  const discovery = await discoverContent(inputDir);
  const report = createReport(discovery.files.length, discovery.treeErrors);
  report.files_without_page_id = discovery.files.filter((file) => file.pageId === null).length;
  const pending: PendingDocument[] = [];

  for (const [index, file] of discovery.files.entries()) {
    if (file.conflict) {
      report.error_files.push({ source: file.source, message: file.conflict });
      log(`[${index + 1}/${discovery.files.length}] error: ${file.source}: ${file.conflict}`);
      continue;
    }
    try {
      const parsed = await readAndNormalizeMarkdown(file.absolutePath, shortNoteThreshold);
      const node = discovery.nodes.get(file.nodeId);
      if (!node) throw new Error(`Tree node '${file.nodeId}' was not found.`);
      if (parsed.title) node.title = parsed.title;

      validateYamlParent(parsed.attributes, node, discovery.nodes, report.tree_error_files);
      if (parsed.title === null) report.files_without_title += 1;
      report.images += parsed.analysis.assets.length;
      report.external_links += parsed.analysis.externalUrls.length;
      for (const replacement of parsed.controlReplacements) {
        report.control_character_replacements += replacement.count;
        report.control_character_warnings.push({
          source: file.source,
          document_id: file.nodeId,
          character: replacement.character,
          count: replacement.count,
        });
      }

      if (parsed.documentType === "empty") {
        report.empty_files += 1;
        log(`[${index + 1}/${discovery.files.length}] empty: ${file.source}`);
        continue;
      }
      pending.push({ file, parsed });
      log(`[${index + 1}/${discovery.files.length}] normalized: ${file.source}`);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      report.error_files.push({ source: file.source, message });
      log(`[${index + 1}/${discovery.files.length}] error: ${file.source}: ${message}`);
    }
  }

  resolveTree(discovery.nodes, report.tree_error_files);
  validateTree(discovery.nodes, report.tree_error_files);
  const documents = pending.map(({ file, parsed }) =>
    createDocument(file, parsed, requireNode(discovery.nodes, file.nodeId)),
  );
  annotateDuplicates(documents, report);

  report.processed_documents = documents.length;
  report.root_documents = documents.filter((document) => document.metadata.depth === 0).length;
  report.short_documents = documents.filter(
    (document) => document.metadata.document_type === "short_note",
  ).length;
  report.external_link_documents = documents.filter(
    (document) => document.metadata.document_type === "external_link",
  ).length;
  report.tree_nodes = discovery.nodes.size;
  report.virtual_nodes = [...discovery.nodes.values()].filter(
    (node) => !node.hasDocument,
  ).length;
  report.max_depth = Math.max(0, ...[...discovery.nodes.values()].map((node) => node.depth));
  report.tree_errors = report.tree_error_files.length;
  report.processing_errors = report.error_files.length;
  report.errors = report.processing_errors;

  const tree: DocumentTree = {
    nodes: orderTreeNodes(discovery.nodes).map((node) => ({
      node_id: node.nodeId,
      parent_node_id: node.parentNodeId,
      page_id: node.pageId,
      title: node.title,
      depth: node.depth,
      has_document: node.hasDocument,
      source: node.source,
    })),
  };

  const outputTemp = temporarySibling(outputFile);
  const treeTemp = temporarySibling(treeFile);
  const reportTemp = temporarySibling(reportFile);
  await Promise.all([
    mkdir(dirname(outputFile), { recursive: true }),
    mkdir(dirname(treeFile), { recursive: true }),
    mkdir(dirname(reportFile), { recursive: true }),
  ]);
  await Promise.all([
    rm(outputTemp, { force: true }),
    rm(treeTemp, { force: true }),
    rm(reportTemp, { force: true }),
  ]);

  try {
    const jsonl = documents.length
      ? `${documents.map((document) => JSON.stringify(document)).join("\n")}\n`
      : "";
    await Promise.all([
      writeFile(outputTemp, jsonl, "utf8"),
      writeFile(treeTemp, `${JSON.stringify(tree, null, 2)}\n`, "utf8"),
      writeFile(reportTemp, `${JSON.stringify(report, null, 2)}\n`, "utf8"),
    ]);
    await publishFiles(
      [
        { temporary: outputTemp, target: outputFile },
        { temporary: treeTemp, target: treeFile },
        { temporary: reportTemp, target: reportFile },
      ],
      Boolean(options.force),
    );
    return { outputFile, treeFile, reportFile, report };
  } catch (error) {
    await Promise.all([
      rm(outputTemp, { force: true }),
      rm(treeTemp, { force: true }),
      rm(reportTemp, { force: true }),
    ]);
    throw error;
  }
}

export async function normalizeMarkdownFile(
  filePath: string,
  source: string,
  shortNoteThreshold = DEFAULT_SHORT_NOTE_THRESHOLD,
): Promise<NormalizedDocument> {
  const normalizedSource = normalizeSource(source);
  const pageId = extractPageId(basename(normalizedSource));
  const nodeId = pageId ? `page_${pageId}` : stableDocumentId(normalizedSource);
  const parsed = await readAndNormalizeMarkdown(filePath, shortNoteThreshold);
  const title = parsed.title ?? cleanNodeTitle(basename(normalizedSource));
  const node: MutableTreeNode = {
    nodeId,
    parentNodeId: null,
    pageId,
    title,
    fallbackTitle: title,
    structuralSource: normalizedSource.replace(/\.md$/iu, ""),
    hasDocument: true,
    source: normalizedSource,
    depth: 0,
    ancestorNodeIds: [],
    ancestorIds: [],
    breadcrumbs: [title],
    rootPageId: pageId,
  };
  const file: DiscoveredFile = {
    absolutePath: filePath,
    source: normalizedSource,
    pageId,
    nodeId,
  };
  return createDocument(file, parsed, node);
}

export function parseFrontMatter(input: string): ParsedFrontMatter {
  const normalized = normalizeLineEndings(input);
  const lines = normalized.split("\n");
  if (lines[0] !== "---") return { attributes: {}, markdown: normalized };

  const closingIndex = lines.findIndex(
    (line, index) => index > 0 && (line === "---" || line === "..."),
  );
  if (closingIndex < 0) {
    throw new Error("YAML Front Matter is missing a closing delimiter.");
  }

  const yamlSource = lines.slice(1, closingIndex).join("\n");
  let parsed: unknown;
  try {
    parsed = parseYaml(yamlSource);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`Invalid YAML Front Matter: ${singleLine(message)}`);
  }
  if (parsed === null || parsed === undefined) parsed = {};
  if (!isPlainRecord(parsed)) {
    throw new Error("Invalid YAML Front Matter: the root value must be an object.");
  }

  return {
    attributes: parsed,
    markdown: lines.slice(closingIndex + 1).join("\n"),
  };
}

export function normalizeMarkdown(markdown: string): string {
  const withLf = normalizeLineEndings(markdown);
  const lines = withLf.split("\n");
  const protectedLines = codeBlockLines(withLf);
  const output: string[] = [];
  let blankRun = 0;

  for (const [index, originalLine] of lines.entries()) {
    if (protectedLines.has(index + 1)) {
      output.push(originalLine);
      blankRun = 0;
      continue;
    }

    const line = decodeEntitiesInExternalUrls(originalLine.replace(/[ \t]+$/u, ""));
    if (line.length === 0) {
      blankRun += 1;
      if (blankRun <= 2) output.push("");
    } else {
      blankRun = 0;
      output.push(line);
    }
  }

  while (output[0] === "") output.shift();
  while (output.at(-1) === "") output.pop();
  return output.join("\n");
}

export function extractFirstH1(markdown: string): string | null {
  if (!markdown) return null;
  const tree = unified().use(remarkParse).parse(markdown);
  let title: string | null = null;
  visit(tree, "heading", (rawNode) => {
    const node = rawNode as SyntaxNode;
    if (title !== null || node.depth !== 1) return;
    const value = syntaxText(node).trim();
    if (value) title = value;
  });
  return title;
}

export function extractPageId(name: string): string | null {
  return name.match(PAGE_ID_PATTERN)?.[1] ?? null;
}

async function readAndNormalizeMarkdown(
  filePath: string,
  shortNoteThreshold: number,
): Promise<ParsedMarkdownDocument> {
  const bytes = await readFile(filePath);
  let decoded: string;
  try {
    decoded = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw new Error("File is not valid UTF-8.");
  }

  const { attributes, markdown } = parseFrontMatter(decoded.replace(/^\uFEFF/, ""));
  const sanitized = sanitizeControlCharacters(markdown);
  const content = normalizeMarkdown(sanitized.value);
  const analysis = analyzeMarkdown(content);
  const usefulCharacterCount = [...analysis.semanticText].length;
  let documentType: DocumentType;
  if (usefulCharacterCount === 0 && analysis.externalUrls.length === 0) {
    documentType = "empty";
  } else if (usefulCharacterCount === 0) {
    documentType = "external_link";
  } else if (usefulCharacterCount < shortNoteThreshold) {
    documentType = "short_note";
  } else {
    documentType = "markdown";
  }

  return {
    attributes,
    content,
    title: optionalMetadataString(attributes, "title") || extractFirstH1(content),
    version: optionalMetadataString(attributes, "version"),
    date: optionalMetadataString(attributes, "date"),
    category: optionalMetadataString(attributes, "category"),
    status: optionalMetadataString(attributes, "status") || "active",
    language: optionalMetadataString(attributes, "language") || "ru",
    contentHash: sha256(content),
    documentType,
    indexable: documentType !== "external_link" && documentType !== "empty",
    analysis,
    controlReplacements: sanitized.replacements,
  };
}

function createDocument(
  file: DiscoveredFile,
  parsed: ParsedMarkdownDocument,
  node: MutableTreeNode,
): NormalizedDocument {
  return {
    document_id: node.nodeId,
    parent_document_id: node.parentNodeId,
    content: parsed.content,
    metadata: {
      title: parsed.title,
      source: file.source,
      page_id: node.pageId,
      parent_page_id: parentPageId(node),
      root_page_id: node.rootPageId,
      depth: node.depth,
      ancestor_ids: [...node.ancestorIds],
      breadcrumbs: [...node.breadcrumbs],
      context_text: node.breadcrumbs.join(" → "),
      document_type: parsed.documentType,
      indexable: parsed.indexable,
      external_urls: [...parsed.analysis.externalUrls],
      assets: parsed.analysis.assets.map((asset) => ({ ...asset })),
      character_count: [...parsed.content].length,
      word_count: parsed.analysis.wordCount,
      heading_count: parsed.analysis.headingCount,
      external_url_count: parsed.analysis.externalUrls.length,
      asset_count: parsed.analysis.assets.length,
      version: parsed.version,
      date: parsed.date,
      category: parsed.category,
      status: parsed.status,
      language: parsed.language,
    },
    content_hash: parsed.contentHash,
  };
}

async function discoverContent(root: string): Promise<DiscoveryResult> {
  const directorySources: string[] = [];
  const rawFiles: Array<{ absolutePath: string; source: string }> = [];

  async function walk(directory: string): Promise<void> {
    const entries = await readdir(directory, { withFileTypes: true });
    entries.sort((left, right) => left.name.localeCompare(right.name, "en"));
    for (const entry of entries) {
      const absolutePath = join(directory, entry.name);
      const source = normalizeSource(relative(root, absolutePath));
      if (entry.isDirectory()) {
        directorySources.push(source);
        await walk(absolutePath);
      } else if (entry.isFile() && extname(entry.name).toLowerCase() === ".md") {
        rawFiles.push({ absolutePath, source });
      }
    }
  }

  await walk(root);
  directorySources.sort(compareSources);
  rawFiles.sort((left, right) => compareSources(left.source, right.source));

  const filesBySource = new Map(rawFiles.map((file) => [file.source, file]));
  const nodes = new Map<string, MutableTreeNode>();
  const nodeByDirectory = new Map<string, string>();
  const treeErrors: NormalizationError[] = [];

  for (const source of directorySources.sort(compareByDepthThenSource)) {
    const pageId = extractPageId(basename(source));
    const counterpart = filesBySource.get(`${source}.md`);
    const nodeId = pageId
      ? `page_${pageId}`
      : counterpart
        ? stableDocumentId(counterpart.source)
        : `node_${sha256(`source:${source}`)}`;
    const parentSource = parentDirectorySource(source);
    const parentNodeId = parentSource ? nodeByDirectory.get(parentSource) ?? null : null;
    const existing = nodes.get(nodeId);
    if (existing) {
      treeErrors.push({
        source,
        message: `Duplicate node ID '${nodeId}' also belongs to '${existing.structuralSource}'.`,
      });
    } else {
      const fallbackTitle = cleanNodeTitle(basename(source));
      nodes.set(nodeId, {
        nodeId,
        parentNodeId,
        pageId,
        title: fallbackTitle,
        fallbackTitle,
        structuralSource: source,
        hasDocument: false,
        source: null,
        depth: 0,
        ancestorNodeIds: [],
        ancestorIds: [],
        breadcrumbs: [],
        rootPageId: null,
      });
    }
    nodeByDirectory.set(source, nodeId);
  }

  const files: DiscoveredFile[] = rawFiles.map((rawFile) => {
    const pageId = extractPageId(basename(rawFile.source));
    const nodeId = pageId ? `page_${pageId}` : stableDocumentId(rawFile.source);
    const parentSource = parentDirectorySource(rawFile.source);
    const parentNodeId = parentSource ? nodeByDirectory.get(parentSource) ?? null : null;
    const counterpartSource = rawFile.source.replace(/\.md$/iu, "");
    const existing = nodes.get(nodeId);
    let conflict: string | undefined;

    if (existing) {
      if (existing.structuralSource !== counterpartSource || existing.hasDocument) {
        conflict = `Duplicate document ID '${nodeId}' also belongs to '${existing.source ?? existing.structuralSource}'.`;
        treeErrors.push({ source: rawFile.source, message: conflict });
      } else {
        if (existing.parentNodeId !== parentNodeId) {
          treeErrors.push({
            source: rawFile.source,
            message: `Folder and document disagree about the parent of '${nodeId}'.`,
          });
        }
        existing.hasDocument = true;
        existing.source = rawFile.source;
      }
    } else {
      const fallbackTitle = cleanNodeTitle(basename(rawFile.source));
      nodes.set(nodeId, {
        nodeId,
        parentNodeId,
        pageId,
        title: fallbackTitle,
        fallbackTitle,
        structuralSource: counterpartSource,
        hasDocument: true,
        source: rawFile.source,
        depth: 0,
        ancestorNodeIds: [],
        ancestorIds: [],
        breadcrumbs: [],
        rootPageId: null,
      });
    }

    return {
      ...rawFile,
      pageId,
      nodeId,
      ...(conflict ? { conflict } : {}),
    };
  });

  return { files, nodes, treeErrors };
}

function resolveTree(
  nodes: Map<string, MutableTreeNode>,
  errors: NormalizationError[],
): void {
  for (const node of nodes.values()) {
    if (node.parentNodeId === node.nodeId) {
      errors.push({ source: node.structuralSource, message: "A node cannot be its own parent." });
      node.parentNodeId = null;
    } else if (node.parentNodeId && !nodes.has(node.parentNodeId)) {
      errors.push({
        source: node.structuralSource,
        message: `Parent node '${node.parentNodeId}' does not exist.`,
      });
      node.parentNodeId = null;
    }
  }

  const state = new Map<string, "visiting" | "resolved">();
  const resolveNode = (node: MutableTreeNode): void => {
    if (state.get(node.nodeId) === "resolved") return;
    if (state.get(node.nodeId) === "visiting") {
      errors.push({ source: node.structuralSource, message: "A cycle was detected in the tree." });
      node.parentNodeId = null;
      return;
    }
    state.set(node.nodeId, "visiting");

    const parent = node.parentNodeId ? nodes.get(node.parentNodeId) : undefined;
    if (parent) resolveNode(parent);
    const resolvedParent = node.parentNodeId ? nodes.get(node.parentNodeId) : undefined;
    if (resolvedParent) {
      node.depth = resolvedParent.depth + 1;
      node.ancestorNodeIds = [...resolvedParent.ancestorNodeIds, resolvedParent.nodeId];
      node.ancestorIds = [
        ...resolvedParent.ancestorIds,
        resolvedParent.pageId ?? resolvedParent.nodeId,
      ];
      node.breadcrumbs = [...resolvedParent.breadcrumbs, node.title];
      node.rootPageId = resolvedParent.rootPageId;
    } else {
      node.depth = 0;
      node.ancestorNodeIds = [];
      node.ancestorIds = [];
      node.breadcrumbs = [node.title];
      node.rootPageId = node.pageId;
    }
    state.set(node.nodeId, "resolved");
  };

  for (const node of nodes.values()) resolveNode(node);
}

function validateTree(
  nodes: Map<string, MutableTreeNode>,
  errors: NormalizationError[],
): void {
  const seenSources = new Set<string>();
  for (const node of nodes.values()) {
    if (node.source && seenSources.has(node.source)) {
      errors.push({ source: node.source, message: "A document source is assigned twice." });
    }
    if (node.source) seenSources.add(node.source);
    if (node.parentNodeId) {
      const parent = nodes.get(node.parentNodeId);
      if (!parent) continue;
      if (node.depth !== parent.depth + 1) {
        errors.push({ source: node.structuralSource, message: "The node depth is invalid." });
      }
      if (node.ancestorNodeIds.at(-1) !== parent.nodeId) {
        errors.push({ source: node.structuralSource, message: "The ancestor order is invalid." });
      }
      if (node.breadcrumbs.at(-1) !== node.title) {
        errors.push({ source: node.structuralSource, message: "The breadcrumb order is invalid." });
      }
    }
    if (node.source && isLocalAbsolutePath(node.source)) {
      errors.push({ source: node.source, message: "An absolute document source is not allowed." });
    }
  }
}

function validateYamlParent(
  attributes: Record<string, unknown>,
  node: MutableTreeNode,
  nodes: Map<string, MutableTreeNode>,
  errors: NormalizationError[],
): void {
  const yamlParent = normalizeParentId(optionalMetadataString(attributes, "parent_id"));
  const treeParent = node.parentNodeId ? nodes.get(node.parentNodeId)?.pageId ?? null : null;
  if (yamlParent !== null && yamlParent !== treeParent) {
    errors.push({
      source: node.source ?? node.structuralSource,
      message: `YAML parent_id '${yamlParent}' does not match tree parent '${treeParent ?? "null"}'.`,
    });
  }
}

function analyzeMarkdown(markdown: string): MarkdownAnalysis {
  if (!markdown) {
    return { externalUrls: [], assets: [], semanticText: "", wordCount: 0, headingCount: 0 };
  }
  const tree = unified().use(remarkParse).parse(markdown) as SyntaxNode;
  const externalUrls: string[] = [];
  const seenUrls = new Set<string>();
  const assets: NormalizedAsset[] = [];
  const semanticParts: string[] = [];
  let headingCount = 0;

  const addUrl = (rawUrl: string): void => {
    const url = cleanExternalUrl(rawUrl);
    if (!url || seenUrls.has(url)) return;
    seenUrls.add(url);
    externalUrls.push(url);
  };

  const walk = (node: SyntaxNode): void => {
    if (node.type === "code" || node.type === "inlineCode") {
      if (node.value) semanticParts.push(node.value);
      return;
    }
    if (node.type === "image") {
      const assetPath = normalizeAssetPath(node.url ?? "");
      assets.push({ type: "image", path: assetPath, alt: node.alt ?? "" });
      return;
    }
    if (node.type === "link") {
      if (node.url) addUrl(node.url);
      for (const child of node.children ?? []) walk(child);
      return;
    }
    if (node.type === "heading") headingCount += 1;
    if (node.type === "text") {
      const value = node.value ?? "";
      for (const url of findExternalUrls(value)) addUrl(url);
      semanticParts.push(removeExternalUrls(value));
      return;
    }
    if (node.type === "html") {
      const value = node.value ?? "";
      for (const url of findExternalUrls(value)) addUrl(url);
      semanticParts.push(removeExternalUrls(value).replace(/<[^>]*>/gu, " "));
      return;
    }
    for (const child of node.children ?? []) walk(child);
  };

  walk(tree);
  const semanticText = semanticParts.join(" ").replace(/\s+/gu, " ").trim();
  const words = semanticText.match(/[\p{L}\p{N}]+(?:[’'-][\p{L}\p{N}]+)*/gu) ?? [];
  return { externalUrls, assets, semanticText, wordCount: words.length, headingCount };
}

function annotateDuplicates(
  documents: NormalizedDocument[],
  report: NormalizationReport,
): void {
  const firstByHash = new Map<string, string>();
  for (const document of documents) {
    const first = firstByHash.get(document.content_hash);
    if (first) {
      document.metadata.duplicate_of = first;
      report.duplicate_files += 1;
      report.exact_duplicates += 1;
    } else {
      firstByHash.set(document.content_hash, document.document_id);
    }
  }
}

function orderTreeNodes(nodes: Map<string, MutableTreeNode>): MutableTreeNode[] {
  const children = new Map<string | null, MutableTreeNode[]>();
  for (const node of nodes.values()) {
    const siblings = children.get(node.parentNodeId) ?? [];
    siblings.push(node);
    children.set(node.parentNodeId, siblings);
  }
  for (const siblings of children.values()) {
    siblings.sort((left, right) => compareSources(left.structuralSource, right.structuralSource));
  }
  const ordered: MutableTreeNode[] = [];
  const seen = new Set<string>();
  const append = (node: MutableTreeNode): void => {
    if (seen.has(node.nodeId)) return;
    seen.add(node.nodeId);
    ordered.push(node);
    for (const child of children.get(node.nodeId) ?? []) append(child);
  };
  for (const root of children.get(null) ?? []) append(root);
  for (const node of nodes.values()) append(node);
  return ordered;
}

function createReport(
  foundFiles: number,
  treeErrors: NormalizationError[],
): NormalizationReport {
  return {
    found_files: foundFiles,
    tree_nodes: 0,
    virtual_nodes: 0,
    processed_documents: 0,
    root_documents: 0,
    max_depth: 0,
    empty_files: 0,
    short_documents: 0,
    external_link_documents: 0,
    images: 0,
    external_links: 0,
    duplicate_files: 0,
    exact_duplicates: 0,
    files_without_page_id: 0,
    files_without_title: 0,
    tree_errors: 0,
    processing_errors: 0,
    errors: 0,
    control_character_replacements: 0,
    control_character_warnings: [],
    tree_error_files: [...treeErrors],
    error_files: [],
  };
}

function codeBlockLines(markdown: string): Set<number> {
  const protectedLines = new Set<number>();
  if (!markdown) return protectedLines;
  const tree = unified().use(remarkParse).parse(markdown);
  visit(tree, "code", (rawNode) => {
    const node = rawNode as SyntaxNode;
    const start = node.position?.start.line;
    const end = node.position?.end.line;
    if (!start || !end) return;
    for (let line = start; line <= end; line += 1) protectedLines.add(line);
  });
  return protectedLines;
}

function syntaxText(node: SyntaxNode): string {
  if (typeof node.value === "string") return node.value;
  if (typeof node.alt === "string") return node.alt;
  return node.children?.map(syntaxText).join("") ?? "";
}

function stableDocumentId(source: string): string {
  return `doc_${sha256(`source:${normalizeSource(source)}`)}`;
}

function optionalMetadataString(
  attributes: Record<string, unknown>,
  field: string,
): string | null {
  const value = attributes[field];
  if (value === null || value === undefined) return null;
  if (typeof value === "string") return value.trim() || null;
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return String(value);
  }
  if (value instanceof Date) return value.toISOString();
  throw new Error(`YAML field '${field}' must be a scalar value or null.`);
}

function cleanNodeTitle(name: string): string {
  const withoutExtension = name.replace(/\.md$/iu, "");
  const withoutId = withoutExtension.replace(/--\d+$/u, "");
  const spaced = withoutId
    .replace(/(?<=\d)-(?=\d)/gu, "–")
    .replace(/[-_]+/gu, " ")
    .replace(/\s+/gu, " ")
    .trim();
  if (!spaced) return withoutExtension;
  return `${spaced.charAt(0).toLocaleUpperCase("ru-RU")}${spaced.slice(1)}`;
}

function normalizeAssetPath(rawPath: string): string {
  const decoded = decodeHtmlEntities(rawPath.trim()).replace(/\\/gu, "/");
  if (!decoded) throw new Error("A Markdown image has an empty path.");
  if (isLocalAbsolutePath(decoded)) {
    throw new Error(`Absolute image paths are not allowed: ${decoded}`);
  }
  return decoded;
}

function isLocalAbsolutePath(value: string): boolean {
  return isAbsolute(value) || /^[a-z]:\//iu.test(value) || value.startsWith("file://");
}

function findExternalUrls(value: string): string[] {
  return [...value.matchAll(/https?:\/\/[^\s<>"'`]+/giu)]
    .map((match) => cleanExternalUrl(match[0]))
    .filter((url): url is string => url !== null);
}

function removeExternalUrls(value: string): string {
  return value.replace(/https?:\/\/[^\s<>"'`]+/giu, " ");
}

function cleanExternalUrl(rawUrl: string): string | null {
  let url = decodeHtmlEntities(rawUrl.trim());
  url = url.replace(/[.,;:!?]+$/gu, "");
  while (url.endsWith(")") && countCharacter(url, ")") > countCharacter(url, "(")) {
    url = url.slice(0, -1);
  }
  url = url.replace(/\]+$/gu, "");
  return /^https?:\/\//iu.test(url) ? url : null;
}

function decodeEntitiesInExternalUrls(line: string): string {
  return line.replace(/https?:\/\/[^\s<>"'`]+/giu, (url) => decodeHtmlEntities(url));
}

function decodeHtmlEntities(value: string): string {
  return value.replace(
    /&(#(?:x[0-9a-f]+|\d+)|amp|quot|apos|lt|gt);/giu,
    (entity, name: string) => {
      const normalized = name.toLowerCase();
      if (normalized === "amp") return "&";
      if (normalized === "quot") return '"';
      if (normalized === "apos") return "'";
      if (normalized === "lt") return "<";
      if (normalized === "gt") return ">";
      const numeric = normalized.startsWith("#x")
        ? Number.parseInt(normalized.slice(2), 16)
        : Number.parseInt(normalized.slice(1), 10);
      return Number.isSafeInteger(numeric) ? String.fromCodePoint(numeric) : entity;
    },
  );
}

function countCharacter(value: string, character: string): number {
  return [...value].filter((item) => item === character).length;
}

function normalizeParentId(value: string | null): string | null {
  return value === null || value === "0" ? null : value;
}

function parentDirectorySource(source: string): string | null {
  const parent = toPosixPath(dirname(source));
  return parent === "." || parent === "" ? null : parent;
}

function normalizeSource(source: string): string {
  const normalized = toPosixPath(source).replace(/^\.\//u, "");
  if (!normalized || isLocalAbsolutePath(normalized) || normalized === ".." || normalized.startsWith("../")) {
    throw new Error(`Source must be a relative path inside the input directory: ${source}`);
  }
  return normalized;
}

function compareSources(left: string, right: string): number {
  return left.localeCompare(right, "en");
}

function compareByDepthThenSource(left: string, right: string): number {
  const depthDifference = left.split("/").length - right.split("/").length;
  return depthDifference || compareSources(left, right);
}

function requireNode(
  nodes: Map<string, MutableTreeNode>,
  nodeId: string,
): MutableTreeNode {
  const node = nodes.get(nodeId);
  if (!node) throw new Error(`Tree node '${nodeId}' was not found.`);
  return node;
}

function parentPageId(node: MutableTreeNode): string | null {
  if (!node.parentNodeId) return null;
  const parentIdentity = node.ancestorIds.at(-1);
  return parentIdentity && /^\d+$/u.test(parentIdentity) ? parentIdentity : null;
}

function normalizeLineEndings(value: string): string {
  return value.replace(/\r\n?/g, "\n");
}

function sha256(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}

function singleLine(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function assertTargetsAvailable(targets: string[], force: boolean): Promise<void> {
  if (force) return;
  for (const target of targets) {
    if (await pathExists(target)) {
      throw new Error(`Output already exists: ${target}. Use --force to replace it.`);
    }
  }
}

function temporarySibling(target: string): string {
  return join(dirname(target), `.${basename(target)}.tmp-${process.pid}-${Date.now()}`);
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

async function pathExists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}
