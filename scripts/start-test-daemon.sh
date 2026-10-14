#!/usr/bin/env bash
# Тестовый daemon (:8081) с НОВОЙ in-memory-подобной БД data/test.db
# для интеграционных тестов (tests/realIntegration.test.ts).
# Каждая запуск — свежая БД (rm test.db*): тесты не засоряют живую data/daemon.db.
# Usage: scripts/start-test-daemon.sh   (останавливать: scripts/stop-test-daemon.sh или kill pid)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PORT="${TEST_DAEMON_PORT:-8081}"
export DAEMON_LISTEN_ADDR="127.0.0.1:$PORT"
export DAEMON_API_KEYS=env-test-key
export DAEMON_SECRET_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
export DAEMON_DB_DSN="sqlite:$ROOT/data/test.db"
export DAEMON_AGENT_SPECS_DIR="$ROOT/agents"

if ! command -v go >/dev/null 2>&1; then
  for d in "$HOME/sdk/go/bin" /usr/local/go/bin /opt/go/bin; do
    if [ -x "$d/go" ]; then export PATH="$d:$PATH"; break; fi
  done
fi
if [ ! -x "$ROOT/backend/bin/daemon" ]; then
  echo "daemon binary not built — run: (cd backend && go build -o ./bin/daemon ./cmd/daemon)"
  exit 1
fi

# свежая БД каждый прогон (миграции пересоздают схему)
mkdir -p "$ROOT/data"
rm -f "$ROOT/data/test.db" "$ROOT/data/test.db-wal" "$ROOT/data/test.db-shm"

echo "Starting TEST daemon on :$PORT (db: $DAEMON_DB_DSN) …"
nohup "$ROOT/backend/bin/daemon" > "$ROOT/logs/daemon-test.log" 2>&1 &
DPID=$!
echo "Started test daemon pid $DPID (log: $ROOT/logs/daemon-test.log)"
echo "$DPID" > "$ROOT/data/test-daemon.pid"
sleep 3
if curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: env-test-key" "http://127.0.0.1:$PORT/api/v1/teams" | grep -q 200; then
  echo "✓ Test daemon up: http://127.0.0.1:$PORT (fresh data/test.db)"
else
  echo "✗ Test daemon did not come up — check $ROOT/logs/daemon-test.log"
  exit 1
fi
