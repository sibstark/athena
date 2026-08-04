import { parseBitrixPayload } from "./form-payload.js";
import type { WebhookDependencies } from "./types.js";

export interface WebhookHandlerOptions {
  applicationToken: string;
  collection: string;
  fallbackMessage?: string;
}

const defaultFallbackMessage = "Извините, сейчас не удалось получить ответ. Попробуйте повторить запрос позже.";

function response(status: number): Response {
  return new Response(null, { status });
}

export function createWebhookHandler(
  dependencies: WebhookDependencies,
  options: WebhookHandlerOptions,
): (request: Request) => Promise<Response> {
  const logger = dependencies.logger ?? console;
  const fallbackMessage = options.fallbackMessage ?? defaultFallbackMessage;

  return async (request) => {
    if (request.method !== "POST") return response(405);

    let event;
    try {
      event = await parseBitrixPayload(request);
    } catch {
      return response(400);
    }

    if (event.applicationToken !== options.applicationToken) return response(401);
    if (event.event !== "ONIMBOTV2MESSAGEADD") return response(200);
    if (event.message.isSystem || event.message.authorId === event.bot.id) return response(200);

    try {
      const answer = await dependencies.rag.answer(event.message.text, [options.collection]);
      await dependencies.bitrix.send(event.chat.dialogId, answer);
    } catch (error) {
      logger.error("Bitrix bot request failed", error);
      try {
        await dependencies.bitrix.send(event.chat.dialogId, fallbackMessage);
      } catch (fallbackError) {
        logger.error("Bitrix bot fallback failed", fallbackError);
      }
    }

    return response(200);
  };
}
