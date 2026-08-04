import { serve } from "bun";
import { BitrixMessageClient } from "./bitrix-client.js";
import { loadConfig } from "./config.js";
import { RagAnswerClient } from "./rag-client.js";
import { createWebhookHandler } from "./webhook-handler.js";

const config = loadConfig();
const webhook = createWebhookHandler(
  {
    rag: new RagAnswerClient(config.ragApiUrl, {
      apiKey: config.ragApiKey,
      collection: config.collection,
      timeoutMs: config.requestTimeoutMs,
    }),
    bitrix: new BitrixMessageClient(config.restWebhookUrl, {
      botId: config.botId,
      botToken: config.botToken,
    }),
  },
  { applicationToken: config.applicationToken, collection: config.collection },
);

const server = serve({
  port: config.port,
  fetch: async (request) => {
    const url = new URL(request.url);
    if (url.pathname === "/health/live" && request.method === "GET") return new Response("ok");
    if (url.pathname === config.webhookPath) return webhook(request);
    return new Response("Not found", { status: 404 });
  },
});

console.log(`bitrix-bot listening on ${server.hostname}:${server.port}${config.webhookPath}`);
