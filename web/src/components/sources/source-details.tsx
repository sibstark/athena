import { ChevronDown, ExternalLink } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Separator } from "@/components/ui/separator"
import type { Source } from "@/types/api"
import { safeExternalURL, sourcePath } from "./source-utils"

export function SourceDetails({ source }: { source: Source }) {
  const path = sourcePath(source)
  const externalURL = safeExternalURL(source.url)

  return (
    <div className="space-y-5 px-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
      <div className="space-y-1">
        <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
          Источник {source.source_id}
        </p>
        <h3 className="text-lg font-semibold wrap-break-word">
          {source.title || source.document_id}
        </h3>
      </div>

      {path && (
        <section className="space-y-1.5">
          <h4 className="text-sm font-medium">Путь</h4>
          <p className="text-sm leading-relaxed text-muted-foreground wrap-break-word">{path}</p>
        </section>
      )}

      {source.heading_path.length > 0 && (
        <section className="space-y-1.5">
          <h4 className="text-sm font-medium">Заголовки раздела</h4>
          <p className="text-sm leading-relaxed text-muted-foreground wrap-break-word">
            {source.heading_path.join(" → ")}
          </p>
        </section>
      )}

      {source.section_title && (
        <section className="space-y-1.5">
          <h4 className="text-sm font-medium">Раздел</h4>
          <p className="text-sm text-muted-foreground wrap-break-word">{source.section_title}</p>
        </section>
      )}

      {source.source && (
        <section className="space-y-1.5">
          <h4 className="text-sm font-medium">Исходный файл</h4>
          <p className="text-sm text-muted-foreground wrap-break-word">{source.source}</p>
        </section>
      )}

      {externalURL && (
        <Button asChild variant="outline" size="sm">
          <a href={externalURL} target="_blank" rel="noopener noreferrer">
            Открыть внешний источник <ExternalLink data-icon="inline-end" />
          </a>
        </Button>
      )}

      <Separator />
      <Collapsible>
        <CollapsibleTrigger asChild>
          <Button variant="ghost" size="sm" className="w-full justify-between">
            Технические детали
            <ChevronDown className="transition-transform group-aria-expanded/button:rotate-180" />
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent className="space-y-1 px-2 pt-2 text-xs text-muted-foreground">
          <p>Релевантность: {source.score.toFixed(4)}</p>
          {source.chunk_id && <p className="wrap-break-word">Chunk ID: {source.chunk_id}</p>}
          {source.document_id && <p className="wrap-break-word">Document ID: {source.document_id}</p>}
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}
