#!/usr/bin/env bash
# One-off maintenance: reclaim the SQLite freelist with VACUUM INTO, then bring
# the ClawBench server back up.
#
# WHY THIS IS A SCRIPT AND NOT A FEW COMMANDS YOU TYPE:
# the interactive agent shell is a CHILD of the server process (clawbench spawns
# the codebuddy --acp session). Killing the server therefore kills the shell
# running this work. The script re-execs itself under `setsid` so it lives in its
# own session, detached from the server's process tree, and survives the kill.
#
# WHY VACUUM INTO AND NOT VACUUM:
# plain VACUUM rewrites the original file in place. An interruption (disk full,
# crash, the shell dying) can leave the only copy of the DB damaged. VACUUM INTO
# writes a NEW file and leaves the original untouched, so the swap can be
# verified first and rolled back at any point.
#
# SAFETY ORDER: verify the copy fully BEFORE touching the original, and register
# the restart in an EXIT trap so the server comes back on every path — success,
# failure, or Ctrl-C.
#
# Paths are overridable via the environment so the script can be exercised
# end-to-end against a throwaway DB before being trusted with the real one.
#   DATA_DIR, PORT, BIN, RELAUNCH_LOG

set -uo pipefail

REPO="${REPO:-/home/xulongzhe/projects/clawbench}"
BIN="${BIN:-$REPO/clawbench}"
DATA_DIR="${DATA_DIR:-$HOME/.clawbench}"
PORT="${PORT:-20000}"
RELAUNCH_LOG="${RELAUNCH_LOG:-$REPO/.clawbench/build-and-restart.log}"

DB="$DATA_DIR/ClawBench.db"
NEW="$DATA_DIR/ClawBench.db.vacuumed"
BAK="$DATA_DIR/ClawBench.db.bak"
LOG_DIR="${LOG_DIR:-$DATA_DIR/logs}"
LOG="$LOG_DIR/vacuum-$(date +%Y%m%d-%H%M%S).log"

mkdir -p "$LOG_DIR" "$(dirname "$RELAUNCH_LOG")" 2>/dev/null

# ── Re-exec under setsid, detached from the server's process tree ──────────────
# CLAWBENCH_VACUUM_DETACHED guards against an infinite re-exec loop.
if [ "${CLAWBENCH_VACUUM_DETACHED:-}" != "1" ]; then
  echo "Detaching from the server process tree (setsid)..."
  echo "Progress log: $LOG"
  CLAWBENCH_VACUUM_DETACHED=1 setsid nohup "$0" "$@" >"$LOG" 2>&1 &
  echo "Started detached (pid $!). This shell will be killed when the server stops — that is expected."
  echo "Follow along with:  tail -f $LOG"
  exit 0
fi

log()  { echo "[$(date +%H:%M:%S)] $*"; }
fail() { log "FAILED: $*"; exit 1; }

# ── Restart is registered up front and runs on EVERY exit path ────────────────
SERVER_STOPPED=0

restart_server() {
  if [ "$SERVER_STOPPED" != "1" ]; then
    return   # never stopped it; leave whatever is running alone
  fi
  log "--- bringing the server back up ---"
  # If something already reclaimed the port (another agent, a manual start),
  # do not race it with a second listener.
  if lsof -ti :"$PORT" >/dev/null 2>&1; then
    log "something is already listening on :$PORT — not launching another"
    return
  fi
  cd "$REPO" || { log "cannot cd $REPO — start the server manually"; return; }
  # Detach from this script (which is about to exit) and append to the log the
  # previous server used, so the launch context stays continuous.
  setsid nohup "$BIN" >>"$RELAUNCH_LOG" 2>&1 &
  local newpid=$!
  log "launched $BIN (pid $newpid), logging to $RELAUNCH_LOG"
  for _ in $(seq 1 30); do
    sleep 1
    if lsof -ti :"$PORT" >/dev/null 2>&1; then
      log "server is listening on :$PORT"
      return
    fi
    kill -0 "$newpid" 2>/dev/null || break
  done
  log "WARNING: server did not come up on :$PORT within 30s — check $RELAUNCH_LOG"
}
trap restart_server EXIT

# ── Preflight ─────────────────────────────────────────────────────────────────
log "=== preflight ==="
log "repo=$REPO  bin=$BIN"
log "db=$DB  port=$PORT"

[ -x "$BIN" ] || fail "$BIN not found or not executable"
for tool in sqlite3 lsof setsid; do
  command -v "$tool" >/dev/null || fail "required tool missing: $tool"
done
[ -f "$DB" ] || fail "$DB not found"

if [ -e "$NEW" ]; then
  fail "$NEW already exists — remove it if it is a stale leftover, then re-run"
fi

SIZE_BEFORE=$(du -m "$DB" | cut -f1)
log "DB is ${SIZE_BEFORE} MB"

# fts5 must be available in the CLI: the schema has an fts5 virtual table and
# VACUUM INTO fails outright if a module referenced by the schema is missing.
sqlite3 :memory: "CREATE VIRTUAL TABLE t USING fts5(x);" >/dev/null 2>&1 \
  || fail "this sqlite3 CLI lacks fts5; VACUUM would fail on rag_chunks_fts"
