import { mkdir, rename, unlink, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const DEFAULT_DELAY_MS = 750;
const MAX_ATTEMPTS = 3;
const REQUEST_TIMEOUT_MS = 30_000;
const WORKSPACE_PAGE_SIZE = 200;
const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(scriptDirectory, "..");
const defaultOutputPath = join(repositoryRoot, "reports", "bitrix-content-audit.json");

const sleep = (delayMs) => new Promise((resolveSleep) => setTimeout(resolveSleep, delayMs));

export function buildApiConfig(webhookUrl) {
  let parsed;
  try {
    parsed = new URL(String(webhookUrl ?? "").trim());
  } catch {
    throw new Error("BITRIX_WEBHOOK_URL must be a valid URL");
  }

  const segments = parsed.pathname.split("/").filter(Boolean).map(decodeURIComponent);
  if (parsed.protocol !== "https:" || segments[0] !== "rest") {
    throw new Error("BITRIX_WEBHOOK_URL must be an HTTPS Bitrix REST webhook URL");
  }

  const credentialOffset = segments[1] === "api" ? 2 : 1;
  if (segments.length !== credentialOffset + 2) {
    throw new Error("BITRIX_WEBHOOK_URL must contain a user ID and webhook secret");
  }

  const userId = segments[credentialOffset];
  const secret = segments[credentialOffset + 1];
  const apiBase = new URL(
    `/rest/api/${encodeURIComponent(userId)}/${encodeURIComponent(secret)}/`,
    parsed.origin,
  );

  return { apiBase, portal: parsed.origin, secret };
}

class BitrixClient {
  constructor(webhookUrl, delayMs = DEFAULT_DELAY_MS) {
    const config = buildApiConfig(webhookUrl);
    this.apiBase = config.apiBase;
    this.portal = config.portal;
    this.secret = config.secret;
    this.delayMs = delayMs;
    this.nextRequestAt = 0;
  }

  redact(value) {
    return String(value).replaceAll(this.secret, "[REDACTED]");
  }

  async waitForRequestSlot() {
    const remaining = this.nextRequestAt - Date.now();
    if (remaining > 0) await sleep(remaining);
    this.nextRequestAt = Date.now() + this.delayMs;
  }

  async call(method, params) {
    const endpoint = new URL(method, this.apiBase);
    for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
      await this.waitForRequestSlot();

      let response;
      try {
        response = await fetch(endpoint, {
          method: "POST",
          headers: { Accept: "application/json", "Content-Type": "application/json" },
          body: JSON.stringify(params),
          signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
        });
      } catch (error) {
        if (attempt < MAX_ATTEMPTS) {
          await sleep(this.delayMs * 2 ** attempt);
          continue;
        }
        throw new Error(`${method}: network error: ${this.redact(error)}`);
      }

      const bodyText = await response.text();
      let body;
      try {
        body = JSON.parse(bodyText);
      } catch {
        if (attempt < MAX_ATTEMPTS && (response.status === 429 || response.status >= 500)) {
          await sleep(this.delayMs * 2 ** attempt);
          continue;
        }
        throw new Error(`${method}: Bitrix returned invalid JSON (HTTP ${response.status})`);
      }

      const apiError = decodeApiError(body, response.status);
      if (apiError) {
        if (attempt < MAX_ATTEMPTS && isRetryable(apiError, response.status)) {
          await sleep(this.delayMs * 2 ** attempt);
          continue;
        }
        throw new Error(this.redact(`${method}: ${apiError.message} (${apiError.code})`));
      }
      if (!("result" in body)) throw new Error(`${method}: Bitrix returned no result`);
      return body.result;
    }
    throw new Error(`${method}: retry attempts exhausted`);
  }

  async listWorkspaces() {
    const result = [];
    let cursor = null;
    const seenCursors = new Set();

    for (;;) {
      const pagination = { limit: WORKSPACE_PAGE_SIZE };
      if (cursor) pagination.afterCursor = cursor;
      const page = await this.call("note.collection.list", { pagination });
      result.push(...(Array.isArray(page.items) ? page.items : []));
      if (!page.nextCursor) return result;

      const cursorKey = JSON.stringify(page.nextCursor);
      if (seenCursors.has(cursorKey)) throw new Error("note.collection.list returned a repeated cursor");
      seenCursors.add(cursorKey);
      cursor = page.nextCursor;
    }
  }
}

