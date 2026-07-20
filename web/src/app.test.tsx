import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SUGGESTED_QUESTIONS } from "@/lib/suggested-questions"

import { App } from "./app"

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe("App", () => {
  it("сразу показывает статус прототипа, ограничения и информационный aside", () => {
    render(<App />)

    expect(screen.getByLabelText("Статус проекта: прототип")).toHaveTextContent(
      "Прототип",
    )

    const aside = screen.getByRole("complementary", {
      name: "Информация о статусе Athena",
    })
    expect(aside).toHaveTextContent("Демонстрационный прототип")
    expect(aside).toHaveTextContent(
      "Athena — экспериментальная версия, а не готовый рабочий инструмент.",
    )
    expect(aside).toHaveTextContent("Только ментальная арифметика")
    expect(aside).toHaveTextContent("Evaluation ещё не проводился")
    expect(aside).toHaveTextContent(
      "Данные и ответы ещё не прошли полноценный этап evaluation",
    )

    const questionButtons = within(aside).getAllByRole("button", {
      name: /Заполнить поле вопросом:/,
    })
    expect(questionButtons).toHaveLength(SUGGESTED_QUESTIONS.length)
    questionButtons.forEach((button) => expect(button.tagName).toBe("BUTTON"))

    expect(
      screen.getByRole("heading", {
        name: "Что найти в материалах по ментальной арифметике?",
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByText("Это прототип. Проверяйте ответы по источникам."),
    ).toBeInTheDocument()
  })

  it("переносит пример в composer, фокусирует и не отправляет запрос", async () => {
    const fetchMock = vi.fn()
    const storageSpy = vi.spyOn(Storage.prototype, "setItem")
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = screen.getByRole("textbox", { name: "Вопрос" })
    const question = SUGGESTED_QUESTIONS[0]
    await user.click(
      screen.getByRole("button", {
        name: `Заполнить поле вопросом: ${question}`,
      }),
    )

    expect(input).toHaveValue(question)
    expect(input).toHaveFocus()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(
      screen.queryByRole("article", { name: "Вопрос пользователя" }),
    ).not.toBeInTheDocument()

    await user.type(input, " Уточните ответ.")
    expect(input).toHaveValue(`${question} Уточните ответ.`)
    expect(fetchMock).not.toHaveBeenCalled()
    expect(storageSpy).not.toHaveBeenCalled()
  })

  it("располагает aside перед чатом и не добавляет элементы истории", () => {
    render(<App />)

    const aside = screen.getByRole("complementary", {
      name: "Информация о статусе Athena",
    })
    const main = screen.getByRole("main", { name: "Чат Athena" })
    const layout = aside.parentElement

    expect(
      aside.compareDocumentPosition(main) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(layout).toHaveClass("lg:grid-cols-[300px_minmax(0,1fr)]")
    expect(main).toHaveClass("min-w-0")
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: /Новый чат/i }),
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/История диалогов/i)).not.toBeInTheDocument()
  })

  it("хранит sources рядом со своим ответом и не делает запрос при открытии источника", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        answerResponse("req-1", "Первый ответ [S1]", "Первый источник"),
      )
      .mockResolvedValueOnce(
        answerResponse("req-2", "Второй ответ [S1]", "Второй источник"),
      )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = screen.getByRole("textbox", { name: "Вопрос" })
    await user.type(input, "Первый вопрос")
    await user.click(screen.getByRole("button", { name: "Отправить вопрос" }))
    const firstAnswer = await screen.findByText(/Первый ответ/)

    expect(
      screen.getByRole("complementary", {
        name: "Информация о статусе Athena",
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByLabelText("Статус проекта: прототип"),
    ).toBeInTheDocument()

    await user.type(input, "Второй вопрос")
    await user.click(screen.getByRole("button", { name: "Отправить вопрос" }))
    const secondAnswer = await screen.findByText(/Второй ответ/)

    expect(
      within(firstAnswer.closest("article")!).getByRole("button", {
        name: "Первый источник",
      }),
    ).toBeInTheDocument()
    expect(
      within(secondAnswer.closest("article")!).getByRole("button", {
        name: "Второй источник",
      }),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await user.click(
      within(firstAnswer.closest("article")!).getByRole("button", {
        name: /Открыть источник S1: Первый источник/,
      }),
    )
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Первый источник",
    )
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

function answerResponse(
  requestID: string,
  answer: string,
  title: string,
): Response {
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
