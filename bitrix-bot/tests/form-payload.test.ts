import { describe, expect, test } from "bun:test";
import { parseBitrixPayload } from "../src/form-payload.js";

describe("parseBitrixPayload", () => {
  test("parses the JSON event shape used by the webhook contract", async () => {
    const payload = await parseBitrixPayload(
      new Request("http://bot.test/webhook", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          event: "ONIMBOTV2MESSAGEADD",
          auth: { application_token: "app-secret" },
          data: {
            message: { text: "Что такое ментальная арифметика?", authorId: 42 },
            chat: { dialogId: "chat17" },
            bot: { id: 7 },
          },
        }),
      }),
    );

    expect(payload).toEqual({
      event: "ONIMBOTV2MESSAGEADD",
      applicationToken: "app-secret",
      message: {
        text: "Что такое ментальная арифметика?",
        authorId: 42,
        isSystem: false,
      },
      chat: { dialogId: "chat17" },
      bot: { id: 7 },
    });
  });

  test("parses nested application/x-www-form-urlencoded fields", async () => {
    const payload = await parseBitrixPayload(
      new Request("http://bot.test/webhook", {
        method: "POST",
        headers: { "content-type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({
          event: "ONIMBOTV2MESSAGEADD",
          "auth[application_token]": "app-secret",
          "message[text]": "Привет",
          "message[authorId]": "42",
          "message[isSystem]": "N",
          "chat[dialogId]": "42",
          "bot[id]": "7",
        }),
      }),
    );

    expect(payload.message.text).toBe("Привет");
    expect(payload.message.authorId).toBe(42);
    expect(payload.message.isSystem).toBe(false);
    expect(payload.chat.dialogId).toBe("42");
  });
});
