import type { Source } from "@/types/api"

export function makeSource(overrides: Partial<Source> = {}): Source {
  return {
    source_id: "S1",
    chunk_id: "chunk_1",
    document_id: "page_1",
    score: 0.8421,
    title: "Ментальная арифметика для 6–8 лет",
    source: "mental/page--1.md",
    breadcrumbs: ["Ментальная арифметика", "Простой счёт", "Цифра 5"],
    heading_path: ["Сценарий урока", "Разминка"],
    section_title: "Разминка",
    ...overrides,
  }
}
