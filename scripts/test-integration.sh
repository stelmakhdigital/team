#!/usr/bin/env bash
# Интеграционные тесты frontend-клиента против ИЗОЛИРОВАННОГО test-daemon
# (:8081, свежая data/test.db) — живая БД daemon'а не засоряется.
# Usage: scripts/test-integration.sh [vitest-аргументы...]
#   напр.: scripts/test-integration.sh run tests/realIntegration.test.ts
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

bash "$ROOT/scripts/start-test-daemon.sh"
TESTPID=$(cat "$ROOT/data/test-daemon.pid")
trap 'kill "$TESTPID" 2>/dev/null || true' EXIT

cd "$ROOT/frontend"
INTEGRATION_BASE_URL="http://127.0.0.1:8081" \
INTEGRATION_API_KEY=env-test-key \
  npx vitest "$@"

cd "$ROOT"
kill "$TESTPID" 2>/dev/null || true
echo "✓ integration suite done (test daemon stopped)"
