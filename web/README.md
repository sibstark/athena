# Athena Web

React/Vite-клиент для `rag-api`. Интерфейс отправляет только same-origin запросы на `/v1/answer`; ключ `RAG_API_KEY` в браузерную сборку не попадает. В Docker заголовок авторизации добавляет nginx, а при локальной разработке — Vite dev proxy.

## Технологии

- Bun, React 19, Vite 8, TypeScript;
- Tailwind CSS v4 через официальный `@tailwindcss/vite`;
- shadcn/ui (`radix-nova`), Radix primitives, CSS variables и Lucide;
- `react-markdown` с GFM и отключённым raw HTML.

Tailwind подключён через `@import "tailwindcss"`; `tailwind.config.js` отсутствует. Alias `@/*` настроен в TypeScript, Vite и `components.json`.

## Локальная разработка

Из корня репозитория настройте `.env`:

```env
RAG_API_DEV_TARGET=http://127.0.0.1:8090
RAG_API_KEY=your-local-rag-api-key
```

Затем:

```bash
cd web
bun install
bun run dev
```

Откройте `http://127.0.0.1:5173`. Значение `RAG_API_KEY` читает только сервер Vite и добавляет в проксируемый запрос; не используйте префикс `VITE_` для секретов.

## Проверки

```bash
bun run typecheck
bun run test
bun run build
```

Тесты проверяют Tooltip, keyboard focus/Escape, desktop Sheet, mobile Drawer, связь цитаты по `source_id`, сноски, fallback пути и безопасное отображение metadata.

## Docker Compose

```bash
bun run web:up
bun run web:status
bun run web:logs
```

Приложение доступно на `http://127.0.0.1:${WEB_PORT:-3000}`. nginx проксирует `/v1/*` в `rag-api` и добавляет серверный `Authorization`.

Важно: встроенный nginx не аутентифицирует конечного пользователя. При публикации сервиса внешний reverse proxy/identity-aware proxy обязан закрыть весь web origin аутентификацией и TLS. Не публикуйте напрямую порты TEI, Qdrant или `rag-api`.

## Источники

Ответ и массив `sources` приходят одним ответом `/v1/answer`. UI не делает отдельного запроса к Qdrant. `[S1]` связывается только с объектом, у которого `source_id === "S1"`; неизвестная `[S99]` остаётся обычным текстом.
