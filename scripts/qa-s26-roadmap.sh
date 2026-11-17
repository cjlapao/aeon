#!/usr/bin/env bash
# S26 roadmap walk: real daemon, real store, real socket, real CLI; replay
# provider so NO real model is called (the walked record says plainly: the
# model lane is REPLAY, not real — and the ideation/planning COMPLETIONS are
# not walked through it, see Findings). Everything else runs for real.
set -euo pipefail
cd "$(dirname "$0")/.."
root="$(git rev-parse --show-toplevel)"
export GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$root/.tmp/cgo-shim" GOCACHE="${GOCACHE:-$root/.tmp/gocache}"
qa=/tmp/opencode/aeon-tmp/s26walk
rm -rf "$qa"; mkdir -p "$qa"
server_pid="" daemon_pid=""
cleanup() {
  [[ -n "$daemon_pid" ]] && { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; }
  [[ -n "$server_pid" ]] && { kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; }
}
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
mkdir -p "$AEON_STATE_DIR" "$qa/wa" "$qa/wb" "$qa/pa" "$qa/pb"
printf 'Aeon S26 walk workspace A\n' > "$qa/wa/README.md"
printf 'Aeon S26 walk workspace B\n' > "$qa/wb/README.md"

# Build the binaries and start the replay provider + daemon (the S8 QA
# pattern, verbatim in shape).
go build -tags sqlite_fts5 -o "$qa/aeon" ./cmd/aeon
go build -o "$qa/fixture-server" ./scripts/fixture-server
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
export AEON_ANTHROPIC_BASE_URL="http://127.0.0.1:$port/v1"
export ANTHROPIC_API_KEY="unused-by-replay"
"$qa/fixture-server" -addr "127.0.0.1:$port" -delay 1ms \
  -fixture internal/model/testdata/anthropic/step-handoff.sse \
  -fixture internal/model/testdata/anthropic/text-only.sse > "$qa/server.log" 2>&1 &
server_pid=$!
"$qa/aeon" serve > "$qa/daemon.log" 2>&1 &
daemon_pid=$!
for _ in $(seq 1 100); do
  if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa/aeon" ping >/dev/null 2>&1; then break; fi
  sleep 0.1
done
"$qa/aeon" ping >/dev/null

# Register the two component roots over the socket (initialize +
# component.create — the doors P34 put on the daemon; no CLI verb and none
# needed, the app calls the same method).
python3 - "$LOCALRPC_ENDPOINT_PATH" "$qa" <<'PY'
import json, socket, sys
ep, qa = sys.argv[1], sys.argv[2]
s=socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(ep); f=s.makefile("rb")
def call(method, params, mid=[0]):
    mid[0] += 1
    s.sendall((json.dumps({"jsonrpc":"2.0","id":mid[0],"method":method,"params":params})+"\n").encode())
    line=f.readline()
    if not line: raise RuntimeError("daemon closed")
    msg=json.loads(line)
    if "error" in msg: raise SystemExit(f"{method} refused: {msg['error']}")
    return msg["result"]
call("initialize",{"clientName":"s26-walk","clientVersion":"0","protocolVersion":2})
for slug, rt in (("walk-a", f"{qa}/pa"), ("walk-b", f"{qa}/pb")):
    call("component.create",{"name":slug,"slug":slug,"root":rt})
    print(f"component {slug} registered at {rt}")
s.close()
PY

A="$qa/pa"; B="$qa/pb"; AEON="$qa/aeon"

# --- Step 1: add renders idea + the literal word unknown -------------------
add_out=$("$AEON" roadmap add --component "$A" --title "dark mode" --epic ui --priority unknown --provenance authored)
id1=$(awk '{gsub(/^#/,""); print $1}' <<< "$add_out")
list_out=$("$AEON" roadmap list --component "$A")
echo "$list_out"
grep -q "idea" <<< "$list_out" && grep -q "unknown" <<< "$list_out" \
  || { echo "F-STEP1: idea/unknown did not render"; exit 1; }

# D8's empty board speaks for the untouched project.
empty=$("$AEON" roadmap list --component "$B")
grep -q "EMPTY" <<< "$empty" && grep -q "loop.start never asked" <<< "$empty" \
  || { echo "F-D8: the empty board did not speak"; exit 1; }

