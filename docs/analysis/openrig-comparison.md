# OpenRIG vs наш UI: сравнение функционала

Дата: 2026-10-09. Источник: openrig.dev/docs (0.6.6: tui, coordination, workflows,
continuity, work-tree, messaging, configuration) + наш UI (12 страниц + Settings).

## 1. Что есть у OpenRIG и чего НЕТ в нашем UI

### A. «Мне нужно» — единый операторский срез (attention / needs / feed)
У них: отдельный view `attention` — всё, что требует человека: escalation-и,
parked/-held задачи, waiting-воркфлоу, outcome/health-апдейты — **отдельно** от
остального потока. `rig workflow status` отвечает «what needs me»: счётчики +
по строке на failed/stuck/waiting + suggested next action.
**У нас**: алерты watchdog + stale-флаги размазаны по Dashboard/Alerts; нет
единого «что требует меня сейчас».

### B. Сроки и overdue (closure_required_at)
У них: `claim` вычисляет дедлайн закрытия; `rig queue overdue` — список
просроченных in-progress (bounded, body-free); `undelivered` — pending, чьё
nudge-уведомление не дошло.
**У нас**: только watchdog-пороги stale (2ч)/blocked (1ч) — нет явных
дедлайнов на задачу, нет overdue-листа, нет «не доставлено».

### C. Block с «пробуждением» (wake)
У них: `queue block --on <blocker>` паркует задачу с записью «как продолжить»
и живым wake: watchdog-job / timer (`--wake-after 15m`) / живая задача, на
которую ждём. `resolve` записывает решение на строку и будит владельца.
**У нас**: blocked — просто состояние + alert; механизма «разбудить, когда X»
нет.

### D. Воркфлоу как движок (transactional-scribe)
У них: воркфлоу = durable-состояние; закрытие шага владельцем → runtime в
**одной транзакции** закрывает текущую queue-строку и проецирует строку
следующего шага. Плюс: `resume` (догнать FAILED с failed-шага, completed не
перезапускаются), `route` (перенаправить текущий шаг на другого владельца),
`revise` (принять изменения графа без replay), `watch` (live-следование),
`trace` (append-only trail), `status` (что требует внимания).
**У нас**: воркфлоу — **статическая диаграмма** (draft/active/archived): блоки,
связи, но ни одного шага воркфлоу не создаёт задач. Связь workflow→tasks
отсутствует.

### E. Watchdog-джобы, которые можно **регистрировать** (не только алерты)
У них: `watchdog register` с политиками: periodic-reminder, artifact-pool-ready,
workflow-keepalive, idle-gate-qitem, **context-usage-threshold**; джоб может быть
wake для parked-строки. `status` — fired ли, жив ли.
**У нас**: watchdog read-only (скан + алерты, пороги из конфига).

### F. Continuity: handover / compaction / agent images / snapshots
У них:
- `handover <seat>` — смена «жильца» сессии: создать наследника → доставить
  контекст → проверить непрерывность → rebind; `--dry-run`; источник —
  fresh/fork/restore-from-artifacts; причина обязательна.
- `compact-plan` / `compact` — список сессий у «стены контекста» и
  управляемая компакция.
- `agent-image` — захват продуктивного сессии-состояния (resumable state) в
  библиотеку: create/list/pin/prune/preview.
- `down --snapshot` / `up` / `restore-check` / `restore` — снапшоты всего
  рига + честный per-seat отчёт результата restore (resume/rebuilt/fresh/waiting/failed).
- `restore-packet` — перенос «знаний» сессии между runtime.
**У нас**: сессия = процесс до остановки; транскрипт есть, но нет handover
(продолжить после смерти/переполнения контекста), нет compact-plan, нет
«снимков агента» (Library — только команды/воркфлоу, не состояние сессии).
**Боль актуальна**: в нашей же pipeline-практике worker'ы упирались в потолок
контекста (91%+) — handover был бы прямым решением.

### G. Work tree + proof (evidence)
У них: project → mission → slice, у каждого SPEC.md (интент), PROGRESS.md
(derive, не руками), PROOF.md; `proof add` — артефакт с typed frontmatter
(verdict, candidate-sha, money-evidence); `proof judge` — атрибутированное
решение по контракту (accept/reject/withdraw) + readiness; `scope audit`,
`workspace doctor` (8-check), epistemic stages (wip→canonical→superseded).
**У нас**: у задач closure_reason и аудит; скриншоты/доказательства не
привязаны к задачам системно (лежат в _workspace). Понятия «доставлено/
принято» (closure ≠ acceptance — у них acceptance это вердикт следующей
ступени на СВОЕЙ строке) у нас не выражены.

### H. Messaging-усиления
У них: `send --verify` (проверка, что текст попал в панель; отказ, если
адресат на interactive prompt/permission), **held-messages** (typing-guard:
сообщения удерживаются, чтение не доставляет), `capture` (скриншот экрана
сессии), `transcript --grep`, `ask` (поиск по истории транскриптов),
`chatroom wait/watch`, `walk` (поэтапная онбординг-подача контекста),
broadcast, terminal tiles (все живые сессии тайлами).
**У нас**: messages/chatrooms есть, delivered_to есть; нет verify, нет
capture (у нас live-терминал — лучше), нет поиска по транскриптам, нет walk.

