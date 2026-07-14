import { describe, expect, it, vi } from "vitest";
import { BitrixApiError, BitrixClient } from "../src/bitrix/client.js";

describe("BitrixClient", () => {
  it("follows collection cursor pagination", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        jsonResponse({
          result: {
            items: [collection(17, "Mental arithmetic")],
            nextCursor: { position: 10, id: 17 },
          },
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          result: {
            items: [collection(18, "Scratch")],
            nextCursor: null,
          },
        }),
      );
    const client = new BitrixClient("https://example.bitrix24.ru/rest/1/secret/", {
      fetch: fetchMock,
    });

    const result = await client.listCollections();

    expect(result.map((item) => item.id)).toEqual([17, 18]);
    const secondBody = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body)) as {
      pagination: { afterCursor: { id: number } };
    };
    expect(secondBody.pagination.afterCursor.id).toBe(17);
  });

  it("retries a temporary server error", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response("temporary", { status: 503 }))
      .mockResolvedValueOnce(jsonResponse({ result: { items: [], nextCursor: null } }));
    const sleep = vi.fn(async () => undefined);
    const client = new BitrixClient("https://example.bitrix24.ru/rest/1/secret/", {
      fetch: fetchMock,
      sleep,
    });

    await expect(client.listCollections()).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(sleep).toHaveBeenCalledOnce();
  });

  it("redacts the webhook secret in API errors", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      jsonResponse(
        {
          error: "INVALID_CREDENTIALS",
          error_description: "Bad https://example.bitrix24.ru/rest/1/secret/ token",
        },
        401,
      ),
    );
    const client = new BitrixClient("https://example.bitrix24.ru/rest/1/secret/", {
      fetch: fetchMock,
    });

    const error = await client.listCollections().catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(BitrixApiError);
    expect(String(error)).not.toContain("/secret/");
    expect(String(error)).toContain("[REDACTED]");
  });
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function collection(id: number, name: string) {
  return {
    id,
    name,
    position: id,
    policyLevel: "view",
    createdBy: 1,
    updatedBy: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}
