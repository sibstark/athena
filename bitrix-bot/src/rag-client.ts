export interface RagAnswerClientOptions {
  apiKey?: string;
  collection: string;
  timeoutMs?: number;
  fetch?: typeof fetch;
}

export class RagAnswerClient {
  private readonly endpoint: string;
  private readonly apiKey: string;
  private readonly collection: string;
  private readonly timeoutMs: number;
  private readonly fetchImpl: typeof fetch;

  constructor(baseUrl: string, options: RagAnswerClientOptions) {
    this.endpoint = new URL("v1/answer", `${baseUrl.replace(/\/$/, "")}/`).toString();
    this.apiKey = options.apiKey ?? "";
    this.collection = options.collection;
    this.timeoutMs = options.timeoutMs ?? 75_000;
    this.fetchImpl = options.fetch ?? fetch;
  }

  async answer(query: string): Promise<string> {
    const response = await this.fetchImpl(this.endpoint, {
      method: "POST",
      headers: {
        accept: "application/json",
        "content-type": "application/json",
        ...(this.apiKey ? { authorization: `Bearer ${this.apiKey}` } : {}),
      },
      body: JSON.stringify({ query, collections: [this.collection] }),
      signal: AbortSignal.timeout(this.timeoutMs),
    });

    const body = await response.json().catch(() => undefined) as { answer?: unknown; error?: unknown } | undefined;
    if (!response.ok || typeof body?.answer !== "string") {
      throw new Error(`RAG API request failed with HTTP ${response.status}`);
    }
    return body.answer;
  }
}
