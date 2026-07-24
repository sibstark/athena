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
  it("сразу показывает статус прототипа, ограничения и информационный aside", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(sessionResponse()))
    render(<App />)

    expect(
      await screen.findByLabelText("Статус проекта: прототип"),
    ).toHaveTextContent("Прототип")

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
    const fetchMock = vi.fn().mockResolvedValueOnce(sessionResponse())
    const storageSpy = vi.spyOn(Storage.prototype, "setItem")
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = await screen.findByRole("textbox", { name: "Вопрос" })
    const question = SUGGESTED_QUESTIONS[0]
    await user.click(
      screen.getByRole("button", {
        name: `Заполнить поле вопросом: ${question}`,
      }),
    )

    expect(input).toHaveValue(question)
    expect(input).toHaveFocus()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(
      screen.queryByRole("article", { name: "Вопрос пользователя" }),
    ).not.toBeInTheDocument()

    await user.type(input, " Уточните ответ.")
    expect(input).toHaveValue(`${question} Уточните ответ.`)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(storageSpy).not.toHaveBeenCalled()
  })

  it("располагает aside перед чатом и не добавляет элементы истории", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(sessionResponse()))
    render(<App />)

    const aside = await screen.findByRole("complementary", {
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
      .mockResolvedValueOnce(sessionResponse())
      .mockResolvedValueOnce(
        answerResponse("req-1", "Первый ответ [S1]", "Первый источник"),
      )
      .mockResolvedValueOnce(
        answerResponse("req-2", "Второй ответ [S1]", "Второй источник"),
      )
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = await screen.findByRole("textbox", { name: "Вопрос" })
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
    expect(fetchMock).toHaveBeenCalledTimes(3)

    await user.click(
      within(firstAnswer.closest("article")!).getByRole("button", {
        name: /Открыть источник S1: Первый источник/,
      }),
    )
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "Первый источник",
    )
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it("не показывает чат во время проверки сессии", async () => {
    let resolveSession!: (response: Response) => void
    const fetchMock = vi.fn().mockReturnValue(
      new Promise<Response>((resolve) => {
        resolveSession = resolve
      }),
    )
    vi.stubGlobal("fetch", fetchMock)
    render(<App />)

    expect(screen.getByRole("status")).toHaveTextContent("Проверяем сессию")
    expect(
      screen.queryByRole("main", { name: "Чат Athena" }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole("textbox", { name: "Логин" }),
    ).not.toBeInTheDocument()

    resolveSession(sessionResponse())
    expect(
      await screen.findByRole("main", { name: "Чат Athena" }),
    ).toBeInTheDocument()
  })

  it("показывает форму входа при отсутствии сессии", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(unauthorizedResponse()))
    render(<App />)

    const username = await screen.findByRole("textbox", { name: "Логин" })
    const password = screen.getByLabelText("Пароль")
    expect(username).toHaveAttribute("autocomplete", "username")
    expect(password).toHaveAttribute("autocomplete", "current-password")
    expect(screen.getByRole("button", { name: "Войти" })).toBeDisabled()
  })

  it("выполняет вход и открывает Athena", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(unauthorizedResponse())
      .mockResolvedValueOnce(sessionResponse())
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await user.type(
      await screen.findByRole("textbox", { name: "Логин" }),
      "admin",
    )
    await user.type(screen.getByLabelText("Пароль"), "password")
    await user.click(screen.getByRole("button", { name: "Войти" }))

    expect(
      await screen.findByRole("main", { name: "Чат Athena" }),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ username: "admin", password: "password" }),
      }),
    )
  })

  it("показывает одинаковую серверную ошибку авторизации", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(unauthorizedResponse())
      .mockResolvedValueOnce(invalidCredentialsResponse())
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    await user.type(
      await screen.findByRole("textbox", { name: "Логин" }),
      "admin",
    )
    await user.type(screen.getByLabelText("Пароль"), "wrong")
    await user.click(screen.getByRole("button", { name: "Войти" }))

    expect(
      await screen.findByText("Неверный логин или пароль"),
    ).toBeInTheDocument()
    expect(screen.getByRole("textbox", { name: "Логин" })).toBeInTheDocument()
  })

  it("восстанавливает действующую сессию и выполняет выход", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(sessionResponse())
      .mockResolvedValueOnce(
        answerResponse("logout-answer", "Ответ [S1]", "Источник"),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ authenticated: false }), { status: 200 }),
      )
      .mockResolvedValueOnce(sessionResponse())
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = await screen.findByRole("textbox", { name: "Вопрос" })
    await user.type(input, "Вопрос перед выходом")
    await user.click(screen.getByRole("button", { name: "Отправить вопрос" }))
    expect(
      await screen.findByRole("article", { name: "Ответ помощника" }),
    ).toBeInTheDocument()

    await user.click(
      await screen.findByRole("button", { name: /Выйти из Athena/ }),
    )
    expect(
      await screen.findByRole("textbox", { name: "Логин" }),
    ).toBeInTheDocument()
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/v1/auth/logout",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    )

    await user.type(screen.getByRole("textbox", { name: "Логин" }), "admin")
    await user.type(screen.getByLabelText("Пароль"), "password")
    await user.click(screen.getByRole("button", { name: "Войти" }))
    expect(
      await screen.findByRole("main", { name: "Чат Athena" }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole("article", { name: "Вопрос пользователя" }),
    ).not.toBeInTheDocument()
  })

  it("после 401 очищает чат и возвращает страницу входа", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(sessionResponse())
      .mockResolvedValueOnce(unauthorizedResponse())
      .mockResolvedValueOnce(sessionResponse())
    vi.stubGlobal("fetch", fetchMock)
    const user = userEvent.setup()
    render(<App />)

    const input = await screen.findByRole("textbox", { name: "Вопрос" })
    await user.type(input, "Вопрос с истёкшей сессией")
    await user.click(screen.getByRole("button", { name: "Отправить вопрос" }))
    await screen.findByRole("textbox", { name: "Логин" })

    await user.type(screen.getByRole("textbox", { name: "Логин" }), "admin")
    await user.type(screen.getByLabelText("Пароль"), "password")
    await user.click(screen.getByRole("button", { name: "Войти" }))
    expect(
      await screen.findByRole("main", { name: "Чат Athena" }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole("article", { name: "Вопрос пользователя" }),
    ).not.toBeInTheDocument()
  })
})

function sessionResponse(): Response {
  return new Response(
    JSON.stringify({ authenticated: true, username: "admin" }),
    {
      status: 200,
      headers: { "Content-Type": "application/json" },
    },
  )
}

function unauthorizedResponse(): Response {
  return new Response(
    JSON.stringify({
      error: { code: "unauthorized", message: "Требуется авторизация" },
    }),
    { status: 401, headers: { "Content-Type": "application/json" } },
  )
}

function invalidCredentialsResponse(): Response {
  return new Response(
    JSON.stringify({
      error: {
        code: "invalid_credentials",
        message: "Неверный логин или пароль",
      },
    }),
    { status: 401, headers: { "Content-Type": "application/json" } },
  )
}

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
