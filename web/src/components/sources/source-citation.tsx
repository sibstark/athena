import type { Source } from "@/types/api"
import { badgeVariants } from "@/components/ui/badge"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useMediaQuery } from "@/hooks/use-media-query"
import { cn } from "@/lib/utils"
import { sourcePath } from "./source-utils"

export type SourceCitationProps = {
  sourceId: string
  source?: Source
  onOpen: (source: Source) => void
}

export function SourceCitation({ sourceId, source, onOpen }: SourceCitationProps) {
  const isTouch = useMediaQuery("(hover: none), (pointer: coarse), (max-width: 767px)")
  const label = `[${sourceId}]`

  if (!source) {
    return <span>{label}</span>
  }

  const trigger = (
    <button
      type="button"
      className={cn(
        badgeVariants({ variant: "secondary" }),
        "mx-0.5 align-baseline text-primary hover:bg-primary/10 hover:text-primary",
      )}
      aria-label={`Открыть источник ${sourceId}: ${source.title || source.document_id}`}
      onClick={() => onOpen(source)}
    >
      {label}
    </button>
  )

  if (isTouch) return trigger

  return (
    <Tooltip>
      <TooltipTrigger asChild>{trigger}</TooltipTrigger>
      <TooltipContent
        className="block max-w-96 space-y-2 p-3 text-left leading-relaxed"
        sideOffset={8}
        collisionPadding={12}
      >
        <p className="font-medium">{source.title || source.document_id}</p>
        {sourcePath(source) && <p className="wrap-break-word opacity-90">{sourcePath(source)}</p>}
        {source.section_title && <p>Раздел: {source.section_title}</p>}
      </TooltipContent>
    </Tooltip>
  )
}
