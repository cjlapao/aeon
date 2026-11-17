#!/usr/bin/env bash
# Replay-only S7 crash walk. All state, sockets, and tool effects stay in a
# disposable directory. This kills only the daemon process this script starts.
set -euo pipefail

cd "$(dirname "$0")/.."
qa_dir=$(mktemp -d /tmp/aeon-s7-qa-XXXXXX)
daemon_pid=""
server_pid=""
step_pid=""
cleanup() {
  if [[ -n "$step_pid" ]]; then kill "$step_pid" 2>/dev/null || true; wait "$step_pid" 2>/dev/null || true; fi
  if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT

go build -tags aeondebug -o "$qa_dir/aeon" ./cmd/aeon
go build -o "$qa_dir/fixture-server" ./scripts/fixture-server

wait_for_socket() {
  for _ in $(seq 1 100); do
    if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa_dir/aeon" ping >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  echo "daemon did not open socket; see $qa_dir/daemon.log" >&2
  return 1
}

wait_for_result() {
  local expected=$1
  for _ in $(seq 1 120); do
    "$qa_dir/aeon" step show 1 > "$qa_dir/result.txt" 2>&1 || true
    if rg -q "outcome $expected" "$qa_dir/result.txt"; then return 0; fi
    sleep 0.1
  done
  echo "step did not reach $expected; see $qa_dir/result.txt and $qa_dir/daemon.log" >&2
  return 1
}

start_case() {
  local name=$1 fixture=$2 delay=${3:-1ms}
  export AEON_STATE_DIR="$qa_dir/$name/state"
  export LOCALRPC_ENDPOINT_PATH="$qa_dir/$name/rpc.sock"
  mkdir -p "$qa_dir/$name/workspace" "$AEON_STATE_DIR"
  local port
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
  export AEON_ANTHROPIC_BASE_URL="http://127.0.0.1:$port/v1"
  export ANTHROPIC_API_KEY="unused-by-replay"
  "$qa_dir/fixture-server" -addr "127.0.0.1:$port" -delay "$delay" \
    -fixture "$fixture" \
    -fixture internal/model/testdata/anthropic/step-handoff.sse > "$qa_dir/$name/server.log" 2>&1 &
  server_pid=$!
  sleep 0.2
}

stop_case() {
  if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; daemon_pid=""; fi
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; server_pid=""; fi
  step_pid=""
}

echo "QA artifacts: $qa_dir"

# A completed bash receipt survives a real SIGKILL before its transcript write.
start_case completed internal/model/testdata/anthropic/step-bash-tool.sse
AEON_DEBUG_CRASH_AFTER_TOOL=1 "$qa_dir/aeon" serve > "$qa_dir/completed/daemon.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step --workspace "$qa_dir/completed/workspace" --route anthropic --allow-bash \
  "run the command" > "$qa_dir/completed/step.log" 2>&1 &
step_pid=$!
wait "$step_pid" 2>/dev/null || true
step_pid=""
[[ -f "$qa_dir/completed/workspace/PROOF-BASH-RAN" ]]
before=$(stat -c %y "$qa_dir/completed/workspace/PROOF-BASH-RAN")
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
"$qa_dir/aeon" serve > "$qa_dir/completed/restarted.log" 2>&1 &
daemon_pid=$!
wait_for_socket
wait_for_result ok
after=$(stat -c %y "$qa_dir/completed/workspace/PROOF-BASH-RAN")
[[ "$before" == "$after" ]]
"$qa_dir/aeon" debug receipts 1 > "$qa_dir/completed/receipts.txt"
rg -q 'result captured true' "$qa_dir/completed/receipts.txt"
cp "$qa_dir/result.txt" "$qa_dir/completed/result.txt"
echo "PASS completed receipt: recovered result, bash did not run again"
stop_case

# A suspended question has the same unanswered tool shape as Case C, but the
# startup scan must leave it alone because its status is suspended.
start_case suspended internal/model/testdata/anthropic/step-question.sse
printf 'Aeon recovery QA\n' > "$qa_dir/suspended/workspace/README.md"
"$qa_dir/aeon" serve > "$qa_dir/suspended/daemon.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step --workspace "$qa_dir/suspended/workspace" --route anthropic --allow-question \
  "read README.md and ask" > "$qa_dir/suspended/step.log" 2>&1
"$qa_dir/aeon" step show 1 > "$qa_dir/suspended/before.txt"
rg -q 'status suspended' "$qa_dir/suspended/before.txt"
kill -9 "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
"$qa_dir/aeon" serve > "$qa_dir/suspended/restarted.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step show 1 > "$qa_dir/suspended/after.txt"
rg -q 'status suspended' "$qa_dir/suspended/after.txt"
"$qa_dir/aeon" answer 1 in-place > "$qa_dir/suspended/answer.txt"
wait_for_result ok
cp "$qa_dir/result.txt" "$qa_dir/suspended/result.txt"
echo "PASS suspended step: untouched by scan and resumed from saved question"
stop_case

# A saved tool result is a complete tail. Kill while the next model request is
# streaming; recovery must keep that result and continue from the frozen turn.
start_case complete-tail internal/model/testdata/anthropic/step-read-tool.sse 250ms
printf 'Aeon recovery QA\n' > "$qa_dir/complete-tail/workspace/README.md"
"$qa_dir/aeon" serve > "$qa_dir/complete-tail/daemon.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step --workspace "$qa_dir/complete-tail/workspace" --route anthropic \
  "read README.md" > "$qa_dir/complete-tail/step.log" 2>&1 &
step_pid=$!
for _ in $(seq 1 200); do
  "$qa_dir/aeon" step show 1 > "$qa_dir/complete-tail/before.txt" 2>/dev/null || true
  if rg -q '\[tool_result toolu_FIXTURE1\]' "$qa_dir/complete-tail/before.txt"; then break; fi
  sleep 0.02
done
rg -q '\[tool_result toolu_FIXTURE1\]' "$qa_dir/complete-tail/before.txt"
kill -9 "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
wait "$step_pid" 2>/dev/null || true
step_pid=""
"$qa_dir/aeon" serve > "$qa_dir/complete-tail/restarted.log" 2>&1 &
daemon_pid=$!
wait_for_socket
wait_for_result ok
rg -q 'recovery case A' "$qa_dir/result.txt"
[[ $(rg -c '\[tool_result toolu_FIXTURE1\]' "$qa_dir/result.txt") -eq 1 ]]
cp "$qa_dir/result.txt" "$qa_dir/complete-tail/result.txt"
echo "PASS complete tail: saved read result preserved once"
stop_case

# Kill after the model has streamed text but before its assistant turn was
# committed. The first call's precise usage is unavailable after SIGKILL, so
# the recovered cost stays unknown with the second call as a lower bound.
start_case model internal/model/testdata/anthropic/step-handoff.sse 250ms
"$qa_dir/aeon" serve > "$qa_dir/model/daemon.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step --workspace "$qa_dir/model/workspace" --route anthropic \
  "summarise the README" > "$qa_dir/model/step.log" 2>&1 &
step_pid=$!
for _ in $(seq 1 150); do
  if rg -q 'README covers' "$qa_dir/model/step.log"; then break; fi
  sleep 0.02
done
rg -q 'README covers' "$qa_dir/model/step.log"
kill -9 "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
wait "$step_pid" 2>/dev/null || true
step_pid=""
"$qa_dir/aeon" serve --no-recover > "$qa_dir/model/dry-run.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step show 1 > "$qa_dir/model/dry-run-step.txt"
rg -q 'status running' "$qa_dir/model/dry-run-step.txt"
for _ in $(seq 1 50); do
  if rg -q 'case B' "$qa_dir/model/dry-run.log"; then break; fi
  sleep 0.1
done
rg -q 'case B' "$qa_dir/model/dry-run.log"
kill "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
"$qa_dir/aeon" serve > "$qa_dir/model/restarted.log" 2>&1 &
daemon_pid=$!
wait_for_socket
wait_for_result ok
rg -q 'recovery case B · model call re-issued' "$qa_dir/result.txt"
rg -q 'stream-aborted-early' "$qa_dir/result.txt"
cp "$qa_dir/result.txt" "$qa_dir/model/result.txt"
echo "PASS incomplete model turn: re-issued with unknown first-call cost named"
stop_case

# Copy a real completed checkpoint into three new running rows. These rows are
# controlled Case A inputs; the next daemon is actually killed after committing
# two outcomes. The second restart must touch only the remaining row.
export AEON_STATE_DIR="$qa_dir/recovery-pass/state"
export LOCALRPC_ENDPOINT_PATH="$qa_dir/recovery-pass/rpc.sock"
mkdir -p "$AEON_STATE_DIR"
python3 - "$qa_dir/model/state/state.db" "$AEON_STATE_DIR/state.db" <<'PY'
import sqlite3, sys
source, target = sqlite3.connect(sys.argv[1]), sqlite3.connect(sys.argv[2])
source.backup(target)
columns = [row[1] for row in target.execute('PRAGMA table_info(step)') if row[1] != 'id']
names = ','.join(columns)
for _ in range(3):
    values = list(target.execute(f'SELECT {names} FROM step WHERE id=1').fetchone())
    values[columns.index('status')] = 'running'
    values[columns.index('outcome')] = None
    values[columns.index('ended_at')] = None
    values[columns.index('ended_by')] = 'open'
    cursor = target.execute(f'INSERT INTO step ({names}) VALUES ({",".join("?" for _ in columns)})', values)
    target.execute('INSERT INTO step_message (step_id,seq,format_version,role,content,created_at) SELECT ?,seq,format_version,role,content,created_at FROM step_message WHERE step_id=1', (cursor.lastrowid,))
target.commit()
PY
AEON_DEBUG_CRASH_DURING_RECOVERY=2 "$qa_dir/aeon" serve > "$qa_dir/recovery-pass/crashed.log" 2>&1 &
daemon_pid=$!
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
python3 - "$AEON_STATE_DIR/state.db" <<'PY'
import sqlite3, sys
statuses = [r[0] for r in sqlite3.connect(sys.argv[1]).execute('SELECT status FROM step WHERE id IN (2,3,4) ORDER BY id')]
assert statuses == ['done','done','running'], statuses
PY
"$qa_dir/aeon" serve > "$qa_dir/recovery-pass/restarted.log" 2>&1 &
daemon_pid=$!
wait_for_socket
for _ in $(seq 1 100); do
  "$qa_dir/aeon" step show 4 > "$qa_dir/recovery-pass/result.txt" 2>&1 || true
  if rg -q 'outcome ok' "$qa_dir/recovery-pass/result.txt"; then break; fi
  sleep 0.1
done
rg -q 'outcome ok' "$qa_dir/recovery-pass/result.txt"
[[ $(rg -c 'recovering [0-9]+ of [0-9]+' "$qa_dir/recovery-pass/restarted.log") -eq 1 ]]
echo "PASS mid-recovery SIGKILL: first two outcomes stayed committed, only third resumed"
stop_case

# An incomplete bash receipt must fail. Kill the exact daemon process while
# its replayed command sleeps, then start the same store normally.
python3 - <<'PY' > "$qa_dir/mid-bash.sse"
from pathlib import Path
text = Path('internal/model/testdata/anthropic/step-bash-tool.sse').read_text()
print(text.replace('touch ', 'sleep ').replace('PROOF-BASH-RAN', '30'), end='')
PY
start_case incomplete "$qa_dir/mid-bash.sse"
"$qa_dir/aeon" serve > "$qa_dir/incomplete/daemon.log" 2>&1 &
daemon_pid=$!
wait_for_socket
"$qa_dir/aeon" step --workspace "$qa_dir/incomplete/workspace" --route anthropic --allow-bash \
  "run the command" > "$qa_dir/incomplete/step.log" 2>&1 &
step_pid=$!
for _ in $(seq 1 100); do
  "$qa_dir/aeon" debug receipts 1 > "$qa_dir/incomplete/receipt-before.txt" 2>/dev/null || true
  if rg -q 'completed —' "$qa_dir/incomplete/receipt-before.txt"; then break; fi
  sleep 0.1
done
rg -q 'completed —' "$qa_dir/incomplete/receipt-before.txt"
kill -9 "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=""
wait "$step_pid" 2>/dev/null || true
step_pid=""
"$qa_dir/aeon" serve > "$qa_dir/incomplete/restarted.log" 2>&1 &
daemon_pid=$!
wait_for_socket
wait_for_result failed
rg -q 'bash receipt.*started but not completed' "$qa_dir/result.txt"
cp "$qa_dir/result.txt" "$qa_dir/incomplete/result.txt"
echo "PASS incomplete receipt: step failed with bash named; command was not re-run"
stop_case

echo "Replay crash walk passed. Inspect $qa_dir for transcripts, receipts, and daemon logs."
