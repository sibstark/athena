import { mkdir, writeFile } from "node:fs/promises";
import { basename, dirname, extname, join } from "node:path";
import { unified } from "unified";
import remarkParse from "remark-parse";
import { visit } from "unist-util-visit";
import {
  assertPathWithin,
  relativePosix,
  safeDecodeURIComponent,
  sanitizeFileName,
  shortHash,
  toPosixPath,
} from "./utils.js";

type AssetKind = "image" | "file";

interface AssetReference {
  literalUrl: string;
  absoluteUrl: string;
  kind: AssetKind;
}

export interface AssetRecord {
  sourceUrl: string;
  status: "downloaded" | "failed";
  localPath?: string;
  contentType?: string;
  bytes?: number;
  error?: string;
}

export interface AssetStoreOptions {
  outputRoot: string;
  portalOrigin: string;
  fetch?: typeof fetch;
  redact?: (value: string) => string;
  concurrency?: number;
  maxBytes?: number;
}

const fileExtensions = new Set([
  ".7z",
  ".avi",
  ".csv",
  ".doc",
  ".docx",
  ".gif",
  ".jpeg",
  ".jpg",
  ".json",
  ".mov",
  ".mp3",
  ".mp4",
  ".ods",
  ".odt",
  ".pdf",
  ".png",
  ".ppt",
  ".pptx",
  ".rar",
  ".svg",
  ".tar",
  ".txt",
  ".webm",
  ".webp",
  ".xls",
  ".xlsx",
  ".xml",
  ".zip",
]);

export class AssetStore {
  private readonly outputRoot: string;
  private readonly assetsRoot: string;
  private readonly portalOrigin: string;
  private readonly fetchImpl: typeof fetch;
  private readonly redact: (value: string) => string;
  private readonly maxBytes: number;
  private readonly inFlight = new Map<string, Promise<AssetRecord>>();
  private readonly records = new Map<string, AssetRecord>();
  private readonly limiter: Limiter;

  constructor(options: AssetStoreOptions) {
    this.outputRoot = options.outputRoot;
    this.assetsRoot = join(options.outputRoot, "assets");
    this.portalOrigin = options.portalOrigin;
    this.fetchImpl = options.fetch ?? fetch;
    this.redact = options.redact ?? ((value) => value);
    this.maxBytes = options.maxBytes ?? 100 * 1024 * 1024;
    this.limiter = new Limiter(options.concurrency ?? 4);
  }

  async processMarkdown(markdown: string, markdownPath: string): Promise<string> {
    const references = this.collectReferences(markdown);
    const replacements = new Map<string, string>();

    await Promise.all(
      references.map(async (reference) => {
        const record = await this.download(reference);
        if (record.status !== "downloaded" || !record.localPath) return;
        const absoluteAssetPath = join(this.outputRoot, record.localPath);
        replacements.set(reference.literalUrl, relativePosix(markdownPath, absoluteAssetPath));
      }),
    );

    let rewritten = markdown;
    for (const [from, to] of [...replacements.entries()].sort(
      ([left], [right]) => right.length - left.length,
    )) {
      rewritten = rewritten.split(from).join(to);
    }
    return rewritten;
  }

  getRecords(): AssetRecord[] {
    return [...this.records.values()].sort((left, right) =>
      left.sourceUrl.localeCompare(right.sourceUrl),
    );
  }

  private collectReferences(markdown: string): AssetReference[] {
    const candidates = new Map<string, AssetReference>();
    const tree = unified().use(remarkParse).parse(markdown);

    visit(tree, (node: unknown) => {
      if (!isUrlNode(node)) return;
      const kind: AssetKind = node.type === "image" ? "image" : "file";
      this.addReference(candidates, node.url, kind);
    });

    const htmlPattern = /<(img|a)\b[^>]*?\b(?:src|href)=["']([^"']+)["'][^>]*>/gi;
    for (const match of markdown.matchAll(htmlPattern)) {
      const tag = match[1]?.toLowerCase();
      const url = match[2];
      if (!url) continue;
      this.addReference(candidates, url, tag === "img" ? "image" : "file");
    }

    return [...candidates.values()];
  }