function decodeApiError(body, status) {
  if (!body?.error && status >= 200 && status < 300) return null;
  if (typeof body?.error === "string") {
    return {
      code: body.error,
      message: body.error_description || `HTTP ${status}`,
    };
  }
  if (body?.error && typeof body.error === "object") {
    return {
      code: body.error.code || `HTTP_${status}`,
      message: body.error.message || body.error_description || `HTTP ${status}`,
    };
  }
  return { code: `HTTP_${status}`, message: body?.error_description || `HTTP ${status}` };
}

function isRetryable(error, status) {
  if (status === 429 || status >= 500) return true;
  return ["QUERY_LIMIT_EXCEEDED", "OPERATION_TIME_LIMIT", "OVERLOAD_LIMIT"].includes(
    String(error.code).toUpperCase(),
  );
}

function normalizeText(value) {
  return String(value)
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[\*_~`#]/g, "")
    .normalize("NFKC")
    .toLocaleLowerCase("ru")
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
}

function markdownHeadings(markdown) {
  const headings = new Set();
  for (const match of markdown.matchAll(/^#{1,6}[ \t]+(.+?)\s*#*\s*$/gm)) {
    const normalized = normalizeText(match[1]);
    if (normalized) headings.add(normalized);
  }
  return headings;
}

function localAnchorLabels(markdown, documentId) {
  const labels = new Map();
  for (const match of markdown.matchAll(/\[([^\]\n]+)\]\(([^)\s]+)\)/g)) {
    const target = match[2];
    let parsed;
    try {
      parsed = new URL(target, "https://bitrix.invalid/");
    } catch {
      continue;
    }
    const expectedPath = `/note/document/${documentId}/`;
    const isSameDocument =
      Boolean(parsed.hash) && (target.startsWith("#") || parsed.pathname === expectedPath);
    if (!isSameDocument) continue;

    const normalized = normalizeText(match[1]);
    if (normalized && !labels.has(normalized)) labels.set(normalized, match[1]);
  }
  return labels;
}

function hasUnclosedFence(markdown, marker) {
  return [...markdown.matchAll(new RegExp(`^${marker}`, "gm"))].length % 2 !== 0;
}

export function analyzeMarkdown(markdown, { documentId, isLeaf, workspaceTreeTruncated = false }) {
  const reasons = [];
  const trimmed = markdown.trimEnd();
  const lastLine = trimmed.split("\n").at(-1)?.trim() ?? "";
  const headings = markdownHeadings(markdown);
  const anchorLabels = localAnchorLabels(markdown, documentId);
  const missingTocSections = [];

  if (anchorLabels.size >= 2) {
    for (const [normalized, original] of anchorLabels) {
      if (!headings.has(normalized)) missingTocSections.push(original);
    }
    if (missingTocSections.length > 0) reasons.push("missing_toc_sections");
  }
  if (/^#{1,6}(?:\s+.*)?$/.test(lastLine)) reasons.push("ends_with_heading");
  if (/^(?:[-*+]|\d+[.)])\s*$/.test(lastLine)) reasons.push("ends_with_empty_list_item");
  if (hasUnclosedFence(markdown, "```") || hasUnclosedFence(markdown, "~~~")) {
    reasons.push("unclosed_fenced_code_block");
  }

  const characters = [...markdown].length;
  if (isLeaf && characters === 0) reasons.push("empty_leaf");
  else if (isLeaf && characters < 50) reasons.push("very_short_leaf");
  if (workspaceTreeTruncated) reasons.push("workspace_tree_truncated");

  return {
    characters,
    bytes: new TextEncoder().encode(markdown).length,
    suspicious: reasons.length > 0,
    reasons,
    ...(missingTocSections.length > 0 ? { missingTocSections } : {}),
  };
}

function flattenTree(nodes, workspaceId, result = []) {
  for (const node of Array.isArray(nodes) ? nodes : []) {
    const children = Array.isArray(node.children) ? node.children : [];
    result.push({
      id: node.id,
      workspaceId,
      parentId: node.parentId ?? null,
      title: node.title ?? "",
      isLeaf: children.length === 0,
    });
    flattenTree(children, workspaceId, result);
  }
  return result;
}