### I. Command palette + explorer + cross-nav (TUI)
У них: command bar с fuzzy-палитрой на ВСЕ команды; drill по иерархии
host/rig/pod/seat/spec/project/mission/slice; cross-nav `spec-of <agent>`,
`running <spec>`; вкладки table/recent/overview/graph/health/topology/
configuration/yaml/**pulse** (event feed).
**У нас**: 12 страниц + drill-down (EntityLink); нет Cmd+K-палитры, нет
единого event feed (WS-события есть, но «pulse»-страницы нет), нет
cross-nav «кто работает по этой спецификации».

### J. Мультихост и humans
У них: зарегистрированные хосты, send/capture/transcript на другую машину
(`--host`), «reach a person».
**У нас**: single-host. (Нам сейчас не нужно.)

### K. MCP-поверхность
У них: полный набор MCP-tools (управление из внешнего агента).
**У нас**: REST+WS (эквивалентно достижимо; MCP не заявлен).

## 2. Что есть у НАС и нет/слабо у OpenRIG (web-смысл)

- **Visual workflow editor** (React Flow, drag&drop) — у них воркфлоу только
  как spec-файл; визуального редактора нет.
- **Settings UI** с layered resolution + провенансом (именно их модель мы
  перенесли; у них — TUI system-view + CLI).
- **Dashboard metrics charts** (1h/24h/7d, LLM-токены).
- **RBAC + API keys + audit + request-ids** как product.
- **SQLite/PG** с migrate.
- Web-UI вообще (их web UI — maintenance mode, операторский surface — TUI).

## 3. Что стоит перенести (моё мнение, по приоритету)

### P0 — малый объём, высокая операторская ценность
1. **Attention view «Что требует меня»** — единый экран/фильтр: blocked-on-
   human + escalation + overdue + waiting-воркфлоу + непрочитанные алерты, с
   suggested action. (A+D-status)
2. **Overdue/deadlines** — `closure_required_at` на claim, overdue-лист. (B)
3. **Block with wake** — при блоке задачи: reason + wake (timer / на задачу /
   watchdog). (C)

### P1 — большая ценность, средний объём
4. **Workflow engine: close-step → next task (транзакционно)** — воркфлоу
   перестаёт быть картинкой: active-воркфлоу с agent/task-блоками создаёт
   задачи, closure шага проецирует следующий; + `status`/`trace`/`watch`-панель
   по инстансу. (D) — **самая ценная** фича: закрывает разрыв «нарисовал
   процесс → он не работает».
5. **Session handover + compact-plan** — список сессий у контекстной стены
   (у нас уже есть context_used_percentage!) + «handover»: новая сессия той же
   роли с переданным контекстом (резюме транскрипта/restore-packet). (F) —
   решает нашу реальную боль (обрывы по контексту).
6. **Панель контекста сессии: compact-триггер** — алерт/watchdog-джоб
   context-usage-threshold (у них policy, у нас пока только метрика). (E+F)

### P2 — удобства
7. **Поиск по транскриптам** (`ask`/`grep`): поиск по истории всех сессий. (H)
8. **Capture**: «снимок» экрана сессии (последние N строк как артефакт). (H)
9. **Pulse feed**: единая лента WS-событий с фильтрами (task./session./
   message./alert.) вместо только dashboard. (I)
10. **Command palette (Cmd+K)**: fuzzy-навигация + быстрые действия. (I)
11. **Send verify**: доставка в сессию с подтверждением (tmux capture). (H)

### P3 — если пойдёт в сторону «долгосрочных проектов»
12. **Agent images / session snapshots**: сохранить продуктивное состояние
    сессии (промпт+скиллы+накопленное) → новый старт из снимка; Library уже
    даёт каркас. (F)
13. **Proof/evidence на задачах**: артефакты (скриншоты, тесты) с verdict
    (accept/reject) и readiness; closure ≠ acceptance. (G)
14. **Walk** (поэтапная онбординг-подача контекста сессии). (H)
15. **Inbox/outbox** (доставка с подтверждением, pending-входящие). (B/H)

### Не переносить сейчас
- Мультихост (J) — single-host продукт.
- TUI-модель в целом — у нас web-смысл (их web UI сами вывели в maintenance).
- MCP-tools — позже, если появится внешний-агент-сценарий.

## 4. Заметка по архитектуре

Модель queue (их coordination) у нас уже зеркалит: closure_reason
идентичны (handed_off_to, blocked_on, denied, canceled, no_follow_on,
escalation), handoff — транзакционно. Значит P0-пункты (overdue, block-wake,
attention) ложатся на существующие таблицы/сервисы без переделки; P1-4
(workflow engine) — новый сервис поверх tasks+workflows; P1-5 (handover) —
новый сервис sessions + transcript-summary.
