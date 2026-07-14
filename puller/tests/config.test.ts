import { describe, expect, it } from "vitest";
import { parsePositiveInteger, requireWebhookUrl } from "../src/config.js";

describe("configuration", () => {
  it("normalizes a valid webhook URL", () => {
    expect(
      requireWebhookUrl({
        BITRIX_WEBHOOK_URL: "https://hwschool.bitrix24.ru/rest/12/a-secret",
      }),
    ).toBe("https://hwschool.bitrix24.ru/rest/12/a-secret/");
  });

  it("rejects malformed webhook URLs", () => {
    expect(() => requireWebhookUrl({ BITRIX_WEBHOOK_URL: "https://example.com/" })).toThrow(
      "must look like",
    );
  });

  it("parses positive collection IDs", () => {
    expect(parsePositiveInteger("17")).toBe(17);
    expect(() => parsePositiveInteger("0")).toThrow("positive integer");
    expect(() => parsePositiveInteger("1.5")).toThrow("positive integer");
  });
});
