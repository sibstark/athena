import { BookOpen, LoaderCircle } from "lucide-react"

export function SessionLoading() {
  return (
    <main className="grid min-h-svh place-items-center bg-background px-4 text-foreground">
      <div
        className="flex flex-col items-center gap-4 text-center"
        role="status"
        aria-live="polite"
      >
        <span className="grid size-12 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-sm">
          <BookOpen aria-hidden="true" className="size-6" />
        </span>
        <div className="space-y-1">
          <p className="font-semibold">Athena</p>
          <p className="text-sm text-muted-foreground">Проверяем сессию…</p>
        </div>
        <LoaderCircle
          aria-hidden="true"
          className="size-5 animate-spin text-muted-foreground"
        />
      </div>
    </main>
  )
}