async function writeJsonAtomically(path, value) {
  await mkdir(dirname(path), { recursive: true });
  const temporaryPath = `${path}.tmp-${process.pid}`;
  try {
    await writeFile(temporaryPath, `${JSON.stringify(value, null, 2)}\n`, "utf8");
    await rename(temporaryPath, path);
  } catch (error) {
    await unlink(temporaryPath).catch(() => {});
    throw error;
  }
}

async function main() {
  const webhookUrl = process.env.BITRIX_WEBHOOK_URL?.trim();
  if (!webhookUrl) {
    throw new Error(
      "BITRIX_WEBHOOK_URL is not set; run the script from fetcher/ so Bun loads fetcher/.env",
    );
  }

  const startedAt = new Date().toISOString();
  const client = new BitrixClient(webhookUrl);
  console.log(`Loading workspaces from ${client.portal}...`);
  const sourceWorkspaces = await client.listWorkspaces();
  const workspaces = [];
  const tasks = [];
  let apiErrors = 0;

  for (const [index, workspace] of sourceWorkspaces.entries()) {
    console.log(`[${index + 1}/${sourceWorkspaces.length}] Loading tree: ${workspace.name} (${workspace.id})`);
    const workspaceReport = {
      id: workspace.id,
      name: workspace.name,
      policyLevel: workspace.policyLevel,
      truncated: false,
      suspicious: false,
      reasons: [],
      documents: [],
    };
    workspaces.push(workspaceReport);

    try {
      const tree = await client.call("note.document.tree.list", { collectionId: workspace.id });
      workspaceReport.truncated = Boolean(tree.truncated);
      if (workspaceReport.truncated) {
        workspaceReport.suspicious = true;
        workspaceReport.reasons.push("workspace_tree_truncated");
      }
      for (const task of flattenTree(tree.items, workspace.id)) {
        tasks.push({ ...task, workspaceReport });
      }
    } catch (error) {
      apiErrors += 1;
      workspaceReport.suspicious = true;
      workspaceReport.reasons.push("api_error");
      workspaceReport.error = client.redact(error);
    }
  }

  for (const [index, task] of tasks.entries()) {
    console.log(`[${index + 1}/${tasks.length}] ${task.title} (${task.id})`);
    try {
      const result = await client.call("note.document.get", { id: task.id });
      const document = result.item;
      if (!document || document.id !== task.id || typeof document.markdown !== "string") {
        throw new Error("note.document.get returned an invalid document");
      }
      const diagnostics = analyzeMarkdown(document.markdown, {
        documentId: task.id,
        isLeaf: task.isLeaf,
        workspaceTreeTruncated: task.workspaceReport.truncated,
      });
      task.workspaceReport.documents.push({
        id: document.id,
        workspaceId: task.workspaceId,
        parentId: document.parentId ?? task.parentId,
        title: document.title || task.title,
        url: `${client.portal}/note/document/${document.id}/`,
        updatedAt: document.updatedAt,
        ...diagnostics,
      });
    } catch (error) {
      apiErrors += 1;
      task.workspaceReport.documents.push({
        id: task.id,
        workspaceId: task.workspaceId,
        parentId: task.parentId,
        title: task.title,
        url: `${client.portal}/note/document/${task.id}/`,
        characters: 0,
        bytes: 0,
        suspicious: true,
        reasons: ["api_error"],
        error: client.redact(error),
      });
    }
  }

  for (const workspace of workspaces) {
    workspace.documents.sort((left, right) => left.id - right.id);
    if (workspace.documents.some((document) => document.suspicious)) workspace.suspicious = true;
  }

  const documents = workspaces.flatMap((workspace) => workspace.documents);
  const report = {
    generatedAt: new Date().toISOString(),
    startedAt,
    portal: client.portal,
    delayMs: DEFAULT_DELAY_MS,
    summary: {
      workspaces: workspaces.length,
      documents: documents.length,
      suspiciousWorkspaces: workspaces.filter((workspace) => workspace.suspicious).length,
      suspiciousDocuments: documents.filter((document) => document.suspicious).length,
      apiErrors,
    },
    workspaces,
  };

  await writeJsonAtomically(defaultOutputPath, report);
  console.log(`Audit written to ${defaultOutputPath}`);
  console.log(
    `Documents: ${report.summary.documents}; suspicious: ${report.summary.suspiciousDocuments}; API errors: ${apiErrors}`,
  );
}

if (import.meta.main) {
  main().catch((error) => {
    console.error(`Audit failed: ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  });
}
