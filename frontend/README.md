# Team Console — Frontend

SPA-консоль управления командами AI-агентов. Mock-first: по умолчанию работает
на контракт-совместимом mock API, переключение на реальный backend — одной
env-переменной.

Документы:
- архитектура и план: `docs/architecture/frontend.md`
- API-контракт (source of truth): `docs/architecture/frontend/20_contract_API.md`, `21_team_builder.md`
- статус: `_workspace/frontend-status.md`, блокеры: `_workspace/blockers.md`

## Стек

Vite 5 · React 18 · TypeScript · react-router-dom v6 · vitest + Testing Library.
Дополнительных runtime-зависимостей нет.

## Команды

```bash
npm install
npm run dev         # http://localhost:5173 (mock mode)
npm run typecheck   # tsc --noEmit
npm test            # vitest run
npm run build       # typecheck + production build
```

## Конфигурация (.env)

| Переменная | Default | Назначение |
|---|---|---|
| `VITE_API_MODE` | `mock` | `mock` — in-browser mock API; `real` — fetch к backend |
| `VITE_API_BASE_URL` | `http://localhost:8080` | REST base URL (real) |
| `VITE_WS_URL` | `ws://localhost:8080/ws` | WebSocket (real) |
| `VITE_API_KEY` | — | Bearer key (auth TBD, blockers #1) |

## Маршруты

`/` Dashboard · `/teams` · `/teams/:id` Team Builder · `/workflows` ·
`/workflows/:id` · `/messages` · `/library` · `/history`

## Mock

- `src/api/mock/adapter.ts` — реализация того же интерфейса `Api`, что и
  `src/api/real.ts`; данные строго по типам `src/types/api.ts` (зеркало контракта).
- Simulate-ошибки: селектор в sidebar (unauthorized/forbidden/not_found/
  conflict/server/network) — для проверки error states UI.
- WS mock генерирует синтетические события контракта каждые 8s.

## Структура

```
src/
├── types/api.ts        # типы 1-в-1 с контрактом
├── api/                # client, real/mock адаптеры, config, errors
├── hooks/              # useQuery (caching/cancel), useMutation, useWebSocket
├── components/         # ui/, layout/, TeamBuilder/, Dashboard/, …
├── pages/              # страницы-маршруты
└── lib/                # topology validation, format
```
