#!/usr/bin/env bash
# (Re)start the LIVE frontend (real mode) on :5175, proxying to the daemon :8080.
# Usage: scripts/start-live-ui.sh   (idempotent: kills any existing :5175 vite first)
set -euo pipefail
cd "$(dirname "$0")/../frontend"
PORT=5175
# stop any existing vite on the port
EXISTING=$(pgrep -f "vite --port $PORT" || true)
if [ -n "$EXISTING" ]; then
  echo "Stopping existing vite (pid $EXISTING)…"
  kill $EXISTING 2>/dev/null || true
  sleep 1
fi
echo "Starting live UI (real mode) on :$PORT → daemon :8080 …"
VITE_API_MODE=real VITE_API_KEY=env-live-key BACKEND_URL=http://127.0.0.1:8080 \
  nohup npx vite --port $PORT --strictPort > /tmp/vite-live.log 2>&1 &
echo "Started vite pid $! (log: /tmp/vite-live.log)"
sleep 5
if curl -s -o /dev/null -w "%{http_code}" "http://localhost:$PORT/" | grep -q 200; then
  echo "✓ Live UI up: http://localhost:$PORT (real mode, LIVE API)"
else
  echo "✗ Live UI did not come up — check /tmp/vite-live.log"
  exit 1
fi
