import { X } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { useMediaQuery } from "@/hooks/use-media-query"
import type { Source } from "@/types/api"
import { SourceDetails } from "./source-details"

type SourcePanelProps = {
  source: Source | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SourcePanel({ source, open, onOpenChange }: SourcePanelProps) {
  const isMobile = useMediaQuery("(max-width: 767px), (pointer: coarse)")
  if (!source) return null

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange} shouldScaleBackground={false}>
        <DrawerContent aria-label="Подробности источника (мобильная панель)">
          <DrawerHeader className="relative pr-12 text-left">
            <DrawerTitle>Источник {source.source_id}</DrawerTitle>
            <DrawerDescription>Метаданные выбранного фрагмента</DrawerDescription>
            <DrawerClose asChild>
              <Button variant="ghost" size="icon" className="absolute top-2 right-3">
                <X />
                <span className="sr-only">Закрыть</span>
              </Button>
            </DrawerClose>
          </DrawerHeader>
          <ScrollArea className="max-h-[65svh]">
            <SourceDetails source={source} />
          </ScrollArea>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="w-full sm:max-w-md"
        aria-label="Подробности источника (боковая панель)"
      >
        <SheetHeader>
          <SheetTitle>Источник {source.source_id}</SheetTitle>
          <SheetDescription>Метаданные выбранного фрагмента</SheetDescription>
        </SheetHeader>
        <ScrollArea className="min-h-0 flex-1">
          <SourceDetails source={source} />
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
