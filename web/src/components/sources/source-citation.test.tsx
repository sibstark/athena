import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { makeSource } from "@/test/fixtures"
import { SourceCitation } from "./source-citation"

function renderCitation(source = makeSource()) {
  const onOpen = vi.fn()
  render(
    <TooltipProvider delayDuration={0} disableHoverableContent={false}>
      <SourceCitation sourceId="S1" source={source} onOpen={onOpen} />
    </TooltipProvider>,
  )
  return onOpen
}

describe("SourceCitation", () => {
  it("показывает title и полный breadcrumb path при hover", async () => {
    const user = userEvent.setup()
    renderCitation()
    await user.hover(screen.getByRole("button", { name: /открыть источник S1/i }))
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Ментальная арифметика для 6–8 лет")
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "Ментальная арифметика → Простой счёт → Цифра 5",
    )
  })

  it("открывает tooltip с клавиатуры и закрывает по Escape", async () => {
    const user = userEvent.setup()
    renderCitation()
    await user.tab()
    expect(await screen.findByRole("tooltip")).toBeVisible()
    await user.keyboard("{Escape}")
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument()
  })

  it("вызывает onOpen только для существующего source", async () => {
    const user = userEvent.setup()
    const onOpen = renderCitation()
    await user.click(screen.getByRole("button", { name: /открыть источник S1/i }))
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ source_id: "S1" }))
  })

  it("оставляет неизвестную цитату обычным текстом", () => {
    render(
      <TooltipProvider>
        <SourceCitation sourceId="S99" onOpen={vi.fn()} />
      </TooltipProvider>,
    )
    expect(screen.getByText("[S99]")).toBeInTheDocument()
    expect(screen.queryByRole("button")).not.toBeInTheDocument()
  })
})
