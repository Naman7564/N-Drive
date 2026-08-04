#!/usr/bin/env bash
# N-Drive one-command deploy: pull + rebuild + restart.
# Usage: ./update.sh [--no-tests]
set -euo pipefail

NDRIVE_DIR="/home/ubuntu/N-Drive"
LOCK_FILE="$NDRIVE_DIR/.update.lock"
PID_FILE="$NDRIVE_DIR/ndrive.pid"
LOG_FILE="$NDRIVE_DIR/ndrive.log"
BIN="$NDRIVE_DIR/ndrive"
PREV_BIN="$NDRIVE_DIR/ndrive.prev"
HEALTH_URL="http://localhost:8080/health"

# Go is not on the default non-interactive PATH; add its install location.
export PATH="$PATH:/usr/local/go/bin"

# --- flags ---------------------------------------------------------------
RUN_TESTS=1
for arg in "$@"; do
  case "$arg" in
    --no-tests) RUN_TESTS=0 ;;
    *)
      echo "unknown option: $arg" >&2
      echo "usage: $0 [--no-tests]" >&2
      exit 2
      ;;
  esac
done

# --- lock (prevents concurrent updates, tolerates stale locks) ----------
if [ -f "$LOCK_FILE" ]; then
  LOCK_PID=$(cat "$LOCK_FILE" 2>/dev/null || true)
  if [ -n "$LOCK_PID" ] && kill -0 "$LOCK_PID" 2>/dev/null; then
    echo "update already in progress (pid $LOCK_PID)" >&2
    exit 1
  fi
  echo "removing stale update lock" >&2
  rm -f "$LOCK_FILE"
fi
echo "$$" > "$LOCK_FILE"
trap 'rm -f "$LOCK_FILE"' EXIT

cd "$NDRIVE_DIR"

# --- 1. pull ------------------------------------------------------------
echo "==> git pull"
git pull --ff-only origin main

# --- 2. tests -----------------------------------------------------------
if [ "$RUN_TESTS" -eq 1 ]; then
  echo "==> go test ./..."
  go test ./...
else
  echo "==> go test ./... (skipped: --no-tests)"
fi

# --- 3. build ------------------------------------------------------------
echo "==> go build"
go build -o "$BIN.new" ./cmd/api

# --- 4. stop old server (graceful) --------------------------------------
# Only signal a PID that actually belongs to the ndrive binary, so a stale
# or recycled pidfile can never take down an unrelated process.
STOPPED=0
if [ -f "$PID_FILE" ]; then
  OLD_PID=$(cat "$PID_FILE" 2>/dev/null || true)
  if [ -n "$OLD_PID" ] && [ -r "/proc/$OLD_PID/cmdline" ] \
     && tr '\0' ' ' < "/proc/$OLD_PID/cmdline" | grep -q 'ndrive'; then
    echo "==> stopping old server (pid $OLD_PID)"
    kill -TERM "$OLD_PID"
    for ((i = 0; i < 30; i++)); do
      kill -0 "$OLD_PID" 2>/dev/null || break
      sleep 1
    done
    if kill -0 "$OLD_PID" 2>/dev/null; then
      echo "warning: old server did not stop within 30s, killing" >&2
      kill -KILL "$OLD_PID" || true
    fi
    STOPPED=1
  else
    echo "==> no running ndrive found for pid ${OLD_PID:-<none>}"
  fi
fi

# --- 5. install new binary, keep previous as fallback --------------------
[ -f "$BIN" ] && mv -f "$BIN" "$PREV_BIN"
mv -f "$BIN.new" "$BIN"
chmod +x "$BIN"

# --- 6. start new server -------------------------------------------------
echo "==> starting new server"
{
  echo ""
  echo "===== server restarted by update at $(date -Is) ====="
} >> "$LOG_FILE"
nohup "$BIN" < /dev/null >> "$LOG_FILE" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" > "$PID_FILE"

# --- 7. health check ------------------------------------------------------
# Prefer an HTTP check via curl; if curl is missing, fall back to grepping
# the log for the server's own "http server listening" line. This avoids
# false failures when curl is absent or the server boots slowly.
server_ready() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsS "$HEALTH_URL" >/dev/null 2>&1
  else
    tail -20 "$LOG_FILE" | grep -q 'http server listening'
  fi
}

READY=0
for ((i = 0; i < 15; i++)); do
  if server_ready; then
    READY=1
    break
  fi
  kill -0 "$NEW_PID" 2>/dev/null || break
  sleep 1
done

if [ "$READY" -eq 1 ]; then
  echo "update complete (pid $NEW_PID, health OK)"
  exit 0
fi

# --- 8. rollback on failed start -----------------------------------------
echo "ERROR: server (pid $NEW_PID) did not become healthy" >&2
kill -TERM "$NEW_PID" 2>/dev/null || true
if [ -f "$PREV_BIN" ]; then
  echo "       rolling back to previous binary" >&2
  mv -f "$BIN" "$BIN.failed"
  mv -f "$PREV_BIN" "$BIN"
  chmod +x "$BIN"
  {
    echo ""
    echo "===== server restarted by update (rollback) at $(date -Is) ====="
  } >> "$LOG_FILE"
  nohup "$BIN" < /dev/null >> "$LOG_FILE" 2>&1 &
  echo "$!" > "$PID_FILE"
  echo "       rollback started; check $LOG_FILE" >&2
  exit 1
fi
echo "       no previous binary to restore; check $LOG_FILE" >&2
exit 1
