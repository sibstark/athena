import { RAGAPIError } from "@/lib/api"
import type { APIErrorResponse } from "@/types/api"

export type AuthSession =
  { authenticated: true; username: string } | { authenticated: false }

export async function requestSession(
  fetchImpl: typeof fetch = fetch,
): Promise<AuthSession> {
  const response = await fetchImpl("/v1/auth/session", {
    method: "GET",
    credentials: "include",
  })
  if (response.status === 401) return { authenticated: false }
  const payload = await parsePayload(response)
  if (!response.ok)
    throw apiError(response, payload, "Не удалось проверить сессию.")
  return authenticatedSession(response, payload)
}

export async function requestLogin(
  username: string,
  password: string,
  fetchImpl: typeof fetch = fetch,
): Promise<AuthSession & { authenticated: true }> {
  const response = await fetchImpl("/v1/auth/login", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  })
  const payload = await parsePayload(response)
  if (!response.ok)
    throw apiError(response, payload, "Не удалось выполнить вход.")
  return authenticatedSession(response, payload)
}

export async function requestLogout(
  fetchImpl: typeof fetch = fetch,
): Promise<void> {
  const response = await fetchImpl("/v1/auth/logout", {
    method: "POST",
    credentials: "include",
  })
  const payload = await parsePayload(response)
  if (!response.ok)
    throw apiError(response, payload, "Не удалось завершить сессию.")
}

async function parsePayload(response: Response): Promise<unknown> {
  return response.json().catch(() => null) as Promise<unknown>
}

function authenticatedSession(
  response: Response,
  payload: unknown,
): AuthSession & { authenticated: true } {
  if (
    !isRecord(payload) ||
    payload.authenticated !== true ||
    typeof payload.username !== "string" ||
    !payload.username
  ) {
    throw new RAGAPIError(
      "Сервер вернул некорректную сессию.",
      "invalid_response",
      response.status,
    )
  }
  return { authenticated: true, username: payload.username }
}

function apiError(
  response: Response,
  payload: unknown,
  fallback: string,
): RAGAPIError {
  const error = isRecord(payload)
    ? (payload as APIErrorResponse).error
    : undefined
  return new RAGAPIError(
    error?.message || fallback,
    error?.code,
    response.status,
  )
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}
