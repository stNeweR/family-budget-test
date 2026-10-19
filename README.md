# Family Budget

Сервис учёта семейных расходов: Telegram-бот с ИИ-агентом и веб-панель для главы семьи.

Член семьи пишет боту «кофе 350» — бот сам определяет сумму, категорию и дату и сохраняет трату.
Веб-панель показывает траты всех членов семьи и позволяет их редактировать.

## Стек

| Слой | Технология |
|---|---|
| Бэкенд | Go 1.24, chi, pgx |
| БД | PostgreSQL 16 |
| Миграции | goose (CLI) |
| LLM | OpenRouter (OpenAI-совместимый API), `openrouter/free` |
| Бот | собственный пакет `pkg/telegram`, доставка через webhook или long polling |
| Фронтенд | Vue 3 + TypeScript + Vite + Tailwind CSS 4 (dev-сервер) |
| Запуск | docker compose + Makefile |

## Структура

```
backend/
  cmd/app/main.go        # запуск приложения
  cmd/seed/main.go       # генератор тестовых данных
  cmd/botcommands/main.go # меню команд бота в Telegram
  internal/
    app/                 # composition root и graceful shutdown
    entity/              # сущности, ошибки, LLM-типы, фильтры
    repo/
      contracts.go       # контракты репозиториев
      persistent/        # PostgreSQL-репозитории
      webapi/llm/        # LLM-адаптер
    usecase/             # use cases и агент
    controller/
      restapi/           # REST API
      telegram/          # транспорт и команды Telegram
    testsupport/         # общая тестовая PostgreSQL
  pkg/
    postgres/            # пул и schema/reset
    telegram/            # низкоуровневый Telegram-клиент
  config/                # конфигурация из env
  migrations/            # goose SQL
frontend/                # Vue 3 + TS (Vite dev server)
docker/
  go/Dockerfile          # образ backend: server + seed + goose
  node/Dockerfile        # образ frontend: vite dev server
docker-compose.yml       # postgres + migrate + backend + frontend
Makefile
.env.example
```

## Быстрый старт

```bash
cp .env.example .env          # заполните LLM_API_KEY и TELEGRAM_BOT_TOKEN
make up                       # postgres + миграции + backend + frontend
make seed-docker              # (опционально) демо-данные
```

Миграции применяет отдельный one-shot контейнер `migrate` до старта backend —
ручной шаг не нужен. Бот с одним `TELEGRAM_BOT_TOKEN` работает через long polling,
без публичного URL и туннеля.

- Панель: <http://localhost:5173>
- Backend API: <http://localhost:8080>

Остановить: `make down`.

## Как это работает

### Четыре контейнера

- `postgres` — база данных.
- `migrate` — one-shot сервис из того же образа backend: выполняет `goose up` и завершается.
  `backend` стартует только после его успешного завершения
  (`depends_on: service_completed_successfully`), поэтому crash-loop на немигрированной схеме
  невозможен.
- `backend` — образ из `docker/go/Dockerfile`: Go-сервер (`server`), а также `seed` и `goose`
  для разовых запусков.
- `frontend` — образ из `docker/node/Dockerfile`: `npm run dev` (Vite dev-сервер).
  Исходники монтируются из `./frontend` (hot-reload), `node_modules` живёт в анонимном томе.
  Vite проксирует `/api` и `/telegram` на `http://backend:8080`.

Это **dev-конфигурация**: фронт раздаётся dev-сервером Vite, а не собранной статикой.
Для продакшена фронт обычно собирают (`npm run build`) и отдают через nginx; в этом репозитории
выбран dev-режим ради простоты запуска одной командой.

### Миграции и сид

В compose есть отдельный one-shot сервис `migrate`: он использует тот же образ, что и
`backend`, монтирует `backend/migrations` и выполняет `goose up` до старта приложения.

```bash
make migrate-status-docker   # статус миграций
make migrate-down-docker     # откатить последнюю
make migrate-up-docker       # применить вручную (обычно не нужно: делает `make up`)
```

Сид запускается разовым контейнером:

```bash
make seed-docker             # или: docker compose run --rm --entrypoint /app/seed backend -reset
```

Приложение всё равно проверяет схему при старте (`CheckSchema`) и падает с сообщением, если миграции не применены (например, при локальном запуске без compose).

### Telegram-бот: long polling или webhook

Режим выбирается автоматически по конфигу:

