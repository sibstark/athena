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
