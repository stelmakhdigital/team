#!/usr/bin/env bash
# (Re)start the LIVE daemon on :8080 with a PERSISTENT SQLite database
# (./data/daemon.db — survives reboots / /tmp cleanup, gitignored).
# Usage: scripts/start-live-daemon.sh   (idempotent: kills any existing :8080 daemon first)
#
# The DB location is persistent (project ./data/), NOT /tmp. To reset the DB:
#   stop daemon, `rm ./data/daemon.db*`, restart (migrations recreate schema, empty data).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# --- config (local dev) ---
export DAEMON_LISTEN_ADDR=127.0.0.1:8080
export DAEMON_API_KEYS=env-live-key
export DAEMON_SECRET_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
export DAEMON_DB_DSN="sqlite:$ROOT/data/daemon.db"
export DAEMON_AGENT_SPECS_DIR="$ROOT/agents"

# --- ensure `go` is available (fallback: ~/sdk/go/bin, /usr/local/go/bin) ---
if ! command -v go >/dev/null 2>&1; then
  for d in "$HOME/sdk/go/bin" /usr/local/go/bin /opt/go/bin; do
    if [ -x "$d/go" ]; then export PATH="$d:$PATH"; break; fi
  done
fi
if ! command -v go >/dev/null 2>&1; then
  echo "✗ go not found (PATH, ~/sdk/go/bin, /usr/local/go/bin) — install Go or add it to PATH"
  exit 1
fi

# --- ensure binary is built (stale vs ANY .go source, not just go.mod) ---
if [ ! -x "$ROOT/backend/bin/daemon" ] || [ -n "$(find "$ROOT/backend" -name '*.go' -newer "$ROOT/backend/bin/daemon" -print -quit 2>/dev/null)" ]; then
  echo "Building daemon binary…"
  ( cd "$ROOT/backend" && go build -o ./bin/daemon ./cmd/daemon )
fi

# --- stop any existing daemon on :8080 ---
EXISTING=$(ss -ltnp 2>/dev/null | grep ':8080' | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2 || true)
if [ -n "$EXISTING" ]; then
  echo "Stopping existing daemon (pid $EXISTING)…"
  kill "$EXISTING" 2>/dev/null || true
  sleep 2
  if ss -ltn 2>/dev/null | grep -q ':8080'; then kill -9 "$EXISTING" 2>/dev/null || true; sleep 1; fi
fi

# --- start ---
echo "Starting live daemon on :8080 (db: $DAEMON_DB_DSN) …"
mkdir -p "$ROOT/data"
nohup "$ROOT/backend/bin/daemon" > /tmp/team-daemon-live.log 2>&1 &
DPID=$!
echo "Started daemon pid $DPID (log: /tmp/team-daemon-live.log)"
sleep 4

# --- verify ---
if curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: env-live-key" "http://127.0.0.1:8080/api/v1/teams" | grep -q 200; then
  TEAMS=$(curl -s -H "X-API-Key: env-live-key" "http://127.0.0.1:8080/api/v1/teams" | python3 -c "import sys,json;print(json.load(sys.stdin).get('total','?'))" 2>/dev/null || echo '?')
  echo "✓ Live daemon up: http://127.0.0.1:8080 (db $DAEMON_DB_DSN, $TEAMS teams)"
else
  echo "✗ Daemon did not come up — check /tmp/team-daemon-live.log"
  exit 1
fi
