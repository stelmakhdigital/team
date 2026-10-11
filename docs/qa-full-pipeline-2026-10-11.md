# QA: полный пайплайн (project → team → task → session → результат) — 11.10.2026

## Вердикт
Полный пайплайн **работает end-to-end** (создание проекта → команды → задачи → запуск
agent-сессии → агент выполнил задачу → evidence → acceptance → задача done).
Но запуск сессии **был полностью сломан** — выявлено и исправлено **9 критических багов**
(все блокировали запуск/работу agent). Плюс **6 findings** (не исправлены, требуют решения).

## Пройденный пайплайн (live, через Web UI)
| Шаг | Что | Результат |
|---|---|---|
| 1 | Проект «QA Pipeline» (path /tmp/qa-workspace) | ✅ id=2 |
| 2 | Team «dev-team» (starter apply: lead/worker/reviewer) | ✅ id=28 |
| 3 | Task #19 «QA: create result file» (role=worker) | ✅ |
| 4 | Запуск сессии: Брифинг → worker + task #19 → «Запустить» | ✅ session (pi) |
| 5 | Агент (pi, qwen3.8-27b) создал `result.txt` = `QA PIPELINE OK` | ✅ за ~10с |
| 6 | Evidence: result.txt (sha 7e4b8cf8…) | ✅ submitted |
| 7 | Verdict судьи: Accept | ✅ acceptance=accepted, readiness=ready |
| 8 | Закрыть задачу (Close as done) | ✅ state=done |

Финальное состояние задачи #19: `state=done, acceptance=accepted, readiness=ready`.

---

## 🔴 Критические баги (блокировали пайплайн) — ВСЕ ИСПРАВЛЕНЫ

### 1. Запуск сессии → 400 (runtime_type не резолвится)
- **Симптом**: «🚀 Запустить с этим брифингом» → «Validation error», `POST /sessions → 400`.
- **Причина**: briefing-launch шлёт только `config:{briefing_override,briefing_append}` —
  без `runtime_type`/`command`. `CreateSession` дефолтил `runtime_type="process"`,
  command пуст → 400 «command is required for runtime process». Runtime из agent-spec роли
  (pi) не подставлялся.
- **Фикс**: `SessionService.RoleSpecFunc` — в `CreateSession`, если runtime_type не задан,
  резолвится из agent-spec роли (`pi_config.model → pi`; иначе `spec.runtime.type`),
  model подставляется в config. (backend `session_service.go`, `main.go`.)

### 2. Agent-spec не резолвится (cwd демона)
- **Симптом**: spec роли `available=false`, model пуст; ConfigPanel показывал дефолты.
- **Причина**: `agent_spec="agents/pi-worker.yaml"` резолвится **относительно cwd демона**.
  Демон, запущенный из `backend/`, не находил `agents/*.yaml` (нужен cwd = корень проекта).
- **Фикс/рекомендация**: запускать демон из корня проекта (`team/`), либо сделать резолв
  robust (пробовать и `<SpecsDir>/<basename>`). Сейчас демон запущен из `team/`. ⚠️ deployment-деталь.

### 3. pi-адаптер шлёт `--config` (pi CLI не поддерживает)
- **Симптом**: pi сразу уходил: `Error: Unknown option: --config`.
- **Причина**: `PiAdapter`/`TMuxAdapter` писали YAML-конфиг и передавали `--config <path>`,
  которого нет в pi CLI.
- **Фикс**: адаптер строит корректные CLI-флаги из полей spec: `--model`, `--thinking`,
  `--tools`, `--skill`, а брифинг — через `--append-system-prompt <file>` (сохраняем базовый
  coding-промпт). `--config` убран из `TMuxAdapter.Start`. (backend `pi.go`, `tmux.go`.)

### 4. Модель из spec недоступна в окружении
- **Симптом**: `Error: Model "claude-3-7-sonnet" not found`.
- **Причина**: agent-spec указывали `model: claude-3-7-sonnet`, а в окружении доступна только
  `cat-vllm/qwen3.8-27b-fp8`.
- **Фикс**: обновил модель в `agents/pi-{lead,worker,reviewer}.yaml` на `cat-vllm/qwen3.8-27b-fp8`.
  ⚠️ Это mismatch фикс-данных и окружения — стоит сделать модель настраиваемой/валидируемой.

### 5. Trust-диалог блокирует non-interactive запуск
- **Симптом**: pi висел на «Проект … доверяешь ли ты ему? (L0/dev/untrusted)».
- **Причина**: первый запуск в новом каталоге → sandbox-trust диалог; в non-interactive
  сессии демона его никто не выбирает.
- **Фикс**: pi-адаптер передаёт `--approve --sandbox-level off` (auto-trust, L0).
  `sandbox_level` настраивается через config сессии. (backend `pi.go`.)

### 6. Пайп `| tee` ломает TUI pi
- **Симптом**: pi печатал предупреждение и выходил; tmux-сессия умирала.
- **Причина**: `TMuxAdapter.Start` пайпил stdout в лог-файл (`pi … | tee log`). pi — TUI,
  требует TTY на stdout; через пайп TUI не рендерится.