log "sqlite3 $(sqlite3 --version | awk '{print $1}'), fts5 present"

AVAIL_MB=$(df -m "$DATA_DIR" | awk 'NR==2{print $4}')
[ "$AVAIL_MB" -gt $((SIZE_BEFORE + 512)) ] \
  || fail "need ~${SIZE_BEFORE}MB free, have ${AVAIL_MB}MB"
log "disk: ${AVAIL_MB} MB free"

# Row counts from the ORIGINAL, for the post-copy comparison. Each table is
# counted separately so a missing table fails loudly instead of making both
# sides an empty string that would compare equal (a false pass).
TABLES=(chat_history chat_sessions chat_thinking rag_chunks summaries chat_tool_calls)

counts() {
  local db="$1" t n out=""
  for t in "${TABLES[@]}"; do
    n=$(sqlite3 "$db" "SELECT COUNT(*) FROM $t;" 2>/dev/null) || return 1
    [ -n "$n" ] || return 1
    out+="$t=$n"$'\n'
  done
  printf '%s' "$out"
}

log "quick_check on the original..."
CHECK_BEFORE=$(sqlite3 "$DB" "PRAGMA quick_check;" 2>&1)
[ "$CHECK_BEFORE" = "ok" ] || fail "original DB failed quick_check: $CHECK_BEFORE"
COUNTS_BEFORE=$(counts "$DB") || fail "could not count rows on the original"
log "row counts recorded:"
echo "$COUNTS_BEFORE" | sed 's/^/    /'

# ── Stop the server ───────────────────────────────────────────────────────────
log "=== stopping the server ==="
PID=$(lsof -ti :"$PORT" 2>/dev/null | head -1)
if [ -n "$PID" ]; then
  log "SIGTERM -> pid $PID (graceful: drains streams, finalizes the DB)"
  kill -TERM "$PID" 2>/dev/null
  SERVER_STOPPED=1
  for _ in $(seq 1 60); do
    kill -0 "$PID" 2>/dev/null || break
    sleep 1
  done
  if kill -0 "$PID" 2>/dev/null; then
    log "did not exit in 60s — SIGKILL"
    kill -KILL "$PID" 2>/dev/null
    sleep 2
  fi
  log "server stopped"
else
  log "no process listening on :$PORT — nothing to stop"
  SERVER_STOPPED=1   # bring it back up at the end regardless
fi

# Nobody may hold the DB: a process still writing after the swap would keep
# writing to the unlinked old inode and the new data would be lost.
if lsof "$DB" >/dev/null 2>&1; then
  lsof "$DB" | sed 's/^/    /'
  fail "a process still holds $DB — refusing to swap"
fi
log "no process holds the DB"

# ── VACUUM INTO a new file ────────────────────────────────────────────────────
log "=== VACUUM INTO (this takes a few minutes) ==="
START=$(date +%s)
sqlite3 "$DB" "VACUUM INTO '$NEW';" || fail "VACUUM INTO failed"
log "finished in $(( $(date +%s) - START ))s"

SIZE_AFTER=$(du -m "$NEW" | cut -f1)
log "new file is ${SIZE_AFTER} MB (was ${SIZE_BEFORE} MB)"

# ── Verify the copy BEFORE touching the original ──────────────────────────────
log "=== verifying the copy ==="
CHECK_AFTER=$(sqlite3 "$NEW" "PRAGMA quick_check;" 2>&1)
[ "$CHECK_AFTER" = "ok" ] || fail "copy failed quick_check: $CHECK_AFTER"
log "quick_check: ok"

COUNTS_AFTER=$(counts "$NEW") || fail "could not count rows on the copy"
if [ "$COUNTS_BEFORE" != "$COUNTS_AFTER" ]; then
  log "BEFORE:"; echo "$COUNTS_BEFORE" | sed 's/^/    /'
  log "AFTER:";  echo "$COUNTS_AFTER"  | sed 's/^/    /'
  fail "row counts differ — the copy is not trustworthy; original left untouched"
fi
log "row counts match exactly"

# ── Swap ──────────────────────────────────────────────────────────────────────
log "=== swapping in the vacuumed DB ==="
mv "$DB" "$BAK" || fail "could not move the original aside"
mv "$NEW" "$DB" || { mv "$BAK" "$DB"; fail "could not move the copy into place (original restored)"; }
rm -f "$DB-wal" "$DB-shm"
log "done. previous DB kept at $BAK"

log "=== SUCCESS ==="
log "  ${SIZE_BEFORE} MB -> ${SIZE_AFTER} MB (reclaimed $((SIZE_BEFORE - SIZE_AFTER)) MB)"
log "  rollback:  rm -f '$DB' '$DB-wal' '$DB-shm' && mv '$BAK' '$DB'"
log "  delete '$BAK' once you are satisfied (it is the old ${SIZE_BEFORE} MB file)"
log "restarting the server now..."
