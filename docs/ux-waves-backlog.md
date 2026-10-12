# UX backlog: волны 2–3 (после Wave 1 — task-ux2.md)

План из UX-анализа 2026-10-12. Именование «волны» без привязки к внешним
продуктам; референс-паттерны описаны по существу.

## Wave 2 — навигация и глубина

### W2.1 Контекст-дерево Projects → Teams → Roles
- Дерево в сайдбаре (или left-panel на ключевых страницах): проекты → команды
  → роли/сегменты, live-точки состояния (активная сессия/failed), клик →
  drill-down (EntityLink-навигация), «текущий» узел подсвечен.
- Источник: teams.getTeams (project_id) + sessions.list; lazy-раскрытие.
- Критерии: vitest (рендер дерева, live-точки, current-подсветка),
  скриншот @2560, mobile: дерево скрывается/сворачивается.

### W2.2 Attribution артефактов (файл → задача/роль)
- В Artifacts (ProjectDetailPage): для каждого файла — «создано/изменено
  задачей #N / роль X» (атрибуция по git blame, если проект git; иначе по
  mtime + active-задачам проекта; честно: «нет атрибуции» вместо фейка).
- Файл → клик на задачу (EntityLink); обратная связь в Task Detail — список
  затронутых файлов (если атрибуция найдена).
- Критерии: backend-эндпоинт опционально (атрибуция может быть клиентской
  по mtime), vitest, скриншот, docs.

### W2.3 Attention: «Решения» / «События»
- Разделение списка Attention на 2 группы: **Решения** (human_requests,
  approve/deny) и **События** (task_overdue, diagnostics_fail, alerts —
  читать, открыть, без approve-семантики).
- Критерии: vitest (2 группы, CTA по типу), скриншот, USAGE-строка.

### W2.4 First-run на Dashboard
- Пустой Dashboard (нет команд/задач) → empty-state с CTA «Apply starter
  dev-team» (связка со Starter bundles в Library) + «Создать команду с нуля».
- Критерии: vitest (empty → CTA, apply → переход), скриншот.

## Wave 3 — полировка

### W3.1 Page-hotkeys
- Контекстные шорткаты: n (new), e (edit), v (validate), a (auto layout),
  / (focus search/palette), Esc (close/exit). Подсказка — в палитре Cmd+K и
  tooltip'ах. Ключи не конфликтуют с браузером (Ctrl/Cmd-meta не использовать
  без необходимости).
- Критерии: vitest (dispatch keydown → действие), docs (USAGE-секция).

### W3.2 Preview → explicit open для тяжёлых панелей
- Terminal, transcript, artifacts-preview: сначала лёгкий preview (статичный
  tail), explicit-кнопка «Open live» для тяжёлого режима (паттерн, как в
  Artifacts Preview/Download).
- Критерии: vitest, UX-консистентность.

### W3.3 Автовалидация топологии
- В edit-режиме: при каждом изменении (create/move/connect/delete) —
  debounce-автовалидация (локальная + server validate), ошибки — в нижней
  панели без ручного «Validate»; ручной Validate остаётся (для Save-gate).
- Критерии: vitest (изменение → авто-валидация, debounce), UX: не мигает при
  drag (валидация по mouseup, не по drag-move).

### W3.4 Specs-поверхность (Agent Specs page)
- Панель/страница «Agent Specs»: список spec-файлов (GET /agentspecs),
  YAML-превью (read-only, из GET /roles/{id}/config паттерн — либо новый
  read-only эндпоинт), «кто использует» (роли по spec) — cross-nav
  spec → роли → команды.
- Критерии: эндпоинт (read-only, teams.read) + OpenAPI, vitest, скриншот,
  USAGE.

## Не делать (решение)
- TUI-модель, мультихост, MCP-tools — вне product-плана.
- Read-only граф (отказ от нашего hybrid-редактора) — не возвращаемся.