# --- Step 2: ideation dispatch starts an ORDINARY loop ----------------------
"$AEON" roadmap ideate "$id1" --workspace "$qa/wa" > "$qa/ideate.txt"
cat "$qa/ideate.txt"
grep -qi "ideating" <<< "$(cat "$qa/ideate.txt")" || { echo "F-STEP2: no ideating answer"; exit 1; }
loops=$("$AEON" loop list --all 2>/dev/null || "$AEON" loop list 2>/dev/null || true)
grep -q "ideate-item-$id1" <<< "$loops" \
  || { echo "F-STEP2: the ideation loop is not in the ordinary loop list"; exit 1; }
# The item sits ideating, not silently advanced.
show1=$("$AEON" roadmap show "$id1")
grep -q "ideating" <<< "$show1" || { echo "F-STEP2: item not ideating"; exit 1; }

# --- Steps 3–4: recorded as NOT walked through the model lane --------------
# brief-agreed/planned need loop.finished(done) from the model lane, which
# on this base is REPLAY here and whose brainstorm capture-lane fails on the
# base itself (the two pre-existing replay failures). The completions are
# proven by the daemon suite driving the real emission path (see T3/T4 Notes
# and Findings); the walk says so plainly rather than implying it walked.

# --- Step 5: estimate + priority render the honest states ------------------
"$AEON" roadmap estimate "$id1" --min 2 --max 4 --basis "one handler, one door" > "$qa/est.txt"
grep -q "2–4 (one handler, one door)" <<< "$(cat "$qa/est.txt")" \
  || { echo "F-STEP5: estimate did not render as the trio"; exit 1; }
# A ready attempt without brief/plan refuses at the attempt, naming D4's gap.
"$AEON" roadmap priority "$id1" --state known --label high > "$qa/pri.txt"
mat_err=$("$AEON" roadmap materialize "$id1" --blueprint two-step --goal "dark mode" --workspace "$qa/wa" 2>&1 || true)
echo "MATERIALIZE REFUSAL >>> $mat_err"
grep -qi "only a ready item" <<< "$mat_err" && grep -qi "brief, plan" <<< "$mat_err" && grep -qi "ideating" <<< "$mat_err" \
  || { echo "F-STEP5: materialize did not refuse-not-ready by naming the gap"; exit 1; }

# --- Steps 6–7: the branches' GATES are walked; the starts are the suite's -
# Queue branch: one real queue row makes the project queue-in-use; the
# materialization of a ready item would append — untestable without ready,
# so the queue probe path is shown by the queue itself working for real:
"$AEON" queue append --component "$A" --blueprint two-step --goal "filler intent" > "$qa/q.txt"
"$AEON" queue list --component "$A" > "$qa/ql.txt"; cat "$qa/ql.txt"
grep -q "filler intent" <<< "$(cat "$qa/ql.txt")" || { echo "F-STEP6: queue row not visible"; exit 1; }
# D8's second half on project B (no queue rows): the probe would take the
# direct branch — walked at the gate; the starts themselves ride loop.start,
# whose door the suite pins by source-scan (the one call).

# --- Step 8: cancel is D7 made visible (a pre-loop rung) -------------------
add2=$("$AEON" roadmap add --component "$A" --title "legacy tidy" --provenance triaged --priority assumed --priority-label someday)
id2=$(awk '{gsub(/^#/,""); print $1}' <<< "$add2")
"$AEON" roadmap cancel "$id2" > "$qa/cancel.txt"
grep -q "cancelled never deletes" <<< "$(cat "$qa/cancel.txt")" || { echo "F-STEP8: cancel words wrong"; exit 1; }
list2=$("$AEON" roadmap list --component "$A")
grep -q "legacy tidy" <<< "$list2" && grep -qi "cancelled" <<< "$list2" \
  || { echo "F-STEP8: the cancelled row vanished or misrendered"; exit 1; }
grep -q "dark mode" <<< "$list2" # the other item still there — distinct from done, both listed

echo "S26 WALK: all walkable steps passed (recorded gaps: model-lane completions — Findings)."
