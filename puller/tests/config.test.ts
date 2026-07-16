import { describe, expect, it } from "vitest";
import {
  parseNonNegativeInteger,
  parsePositiveInteger,
  requireWebhookUrl,
} from "../src/config.js";

describe("configuration", () => {
  it("normalizes a valid webhook URL", () => {
    expect(
      requireWebhookUrl({
        BITRIX_WEBHOOK_URL: "https://hwschool.bitrix24.ru/rest/12/a-secret",
      }),
    ).toBe("https://hwschool.bitrix24.ru/rest/12/a-secret/");
  });

  it("accepts a REST 3.0 webhook URL", () => {
    expect(
      requireWebhookUrl({
        BITRIX_WEBHOOK_URL: "https://hwschool.bitrix24.ru/rest/api/12/a-secret/",
      }),
    ).toBe("https://hwschool.bitrix24.ru/rest/api/12/a-secret/");
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

  it("accepts zero for optional chunk overlap", () => {
    expect(parseNonNegativeInteger("0")).toBe(0);
    expect(parseNonNegativeInteger("80")).toBe(80);
    expect(() => parseNonNegativeInteger("-1")).toThrow("non-negative integer");
  });
});
