export interface BotConfig {
  port: number;
  webhookPath: string;
  applicationToken: string;
  restWebhookUrl: string;
  botId: number;
  botToken: string;
  ragApiUrl: string;
  ragApiKey: string;
  collection: string;
  requestTimeoutMs: number;
}

function required(env: NodeJS.ProcessEnv, name: string): string {
  const value = env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): BotConfig {
  const botId = Number(required(env, "BITRIX_BOT_ID"));
  if (!Number.isInteger(botId) || botId < 1) throw new Error("BITRIX_BOT_ID must be a positive integer");
  const requestTimeoutMs = Number(env.BOT_REQUEST_TIMEOUT_MS ?? "75000");
  if (!Number.isInteger(requestTimeoutMs) || requestTimeoutMs < 1) throw new Error("BOT_REQUEST_TIMEOUT_MS must be positive");

  return {
    port: Number(env.BITRIX_BOT_PORT ?? "8080"),
    webhookPath: env.BITRIX_WEBHOOK_PATH?.trim() || "/webhook",
    applicationToken: required(env, "BITRIX_APPLICATION_TOKEN"),
    restWebhookUrl: required(env, "BITRIX_REST_WEBHOOK_URL"),
    botId,
    botToken: required(env, "BITRIX_BOT_TOKEN"),
    ragApiUrl: env.RAG_API_URL?.trim() || "http://rag-api:8080",
    ragApiKey: env.RAG_API_KEY?.trim() || "",
    collection: env.BITRIX_BOT_COLLECTION?.trim() || "methodology_e5_small_v1",
    requestTimeoutMs,
  };
}
