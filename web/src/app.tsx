import * as React from "react"
import { BookOpen, LogOut, Trash2 } from "lucide-react"

import { LoginPage } from "@/components/auth/login-page"
import { SessionLoading } from "@/components/auth/session-loading"
import { AssistantMessage } from "@/components/chat/assistant-message"
import { Composer } from "@/components/chat/composer"
import { LoadingMessage } from "@/components/chat/loading-message"
import { PrototypeInfo } from "@/components/prototype-info"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useMediaQuery } from "@/hooks/use-media-query"
import { requestLogout, requestSession } from "@/lib/auth"
import { requestAnswer, APIError } from "@/lib/api"
import type { ChatMessage } from "@/types/api"

const CHAT_COLLECTIONS = ["methodology_e5_small_v1"]

export function App() {
  const [session, setSession] = React.useState<
    | { status: "checking" }
    | { status: "authenticated"; username: string }
    | { status: "unauthenticated"; error?: string }
  >({ status: "checking" })
  const [messages, setMessages] = React.useState<ChatMessage[]>([])
  const [query, setQuery] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const [logoutPending, setLogoutPending] = React.useState(false)
  const abortRef = React.useRef<AbortController | null>(null)
  const bottomRef = React.useRef<HTMLDivElement | null>(null)
  const composerRef = React.useRef<HTMLTextAreaElement | null>(null)
  const prefersReducedMotion = useMediaQuery("(prefers-reduced-motion: reduce)")

  React.useEffect(() => {
    bottomRef.current?.scrollIntoView({
      behavior: prefersReducedMotion ? "auto" : "smooth",
      block: "end",
    })
  }, [messages, pending, error, prefersReducedMotion])

  React.useEffect(() => () => abortRef.current?.abort(), [])

  React.useEffect(() => {
    let active = true
    requestSession()
      .then((result) => {
        if (!active) return
        setSession(
          result.authenticated
            ? { status: "authenticated", username: result.username }
            : { status: "unauthenticated" },
        )
      })
      .catch(() => {
        if (active) {
          setSession({
            status: "unauthenticated",
            error: "Не удалось проверить сессию. Войдите повторно.",
          })
        }
      })
    return () => {
      active = false
    }
  }, [])

  const resetChat = React.useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setMessages([])
    setQuery("")
    setError(null)
    setPending(false)
  }, [])

  const endSession = React.useCallback(() => {
    resetChat()
    setSession({ status: "unauthenticated" })
  }, [resetChat])

  const submit = React.useCallback(
    async (text = query) => {
      const trimmed = text.trim()
      if (!trimmed || pending) return

      const controller = new AbortController()
      abortRef.current = controller
      setPending(true)
      setError(null)
      setQuery("")
      setMessages((current) => [
        ...current,
        { id: makeID(), role: "user", content: trimmed },
      ])

      try {
        const response = await requestAnswer(trimmed, {
          collections: CHAT_COLLECTIONS,
          signal: controller.signal,
        })
        setMessages((current) => [
          ...current,
          {
            id: response.request_id || makeID(),
            role: "assistant",
            query: trimmed,
            answer: response.answer,
            status: response.status,
            sources: response.sources,
            generation: response.generation,
          },
        ])
      } catch (reason) {
        if (controller.signal.aborted) return
        if (reason instanceof APIError && reason.status === 401) {
          endSession()
          return
        }
        setError(
          reason instanceof APIError
            ? reason.message
            : "Не удалось связаться с сервисом. Попробуйте ещё раз.",
        )
        setQuery(trimmed)
      } finally {
        if (abortRef.current === controller) abortRef.current = null
        setPending(false)
      }
    },
    [endSession, pending, query],
  )

  const logout = async () => {
    if (logoutPending) return
    setLogoutPending(true)
    try {
      await requestLogout()
    } catch {
      // Locally end the session even if the server is temporarily unavailable.
    } finally {
      setLogoutPending(false)
      endSession()
    }
  }

  const selectSuggestedQuestion = React.useCallback((question: string) => {
    setQuery(question)
    composerRef.current?.focus()
  }, [])

  if (session.status === "checking") {
    return <SessionLoading />
  }

  if (session.status === "unauthenticated") {
    return (
      <LoginPage
        initialError={session.error}
        onAuthenticated={(username) =>
          setSession({ status: "authenticated", username })
        }
      />
    )
  }

  return (
    <TooltipProvider delayDuration={350} disableHoverableContent={false}>
      <div className="flex h-svh min-h-svh min-w-0 flex-col overflow-x-hidden bg-background text-foreground">
        <header className="shrink-0 border-b bg-background/90 px-4 py-3 backdrop-blur">
          <div className="mx-auto flex w-full max-w-7xl min-w-0 items-center justify-between gap-3 lg:px-2">
            <div className="flex min-w-0 items-center gap-3">
              <span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground">
                <BookOpen className="size-5" />
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h1 className="font-semibold">Athena</h1>
                  <Badge
                    variant="outline"
                    aria-label="Статус проекта: прототип"
                    className="border-warning-border/50 bg-warning/15 text-warning-foreground"
                  >
                    Прототип
                  </Badge>
                </div>
                <p className="truncate text-xs text-muted-foreground">
                  Помощник по методическим материалам
                </p>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              {messages.length > 0 && (
                <Button variant="ghost" size="sm" onClick={resetChat}>
                  <Trash2 /> Очистить
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                onClick={logout}
                disabled={logoutPending}
                aria-label={`Выйти из Athena (${session.username})`}
              >
                <LogOut /> Выйти
              </Button>
            </div>
          </div>
        </header>

        <ScrollArea className="min-h-0 flex-1">
          <div className="mx-auto grid w-full max-w-7xl min-w-0 items-start gap-6 px-4 py-4 lg:grid-cols-[300px_minmax(0,1fr)] lg:px-6 lg:py-6">
            <PrototypeInfo
              className="lg:sticky lg:top-6 lg:self-start"
              onSelectQuestion={selectSuggestedQuestion}
            />

            <main
              className="min-w-0 w-full max-w-4xl justify-self-center"
              aria-label="Чат Athena"
            >
              {messages.length === 0 && !pending ? (
                <section className="grid min-h-[40svh] place-items-center py-8 text-center lg:min-h-[55svh]">
                  <div className="max-w-xl space-y-3">
                    <h2 className="text-2xl font-semibold tracking-tight">
                      Что найти в материалах по ментальной арифметике?
                    </h2>
                    <p className="text-sm leading-6 text-muted-foreground">
                      Задайте вопрос о проведении урока, упражнениях или
                      методике. Athena найдёт подходящие фрагменты в загруженных
                      материалах и покажет использованные источники.
                    </p>
                    <p className="text-sm font-medium text-foreground">
                      Это прототип. Проверяйте ответы по источникам.
                    </p>
                  </div>
                </section>
              ) : (
                <div className="divide-y divide-border/70">
                  {messages.map((message) =>
                    message.role === "user" ? (
                      <article
                        key={message.id}
                        className="py-5"
                        aria-label="Вопрос пользователя"
                      >
                        <div className="ml-auto max-w-2xl rounded-2xl bg-muted px-4 py-3 text-sm leading-6 wrap-break-word">
                          {message.content}
                        </div>
                      </article>
                    ) : (
                      <AssistantMessage
                        key={message.id}
                        message={message}
                        onRetry={submit}
                        pending={pending}
                      />
                    ),
                  )}
                  {pending && <LoadingMessage />}
                </div>
              )}
              {error && (
                <Alert variant="destructive" className="my-4">
                  <AlertTitle>Ошибка</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <div ref={bottomRef} />
            </main>
          </div>
        </ScrollArea>

        <Composer
          inputRef={composerRef}
          value={query}
          pending={pending}
          onChange={setQuery}
          onSubmit={() => submit()}
          onStop={() => abortRef.current?.abort()}
        />
      </div>
    </TooltipProvider>
  )
}

function makeID(): string {
  return typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
}
