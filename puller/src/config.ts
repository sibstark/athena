import { resolve } from "node:path";

export interface AppConfig {
  webhookUrl: string;
  collectionId: number;
  outputDir: string;
}

export function requireWebhookUrl(env: NodeJS.ProcessEnv = process.env): string {
  const value = env.BITRIX_WEBHOOK_URL?.trim();
  if (!value) {
    throw new Error(
      "BITRIX_WEBHOOK_URL is not set. Copy .env.example to .env and add your incoming webhook URL.",
    );
  }

  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("BITRIX_WEBHOOK_URL must be a valid URL.");
  }

  if (url.protocol !== "https:" && url.hostname !== "localhost") {
    throw new Error("BITRIX_WEBHOOK_URL must use HTTPS.");
  }
  if (!/^\/rest\/(?:api\/)?[^/]+\/[^/]+\/?$/.test(url.pathname)) {
    throw new Error(
      "BITRIX_WEBHOOK_URL must look like https://portal.bitrix24.ru/rest/USER_ID/WEBHOOK_SECRET/ " +
        "or https://portal.bitrix24.ru/rest/api/USER_ID/WEBHOOK_SECRET/.",
    );
  }

  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/?$/, "/");
}

export function parsePositiveInteger(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`Expected a positive integer, received: ${value}`);
  }
  return parsed;
}

export function resolveOutputDir(value: string): string {
  return resolve(process.cwd(), value);
}
