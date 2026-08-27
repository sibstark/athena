# Интеграция Bitrix24 Chat Bot с Athena

Статус исследования: реализовано в API-контракте Athena и зафиксировано на 2026-08-04.

## Короткий вывод

Bitrix-бот может работать как thin adapter между Bitrix24 и Athena:

```text
Bitrix Chat
  → событие сообщения
  → Bitrix-бот определяет пользователя и его разрешённые коллекции
  → POST /v1/answer { query, collections }
  → ответ в исходный Bitrix dialog
```

Вся информация о Bitrix, пользователе, роли, отделе и разрешениях находится только в боте.

`api` является универсальным API. Он не знает о Bitrix, web-приложении, ролях и назначении коллекций. Он получает запрос и список коллекций, выполняет поиск в них и возвращает ответ с источниками.

## Bitrix Chat Bots 2.0

Предоставленный tutorial следует считать устаревшим по сравнению с актуальным Chat Bots 2.0 API:

- [старый tutorial Chat Bots](https://apidocs.bitrix24.ru/tutorials/chat-bots/index.html);
- [актуальный обзор Chat Bots 2.0](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/index.html);
- [`imbot.v2.Bot.register`](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/bots/bot-register.html);
- [`imbot.v2.Event.get`](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/events/event-get.html);
- [`imbot.v2.Chat.Message.send`](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/messages/chat-message-send.html).

Рекомендуемый тип регистрации — `type=bot`. Такой бот активируется при mention в групповой беседе и при личном сообщении. Типы `supervisor` и `personal` для этого сценария не нужны: они получают существенно более широкий объём сообщений.

Событие, которое требуется обрабатывать, — `ONIMBOTV2MESSAGEADD`. В нём доступны:

- текст сообщения;
- `message.authorId`;
- объект `user` с Bitrix `user.id`;
- отделы пользователя;
- объект `chat` с `dialogId`;
- тип и идентификатор чата.

Один и тот же обработчик подходит для групповых и личных сообщений. Для ответа нужно использовать `dialogId` из события:

- группа: обычно `chat{chatId}`;
- личный диалог: идентификатор пользователя.

Бот не должен вычислять `dialogId` по имени пользователя или тексту сообщения.

### Получение событий

Для согласованного MVP выбран `eventMode=webhook` через существующий HTTPS Nginx:

- Bitrix отправляет POST на публичный URL;
- Nginx завершает TLS и проксирует запрос во внутренний `bot:8080`;
- бот проверяет событие, возвращает HTTP 200 и асинхронно вызывает `/v1/answer`, после чего отправляет ответ в исходный `dialogId`.

Это сохраняет требуемый сценарий «одно сообщение → один ответ» без polling, очереди и собственной базы. Вызов `/v1/answer` ограничен таймаутом; при ошибке бот пытается отправить стандартное сообщение-заглушку. В MVP нет deduplication storage: память диалога и история сообщений намеренно не сохраняются.

Перед началом webhook-режима бот регистрируется через `imbot.v2.Bot.register` с `type=bot`, `eventMode=webhook` и `webhookUrl`, получает `botId` и использует `imbot.v2.Chat.Message.send` для ответа. Для интеграции с пользовательскими и HR-методами используется OAuth-приложение с необходимыми scopes. Общая документация: [REST authorization](https://apidocs.bitrix24.ru/api-reference/how-to-call-rest-api/authorization.html).

## Идентификация и роли пользователя

В событии сообщения уже присутствует `user.id`, поэтому отдельный способ идентификации для личных сообщений не нужен.

Для обогащения профиля бот может использовать:

- [`user.current`](https://apidocs.bitrix24.ru/api-reference/user/user-current.html);
- [`user.get`](https://apidocs.bitrix24.ru/api-reference/user/user-get.html);
- `im.user.get`;
- `humanresources.node.list` / `humanresources.node.get`;
- `humanresources.node.member.field.get`;
- `im.department.employees.get`;
- `im.department.managers.get`.

У Bitrix нет единого универсального поля, которое всегда означает бизнес-роль пользователя. `WORK_POSITION` является свободным текстом и не должен быть единственным источником авторизации.

Для роли менеджера надёжнее использовать HR-роли подразделения:

- `MEMBER_HEAD`;
- `MEMBER_DEPUTY_HEAD`;
- `MEMBER_EMPLOYEE`.

Преподаватель, бухгалтер и IT обычно определяются через заранее настроенные ID отделов либо через отдельное согласованное пользовательское поле. Реальные ID отделов пока не настроены, поэтому они должны находиться в конфигурации бота, а не в `api`.

Пример bot policy:

```json
{
  "rules": [
    {
      "name": "teacher",
      "departmentIds": [101],
      "collections": ["methodology_e5_small_v1"]
    },
    {
      "name": "accountant",
      "departmentIds": [202],
      "collections": ["accounting_e5_small_v1"]
    },
    {
      "name": "it",
      "departmentIds": [303],
      "collections": ["it_docs_e5_small_v1"]
    }
  ],
  "unknownCollections": ["methodology_e5_small_v1"]
}
```

Если пользователь совпадает с несколькими правилами, бот объединяет разрешённые коллекции. Если роль не определена или HR API недоступен, бот не должен передавать пустой список с последующим fallback на все данные.

## Qdrant-коллекции как граница данных

Qdrant поддерживает независимые именованные коллекции. Каждая коллекция содержит собственные points и vectors. Для Athena рекомендуется держать домены отдельно:

```text
methodology_e5_small_v1
accounting_e5_small_v1
it_docs_e5_small_v1
salary_e5_small_v1
```

Go-indexer в `fetcher` поддерживает отдельный запуск:

```bash
cd fetcher
go run ./cmd/fetcher index \
  --input ../data/normalized/accounting/chunks.jsonl \
  --collection accounting_e5_small_v1
```

Все коллекции, которые участвуют в одном запросе, должны использовать совместимые параметры embedding pipeline:

- embedding-модель из обязательного корневого `.env`;
- закреплённая revision;
- размерность 384;
- Cosine.

Для другой embedding-модели нужна отдельная совместимая конфигурация и отдельный migration plan.

Коллекция — это техническая граница данных. Она не содержит Bitrix role и не знает, почему конкретный клиент получил её имя.

## Универсальный API-контракт

Основной запрос:

```http
POST /v1/answer
Authorization: Bearer <service-token>
Content-Type: application/json
```

```json
{
  "query": "Что такое ментальная арифметика",
  "collections": [
    "methodology_e5_small_v1"
  ]
}
```

Несколько коллекций:

```json
{
  "query": "Какие правила начисления зарплаты?",
  "collections": [
    "methodology_e5_small_v1",
    "salary_e5_small_v1"
  ]
}
```

Поведение `api`:

1. embedding запроса выполняется один раз;
2. поиск выполняется в каждой переданной коллекции;
3. результаты объединяются и сортируются по score;
4. дубли удаляются;
5. формируется общий контекст;
6. запускается существующий DeepSeek flow;
7. источники ответа содержат имя коллекции.

API не добавляет и не удаляет коллекции, исходя из каких-либо данных о пользователе. Он работает ровно с тем массивом, который получил от вызывающего клиента.

Поле `collections` обязательно для `/v1/search` и `/v1/answer`. Запрос без непустого списка получает `400 invalid_request`; Bitrix-бот всегда передаёт настроенную коллекцию явно.

## Пример сценария

1. Преподаватель пишет в группе: `@Athena Что такое ментальная арифметика`.
2. Bitrix отправляет `ONIMBOTV2MESSAGEADD`.
3. Бот извлекает `user.id`, `message.text` и `chat.dialogId`.
4. Бот определяет, что пользователь относится к преподавателям.
5. Бот формирует запрос только с `methodology_e5_small_v1`.
6. Бот вызывает `/v1/answer`.
7. Бот отправляет ответ через `imbot.v2.Chat.Message.send` в тот же `dialogId`.

Если преподаватель спрашивает о бухгалтерских документах, бот не добавляет `accounting_e5_small_v1`. API не должен знать, что коллекция была запрещена: он просто получает список, содержащий только методические материалы.

## Граница доверия

Коллекции в запросе — входной параметр доверенного клиента, а не ACL внутри API.

Следствие архитектуры:

- Bitrix-бот обязан хранить policy и service credentials на сервере;
- API нельзя вызывать с пользовательским токеном, если пользователь может самостоятельно менять `collections`;
- Qdrant и внутренний API не должны быть опубликованы напрямую;
- сырые credentials не следует индексировать в `it_docs_e5_small_v1`;
- для реальных секретов потребуется отдельный защищённый secret store.

Добавление Bitrix ACL внутрь `api` противоречило бы выбранной модели и не планируется.

## Реализованные изменения Athena

- `SearchRequest` и `SearchOptions` поддерживают `collections`;
- Qdrant client делает fan-out по нескольким коллекциям;
- embedding создаётся один раз на запрос;
- результаты объединяются в retrieval service;
- в `SearchResult` и `AnswerSource` добавлено имя коллекции;
- `/v1/search` возвращает `retrieval.collections`;
- текст `insufficient_context` больше не привязан к методическим материалам;
- web API client умеет передавать коллекции, не добавляя Bitrix-логики в API;
- существующий single-collection режим сохранён для совместимости.

## Проверки

Обязательные сценарии:

- один запрос в одну коллекцию;
- один запрос в несколько коллекций;
- один embedding на несколько поисков;
- корректный merge и deduplication;
- сохранение имени коллекции в источнике;
- пустое имя коллекции отклоняется;
- Bitrix-бот никогда не передаёт коллекцию, не разрешённую его policy;
- личное и групповое Bitrix-сообщение используют один event flow;
- неизвестная Bitrix-роль не получает unrestricted collection list.

## Источники Bitrix

- [Chat Bots 2.0 overview](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/index.html)
- [`imbot.v2.Bot.register`](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/bots/bot-register.html)
- [`imbot.v2.Event.get`](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/events/event-get.html)
- [`imbot.v2.Chat.Message.send`](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/messages/chat-message-send.html)
- [REST authorization](https://apidocs.bitrix24.ru/api-reference/how-to-call-rest-api/authorization.html)
- [`user.current`](https://apidocs.bitrix24.ru/api-reference/user/user-current.html)
- [`user.get`](https://apidocs.bitrix24.ru/api-reference/user/user-get.html)
- [устаревший Chat Bots tutorial](https://apidocs.bitrix24.ru/tutorials/chat-bots/index.html)

## Источники Qdrant

- [Collections](https://qdrant.tech/documentation/manage-data/collections/)
- [Search](https://qdrant.tech/documentation/search/search/)
