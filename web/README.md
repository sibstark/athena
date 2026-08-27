# Athena Web

React/Vite-клиент для `api`. Интерфейс отправляет same-origin запросы с `credentials: "include"` и использует только `HttpOnly` session cookie. `API_KEY` предназначен для внутренних клиентов и браузерному proxy не передаётся.

## Технологии

- Bun, React 19, Vite 8, TypeScript;
- Tailwind CSS v4 через официальный `@tailwindcss/vite`;
- shadcn/ui (`radix-nova`), Radix primitives, CSS variables и Lucide;
- `react-markdown` с GFM и отключённым raw HTML.

Tailwind подключён через `@import "tailwindcss"`; `tailwind.config.js` отсутствует. Alias `@/*` настроен в TypeScript, Vite и `components.json`.

## Локальная разработка

Из корня репозитория настройте `.env`:

```env
API_DEV_TARGET=http://127.0.0.1:8090
AUTH_USERNAME=admin
AUTH_PASSWORD_HASH='<bcrypt-хеш>'
AUTH_SESSION_SECRET=<случайный секрет минимум 32 байта>
```

Затем:

```bash
cd web
bun install
bun run dev
```

Откройте `http://127.0.0.1:5173`. Vite проксирует `/v1/*` без добавления Bearer token; cookie устанавливается для same-origin приложения. Не помещайте пароль, bcrypt-хеш, session secret или API key в переменные с префиксом `VITE_`.

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

Приложение доступно на `http://127.0.0.1:${WEB_PORT:-3000}`. nginx проксирует `/v1/*` и cookie в `api`, одновременно удаляя браузерный заголовок `Authorization`.

При публикации используйте HTTPS и `AUTH_COOKIE_SECURE=true`. Корневой Compose содержит opt-in профиль `https` на базе `umputun/nginx-le:v1.4.3`; настройка домена и команды запуска описаны в корневом README. Не публикуйте напрямую порты TEI, Qdrant или `api`.

## Источники

Ответ и массив `sources` приходят одним ответом `/v1/answer`. UI не делает отдельного запроса к Qdrant. `[S1]` связывается только с объектом, у которого `source_id === "S1"`; неизвестная `[S99]` остаётся обычным текстом. Безопасное HTTP(S)-поле `source.url` показывается в деталях как ссылка «Открыть источник в БЗ» и открывается в новой вкладке.
