import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { App } from "./app"

afterEach(() => vi.unstubAllGlobals())

describe("App", () => {
  it("хранит sources рядом со своим ответом и не делает запрос при открытии источника", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(answerResponse("req-1", "Первый ответ [S1]", "Первый источник"))
      .mockResolvedValueOnce(answerResponse("req-2", "Второй ответ [S1]", "Второй источник"))
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = screen.getByRole("textbox", { name: "Вопрос" })
    await user.type(input, "Первый вопрос")
    await user.click(screen.getByRole("button", { name: "Отправить вопрос" }))
    const firstAnswer = await screen.findByText(/Первый ответ/)

    await user.type(input, "Второй вопрос")
    await user.click(screen.getByRole("button", { name: "Отправить вопрос" }))
    const secondAnswer = await screen.findByText(/Второй ответ/)

    expect(
      within(firstAnswer.closest("article")!).getByRole("button", { name: "Первый источник" }),
    ).toBeInTheDocument()
    expect(
      within(secondAnswer.closest("article")!).getByRole("button", { name: "Второй источник" }),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await user.click(
      within(firstAnswer.closest("article")!).getByRole("button", {
        name: /Открыть источник S1: Первый источник/,
      }),
    )
    expect(await screen.findByRole("dialog")).toHaveTextContent("Первый источник")
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

function answerResponse(requestID: string, answer: string, title: string): Response {
  return new Response(
    JSON.stringify({
      request_id: requestID,
      status: "ok",
      answer,
      sources: [
        {
          source_id: "S1",
          chunk_id: `chunk-${requestID}`,
          document_id: `page-${requestID}`,
          score: 0.8,
          title,
          source: `${requestID}.md`,
          breadcrumbs: [title],
          heading_path: [],
          section_title: "Раздел",
        },
      ],
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  )
}
