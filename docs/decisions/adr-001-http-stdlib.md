# ADR-001: Go stdlib HTTP, без web-фреймворка

Status: accepted (2026-10-07)

## Context
Daemon — внутренний сервис с REST API для UI/CLI. ТЗ (01_daemon.md) упоминало
chi/gin/echo, но не обязывает.

## Decision
Используем стандартный `net/http` с `http.ServeMux` (паттерны `METHOD /path/{id}`,
Go 1.22+), `log/slog` для логов. Никакого web-фреймворка.

## Consequences
- Меньше зависимостей (в slice 1 только sqlite- и pg-драйверы).
- Маршруты и middleware пишутся руками (мало, предсказуемо).
- При появлении gRPC/MCP (slice 3+) добавим отдельный transport, без смены HTTP-слоя.
