import { describe, expect, test } from "bun:test";
import { BitrixMessageClient } from "../src/bitrix-client.js";
import { RagAnswerClient } from "../src/rag-client.js";

describe("RAG client", () => {
  test("posts only query and the configured collection to /v1/answer", async () => {
    let request: Request | undefined;
    const client = new RagAnswerClient("http://rag-api:8080", {
      apiKey: "rag-secret",
      collection: "methodology_e5_small_v1",
      fetch: async (input, init) => {
        request = new Request(input, init);
        return new Response(JSON.stringify({ status: "ok", answer: "Ответ API" }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await expect(client.answer("Вопрос")).resolves.toBe("Ответ API");
    expect(request?.url).toBe("http://rag-api:8080/v1/answer");
    expect(request?.headers.get("authorization")).toBe("Bearer rag-secret");
    expect(await request?.json()).toEqual({
      query: "Вопрос",
      collections: ["methodology_e5_small_v1"],
    });
  });
});

describe("Bitrix message client", () => {
  test("sends a v2 message to the dialog", async () => {
    let request: Request | undefined;
    const client = new BitrixMessageClient("https://portal.bitrix24.ru/rest/1/webhook/", {
      botId: 7,
      botToken: "bot-secret",
      fetch: async (input, init) => {
        request = new Request(input, init);
        return new Response(JSON.stringify({ result: { id: 123 } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await expect(client.send("chat17", "Ответ")).resolves.toBeUndefined();
    expect(request?.url).toBe("https://portal.bitrix24.ru/rest/1/webhook/imbot.v2.Chat.Message.send");
    expect(await request?.json()).toEqual({
      botId: 7,
      botToken: "bot-secret",
      dialogId: "chat17",
      fields: { message: "Ответ" },
    });
  });
});