- **long polling** (по умолчанию) — если задан только `TELEGRAM_BOT_TOKEN`. Бот сам опрашивает
  `getUpdates` с таймаутом 50 секунд; публичный URL и туннель не нужны. На старте вызывается
  `deleteWebhook` (иначе `getUpdates` возвращает 409), накопленные апдейты не теряются и
  обрабатываются после рестарта. Обработка последовательная; при сбоях — backoff, при 429 —
  пауза из `retry_after`.
- **webhook** — если дополнительно задан `TELEGRAM_WEBHOOK_URL`. Бот принимает апдейты на
  `POST /telegram/webhook`, проверяет заголовок `X-Telegram-Bot-Api-Secret-Token`, отвечает
  `200` сразу (обработка асинхронная) и вызывает `setWebhook` на
  `<TELEGRAM_WEBHOOK_URL><TELEGRAM_WEBHOOK_PATH>`.

Для webhook-режима нужен публичный HTTPS-URL, например через (localtunnel)[https://theboroer.github.io/localtunnel-www/], (serveo)[https://serveo.net/] или (ngrok)[https://ngrok.com/]:

```bash
make tunnel    # npx localtunnel --port 8080
# полученный https://... адрес прописать в TELEGRAM_WEBHOOK_URL и перезапустить backend
```

Если `TELEGRAM_BOT_TOKEN` не задан — бот просто не поднимается, API и панель работают.

### Команды бота

- `/start` — регистрация в системе и приветствие со списком возможностей. Обрабатывается без
  обращения к LLM, пользователь сразу появляется в веб-панели (и в групповых чатах работает
  `/start@имя_бота`).

Все остальные сообщения — обычный текст, который уходит в ИИ-агента: «кофе 350»,
«сколько потратил на еду за месяц», «удали такси».

### ИИ-агент

Агент работает по схеме **tool calling**: модель получает сообщение пользователя и набор
функций (`add_expense`, `list_expenses`, `get_total`, `update_expense`, `delete_expense`),
возвращает вызов инструмента, бэкенд исполняет его и просит модель сформулировать ответ.
Все операции исполняются строго от имени отправителя: `user_id` берётся из Telegram-апдейта,
а не из текста модели, поэтому чужие траты недоступны.

Доступ к модели настраивается через конфиг (`LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`).

## REST API

```
GET    /api/health
GET    /api/users
GET    /api/expenses?user_id=&from=&to=&category=&limit=&offset=
POST   /api/expenses
GET    /api/expenses/{id}
PATCH  /api/expenses/{id}
DELETE /api/expenses/{id}
```

Даты — `YYYY-MM-DD`. Категории: `еда, транспорт, жильё, развлечения, здоровье, покупки, другое`.

## Локальная разработка (без Docker)

```bash
# goose CLI для локальных миграций (один раз)
make tools

# бэкенд (нужен PostgreSQL)
export DATABASE_URL=postgres://family:family@localhost:5432/family?sslmode=disable
make migrate-up
make run

# фронтенд с hot-reload, проксирует /api на :8080
make web-dev
```

## Команды Makefile

`make help` — полный список. Основные:

| Команда | Действие |
|---|---|
| `make up` / `make down` | поднять / остановить (docker compose) |
| `make logs` | логи backend |
| `make tools` | установить goose CLI (та же версия, что в образе) |
| `make migrate-up` / `-down` / `-status` | миграции локально (goose в PATH) |
| `make migrate-up-docker` / `-down-docker` / `-status-docker` | миграции через образ backend |
| `make seed` / `seed-reset` / `seed-docker` | демо-данные |
| `make build` / `test` / `lint` | сборка / тесты / проверки |
| `make tunnel` | localtunnel для вебхука |

## Конфигурация

Все параметры — через переменные окружения (см. `.env.example`): `DATABASE_URL`,
`LLM_*`, `TELEGRAM_*`, порты `BACKEND_PORT`/`FRONTEND_PORT`.

## Тесты

```bash
make test-db  # поднять postgres, создать и мигрировать family_test
make test     # все тесты на реальной PostgreSQL family_test
```

`make up` также создаёт и мигрирует `family_test` в том же контейнере `postgres`.
Тесты выполняются с `-p 1`, потому что используют общую тестовую БД. Для запуска
из IDE задайте `TEST_DATABASE_URL=postgres://family:family@localhost:5432/family_test?sslmode=disable`.
