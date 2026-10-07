# Frontend Architecture

Статус: **approved-draft** (v1)
Область: `frontend/**`
Контракт (source of truth): `docs/architecture/frontend/20_contract_API.md`, `docs/architecture/frontend/21_team_builder.md`

## 1. Цель и границы

SPA-консоль для управления командами AI-агентов: Team Builder (первый
вертикальный сценарий), Dashboard, Workflow Editor, Message Center, Library,
History Viewer. Frontend не содержит серверной бизнес-логики; валидация на
клиенте — только UX-подсказка, backend остаётся источником истины.

Backend на момент подготовки **пустой** → frontend развивается mock-first по
утверждённому контракту (см. §8).

## 2. Стек и зависимости

| Слой | Выбор | Обоснование |
|---|---|---|
| Сборка | Vite 5 | быстрая dev-сборка, стандарт |
| UI | React 18 + TypeScript | типобезопасность по контракту |
| Маршрутизация | react-router-dom v6 | client-side routing |
| Состояние | React hooks + собственный `useQuery`/store (без внешних state-либ) | минимальный набор зависимостей |
| Canvas | собственный SVG/DOM drag&drop (без react-flow) | контракт не требует внешней lib; меньше зависимостей |
| Тесты | vitest + @testing-library/react | unit + component |
| Стили | plain CSS (единый stylesheet) | без UI-фреймворка на старте |

Дополнительные зависимости не добавляются без ADR.

## 3. Структура проекта

```
frontend/
├── index.html
├── package.json
├── vite.config.ts
├── tsconfig.json
└── src/
    ├── main.tsx                 # вход, Router
    ├── App.tsx                  # Layout + <Routes>
    ├── styles.css
    ├── types/api.ts             # ВСЕ типы 1-в-1 с контрактом (20 + 21)
    ├── api/
    │   ├── config.ts            # API mode: mock|real, base URL, API key header
    │   ├── client.ts            # fetch-обёртка: http<T>(), ApiClientError
    │   ├── adapter.ts           # интерфейс ApiAdapter + выбор impl по config
    │   ├── teams.ts             # GET/POST teams, segments, roles, relatives, validate, save, topology
    │   ├── workflows.ts
    │   ├── dashboard.ts
    │   ├── messages.ts
    │   ├── library.ts
    │   ├── history.ts
    │   └── mock/
    │       ├── data.ts          # seed-данные строго по контракту
    │       ├── store.ts         # in-memory store с мутациями
    │       └── adapter.ts       # mock-реализация ApiAdapter (latency, errors)
    ├── hooks/
    │   ├── useQuery.ts          # loading/error/data + refetch + cancel (AbortController)
    │   ├── useMutation.ts       # pending/error + onSuccess
    │   └── useWebSocket.ts      # WS-клиент с reconnect (режим mock: синтетика)
    ├── components/
    │   ├── ui/                  # Button, Card, Badge, Spinner, EmptyState,
    │   │                        # ErrorState, Field, Select, Modal, Toast
    │   ├── layout/              # AppShell, Sidebar, Topbar
    │   ├── TeamBuilder/         # TeamBuilderPage-виджеты (см. §5)
    │   ├── Dashboard/           # SummaryCards, TaskList, SessionGrid, AlertsPanel, MetricsChart
    │   ├── WorkflowEditor/
    │   ├── MessageCenter/
    │   ├── Library/
    │   └── History/
    ├── pages/
    │   ├── DashboardPage.tsx
    │   ├── TeamsPage.tsx
    │   ├── TeamBuilderPage.tsx
    │   ├── WorkflowsPage.tsx
    │   ├── WorkflowEditorPage.tsx
    │   ├── MessagesPage.tsx
    │   ├── LibraryPage.tsx
    │   └── HistoryPage.tsx
    └── lib/
        ├── format.ts            # ISO-даты, uptime, память
        └── topology.ts          # client-side валидация топологии (UX)
```

## 4. Маршруты

| Path | Page | Данные |
|---|---|---|
| `/` | DashboardPage | `/dashboard/summary|tasks|sessions|alerts|metrics` |
| `/teams` | TeamsPage | `GET /teams` |
| `/teams/:id` | TeamBuilderPage | `GET /teams/:id/topology`, CRUD segments/roles/relatives, `validate`, `save` |
| `/workflows` | WorkflowsPage | список из `GET /teams` (workflow-список endpoint **открыт** — см. blockers) |
| `/workflows/:id` | WorkflowEditorPage | `GET/POST /workflows…` |
| `/messages` | MessagesPage | `GET /messages`, `GET /chatrooms` |
| `/library` | LibraryPage | `GET /library` |
| `/history` | HistoryPage | `GET /audit`, history task/session |
| `*` | NotFound | — |

## 5. Team Builder (первый вертикальный сценарий)

Компоненты:

- `TeamCanvas` — SVG-холст: pan/zoom, grid, snap; drag&drop сегментов и ролей
  (нативные pointer events; position пишется в локальное состояние,
  `PATCH …/layout` — debounced 300ms).
- `SegmentBlock` / `RoleBlock` — перетаскиваемые узлы, highlight при
  перетаскивании из тулбара.
- `RelativeConnector` — bezier-кривая между ролями, label типа связи, клик —
  выбрать/удалить.
- `Toolbar` (слева) — палитра Segments/Roles/Relations (drag на холст).
- `ConfigPanel` (справа) — конфигурация выбранной роли/сегмента
  (`GET/PATCH /roles/:id/config`), клиентская валидация полей.
