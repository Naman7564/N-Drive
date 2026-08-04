#!/usr/bin/env bash
# N-Drive one-command deploy: pull + rebuild + restart.
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

# --- 1. pull (best effort) ----------------------------------------------
# The build below compiles the current working tree, so a failed pull must
# not abort the deploy: otherwise an uncommitted edit (or an offline remote)
# would silently leave the old server running.
echo "==> git pull"
if ! git pull --ff-only origin main; then
  echo "warning: git pull failed; deploying current working tree as-is" >&2
fi

# --- 2. build ------------------------------------------------------------
echo "==> go build"
go build -o "$BIN.new" ./cmd/api

# --- 3. stop old server (graceful) --------------------------------------
# Only signal a PID that actually belongs to the ndrive binary, so a stale
# or recycled pidfile can never take down an unrelated process. If the
# pidfile is stale or missing, fall back to finding the real server by its
# binary path; otherwise an orphaned old server would keep port 8080 and
# the new binary would silently never serve.
OLD_PID=""
if [ -f "$PID_FILE" ]; then
  OLD_PID=$(cat "$PID_FILE" 2>/dev/null || true)
fi
STOPPED=0
OLD_PIDS=""
if [ -n "$OLD_PID" ] && [ -r "/proc/$OLD_PID/cmdline" ] \
   && tr '\0' ' ' < "/proc/$OLD_PID/cmdline" | grep -q 'ndrive'; then
  echo "==> stopping old server (pid $OLD_PID)"
  OLD_PIDS="$OLD_PID"
  STOPPED=1
else
  # pidfile stale or missing: find every running ndrive. Match the absolute
  # binary path first, and the process name as a fallback so servers started
  # via a relative path (e.g. ./ndrive) are still found. Stopping all of them
  # matters: a previous failed update can leave an orphan holding port 8080.
  OLD_PIDS="$(pgrep -f "^$BIN( |$)" 2>/dev/null || true) $(pgrep -x ndrive 2>/dev/null || true)"
  OLD_PIDS=$(echo "$OLD_PIDS" | tr ' ' '\n' | sort -nu | tr '\n' ' ')
  if [ -n "$OLD_PIDS" ]; then
    echo "==> pidfile stale (${OLD_PID:-<none>}); stopping running server(s): ${OLD_PIDS% }"
    for p in $OLD_PIDS; do kill -TERM "$p" 2>/dev/null || true; done
    STOPPED=1
  else
    echo "==> no running ndrive found for pid ${OLD_PID:-<none>}"
  fi
fi

if [ "$STOPPED" -eq 1 ]; then
  for ((i = 0; i < 30; i++)); do
    ALIVE=0
    for p in $OLD_PIDS; do
      if kill -0 "$p" 2>/dev/null; then ALIVE=1; break; fi
    done
    [ "$ALIVE" -eq 0 ] && break
    sleep 1
  done
  for p in $OLD_PIDS; do
    if kill -0 "$p" 2>/dev/null; then
      echo "warning: server $p did not stop within 30s, killing" >&2
      kill -KILL "$p" || true
    fi
  done
fi

# --- 4. install new binary, keep previous as fallback --------------------
[ -f "$BIN" ] && mv -f "$BIN" "$PREV_BIN"
mv -f "$BIN.new" "$BIN"
chmod +x "$BIN"

# --- 5. start new server -------------------------------------------------
echo "==> starting new server"
{
  echo ""
  echo "===== server restarted by update at $(date -Is) ====="
} >> "$LOG_FILE"
nohup "$BIN" < /dev/null >> "$LOG_FILE" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" > "$PID_FILE"

# --- 6. health check ------------------------------------------------------
# The check only counts as ready when the NEW process is alive AND answering.
# This prevents a false "update complete" when an old server still holds the
# port: the curl would succeed against the old server, the script would exit
# 0, and the new code would never be served. Prefer an HTTP check via curl;
# without curl, fall back to the log, but only trust a "listening" line
# written after the marker of THIS boot, never a line from an older boot.
server_ready() {
  kill -0 "$NEW_PID" 2>/dev/null || return 1
  if command -v curl >/dev/null 2>&1; then
    curl -fsS "$HEALTH_URL" >/dev/null 2>&1
  else
    awk '/server restarted by update/{marker=NR} marker && /http server listening/{seen=1} END{exit !seen}' "$LOG_FILE"
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

# --- 7. rollback on failed start -----------------------------------------
echo "ERROR: server (pid $NEW_PID) did not become healthy" >&2
if awk '/server restarted by update/{marker=NR} marker && /address already in use/{seen=1} END{exit !seen}' "$LOG_FILE"; then
  echo "       port 8080 may still be held by an old process; check $LOG_FILE" >&2
fi
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
