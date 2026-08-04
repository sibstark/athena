import { describe, expect, test } from "bun:test";
import { createWebhookHandler } from "../src/webhook-handler.js";

const event = (overrides: Record<string, unknown> = {}) => ({
  event: "ONIMBOTV2MESSAGEADD",
  auth: { application_token: "app-secret" },
  message: { text: "Что такое ментальная арифметика?", authorId: 42 },
  chat: { dialogId: "chat17" },
  bot: { id: 7 },
  ...overrides,
});

const request = (body: unknown) =>
  new Request("http://bot.test/webhook", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });

describe("Bitrix webhook handler", () => {
  test("sends the current message to RAG and replies in the same dialog", async () => {
    const calls: Array<{ kind: string; value: unknown }> = [];
    const handler = createWebhookHandler(
      {
        rag: {
          answer: async (query, collections) => {
            calls.push({ kind: "rag", value: { query, collections } });
            return "Ответ API";
          },
        },
        bitrix: {
          send: async (dialogId, message) => {
            calls.push({ kind: "bitrix", value: { dialogId, message } });
          },
        },
      },
      { applicationToken: "app-secret", collection: "methodology_e5_small_v1" },
    );

    const response = await handler(request(event()));

    expect(response.status).toBe(200);
    expect(calls).toEqual([
      {
        kind: "rag",
        value: {
          query: "Что такое ментальная арифметика?",
          collections: ["methodology_e5_small_v1"],
        },
      },
      { kind: "bitrix", value: { dialogId: "chat17", message: "Ответ API" } },
    ]);
  });

  test("uses the same flow for a private dialog", async () => {
    let target = "";
    const handler = createWebhookHandler(
      {
        rag: { answer: async () => "Ответ" },
        bitrix: { send: async (dialogId) => void (target = dialogId) },
      },
      { applicationToken: "app-secret", collection: "methodology_e5_small_v1" },
    );

    const response = await handler(request(event({ chat: { dialogId: "42" } })));

    expect(response.status).toBe(200);
    expect(target).toBe("42");
  });

  test("does not answer a system or bot-authored message", async () => {
    let calls = 0;
    const handler = createWebhookHandler(
      {
        rag: { answer: async () => void (calls += 1) || "Ответ" },
        bitrix: { send: async () => void (calls += 1) },
      },
      { applicationToken: "app-secret", collection: "methodology_e5_small_v1" },
    );

    expect((await handler(request(event({ message: { text: "system", authorId: 42, isSystem: true } })))).status).toBe(200);
    expect((await handler(request(event({ message: { text: "self", authorId: 7 } })))).status).toBe(200);
    expect(calls).toBe(0);
  });

  test("rejects an invalid application token", async () => {
    const handler = createWebhookHandler(
      { rag: { answer: async () => "Ответ" }, bitrix: { send: async () => {} } },
      { applicationToken: "app-secret", collection: "methodology_e5_small_v1" },
    );

    const response = await handler(request(event({ auth: { application_token: "wrong" } })));

    expect(response.status).toBe(401);
  });

  test("sends a safe fallback when RAG fails", async () => {
    let reply = "";
    const logs: unknown[][] = [];
    const handler = createWebhookHandler(
      {
        rag: { answer: async () => { throw new Error("RAG unavailable"); } },
        bitrix: { send: async (_dialogId, message) => void (reply = message) },
        logger: { error: (...args) => logs.push(args) },
      },
      { applicationToken: "app-secret", collection: "methodology_e5_small_v1" },
    );

    const response = await handler(request(event()));

    expect(response.status).toBe(200);
    expect(reply).toContain("не удалось");
    expect(logs[0]?.[0]).toBe("Bitrix bot request failed");
    expect(logs[0]?.[1]).toEqual(expect.objectContaining({ message: "RAG unavailable" }));
  });
});