- **Фикс**: `Spec.DirectTTY` — для pi команда запускается **без пайпа** (stdout = TTY);
  транскрипт для таких сессий — через capture-pane. (backend `runtime.go`, `tmux.go`, `pi.go`.)

### 7. TMux.Stop не идемпотентен для мёртвых сессий (400)
- **Симптом**: `DELETE /sessions/{id}` → 400, если tmux-сессия уже мертва.
- **Причина**: чек ловил только «no session», а tmux выдаёт «can't find session».
- **Фикс**: `TMuxAdapter.Stop` теперь идемпотентен на «no session / can't find session /
  no such session». (backend `tmux.go`.)

### 8. pi-guard'ы блокируют автономного агента
- **Симптом**: агент вставал на «Потенциально разрушительная bash-команда → Выполнить/Отменить»
  и ждал подтверждения (в non-interactive сессии — навсегда).
- **Причина**: глобальные pi-расширения `bash-guard`/`dir-guard` интерактивные; в автономной
  daemon-сессии оператора нет.
- **Фикс**: опция `no_extensions` (config сессии) → pi-адаптер шлёт `--no-extensions`;
  briefing-launch передаёт `no_extensions: true`. Governance чувствительных операций остаётся
  на уровне API (posture, P3.16). (backend `pi.go`, `session_service.go`; frontend
  `BriefingPreviewModal.tsx`.)

### 9. `--sandbox-level` конфликтует с `--no-extensions`
- **Симптом**: `Error: Unknown option: --sandbox-level` при `--no-extensions`.
- **Причина**: `--sandbox-level` — флаг sandbox-**расширения**, а не core pi; с
  `--no-extensions` расширение выключено → флаг неизвестен.
- **Фикс**: при `no_extensions` `--sandbox-level` не передаётся (и без расширений
  sandbox-диалог не показывается). (backend `pi.go`.)

---

## 🟡 Findings (не исправлены — требуют решения)

### 10. Застарелое состояние сессии (running при мёртвом pi)
Сессия оставалась `state=running`, когда pi-процесс/tmux-сессия уже мертвы (state-
несогласованность). Watchdog не обновляет state автоматически. Ручной stop теперь работает
(фикс #7), но автодетекции «pi умер → state=failed/stopped» нет. **Рекомендация**: watchdog
проверяет живость runtime и переводит мёртвые сессии в `failed`/`stopped`.

### 11. Пустой transcript для pi DirectTTY-сессий
`GET /sessions/{id}/transcript` читает лог-файл; для DirectTTY-сессий (без пайпа) лог не
пишется → transcript пуст (в UI «No messages»). Live-вид работает только через Capture.
**Рекомендация**: для pi-сессий транскрипт собирать периодическим capture-pane (или парсить
pi session-файлы), чтобы transcript был персистентным.

### 12. UI не обновляется live после evidence verdict
После «Accept» evidence карточка обновилась (accepted), но шапка задачи оставалась
`acceptance: pending` до ручного reload (данные в API уже `accepted`). **Рекомендация**:
пере-fetch task detail после verdict (WS-событие или refetch).

### 13. Team Builder не показывает привязанный проект
В builder не видно, к какому проекту привязана команда (только в Project Detail).
**Рекомендация**: добавить project-бейдж/селект в шапку Team Builder.

### 14. Workflow-имена — автогенерируемые таймстемпы
Напр. `IT-flow-1791469879007` — нечитаемо. **Рекомендация**: редактируемое имя / человекочитаемый
дефолт.

### 15. Агент ищет briefing-файл
Агент пытался `cat /tmp/qa-workspace/briefing*` (ожидал файл), хотя briefing в system-prompt.
Минорная путаница в delivery. **Рекомендация**: в брифинге явно указать, что контекст уже в
system-prompt (или отдавать briefing-файл в working_dir).

### 16. (UI) Неоднозначность селекторов
Page-level фильтры `<select>` дублируют опции модалок (сложно тестировать/различать).
**Рекомендация**: разные label/aria для фильтров и форм.

---

## Проверки (после фиксов)
- `go build ./...` — OK
- `go test ./... -count=1` — 6 пакетов ok
- `npx vitest run` — 219 passed / 21 skipped (41 file)
- `scripts/check_openapi.py` — PASS (116 routes = 116 ops)
- `frontend/scripts/contrast-check.mjs` — 22/22 pass

## Изменённые файлы (фиксы)
- `backend/internal/service/session_service.go` — RoleSpecFunc + резолв runtime/model + sandbox_level/no_extensions
- `backend/cmd/daemon/main.go` — wiring RoleSpecFunc
- `backend/internal/runtime/pi.go` — CLI-флаги вместо --config, --approve/--sandbox-level, --no-extensions, DirectTTY, writePromptFile
- `backend/internal/runtime/tmux.go` — убран --config, DirectTTY (без пайпа), Stop идемпотентен
- `backend/internal/runtime/runtime.go` — Spec.Pi, Spec.DirectTTY
- `frontend/src/components/BriefingPreviewModal.tsx` — no_extensions: true
- `agents/pi-{lead,worker,reviewer}.yaml` — model → cat-vllm/qwen3.8-27b-fp8

## Скриншоты
`/tmp/team-qa/shots/` — b1…b5 (project, team, task, briefing, session detail, evidence, done).
