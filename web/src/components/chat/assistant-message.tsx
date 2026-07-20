import * as React from "react"
import { Check, Copy, RotateCcw } from "lucide-react"
import ReactMarkdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import type { AssistantMessage as AssistantMessageData, Source } from "@/types/api"
import { SourceCitation } from "@/components/sources/source-citation"
import { SourceFootnotes } from "@/components/sources/source-footnotes"
import { SourcePanel } from "@/components/sources/source-panel"

type AssistantMessageProps = {
  message: AssistantMessageData
  onRetry: (query: string) => void
  pending?: boolean
}

export function AssistantMessage({ message, onRetry, pending }: AssistantMessageProps) {
  const [selectedSource, setSelectedSource] = React.useState<Source | null>(null)
  const [copied, setCopied] = React.useState(false)
  const triggerRef = React.useRef<HTMLElement | null>(null)

  const openSource = React.useCallback((source: Source) => {
    triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setSelectedSource(source)
  }, [])

  const closeSource = React.useCallback((open: boolean) => {
    if (open) return
    setSelectedSource(null)
    window.setTimeout(() => triggerRef.current?.focus(), 0)
  }, [])

  const copyAnswer = async () => {
    await navigator.clipboard.writeText(message.answer)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  const components = React.useMemo(
    () => markdownComponents(message.sources, openSource),
    [message.sources, openSource],
  )

  return (
    <article className="animate-in fade-in-0 slide-in-from-bottom-1 py-5" aria-label="Ответ помощника">
      <div className="mb-2 flex items-center gap-2 text-sm font-medium">
        <span className="grid size-7 place-items-center rounded-full bg-primary text-xs text-primary-foreground">
          A
        </span>
        Athena
      </div>

      {message.status === "insufficient_context" ? (
        <Alert>
          <AlertTitle>Недостаточно контекста</AlertTitle>
          <AlertDescription>{message.answer}</AlertDescription>
        </Alert>
      ) : (
        <div className="prose-rag text-[0.975rem] leading-7">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            skipHtml
            components={components}
            urlTransform={safeMarkdownURL}
          >
            {message.answer}
          </ReactMarkdown>
        </div>
      )}

      <SourceFootnotes sources={message.sources} onOpen={openSource} />

      <div className="mt-3 flex gap-1">
        <Button variant="ghost" size="icon-sm" onClick={copyAnswer} aria-label="Копировать ответ">
          {copied ? <Check /> : <Copy />}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => onRetry(message.query)}
          disabled={pending}
          aria-label="Повторить запрос"
        >
          <RotateCcw />
        </Button>
      </div>

      <SourcePanel source={selectedSource} open={selectedSource !== null} onOpenChange={closeSource} />
    </article>
  )
}

function markdownComponents(sources: Source[], onOpen: (source: Source) => void): Components {
  const citations = (children: React.ReactNode) => citationText(children, sources, onOpen)
  return {
    p: ({ children }) => <p className="mb-4 last:mb-0">{citations(children)}</p>,
    h1: ({ children }) => <h1 className="mt-7 mb-3 text-2xl font-semibold">{citations(children)}</h1>,
    h2: ({ children }) => <h2 className="mt-6 mb-3 text-xl font-semibold">{citations(children)}</h2>,
    h3: ({ children }) => <h3 className="mt-5 mb-2 text-lg font-semibold">{citations(children)}</h3>,
    h4: ({ children }) => <h4 className="mt-4 mb-2 font-semibold">{citations(children)}</h4>,
    h5: ({ children }) => <h5 className="mt-4 mb-2 text-sm font-semibold">{citations(children)}</h5>,
    h6: ({ children }) => <h6 className="mt-4 mb-2 text-sm font-medium">{citations(children)}</h6>,
    strong: ({ children }) => <strong className="font-semibold">{citations(children)}</strong>,
    em: ({ children }) => <em>{citations(children)}</em>,
    li: ({ children }) => <li className="my-1 pl-1">{citations(children)}</li>,
    ul: ({ children }) => <ul className="mb-4 list-disc space-y-1 pl-6">{children}</ul>,
    ol: ({ children }) => <ol className="mb-4 list-decimal space-y-1 pl-6">{children}</ol>,
    blockquote: ({ children }) => (
      <blockquote className="my-4 border-l-2 border-border pl-4 text-muted-foreground">{children}</blockquote>
    ),
    table: ({ children }) => (
      <div className="my-4 overflow-x-auto rounded-lg border">
        <table className="w-full border-collapse text-sm">{children}</table>
      </div>
    ),
    th: ({ children }) => (
      <th className="border-b bg-muted px-3 py-2 text-left font-medium">{citations(children)}</th>
    ),
    td: ({ children }) => <td className="border-b px-3 py-2 align-top">{citations(children)}</td>,
    code: ({ className, children }) => (
      <code className={className ? `${className} text-sm` : "rounded bg-muted px-1.5 py-0.5 text-sm"}>
        {children}
      </code>
    ),
    pre: ({ children }) => (
      <pre className="my-4 overflow-x-auto rounded-lg bg-muted p-4 text-sm">{children}</pre>
    ),
    a: ({ href, children }) => (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="font-medium text-primary underline underline-offset-4"
      >
        {children}
      </a>
    ),
  }
}

function citationText(
  children: React.ReactNode,
  sources: Source[],
  onOpen: (source: Source) => void,
): React.ReactNode {
  return React.Children.map(children, (child) => {
    if (typeof child !== "string") return child
    const result: React.ReactNode[] = []
    const pattern = /\[(S[1-9]\d*)\]/g
    let cursor = 0
    let match: RegExpExecArray | null
    while ((match = pattern.exec(child)) !== null) {
      if (match.index > cursor) result.push(child.slice(cursor, match.index))
      const sourceId = match[1]
      const source = sources.find((candidate) => candidate.source_id === sourceId)
      result.push(
        <SourceCitation
          key={`${sourceId}-${match.index}`}
          sourceId={sourceId}
          source={source}
          onOpen={onOpen}
        />,
      )
      cursor = pattern.lastIndex
    }
    if (cursor < child.length) result.push(child.slice(cursor))
    return result.length > 0 ? result : child
  })
}

function safeMarkdownURL(value: string): string {
  try {
    const url = new URL(value)
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : ""
  } catch {
    return ""
  }
}
