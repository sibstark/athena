import type {
  AnswerResponse,
  APIErrorResponse,
  SearchFilters,
  SearchResponse,
  SearchResult,
  Source,
} from "@/types/api"

export class RAGAPIError extends Error {
  readonly code: string
  readonly status: number

  constructor(message: string, code = "request_failed", status = 0) {
    super(message)
    this.name = "RAGAPIError"
    this.code = code
    this.status = status
  }
}

export async function requestAnswer(
  query: string,
  options: { signal?: AbortSignal; fetchImpl?: typeof fetch } = {},
): Promise<AnswerResponse> {
  const fetchImpl = options.fetchImpl ?? fetch
  const response = await fetchImpl("/v1/answer", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query }),
    signal: options.signal,
  })

  const payload = (await response.json().catch(() => null)) as unknown
  if (!response.ok) {
    const error = isRecord(payload)
      ? (payload as APIErrorResponse).error
      : undefined
    throw new RAGAPIError(
      error?.message || "Не удалось получить ответ. Попробуйте ещё раз.",
      error?.code,
      response.status,
    )
  }

  if (!isRecord(payload) || typeof payload.answer !== "string") {
    throw new RAGAPIError(
      "Сервер вернул некорректный ответ.",
      "invalid_response",
      response.status,
    )
  }

  const status = payload.status
  if (status !== "ok" && status !== "insufficient_context") {
    throw new RAGAPIError(
      "Сервер вернул неизвестный статус.",
      "invalid_response",
      response.status,
    )
  }

  return {
    request_id: stringValue(payload.request_id),
    status,
    answer: payload.answer,
    sources: Array.isArray(payload.sources)
      ? payload.sources.flatMap(parseSource)
      : [],
    generation: isRecord(payload.generation)
      ? {
          provider: stringValue(payload.generation.provider),
          model: stringValue(payload.generation.model),
          duration_ms: numberValue(payload.generation.duration_ms),
          ...(typeof payload.generation.input_tokens === "number"
            ? { input_tokens: payload.generation.input_tokens }
            : {}),
          ...(typeof payload.generation.output_tokens === "number"
            ? { output_tokens: payload.generation.output_tokens }
            : {}),
        }
      : undefined,
  }
}

export async function requestSearch(
  query: string,
  options: {
    topK?: number
    scoreThreshold?: number
    filters?: SearchFilters
    signal?: AbortSignal
    fetchImpl?: typeof fetch
  } = {},
): Promise<SearchResponse> {
  const fetchImpl = options.fetchImpl ?? fetch
  const response = await fetchImpl("/v1/search", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      query,
      ...(options.topK === undefined ? {} : { top_k: options.topK }),
      ...(options.scoreThreshold === undefined
        ? {}
        : { score_threshold: options.scoreThreshold }),
      ...(options.filters === undefined ? {} : { filters: options.filters }),
    }),
    signal: options.signal,
  })
  const payload = (await response.json().catch(() => null)) as unknown
  if (!response.ok) {
    const error = isRecord(payload)
      ? (payload as APIErrorResponse).error
      : undefined
    throw new RAGAPIError(
      error?.message || "Не удалось выполнить поиск. Попробуйте ещё раз.",
      error?.code,
      response.status,
    )
  }
  if (!isRecord(payload) || !Array.isArray(payload.results)) {
    throw new RAGAPIError(
      "Сервер вернул некорректный результат поиска.",
      "invalid_response",
      response.status,
    )
  }
  const retrieval = isRecord(payload.retrieval) ? payload.retrieval : {}
  return {
    request_id: stringValue(payload.request_id),
    query: stringValue(payload.query),
    results: payload.results.flatMap(parseSearchResult),
    retrieval: {
      collection: stringValue(retrieval.collection),
      embedding_model: stringValue(retrieval.embedding_model),
      returned: numberValue(retrieval.returned),
      duration_ms: numberValue(retrieval.duration_ms),
    },
  }
}

function parseSearchResult(value: unknown): SearchResult[] {
  const sources = parseSource(value)
  if (
    sources.length === 0 ||
    !isRecord(value) ||
    typeof value.content !== "string"
  )
    return []
  return [{ ...sources[0], content: value.content }]
}

function parseSource(value: unknown): Source[] {
  if (
    !isRecord(value) ||
    typeof value.source_id !== "string" ||
    !value.source_id
  )
    return []

  return [
    {
      source_id: value.source_id,
      chunk_id: stringValue(value.chunk_id),
      document_id: stringValue(value.document_id),
      score: numberValue(value.score),
      title: stringValue(value.title),
      source: stringValue(value.source),
      breadcrumbs: stringArray(value.breadcrumbs),
      heading_path: stringArray(value.heading_path),
      section_title: stringValue(value.section_title),
      ...(typeof value.url === "string" ? { url: value.url } : {}),
    },
  ]
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function numberValue(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : []
}
