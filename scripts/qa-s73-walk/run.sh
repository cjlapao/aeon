#!/usr/bin/env bash
# S73 T8 — one-call runner for the walk of `/config/agents`.
#
# Three processes, in the order the walk needs them, on a THROWAWAY store:
#   1. `aeon serve` on its own XDG_STATE_HOME and its own socket (never the real
#      store; the real one is not even read),
#   2. the S72 T4 bridge — a plain browser's stand-in for the Tauri shell, with
#      the built `ui/dist` on one side and the daemon's socket on the other,
#   3. walk.mjs, driving system Chrome through the steps, then a second run of it
#      with the daemon stopped (step 2.9).
#
# Everything lands under .s73-scratch/walk-run/ : daemon.log, bridge.log,
# walk.log, walk-disconnect.log. Exit code is 0 only if both walks reported zero
# FAIL steps.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AEON_ROOT="$(cd "$HERE/../.." && pwd)"
REPO="$(cd "$AEON_ROOT/.." && pwd)"
SCRATCH="${SCRATCH:-$AEON_ROOT/.s73-scratch/walk-run}"
AEON="${AEON:-$AEON_ROOT/.s73-scratch/bin/aeon}"
PORT="${PORT:-8733}"
SOCK="$SCRATCH/run/rpc.sock"
PW="${PW:-$REPO/node_modules/playwright-core}"

rm -rf "$SCRATCH/state" "$SCRATCH/run"
mkdir -p "$SCRATCH/state" "$SCRATCH/run"
[ -x "$AEON" ] || { echo "no binary at $AEON — go build -tags sqlite_fts5 -o $AEON ./cmd/aeon first"; exit 2; }
[ -d "$PW" ] || { echo "no playwright-core at $PW"; exit 2; }
[ -f "$AEON_ROOT/ui/dist/index.html" ] || { echo "no ui/dist — npx vite build first"; exit 2; }

echo "== daemon on $SOCK (throwaway store $SCRATCH/state)"
XDG_STATE_HOME="$SCRATCH/state" LOCALRPC_ENDPOINT_PATH="$SOCK" \
  nohup "$AEON" serve > "$SCRATCH/daemon.log" 2>&1 &
DAEMON=$!
for _ in $(seq 1 240); do
  [ -S "$SOCK" ] && break
  sleep 0.5
done
[ -S "$SOCK" ] || { echo "daemon never bound $SOCK"; tail -20 "$SCRATCH/daemon.log"; exit 1; }
# The first-party seed lays its agents during boot; give it the same grace the
# seed's own tests use rather than racing it.
sleep 25

echo "== bridge on :$PORT"
node "$HERE/../qa-s72-walk/bridge.mjs" --sock "$SOCK" --dist "$AEON_ROOT/ui/dist" --port "$PORT" \
  > "$SCRATCH/bridge.log" 2>&1 &
BRIDGE=$!
sleep 3

echo "== walk (steps 2.1–2.8, 2.10)"
TMPDIR=/tmp PW="$PW" AEON="$AEON" XDG_STATE_HOME="$SCRATCH/state" LOCALRPC_ENDPOINT_PATH="$SOCK" \
  WALK_URL="http://127.0.0.1:$PORT/" node "$HERE/walk.mjs" > "$SCRATCH/walk.log" 2>&1
RC_MAIN=$?

echo "== walk with the daemon down (step 2.9)"
kill "$DAEMON" 2>/dev/null
for _ in $(seq 1 40); do kill -0 "$DAEMON" 2>/dev/null || break; sleep 0.5; done
TMPDIR=/tmp PW="$PW" AEON="$AEON" XDG_STATE_HOME="$SCRATCH/state" LOCALRPC_ENDPOINT_PATH="$SOCK" \
  WALK_URL="http://127.0.0.1:$PORT/" node "$HERE/walk.mjs" --disconnect > "$SCRATCH/walk-disconnect.log" 2>&1
RC_DOWN=$?

kill "$BRIDGE" 2>/dev/null
echo "rc=$RC_MAIN for walk.mjs            ($SCRATCH/walk.log)"
echo "rc=$RC_DOWN for walk.mjs --disconnect ($SCRATCH/walk-disconnect.log)"
[ "$RC_MAIN" -eq 0 ] && [ "$RC_DOWN" -eq 0 ]