  private addReference(
    candidates: Map<string, AssetReference>,
    literalUrl: string,
    kind: AssetKind,
  ): void {
    if (!literalUrl || /^(?:#|data:|mailto:|tel:|javascript:)/i.test(literalUrl)) return;
    let absoluteUrl: string;
    try {
      absoluteUrl = new URL(literalUrl.replaceAll("&amp;", "&"), this.portalOrigin).toString();
    } catch {
      return;
    }
    if (kind === "file" && !isLikelyFile(absoluteUrl)) return;
    candidates.set(`${kind}:${absoluteUrl}`, { literalUrl, absoluteUrl, kind });
  }

  private download(reference: AssetReference): Promise<AssetRecord> {
    const existing = this.inFlight.get(reference.absoluteUrl);
    if (existing) return existing;
    const promise = this.limiter.run(() => this.downloadNow(reference));
    this.inFlight.set(reference.absoluteUrl, promise);
    return promise;
  }

  private async downloadNow(reference: AssetReference): Promise<AssetRecord> {
    const safeUrl = this.redact(reference.absoluteUrl);
    try {
      const response = await this.fetchImpl(reference.absoluteUrl, {
        headers: { accept: "*/*" },
        redirect: "follow",
        signal: AbortSignal.timeout(45_000),
      });
      if (!response.ok) throw new Error(`HTTP ${response.status} ${response.statusText}`);

      const contentLength = Number(response.headers.get("content-length"));
      if (Number.isFinite(contentLength) && contentLength > this.maxBytes) {
        throw new Error(`file is larger than ${this.maxBytes} bytes`);
      }

      const contentType = response.headers.get("content-type")?.split(";")[0]?.trim() || undefined;
      if (reference.kind === "image" && contentType && !contentType.startsWith("image/")) {
        throw new Error(`expected an image, received ${contentType}`);
      }
      if (reference.kind === "file" && contentType === "text/html") {
        throw new Error("received an HTML page instead of a file (authorization may be required)");
      }

      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.byteLength > this.maxBytes) {
        throw new Error(`file is larger than ${this.maxBytes} bytes`);
      }

      const fileName = chooseFileName(reference.absoluteUrl, response.headers, contentType);
      const relativePath = toPosixPath(join("assets", `${shortHash(reference.absoluteUrl)}-${fileName}`));
      const absolutePath = join(this.outputRoot, relativePath);
      assertPathWithin(this.assetsRoot, absolutePath);
      await mkdir(dirname(absolutePath), { recursive: true });
      await writeFile(absolutePath, bytes);

      const record: AssetRecord = {
        sourceUrl: safeUrl,
        status: "downloaded",
        localPath: relativePath,
        ...(contentType ? { contentType } : {}),
        bytes: bytes.byteLength,
      };
      this.records.set(reference.absoluteUrl, record);
      return record;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const record: AssetRecord = {
        sourceUrl: safeUrl,
        status: "failed",
        error: this.redact(message),
      };
      this.records.set(reference.absoluteUrl, record);
      return record;
    }
  }
}

class Limiter {
  private active = 0;
  private readonly queue: Array<() => void> = [];

  constructor(private readonly limit: number) {
    if (!Number.isInteger(limit) || limit < 1) throw new Error("Concurrency must be positive");
  }

  run<T>(operation: () => Promise<T>): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const start = () => {
        this.active += 1;
        void operation()
          .then(resolve, reject)
          .finally(() => {
            this.active -= 1;
            this.queue.shift()?.();
          });
      };
      if (this.active < this.limit) start();
      else this.queue.push(start);
    });
  }
}

function isUrlNode(node: unknown): node is { type: "image" | "link"; url: string } {
  if (!node || typeof node !== "object") return false;
  const candidate = node as Record<string, unknown>;
  return (
    (candidate.type === "image" || candidate.type === "link") &&
    typeof candidate.url === "string"
  );
}

function isLikelyFile(value: string): boolean {
  const url = new URL(value);
  const extension = extname(url.pathname).toLowerCase();
  if (fileExtensions.has(extension)) return true;
  return (
    /\/(?:download|showfile|file)\//i.test(url.pathname) ||
    /\/(?:disk|uf)\//i.test(url.pathname) ||
    url.searchParams.has("download")
  );
}

function chooseFileName(urlValue: string, headers: Headers, contentType?: string): string {
  const disposition = headers.get("content-disposition") ?? "";
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  const plain = disposition.match(/filename=["']?([^"';]+)["']?/i)?.[1];
  const urlName = basename(new URL(urlValue).pathname);
  const candidate = encoded ? safeDecodeURIComponent(encoded) : plain || safeDecodeURIComponent(urlName);
  let fileName = sanitizeFileName(candidate, "asset");
  if (!extname(fileName)) fileName += extensionForContentType(contentType);
  return fileName;
}

function extensionForContentType(contentType?: string): string {
  const mapping: Record<string, string> = {
    "application/json": ".json",
    "application/pdf": ".pdf",
    "application/zip": ".zip",
    "image/gif": ".gif",
    "image/jpeg": ".jpg",
    "image/png": ".png",
    "image/svg+xml": ".svg",
    "image/webp": ".webp",
    "text/plain": ".txt",
  };
  return contentType ? mapping[contentType] ?? "" : "";
}
