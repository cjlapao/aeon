#!/usr/bin/env bash
# S8 replay walk: real daemon, store, tools and UI socket; local provider fixture.
set -euo pipefail
cd "$(dirname "$0")/.."
qa_dir=$(mktemp -d /tmp/aeon-s8-qa-XXXXXX)
server_pid=""
daemon_pid=""
cleanup() {
  if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
export AEON_STATE_DIR="$qa_dir/state"
export LOCALRPC_ENDPOINT_PATH="$qa_dir/rpc.sock"
mkdir -p "$AEON_STATE_DIR" "$qa_dir/workspace"
printf 'Aeon S8 replay workspace\n' > "$qa_dir/workspace/README.md"
export GOCACHE="${GOCACHE:-/tmp/aeon-go-cache}"
go build -o "$qa_dir/aeon" ./cmd/aeon
go build -o "$qa_dir/fixture-server" ./scripts/fixture-server
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
export AEON_ANTHROPIC_BASE_URL="http://127.0.0.1:$port/v1"
export ANTHROPIC_API_KEY="unused-by-replay"
"$qa_dir/fixture-server" -addr "127.0.0.1:$port" -delay 1ms \
  -fixture internal/model/testdata/anthropic/s8-survey-write.sse \
  -fixture internal/model/testdata/anthropic/step-handoff.sse \
  -fixture internal/model/testdata/anthropic/s8-summary-write.sse \
  -fixture internal/model/testdata/anthropic/step-handoff.sse > "$qa_dir/server.log" 2>&1 &
server_pid=$!
"$qa_dir/aeon" serve > "$qa_dir/daemon.log" 2>&1 &
daemon_pid=$!
for _ in $(seq 1 100); do
  if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa_dir/aeon" ping >/dev/null 2>&1; then break; fi
  sleep 0.1
done
"$qa_dir/aeon" ping >/dev/null
"$qa_dir/aeon" blueprint validate --strict two-step > "$qa_dir/strict.txt"
"$qa_dir/aeon" blueprint validate dev-loop > "$qa_dir/dev-loop.txt"
rg -q '0 invalid' "$qa_dir/dev-loop.txt"
start=$("$qa_dir/aeon" loop start --project "$qa_dir/workspace" --blueprint two-step --route anthropic "Survey README.md, write SURVEY.md, then summarize it in SUMMARY.md")
loop_id=$(awk 'NR==1 {print $2}' <<< "$start")
[[ -n "$loop_id" ]]
for _ in $(seq 1 120); do
  "$qa_dir/aeon" loop show "$loop_id" --json > "$qa_dir/waiting.json"
  if rg -q '"status": "waiting"' "$qa_dir/waiting.json"; then break; fi
  sleep 0.1
done
rg -q '"status": "waiting"' "$qa_dir/waiting.json"
[[ -f "$qa_dir/workspace/SURVEY.md" ]]
[[ ! -f "$qa_dir/workspace/SUMMARY.md" ]]
"$qa_dir/aeon" loop show "$loop_id" --blueprint > "$qa_dir/pinned-before.txt"
blueprint_path=$("$qa_dir/aeon" blueprint path two-step)
python3 - "$blueprint_path" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1]); text=p.read_text(); extra='  - id: editorial-check\n    step: {kind: gate, question: approve-survey}\n'
text=text.replace('questions:\n',extra+'questions:\n',1); p.write_text(text)
PY
"$qa_dir/aeon" blueprint validate --strict two-step > "$qa_dir/edited.txt"
"$qa_dir/aeon" loop show "$loop_id" --blueprint > "$qa_dir/pinned-after.txt"
cmp "$qa_dir/pinned-before.txt" "$qa_dir/pinned-after.txt"
"$qa_dir/aeon" answer "$loop_id" approve > "$qa_dir/answer.txt"
for _ in $(seq 1 120); do
  "$qa_dir/aeon" loop show "$loop_id" --json > "$qa_dir/done.json"
  if rg -q '"status": "done"' "$qa_dir/done.json"; then break; fi
  sleep 0.1
done
rg -q '"status": "done"' "$qa_dir/done.json"
[[ -f "$qa_dir/workspace/SUMMARY.md" ]]
new_start=$("$qa_dir/aeon" loop start --project "$qa_dir/workspace" --blueprint two-step --route anthropic "Survey README.md again")
new_id=$(awk 'NR==1 {print $2}' <<< "$new_start")
"$qa_dir/aeon" loop show "$new_id" --blueprint > "$qa_dir/new-pin.txt"
rg -q 'editorial-check' "$qa_dir/new-pin.txt"
if rg -q 'editorial-check' "$qa_dir/pinned-after.txt"; then echo 'old pin followed source edit' >&2; exit 1; fi
echo "S8 replay walk passed; QA artifacts: $qa_dir"
