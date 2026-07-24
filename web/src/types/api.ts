export type Source = {
  source_id: string
  chunk_id: string
  document_id: string
  score: number
  title: string
  source: string
  breadcrumbs: string[]
  heading_path: string[]
  section_title: string
  url?: string
}

export type GenerationInfo = {
  provider: string
  model: string
  input_tokens?: number
  output_tokens?: number
  duration_ms: number
}

export type AnswerResponse = {
  request_id: string
  status: "ok" | "insufficient_context"
  answer: string
  sources: Source[]
  generation?: GenerationInfo
}

export type SearchFilters = Partial<{
  document_id: string
  parent_document_id: string
  page_id: string
  parent_page_id: string
  root_page_id: string
  language: string
  category: string
  status: string
}>

export type SearchResult = Source & { content: string }

export type SearchResponse = {
  request_id: string
  query: string
  results: SearchResult[]
  retrieval: {
    collection: string
    embedding_model: string
    returned: number
    duration_ms: number
  }
}

export type APIErrorResponse = {
  request_id?: string
  error?: {
    code?: string
    message?: string
  }
}

export type UserMessage = {
  id: string
  role: "user"
  content: string
}

export type AssistantMessage = {
  id: string
  role: "assistant"
  query: string
  answer: string
  status: AnswerResponse["status"]
  sources: Source[]
  generation?: GenerationInfo
}

export type ChatMessage = UserMessage | AssistantMessage
