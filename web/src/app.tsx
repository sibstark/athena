import * as React from "react"
import { BookOpen, Trash2 } from "lucide-react"

import { AssistantMessage } from "@/components/chat/assistant-message"
import { Composer } from "@/components/chat/composer"
import { LoadingMessage } from "@/components/chat/loading-message"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { TooltipProvider } from "@/components/ui/tooltip"
import { requestAnswer, RAGAPIError } from "@/lib/api"
import type { ChatMessage } from "@/types/api"

export function App() {
  const [messages, setMessages] = React.useState<ChatMessage[]>([])
  const [query, setQuery] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const abortRef = React.useRef<AbortController | null>(null)
  const bottomRef = React.useRef<HTMLDivElement | null>(null)

  React.useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" })
  }, [messages, pending, error])

  React.useEffect(() => () => abortRef.current?.abort(), [])

  const submit = React.useCallback(async (text = query) => {
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
      const response = await requestAnswer(trimmed, { signal: controller.signal })
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
      setError(
        reason instanceof RAGAPIError
          ? reason.message
          : "Не удалось связаться с сервисом. Попробуйте ещё раз.",
      )
      setQuery(trimmed)
    } finally {
      if (abortRef.current === controller) abortRef.current = null
      setPending(false)
    }
  }, [pending, query])

  const clear = () => {
    abortRef.current?.abort()
    setMessages([])
    setError(null)
    setPending(false)
  }

  return (
    <TooltipProvider delayDuration={350} disableHoverableContent={false}>
      <main className="flex h-svh min-h-svh flex-col bg-background text-foreground">
        <header className="shrink-0 border-b bg-background/90 px-4 py-3 backdrop-blur">
          <div className="mx-auto flex w-full max-w-4xl items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground">
                <BookOpen className="size-5" />
              </span>
              <div>
                <h1 className="font-semibold">Athena</h1>
                <p className="text-xs text-muted-foreground">Помощник по методическим материалам</p>
              </div>
            </div>
            {messages.length > 0 && (
              <Button variant="ghost" size="sm" onClick={clear}>
                <Trash2 /> Очистить
              </Button>
            )}
          </div>
        </header>

        <ScrollArea className="min-h-0 flex-1">
          <div className="mx-auto w-full max-w-4xl px-4 py-4">
            {messages.length === 0 && !pending ? (
              <section className="grid min-h-[55svh] place-items-center text-center">
                <div className="max-w-xl space-y-3">
                  <h2 className="text-2xl font-semibold tracking-tight">Что найти в базе знаний?</h2>
                  <p className="text-sm leading-6 text-muted-foreground">
                    Задайте вопрос о проведении урока, упражнении или методике. В ответе можно
                    открыть цитаты и проверить путь к исходному материалу.
                  </p>
                </div>
              </section>
            ) : (
              <div className="divide-y divide-border/70">
                {messages.map((message) =>
                  message.role === "user" ? (
                    <article key={message.id} className="py-5" aria-label="Вопрос пользователя">
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
          </div>
        </ScrollArea>

        <Composer
          value={query}
          pending={pending}
          onChange={setQuery}
          onSubmit={() => submit()}
          onStop={() => abortRef.current?.abort()}
        />
      </main>
    </TooltipProvider>
  )
}

function makeID(): string {
  return typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
}
