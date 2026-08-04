export interface BitrixMessageClientOptions {
  botId: number;
  botToken: string;
  fetch?: typeof fetch;
  timeoutMs?: number;
}

export class BitrixMessageClient {
  private readonly endpoint: string;
  private readonly botId: number;
  private readonly botToken: string;
  private readonly fetchImpl: typeof fetch;
  private readonly timeoutMs: number;

  constructor(webhookUrl: string, options: BitrixMessageClientOptions) {
    this.endpoint = new URL(
      "imbot.v2.Chat.Message.send",
      `${webhookUrl.replace(/\/$/, "")}/`,
    ).toString();
    this.botId = options.botId;
    this.botToken = options.botToken;
    this.fetchImpl = options.fetch ?? fetch;
    this.timeoutMs = options.timeoutMs ?? 15_000;
  }

  async send(dialogId: string, message: string): Promise<void> {
    const response = await this.fetchImpl(this.endpoint, {
      method: "POST",
      headers: { accept: "application/json", "content-type": "application/json" },
      body: JSON.stringify({
        botId: this.botId,
        botToken: this.botToken,
        dialogId,
        fields: { message },
      }),
      signal: AbortSignal.timeout(this.timeoutMs),
    });
    const body = await response.json().catch(() => undefined) as { error?: unknown } | undefined;
    if (!response.ok || body?.error) throw new Error(`Bitrix send failed with HTTP ${response.status}`);
  }
}
