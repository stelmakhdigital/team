# UI-исследование: openRIG (openrig.dev, github.com/mvschwarz/openrig) — 2026-10-08, лид

> Цель: референс для редизайна нашего UI (граф топологии в первую очередь).
> Пользователю в openRIG нравится графическая топология; у нас — дашборд с метриками и
> тёмная тема (оставляем).

## 1. Что такое openRIG (кратко)

- Open-source мульти-агентный harness (Node 22/24, Hono HTTP-демон, SQLite, tmux).
  Рантаймы агентов: Claude Code, Codex, Pi, terminal.
- **RigSpec (YAML, v0.2)** — декларативная топология:
  `pods[]` (bounded context: группа seats с общим контекстом) → `members[]` (seats:
  роль-адрес `pod.member`, agent_ref, runtime, profile, model) → `edges[]`
  (pod-local: unqualified id; cross-pod: `pod.member`).
  Плюс: continuity_policy (sync/restore), startup (files/actions), services,
  RigBundle (портативный архив spec'ов, SHA-256).
- **Edge kinds (5)**: `delegates_to`, `spawned_by`, `can_observe`, `collaborates_with`,
  `escalates_to`. Только `delegates_to`/`spawned_by` влияют на runtime (порядок запуска
  через topological sort; цикл → cycle_error). Остальные — design intent (не
  маршрутизируют сообщения, не дают прав). `delegates_to` ещё задаёт «оркестратора»
  очереди для wake-ladder.
- **Наша модель почти 1-в-1**: segment↔pod, role↔seat, relative↔edge (у нас те же
  kinds: delegates_to, spawned_by, can_observe, …). Контракт переносится на их визуал
  без изменения семантики.

## 2. Топология в UI (что нравится визуально)

Две поверхности:
- **TUI** (основная в проекте): `topology` → вкладки TABLE/RECENT/OVERVIEW/**GRAPH**/
  HEALTH. GRAPH = ASCII-граф: рамка rig'а (hatched), внутри dashed-фреймы pods
  (название + счётчик агентов), внутри — карточки-ноды (имя, activity-точка, runtime-
  иконка, `> _` терминальная строка), стрелки между нодами (→ горизонтально, ▾
  вертикально). Слева — Explorer-дерево host→rig→pods. Стили: hatched/braille
  (переключение через command bar).
- **Web UI** (React + TS + **React Flow**, maintenance mode, но референс именно он):
  иерархия **host → rig → pod → seat**; вкладки Graph / Table / Terminal (+ Overview,
  Health); Tree — в сайдбаре (Explorer). Узкие viewports → граф заменяется таблицей.

### 2.1 Авто-лейаут (web) — `lib/graph-layout.ts`, `applyTreeLayout`
- **Один вертикальный столбец** сущностей, порядок — **toposort по edges
  delegates_to/spawned_by** (при их отсутствии — по всем edges).
- Pod — контейнер (React Flow parent/child): `measureGroup` — до **3 колонок**
  members, gaps 36/32, padding 28/44/28; member 240×160; gap между сущностями 120.
- Сортировка внутри уровня: сначала по числу исходящих рёбер.
- Итог: «пайплайн сверху вниз» (orchestrator → dev → review), без ручных x/y.
- Host-уровень (несколько rig'ов на канвасе) — `hybrid-layout.ts` (префиксы id `rig::`).

### 2.2 Ноды (web) — `components/topology/HybridTopologyNodes.tsx`
- **Agent-карточка** (240×160, glass: `bg-surface-lowest/40 backdrop-blur hard-shadow`):
  - шапка: имя + **activity dot** (pulse по state: active/needs-input/blocked/idle),
    фон шапки по роли (lead/orchestrator — inverse);
  - body: session name (mono), **RuntimeBadge** (runtime+model), **Context %**
    (14px bold; <60 green / 60-80 amber / ≥80 red; stale → opacity-50), **token total**
    (compact);
  - **ActivityRing** вокруг карточки (обводка по activity state, flash);
  - border по `startupStatus`: failed → red, attention → amber;
  - **TerminalPreviewPopover** (hover: статичное зеркало терминала → click = live,
    page-wide cap на live-терминалы; progressive rendering);
  - кнопки: open-in-cmux (локально), error chip;
  - **memo + кастомный areEqual** по ~18 полям — 1s-тик activity не ре-рендерит
    карточки.
- **Pod** — dashed-фрейм (`border-dashed border-outline/55 bg-background/25`),
  label + count (mono 9px), **не перетаскивается** (handles opacity-0,
  pointer-events-none).
- **Rig** — «soft frame» с registration marks + aggregate activity
  (`RigGroupNode.tsx`); expand/collapse rig'ов (default expanded, lazy fetch).

### 2.3 Рёбра (web) — `HotPotatoEdge.tsx`
- Bezier (React Flow `getBezierPath`), два слоя:
  - «направление» — **пунктир с анимацией stroke-dashoffset** (CSS keyframes, без
    framer-motion);
  - «пакет» — **`<circle>` + `<animateMotion>`** (нативный SVG, spline 0.2 0 0 1,
    fill freeze, key по packet.id) — «горячая картошка» ползёт по ребру, пока
    на нём активная очередь.
- Стили по `crossRig` (меж-rig hop: толще/зелёнее), `vectorEffect=non-scaling-stroke`,
  **reduced-motion** → статичная точка вместо анимации.

### 2.4 Что это НЕ есть
- **Нет drag-editing**: ноды не перетаскиваются (handles скрыты), layout —
  авто (toposort). Редактирование топологии — **YAML RigSpec + CLI**
  (`rig grow` / `rig shrink` / `rig launch` / `rig remove`) + MCP-инструменты
  (агенты сами редактируют). Граф — **живая визуализация**, а не редактор.
- Edge kinds не «проводят» сообщения — только визуал+порядок запуска+wake-ladder.

## 3. Стек web UI (openRIG)
React + TypeScript + **React Flow (@xyflow/react)** + Tailwind-классы (дизайн-токены
surface/on-surface) + CSS keyframes. Тёмная тема по умолчанию. Данные: 1s-тик
`useTopologyActivity` + REST. Версия пакета 0.6.6, UI в maintenance mode.

## 4. Что берём для нашего редизайна

**Забираем (ядро запроса):**
1. **Авто-лейаут топологии** — toposort по delegates_to/spawned_by, один столбец,
   pods/segments — контейнеры (до 3 колонок), никаких ручных x/y (наш текущий
   «ручной canvas» с layout x/y/width/height — заменяем; ручное смещение — опционально).
2. **Карточки ролей как live-ноды**: activity dot + ring (active/needs-input/blocked),
   session-state, runtime, context/tokens (data-модель уточнить — backend сейчас не
   отдаёт context% для наших сессий), startup-status border.
3. **Сегменты = dashed-фреймы** с label + count (как у нас уже, но в стиле их glass).
4. **Рёбра**: bezier + направление + анимация «пакета» при активных задачах
   (у нас есть tasks/queue — можно анимировать реальные задачи по relatives!).
5. **Виды**: Graph / Table (+ Tree в сайдбаре), таблица на узких экранах,
   expand/collapse, reduced-motion.
6. Тёмная тема + mono-детали (у нас уже есть, усиливаем).

**Оставляем своё:** дашборд с целевыми метриками (нравится пользователю),
остальные страницы (tasks/messages/library/history), mock-режим.

**Решения, которые нужно принять (вопросы пользователю):**
- Q1. **Редактирование**: (a) оставить drag-editing (наш canvas) поверх авто-layout
  (авто по умолчанию, drag — fine-tune), (b) как в openRIG — граф read-only display,
  редактирование через spec/форму, (c) hybrid: авто-layout + «режим редактирования»
  (drag/создание связей мышью) — я рекомендую (c).
- Q2. **Стек графа**: (a) **React Flow** (@xyflow/react) — pan/zoom/handles/minimap/
  анимации «из коробки» (как в openRIG; +~40KB gzip), (b) остаться на своём SVG
  (без зависимостей, но pan/zoom/handles/анимации писать сами). Рекомендую (a) —
  редизайн именно под этот паттерн.
- Q3. **Live-данные на нодах**: что показывать реально (session state, uptime,
  exit_code, watchdog-статусы; context%/tokens — нет в backend, либо 0). Предлагаю
  начать с того, что есть (state + uptime + task-очередь), и расширить контракт
  при наличии данных.
- Q4. Переименовать segment→pod / role→seat в UI? Рекомендую **не** (наш контракт,
  backend, тесты; визуально идентично).

## 5. Ссылки
- Сайт: https://openrig.dev (guides: product-team-topology, factory-topology; explore)
- Репо: https://github.com/mvschwarz/openrig (docs/reference/rig-spec.md,
  edge-types.md; docs/as-built/ui/topology.md)
- Web UI: packages/ui/src/components/topology/* (HostMultiRigGraph,
  HybridTopologyNodes, HotPotatoEdge, ActivityRing, TopologyTableView/Tree),
  packages/ui/src/lib/graph-layout.ts, hybrid-layout.ts
- Скриншот TUI: assets/ui/screenshots/tui-topology.png
