import { Skeleton } from "@/components/ui/skeleton"

export function LoadingMessage() {
  return (
    <div className="py-5" role="status" aria-label="Athena формирует ответ">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <span className="grid size-7 place-items-center rounded-full bg-primary text-xs text-primary-foreground">
          A
        </span>
        Athena
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-full max-w-2xl" />
        <Skeleton className="h-4 w-5/6 max-w-xl" />
        <Skeleton className="h-4 w-2/3 max-w-lg" />
      </div>
    </div>
  )
}
