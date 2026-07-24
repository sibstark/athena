import * as React from "react"
import { BookOpen, LoaderCircle, LogIn } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { requestLogin } from "@/lib/auth"
import { RAGAPIError } from "@/lib/api"

type LoginPageProps = {
  initialError?: string
  onAuthenticated: (username: string) => void
}

export function LoginPage({ initialError, onAuthenticated }: LoginPageProps) {
  const [username, setUsername] = React.useState("")
  const [password, setPassword] = React.useState("")
  const [pending, setPending] = React.useState(false)
  const [error, setError] = React.useState<string | null>(initialError || null)

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (pending) return
    setPending(true)
    setError(null)
    try {
      const session = await requestLogin(username, password)
      setPassword("")
      onAuthenticated(session.username)
    } catch (reason) {
      setError(
        reason instanceof RAGAPIError
          ? reason.message
          : "Не удалось связаться с сервисом. Попробуйте ещё раз.",
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="grid min-h-svh place-items-center bg-muted/35 px-4 py-10 text-foreground">
      <section
        className="w-full max-w-sm rounded-2xl border bg-card p-6 shadow-sm"
        aria-labelledby="login-title"
      >
        <div className="mb-6 flex flex-col items-center text-center">
          <span className="mb-3 grid size-12 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-sm">
            <BookOpen aria-hidden="true" className="size-6" />
          </span>
          <h1 id="login-title" className="text-xl font-semibold">
            Athena
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Помощник по методическим материалам
          </p>
        </div>

        <form className="space-y-4" onSubmit={submit}>
          <div className="space-y-1.5">
            <label htmlFor="auth-username" className="text-sm font-medium">
              Логин
            </label>
            <input
              id="auth-username"
              name="username"
              type="text"
              autoComplete="username"
              required
              autoFocus
              disabled={pending}
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              className="h-10 w-full rounded-lg border border-input bg-background px-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60"
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="auth-password" className="text-sm font-medium">
              Пароль
            </label>
            <input
              id="auth-password"
              name="password"
              type="password"
              autoComplete="current-password"
              required
              disabled={pending}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              className="h-10 w-full rounded-lg border border-input bg-background px-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60"
            />
          </div>

          {error && (
            <Alert variant="destructive" aria-live="polite">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <Button
            type="submit"
            size="lg"
            className="w-full"
            disabled={pending || !username || !password}
          >
            {pending ? (
              <LoaderCircle aria-hidden="true" className="animate-spin" />
            ) : (
              <LogIn aria-hidden="true" />
            )}
            {pending ? "Входим…" : "Войти"}
          </Button>
        </form>
      </section>
    </main>
  )
}
