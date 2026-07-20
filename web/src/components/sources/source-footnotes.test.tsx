import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import { makeSource } from "@/test/fixtures"
import { SourceFootnotes } from "./source-footnotes"

describe("SourceFootnotes", () => {
  it("сохраняет порядок backend, дедуплицирует и использует fallback пути", () => {
    const sources = [
      makeSource({ source_id: "S2", chunk_id: "chunk_2", title: "Второй" }),
      makeSource({ source_id: "S1", chunk_id: "chunk_1", breadcrumbs: [], heading_path: ["Fallback"] }),
      makeSource({ source_id: "S1", chunk_id: "chunk_duplicate" }),
    ]
    render(<SourceFootnotes sources={sources} onOpen={vi.fn()} />)
    const buttons = screen.getAllByRole("button")
    expect(buttons[0]).toHaveTextContent("Второй")
    expect(buttons[1]).toHaveTextContent("Ментальная арифметика")
    expect(screen.getAllByText("[S1]")).toHaveLength(1)
    expect(screen.getByText("Fallback")).toBeInTheDocument()
  })

  it("показывает первые пять и раскрывает остальные", async () => {
    const user = userEvent.setup()
    const sources = Array.from({ length: 6 }, (_, index) =>
      makeSource({
        source_id: `S${index + 1}`,
        chunk_id: `chunk_${index + 1}`,
        title: `Источник ${index + 1}`,
      }),
    )
    render(<SourceFootnotes sources={sources} onOpen={vi.fn()} />)
    expect(screen.queryByText("Источник 6")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: /показать все/i }))
    expect(screen.getByText("Источник 6")).toBeVisible()
  })
})
