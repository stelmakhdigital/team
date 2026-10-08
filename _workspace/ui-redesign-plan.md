# UI Redesign Plan (R1–R5) — 2026-10-08, лид

> Решение пользователя: редизайн в стиле openRIG (топология — центр), сохраняем наш
> дашборд с целевыми метриками и тёмную тему. Детали референса —
> `_workspace/openrig-ui-research.md`.

## Принятые решения
| # | Вопрос | Решение |
|---|--------|---------|
| Q1 | Редактирование | **Hybrid**: по умолчанию авто-layout (toposort, контейнеры-сегменты); режим редактирования мышью (drag-файн-тюнинг, создание/удаление связей) — наши PATCH/POST/DELETE эндпоинты |
| Q2 | Стек графа | **React Flow (@xyflow/react)** — pan/zoom/handles/parent-child/анимации |
| Q3 | Live-данные нод | **Полный паритет**: activity/ring, runtime, uptime, очередь + **context%, tokens, live-терминал** (расширяем контракт/backend — R4) |
| Q4 | Термины | Не переименовываем (segment/role/relative; визуально = pod/seat/edge) |

## Архитектура нового графа
- `src/components/Topology/*` (React Flow):
  - `TopologyCanvas.tsx` — обёртка ReactFlow: pan/zoom/minimap, dark fit, dnd-режим;
  - `nodes/RoleNode.tsx` — live-карточка роли (openRIG-стиль: activity dot + ring,
    runtime+model, context% (slots «--» до R4), tokens, uptime, очередь задач);
  - `nodes/SegmentGroupNode.tsx` — dashed-фрейм (label + count, glass);
  - `edges/TaskEdge.tsx` — bezier + пунктир-направление + «пакет» (animateMotion)
    при активных задачах по связи; стили по kind; non-scaling-stroke; reduced-motion;
  - `layout/autoLayout.ts` — toposort по `delegates_to`/`spawned_by` (fallback: все
    edges), один столбец, segments — контейнеры (до 3 колонок, 240×160, gaps 36/32/120,
    padding 28/44/28); сохраняет пользовательский layout, если он есть (hybrid),
    иначе авто; кнопка «Reset to auto»;
  - `TopologyEditToolbar.tsx` — режим edit: palette (добавить segment/role),
    connect (handle→handle), delete, validate/save (наша BottomPanel-логика).
- Данные: `useQuery` (topology) + `useWebSocket` (session.started/stopped/failed,
  task.created/state_changed, alert.created → обновление нод; 1s-тик не нужен —
  события event-driven + refetch по событию).
- Вид Table — таблица ролей (state/runtime/uptime) — fallback на узких экранах
  и как альтернативный таб (как у openRIG).

## Фазы
- **R1 — ГРАФ (ядро)**: React Flow, авто-layout, RoleNode/SegmentGroupNode/TaskEdge
  (с текущими данными: state, runtime, uptime, очередь), zoom/minimap, dark fit,
  replace нашего SVG-canvas на TeamBuilderPage. Тесты: autoLayout (unit), appSmoke.
- **R2 — РЕДАКТИРОВАНИЕ (hybrid)**: edit-режим (drag с сохранением позиций в layout —
  наши PATCH layout), create/delete segments/roles/relatives мышью, palette,
  validate/save, «Reset to auto». Тесты: unit (interactions через mock API).
- **R3 — ВИЗУАЛЬНАЯ КОНСISTЕНТНОСТЬ**: дизайн-токены (surface/on-surface), mono-детали,
  карточки/фреймы на остальных страницах (dashboard-панели, tasks, messages, library,
  history) в едином стиле; таблица топологии; reduced-motion; полировка dашборда
  (сохраняем — нравится).
- **R4 — LIVE-ДАННЫЕ (контракт + backend + UI)**:
  - Контракт 20 §3.6: `SessionDetail` += `context_used_percentage?`,
    `context_total_input_tokens?`, `context_total_output_tokens?`, `model?`;
    `GET /sessions/:id/transcript` — как есть; **WS**: событие `session.output`
    (канал `session:{id}` + `dashboard`): `{session_id, lines: [{ts, text, stream}]}`
    (batch ≤ 500ms) → live-терминал в popover (static mirror → click-to-live,
    page-wide cap);
  - UI: context%/tokens на RoleNode (реальные значения), TerminalPreviewPopover.
  - Backend — отдельный срез (требования в answer_backend.md).
- **R5 — РЕФАКТОРИНГ**: хуки (useQuery/useMutation/useWebSocket — как есть),
  удаление старого SVG-canvas (TeamBuilder/*), дедупликация стейт-компонентов,
  переезд дизайн-токенов, тесты (e2e smoke на страницах), performance (memo нод).

## Риски / замечания
- React Flow — новая runtime-зависимость (~+40KB gzip) — принято (Q2).
- R4 зависит от backend-среза; UI-слоты («--») позволяют R1–R3 не ждать.
- Terminal: наш transcript — строки лога файла; live-потока сейчас нет → backend
  должен стримить tail файла по WS (срез R4).
- Mock-режим: все новые поля/события покрываем в mock (принцип «mock не отстаёт»).

## Команда/порядок
R1 → R2 → R4 (backend параллельно) → R3 → R5. Каждая фаза — коммит + зелёные тесты.
