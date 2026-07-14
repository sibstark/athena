import type {
  BitrixEnvelope,
  CollectionListResult,
  DocumentGetResult,
  DocumentTreeResult,
  KnowledgeCollection,
  KnowledgeDocument,
} from "./types.js";

export interface BitrixClientOptions {
  fetch?: typeof fetch;
  sleep?: (milliseconds: number) => Promise<void>;
  maxAttempts?: number;
}

export class BitrixApiError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = "BitrixApiError";
  }
}

const defaultSleep = (milliseconds: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, milliseconds));

export class BitrixClient {
  readonly portalOrigin: string;
  private readonly baseUrl: string;
  private readonly fetchImpl: typeof fetch;
  private readonly sleep: (milliseconds: number) => Promise<void>;
  private readonly maxAttempts: number;
  private readonly secret: string;

  constructor(webhookUrl: string, options: BitrixClientOptions = {}) {
    const parsed = new URL(webhookUrl);
    this.baseUrl = parsed.toString().replace(/\/?$/, "/");
    this.portalOrigin = parsed.origin;
    this.fetchImpl = options.fetch ?? fetch;
    this.sleep = options.sleep ?? defaultSleep;
    this.maxAttempts = options.maxAttempts ?? 4;
    this.secret = parsed.pathname.split("/").filter(Boolean).at(-1) ?? "";
  }

  redact(value: string): string {
    if (!this.secret) return value;
    return value.split(this.secret).join("[REDACTED]");
  }

  async call<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
    const endpoint = new URL(`${method}.json`, this.baseUrl);

    for (let attempt = 1; attempt <= this.maxAttempts; attempt += 1) {
      let response: Response;
      try {
        response = await this.fetchImpl(endpoint, {
          method: "POST",
          headers: {
            accept: "application/json",
            "content-type": "application/json",
          },
          body: JSON.stringify(params),
          signal: AbortSignal.timeout(30_000),
        });
      } catch (error) {
        if (attempt < this.maxAttempts) {
          await this.sleep(this.retryDelay(attempt));
          continue;
        }
        const message = error instanceof Error ? error.message : String(error);
        throw new BitrixApiError(
          `Network error calling ${method}: ${this.redact(message)}`,
          "NETWORK_ERROR",
        );
      }

      if ((response.status === 429 || response.status >= 500) && attempt < this.maxAttempts) {
        await this.sleep(this.retryDelay(attempt, response.headers.get("retry-after")));
        continue;
      }

      let envelope: BitrixEnvelope<T>;
      try {
        envelope = (await response.json()) as BitrixEnvelope<T>;
      } catch {
        throw new BitrixApiError(
          `Bitrix returned non-JSON response for ${method} (HTTP ${response.status}).`,
          "INVALID_RESPONSE",
          response.status,
        );
      }

      if (!response.ok || envelope.error) {
        const code = envelope.error ?? `HTTP_${response.status}`;
        const description = envelope.error_description ?? response.statusText;
        throw new BitrixApiError(
          `${method} failed: ${this.redact(description)} (${code})`,
          code,
          response.status,
        );
      }
      if (envelope.result === undefined) {
        throw new BitrixApiError(
          `${method} returned no result.`,
          "INVALID_RESPONSE",
          response.status,
        );
      }
      return envelope.result;
    }

    throw new BitrixApiError(`Failed to call ${method}.`, "RETRY_EXHAUSTED");
  }

  async listCollections(): Promise<KnowledgeCollection[]> {
    const collections: KnowledgeCollection[] = [];
    let afterCursor: CollectionListResult["nextCursor"] = null;

    do {
      const pagination: Record<string, unknown> = { limit: 200 };
      if (afterCursor) pagination.afterCursor = afterCursor;
      const page = await this.call<CollectionListResult>("note.collection.list", {
        pagination,
      });
      collections.push(...page.items);
      afterCursor = page.nextCursor;
    } while (afterCursor);

    return collections;
  }

  getDocumentTree(collectionId: number): Promise<DocumentTreeResult> {
    return this.call<DocumentTreeResult>("note.document.tree.list", { collectionId });
  }

  async getDocument(id: number): Promise<KnowledgeDocument> {
    const result = await this.call<DocumentGetResult>("note.document.get", { id });
    return result.item;
  }

  private retryDelay(attempt: number, retryAfter: string | null = null): number {
    const seconds = retryAfter ? Number(retryAfter) : Number.NaN;
    if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1_000;
    return Math.min(500 * 2 ** (attempt - 1), 8_000);
  }
}
