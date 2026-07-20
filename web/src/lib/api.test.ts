import { describe, expect, it, vi } from "vitest"

import { requestAnswer } from "./api"

describe("requestAnswer", () => {
  it("делает один same-origin запрос и нормализует sources", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          request_id: "req-1",
          status: "ok",
          answer: "Ответ [S1]",
          sources: [{ source_id: "S1", score: 0.8, breadcrumbs: ["Путь"] }],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    )
    const result = await requestAnswer("Вопрос", { fetchImpl })
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(fetchImpl).toHaveBeenCalledWith(
      "/v1/answer",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ query: "Вопрос" }) }),
    )
    expect(result.sources[0]).toMatchObject({ source_id: "S1", breadcrumbs: ["Путь"] })
  })
})
