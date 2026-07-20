import { Send, Square } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"

type ComposerProps = {
  value: string
  pending: boolean
  onChange: (value: string) => void
  onSubmit: () => void
  onStop: () => void
}

export function Composer({ value, pending, onChange, onSubmit, onStop }: ComposerProps) {
  return (
    <div className="border-t bg-background/95 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] backdrop-blur">
      <form
        className="mx-auto flex w-full max-w-4xl items-end gap-2 rounded-xl border bg-card p-2 shadow-sm focus-within:ring-3 focus-within:ring-ring/30"
        onSubmit={(event) => {
          event.preventDefault()
          onSubmit()
        }}
      >
        <Textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault()
              onSubmit()
            }
          }}
          placeholder="Задайте вопрос по методическим материалам…"
          aria-label="Вопрос"
          rows={1}
          className="max-h-48 min-h-11 resize-none border-0 bg-transparent shadow-none focus-visible:ring-0"
        />
        {pending ? (
          <Button type="button" size="icon-lg" variant="outline" onClick={onStop} aria-label="Остановить">
            <Square />
          </Button>
        ) : (
          <Button type="submit" size="icon-lg" disabled={!value.trim()} aria-label="Отправить вопрос">
            <Send />
          </Button>
        )}
      </form>
      <p className="mx-auto mt-2 max-w-4xl text-center text-xs text-muted-foreground">
        Ответ формируется по внутренним материалам. Проверяйте важные сведения по источникам.
      </p>
    </div>
  )
}
