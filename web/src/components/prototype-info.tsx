import {
  ArrowUpRight,
  FlaskConical,
  MessageCircle,
  SearchCheck,
  ShieldAlert,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { SUGGESTED_QUESTIONS } from "@/lib/suggested-questions"
import { cn } from "@/lib/utils"

type PrototypeInfoProps = {
  className?: string
  onSelectQuestion: (question: (typeof SUGGESTED_QUESTIONS)[number]) => void
}

export function PrototypeInfo({
  className,
  onSelectQuestion,
}: PrototypeInfoProps) {
  return (
    <aside
      aria-label="Информация о статусе Athena"
      className={cn("min-w-0 space-y-5", className)}
    >
      <Alert
        role="note"
        className="border-warning-border/50 bg-warning/10 px-4 py-4 text-warning-foreground shadow-xs"
      >
        <FlaskConical aria-hidden="true" className="mt-0.5" />
        <AlertTitle className="text-base font-semibold">
          Демонстрационный прототип
        </AlertTitle>
        <AlertDescription className="space-y-3 text-left text-sm leading-5 text-warning-foreground/90 [&_p:not(:last-child)]:mb-0">
          <p>
            Athena — экспериментальная версия, а не готовый рабочий инструмент.
          </p>
          <p>
            Сейчас система работает только с методическими материалами по
            ментальной арифметике.
          </p>
          <p>
            Данные и ответы ещё не прошли полноценный этап evaluation, поэтому
            результаты могут быть неполными или неточными. Проверяйте важную
            информацию по указанным источникам.
          </p>

          <div className="grid gap-2 pt-1" aria-label="Ограничения прототипа">
            <div className="flex items-start gap-2 rounded-md border border-warning-border/40 bg-background/65 px-2.5 py-2 font-medium text-foreground">
              <SearchCheck
                aria-hidden="true"
                className="mt-0.5 size-4 shrink-0 text-warning-foreground"
              />
              <span>Только ментальная арифметика</span>
            </div>
            <div className="flex items-start gap-2 rounded-md border border-warning-border/40 bg-background/65 px-2.5 py-2 font-medium text-foreground">
              <ShieldAlert
                aria-hidden="true"
                className="mt-0.5 size-4 shrink-0 text-warning-foreground"
              />
              <span>Evaluation ещё не проводился</span>
            </div>
          </div>
        </AlertDescription>
      </Alert>

      <Separator />

      <section
        aria-labelledby="suggested-questions-title"
        className="space-y-3"
      >
        <div className="space-y-1">
          <h2 id="suggested-questions-title" className="text-sm font-semibold">
            Попробуйте спросить
          </h2>
          <p className="text-xs leading-5 text-muted-foreground">
            Примеры запросов, для которых в базе есть материалы
          </p>
        </div>

        <div className="grid min-w-0 gap-2.5">
          {SUGGESTED_QUESTIONS.map((question) => (
            <Button
              key={question}
              type="button"
              variant="outline"
              aria-label={`Заполнить поле вопросом: ${question}`}
              className="h-auto min-h-12 w-full min-w-0 justify-start gap-2.5 whitespace-normal border-primary/20 bg-primary/5 px-3 py-2.5 text-left font-medium leading-5 text-foreground shadow-xs hover:border-primary/35 hover:bg-primary/10 focus-visible:ring-primary/30"
              onClick={() => onSelectQuestion(question)}
            >
              <MessageCircle
                aria-hidden="true"
                className="size-4 shrink-0 text-primary"
              />
              <span className="min-w-0 flex-1 wrap-break-word">{question}</span>
              <ArrowUpRight
                aria-hidden="true"
                className="size-4 shrink-0 text-primary"
              />
            </Button>
          ))}
        </div>
      </section>
    </aside>
  )
}
