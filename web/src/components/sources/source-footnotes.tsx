import * as React from "react"
import { ChevronDown } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Separator } from "@/components/ui/separator"
import type { Source } from "@/types/api"
import { sourcePath, uniqueSources } from "./source-utils"

type SourceFootnotesProps = {
  sources: Source[]
  onOpen: (source: Source) => void
}

export function SourceFootnotes({ sources, onOpen }: SourceFootnotesProps) {
  const headingID = React.useId()
  const unique = uniqueSources(sources)
  if (unique.length === 0) return null

  const visible = unique.slice(0, 5)
  const hidden = unique.slice(5)

  return (
    <section aria-labelledby={headingID} className="mt-6 space-y-3">
      <Separator />
      <h3 id={headingID} className="pt-2 text-sm font-semibold">
        Источники
      </h3>
      <div className="space-y-3">
        {visible.map((source) => (
          <Footnote key={source.source_id} source={source} onOpen={onOpen} />
        ))}
      </div>
      {hidden.length > 0 && (
        <Collapsible>
          <CollapsibleContent className="space-y-3 pb-2">
            {hidden.map((source) => (
              <Footnote key={source.source_id} source={source} onOpen={onOpen} />
            ))}
          </CollapsibleContent>
          <CollapsibleTrigger asChild>
            <Button variant="ghost" size="sm">
              Показать все ({unique.length})
              <ChevronDown className="transition-transform group-aria-expanded/button:rotate-180" />
            </Button>
          </CollapsibleTrigger>
        </Collapsible>
      )}
    </section>
  )
}

function Footnote({ source, onOpen }: { source: Source; onOpen: (source: Source) => void }) {
  const path = sourcePath(source)
  return (
    <div className="grid grid-cols-[auto_1fr] items-start gap-x-2 text-sm">
      <Badge variant="secondary">[{source.source_id}]</Badge>
      <div className="min-w-0 space-y-0.5">
        <button
          type="button"
          className="text-left font-medium wrap-break-word underline-offset-4 hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={() => onOpen(source)}
        >
          {source.title || source.document_id}
        </button>
        {path && <p className="text-xs leading-relaxed text-muted-foreground wrap-break-word">{path}</p>}
        {source.section_title && (
          <p className="text-xs text-muted-foreground">Раздел: {source.section_title}</p>
        )}
      </div>
    </div>
  )
}
