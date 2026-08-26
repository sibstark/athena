import { describe, expect, it, vi } from "vitest"

import { requestAnswer, requestSearch } from "./api"

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
    const result = await requestAnswer("Вопрос", {
      collections: ["methodology_e5_small_v1"],
      fetchImpl,
    })
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(fetchImpl).toHaveBeenCalledWith(
      "/v1/answer",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({
          query: "Вопрос",
          collections: ["methodology_e5_small_v1"],
        }),
      }),
    )
    expect(result.sources[0]).toMatchObject({
      source_id: "S1",
      breadcrumbs: ["Путь"],
    })
  })

  it("отправляет search с cookie credentials", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          request_id: "req-search",
          query: "Вопрос",
          results: [
            {
              source_id: "S1",
              chunk_id: "chunk-1",
              document_id: "page-1",
              score: 0.8,
              content: "Фрагмент",
              title: "Источник",
              source: "source.md",
              breadcrumbs: [],
              heading_path: [],
              section_title: "Раздел",
            },
          ],
          retrieval: {
            collections: ["collection"],
            embedding_model: "model",
            returned: 1,
            duration_ms: 10,
          },
        }),
        { status: 200 },
      ),
    )
    const result = await requestSearch("Вопрос", {
      collections: ["collection"],
      fetchImpl,
    })
    expect(fetchImpl).toHaveBeenCalledWith(
      "/v1/search",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    )
    expect(result.results[0]).toMatchObject({
      source_id: "S1",
      content: "Фрагмент",
    })
  })

  it("может передать список коллекций в универсальный API", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          request_id: "req-collections",
          status: "insufficient_context",
          answer: "Нет контекста",
          sources: [],
        }),
        { status: 200 },
      ),
    )

    await requestAnswer("Вопрос", {
      collections: ["accounting_e5_small_v1"],
      fetchImpl,
    })

    expect(fetchImpl).toHaveBeenCalledWith(
      "/v1/answer",
      expect.objectContaining({
        body: JSON.stringify({
          query: "Вопрос",
          collections: ["accounting_e5_small_v1"],
        }),
      }),
    )
  })
})
