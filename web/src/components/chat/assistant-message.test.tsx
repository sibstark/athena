import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { makeSource } from "@/test/fixtures"
import type { AssistantMessage as AssistantMessageData } from "@/types/api"
import { AssistantMessage } from "./assistant-message"

function message(overrides: Partial<AssistantMessageData> = {}): AssistantMessageData {
  return {
    id: "answer-1",
    role: "assistant",
    query: "Вопрос",
    answer: "Ответ [S1], неизвестный [S99].",
    status: "ok",
    sources: [
      makeSource({ source_id: "S2", chunk_id: "chunk_2", title: "Не тот источник" }),
      makeSource({ source_id: "S1", chunk_id: "chunk_1", title: "Правильный источник" }),
    ],
    ...overrides,
  }
}

function renderMessage(value = message()) {
  render(
    <TooltipProvider delayDuration={0}>
      <AssistantMessage message={value} onRetry={vi.fn()} />
    </TooltipProvider>,
  )
}

afterEach(() => {
  vi.mocked(window.matchMedia).mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
})

describe("AssistantMessage", () => {
  it("находит источник по source_id, а не по позиции массива", async () => {
    const user = userEvent.setup()
    renderMessage()
    await user.click(screen.getByRole("button", { name: /открыть источник S1/i }))
    expect(await screen.findByRole("dialog")).toHaveTextContent("Правильный источник")
    expect(screen.getByText("[S99]")).toBeInTheDocument()
  })

  it("закрывает Sheet по Escape и возвращает focus", async () => {
    const user = userEvent.setup()
    renderMessage()
    const citation = screen.getByRole("button", { name: /открыть источник S1/i })
    citation.focus()
    await user.click(citation)
    expect(await screen.findByRole("dialog")).toBeVisible()
    await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
    await waitFor(() => expect(citation).toHaveFocus())
  })

  it("на мобильном открывает bottom sheet", async () => {
    vi.mocked(window.matchMedia).mockImplementation((query: string) => ({
      matches: query.includes("max-width") || query.includes("pointer"),
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
    const user = userEvent.setup()
    renderMessage()
    await user.click(screen.getByRole("button", { name: /открыть источник S1/i }))
    expect(await screen.findByRole("dialog")).toBeVisible()
  })

  it("экранирует HTML в metadata", async () => {
    const user = userEvent.setup()
    renderMessage(message({ sources: [makeSource({ title: "<img src=x onerror=alert(1)>" })] }))
    await user.click(screen.getByRole("button", { name: /открыть источник S1/i }))
    expect(await screen.findAllByText("<img src=x onerror=alert(1)>")).not.toHaveLength(0)
    expect(document.querySelector("img")).toBeNull()
  })
})