- `BottomPanel` — zoom/grid/snap, `[Validate]` (`POST /teams/:id/validate` —
  server truth) + local hints из `lib/topology.ts`, `[Save to Library]`
  (`POST /teams/:id/save`).
- `ConnectionTool` — создание связи: from → клик по to → выбор типа.

Поток данных сценария:
`TeamsPage → create team (POST /teams) → TeamBuilderPage → getTopology →
render canvas → drop segment/role (POST, оптимистичное добавление) → connect
(POST /relatives) → PATCH layout (debounce) → validate → save`.

## 6. Состояние

- Серверное состояние — через `useQuery(key, fetcher)` с кешем по key,
  инвалидацией после мутаций, отменой устаревших запросов (AbortController).
- Локальное UI-состояние (выделение, zoom, drag) — в `useState` компонента.
- Global (toast'ы, mock mode indicator) — лёгкий React context.
- Optimistic updates: только для layout-drag и add-segment/role (откат при
  ошибке). Бизнес-состояние (state задач, сессий) — только из ответа API/WS.

## 7. API client

- `adapter.ts` определяет единый интерфейс для всех доменов; impl выбирается:
  `VITE_API_MODE=mock` (default) → `MockAdapter`; `real` → `HttpAdapter`
  (fetch, base URL `VITE_API_BASE_URL`, `Authorization: Bearer` из
  `VITE_API_KEY`, если задан — см. open question по auth).
- HTTP-клист нормализует ошибки в `ApiClientError { status, code, message }`.
  Формат тела ошибок в контракте **не зафиксирован** → frontend принимает и
  `{ error: { code, message } }`, и `{ message }`, и текст (blockers.md #2).
- Контракт использует offset-пагинацию (`limit`/`offset`, `has_more`) —
  поддерживается как есть.

## 8. Mock-first стратегия

- Mock-данные — единственное представление: `src/api/mock/data.ts`, строго по
  типам контракта (compile-time проверка).
- `MockAdapter` реализует те же методы, что и HttpAdapter: in-memory store,
  задержка 150–400ms, ID-последовательности, мутации (create/patch/delete),
  валидация топологии (те же коды: `ROLE_NO_AGENT_SPEC`,
  `CIRCULAR_DEPENDENCY`, `ORPHAN_ROLE`, `NO_OUTGOING_EDGES` …).
- Симуляция состояний: `?mockError=unauthorized|forbidden|not_found|conflict|
  server|network` (query param / dev-переключатель) для проверки UI states.
- Mock не зашит в компоненты: UI знает только `api.*` методы.
- Переключение на реальный backend — одна env-переменная, без кода.

## 9. UX-состояния

Каждый data-экран: `loading` (skeleton/spinner) → `success` | `empty`
(EmptyState + CTA) | `error` (ErrorState + Retry: unauthorized → «нет доступа»,
conflict → подсказка обновить данные, server/network → retry). Toast для
результата мутаций. Нет скрытия ошибок пустым экраном.

## 10. Real-time

`useWebSocket(url, channels)`: reconnect с backoff, resubscribe. В mock-режиме
генерирует синтетические события (`task.state_changed`, `alert.created`,
`message.sent`) для проверки живых панелей Dashboard/Messages.

## 11. Тестирование

- Unit: `lib/topology.ts` (циклы, orphan, required fields), `api/client.ts`
  (ошибки), mock-adapter (контракт: формы ответов).
- Component: TeamCanvas (drop/связи), ConfigPanel (валидация), Dashboard
  states (empty/error).
- Команды: `npm run lint` (tsc --noEmit + eslint), `npm test`,
  `npm run build`.

## 12. Конфигурация

| Env | Default | Назначение |
|---|---|---|
| `VITE_API_MODE` | `mock` | `mock` \| `real` |
| `VITE_API_BASE_URL` | `http://localhost:8080` | базовый URL REST |
| `VITE_WS_URL` | `ws://localhost:8080/ws` | WebSocket |
| `VITE_API_KEY` | — | Bearer key (auth не зафиксирован в ТЗ) |

## 13. Риски и ограничения

1. **Ошибка-модель не зафиксирована** → флекс-парсинг (см. §7), запрос в
   backend на каноническую схему (blockers #2).
2. **Auth не определён** в контракте → задел под Bearer, open question
   (blockers #1).
3. **Список workflows** endpoint в контракте отсутствует (есть `GET
   /workflows/:id`) → WorkflowsPage на старте перечисляет workflows по team
   (нужен `GET /api/v1/workflows`) — blockers #3.
4. Контракт 21 расширяет request'ы полями `layout` — mock поддерживает,
   backend должен принять (blockers #4).
5. Drag&drop без внешней lib — больше кода, но без риска версий.

## 14. План реализации (frontend-план)

| Этап | Содержание | Статус |
|---|---|---|
| F1 | Каркас: Vite+React+TS, layout, маршруты, env config | ✅ done |
| F2 | Типы по контракту, api client, mock adapter + seed | ✅ done |
| F3 | Dashboard (summary, tasks, sessions, alerts, metrics) на mocks | ✅ done |
| F4 | **Team Builder вертикальный срез**: list → create → canvas (drop, drag, connect, config, validate, save) | ✅ done |
| F5 | UX-states (empty/error/loading/retry), toasts, dev-error-simulator | ✅ done |
| F6 | Message Center, Library, History — базовые экраны на mocks | ✅ done |
| F7 | Workflow Editor — базовый (canvas blocks/connections) | ✅ done |
| F8 | WebSocket (mock-синтетика), unit-тесты (topology, client, mock adapter, app smoke) | ✅ done |
| F9 | Интеграция с реальным backend (переключение `VITE_API_MODE=real`) | ⏳ после backend |
