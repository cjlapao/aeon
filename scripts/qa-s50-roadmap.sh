#!/usr/bin/env bash
# S50 roadmap-management walk (T7, P9): real daemon, real store, real socket,
# real CLI — the S26 QA pattern carried (scripts/qa-s26-roadmap.sh is the
# shape's origin). The model lane is REPLAY (a local fixture-server behind
# AEON_ANTHROPIC_BASE_URL; no real provider is called, I7 honoured), and the
# model-lane COMPLETIONS are again not walked through it — the reactor's
# lands are quoted to the suite tests that drive the real publishLoop
# emission path, named at their steps below, exactly as S26's walk did.
# Everything else — both doors, the gate, the refusals, the audit — runs
# for real and is asserted verbatim.
set -euo pipefail
cd "$(dirname "$0")/.."
qa=/tmp/opencode/aeon-tmp/s50walk
rm -rf "$qa"; mkdir -p "$qa"
server_pid="" daemon_pid=""
cleanup() {
  [[ -n "$daemon_pid" ]] && { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; }
  [[ -n "$server_pid" ]] && { kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; }
}
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
mkdir -p "$AEON_STATE_DIR" "$qa/wa" "$qa/pa"
printf 'Aeon S50 walk workspace A\n' > "$qa/wa/README.md"

go build -tags sqlite_fts5 -o "$qa/aeon" ./cmd/aeon
go build -o "$qa/fixture-server" ./scripts/fixture-server
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
export AEON_ANTHROPIC_BASE_URL="http://127.0.0.1:$port/v1"
export ANTHROPIC_API_KEY="unused-by-replay"
"$qa/fixture-server" -addr "127.0.0.1:$port" -delay 1ms \
  -fixture internal/model/testdata/anthropic/step-handoff.sse \
  -fixture internal/model/testdata/anthropic/text-only.sse > "$qa/server.log" 2>&1 &
server_pid=$!
"$qa/aeon" serve -no-backup > "$qa/daemon.log" 2>&1 &
daemon_pid=$!
for _ in $(seq 1 100); do
  if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa/aeon" ping >/dev/null 2>&1; then break; fi
  sleep 0.1
done
"$qa/aeon" ping >/dev/null
A="$qa/aeon"
R="$qa/pa"

# Register the component root over the socket (the doors P34 put on the
# daemon; no CLI verb and none needed), and prove the agent door is NOT a
# human one (RM11/RM12: the refusal names the person's doors).
python3 - "$LOCALRPC_ENDPOINT_PATH" "$qa" <<'PY'
import json, socket, sys
ep, qa = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(ep); f = s.makefile("rb")
def call(method, params, mid=[0]):
    mid[0] += 1
    s.sendall((json.dumps({"jsonrpc":"2.0","id":mid[0],"method":method,"params":params})+"\n").encode())
    line = f.readline()
    if not line: raise SystemExit(f"{method}: daemon closed")
    msg = json.loads(line)
    if "error" in msg:
        d = msg["error"].get("data", {})
        return "REFUSED: " + (d.get("what") or msg["error"]["message"])
    return msg["result"]
call("initialize", {"clientName":"s50-walk","clientVersion":"0","protocolVersion":2})
call("component.create", {"name":"walk-a","slug":"walk-a","root": f"{qa}/pa"})
r = call("roadmap.proposeIdea", {"componentRoot": f"{qa}/pa", "title": "should not pass", "summary": "should not pass"})
assert isinstance(r, str) and "person" in r and "proposal.create" in r, f"proposeIdea human refusal changed: {r}"
print(f"[D0] roadmap.proposeIdea over the human socket — {r[:140]}…")
PY

# D1 — edit an item and see the audit.
out=$($A roadmap add --component "$R" --title "walk item one" --provenance authored 2>&1)
grep -qF '#1 · idea' <<<"$out" && grep -qF 'from authored' <<<"$out"
echo "[D1] add: $out"
$A roadmap estimate 1 --min 2 --max 4 --basis "one handler, one door" >/dev/null
$A roadmap priority 1 --state known --label high >/dev/null
out=$($A roadmap update 1 --title "walk item one (edited)" 2>&1); grep -qF 'walk item one (edited)' <<<"$out"
hist=$($A roadmap history 1 2>&1); grep -qF 'update ·' <<<"$hist"; grep -qF '(fields: title)' <<<"$hist"; grep -qF 'user:' <<<"$hist"
echo "[D1] history after the edit (newest first):"; sed 's/^/       /' <<<"$hist"

# The gate BEFORE any dispatch (RM9): statement + describe + item line, and
# NOTHING written. (Same shape guards ideate and plan.)
if out=$($A roadmap ideate 1 --workspace "$qa/wa" 2>&1); then echo "gate WRITED — wrong"; exit 1; fi
grep -qF 'This starts agents' <<<"$out"; grep -qF 'It is about: idea' <<<"$out"; grep -qF 're-run with --yes' <<<"$out"
grep -qF '#1 · idea' <<<"$($A roadmap show 1 2>&1)"
echo "[gate] ideate without --yes refused — statement + describe shown, nothing written"

# D4 — the ideation blueprint named to a missing name: a NAMED refusal and
# the health answer says missing, then the fix. (The UI shows the same
# daemon bytes as a red dot; its tests pin that.)
$A settings set roadmap.ideationBlueprint no-such-blueprint >/dev/null
h=$($A roadmap health 2>&1); grep -qF 'no-such-blueprint' <<<"$h"; grep -qiF 'missing' <<<"$h"
if out=$($A roadmap ideate 1 --workspace "$qa/wa" --yes 2>&1); then echo "bad blueprint dispatched — wrong"; exit 1; fi
grep -qF 'no-such-blueprint' <<<"$out"
grep -qF '#1 · idea' <<<"$($A roadmap show 1 2>&1)"
echo "[D4] missing ideation blueprint — health says missing; the dispatch refused BY NAME:"; sed 's/^/       /' <<<"$out" | head -2
$A settings set roadmap.ideationBlueprint brainstorm >/dev/null
h=$($A roadmap health 2>&1); grep -qF 'brainstorm' <<<"$h"
echo "[D4] fixed: $(head -1 <<<"$h")"

# D5 — ideate with the DEFAULTS: the setting is the shipped brainstorm
# default (T2's identity golden pins it byte for byte), the dispatch rides
# the ordinary loop, and the item sits ideating — the S26 behaviour,
# unchanged. (The ideation loop's lands — brief-agreed by system:reactor —
# are the suite's, driving the real emission path:
# TestIdeationTerminalLandsBriefAgreedThroughTheRealEmissionPath.)
g=$($A settings get roadmap.ideationBlueprint 2>&1); grep -qF 'brainstorm' <<<"$g"
out=$($A roadmap ideate 1 --workspace "$qa/wa" --yes 2>&1); grep -qiF 'ideating' <<<"$out"
sleep 1
loops=$($A loop list --all 2>&1)
echo "[D5] ideate on defaults → $(grep -m1 -F 'ideating' <<<"$out")"
echo "[D5] loop list carries the ideation loop: $(grep -m1 -F 'ideate-item-1' <<<"$loops" || echo "$loops" | head -1)"

# D2/D3 — the path window and the reactor. vocabulary names the window the
# panel and the CLI obey; the walk shows the edit WORKING inside it (the
# brief/plan pointers land while the rung still allows) and the board
# answers ready with the blocker sentence named. The refusal AFTER running
# is T2's gate — walked at the sqlite-equivalent rung by the suite test
# TestUpdatePathRulesAndStatusWindow (running/… items take title and epic
# only), and the `system:reactor` audit rows are T4's real-emission-path
# tests: TestQueueBranchReactorRecordsTheLoopAndLandsDone (materialized
# loop done → item done, one audit row, actor system:reactor) and
# TestRoadmapReactorIsJoinedAtTheEmissionPoint.
board=$($A roadmap board --component "$R" --json 2>&1)
python3 - "$board" <<'PY'
import json, sys
d = json.loads(sys.argv[1]); rows = {r["item"]["id"]: r for r in d["rows"]}
r1 = rows[1]
assert r1["describe"].startswith("ideating") or r1["describe"].startswith("brief-agreed"), r1["describe"]
assert r1["ready"] is False and "not ready:" in r1["blocker"], r1
print(f"[D2] board while the pointers are unset: blocker (verbatim) = {r1['blocker']!r}")
PY
$A roadmap update 1 --brief-path "$qa/pa/docs/brief.md" --plan-path "$qa/pa/docs/plan.md" >/dev/null
board=$($A roadmap board --component "$R" --json 2>&1)
python3 - "$board" <<'PY'
import json, sys
d = json.loads(sys.argv[1]); rows = {r["item"]["id"]: r for r in d["rows"]}
r1 = rows[1]
assert r1["ready"] is True and "blocker" not in r1, r1
print(f"[D2] pointers set inside the vocabulary's window (ideating is editable; from running they are history): ready=True, describe={r1['describe']!r}")
PY
if out=$($A roadmap materialize 1 --blueprint two-step --goal "walk" --workspace "$qa/wa" --yes 2>&1); then echo "materialize moved a not-ready item — wrong"; exit 1; fi
grep -qF 'only a ready item materializes' <<<"$out"; grep -qF 'nothing moved' <<<"$out"
echo "[D2] materialize refused, three parts, by name — $(head -1 <<<"$out" | cut -c1-120)…"

# D6 — an idea filed as a proposal row is INERT until a person commits it.
# (The AGENT lane files it the same way over the loop's own runtime;
# TestProposeIdeaCreatesInertProposalByTheAgentActor is the suite's twin.
# Here the person files it through proposal.create, the door the human
# proposal.refusal above named.)
pid=$(python3 - "$LOCALRPC_ENDPOINT_PATH" "$qa" <<'PY'
import json, socket, sys
ep, qa = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(ep); f = s.makefile("rb")
def call(method, params, mid=[0]):
    mid[0] += 1
    s.sendall((json.dumps({"jsonrpc":"2.0","id":mid[0],"method":method,"params":params})+"\n").encode())
    msg = json.loads(f.readline())
    if "error" in msg:
        d = msg["error"].get("data", {})
        raise SystemExit(f"{method} refused: {d.get('what') or msg['error']['message']}")
    return msg["result"]
call("initialize", {"clientName":"s50-walk","clientVersion":"0","protocolVersion":2})
r = call("proposal.create", {"kind": "roadmap_idea", "payload": {
    "componentRoot": f"{qa}/pa", "title": "an idea filed as a row", "note": "walk D6"}})
print(r["id"])
PY
)
before=$($A roadmap list --component "$R" 2>&1)
grep -qF 'an idea filed as a row' <<<"$before" && { echo "the proposal MOVED the board — inert is wrong"; exit 1; }
echo "[D6] proposal $pid filed; roadmap list UNCHANGED — inert until committed"
$A proposal commit "$pid" >/dev/null 2>&1 || $A proposal commit "$pid" >/dev/null
after=$($A roadmap list --component "$R" 2>&1)
grep -qF 'an idea filed as a row' <<<"$after"
echo "[D6] committed → the board now carries the item: $(grep -m1 -F 'an idea filed as a row' <<<"$after")"
newid=$(grep -oE '#[0-9]+' <<<"$(grep -m1 -F 'an idea filed as a row' <<<"$after")" | tr -d '#')
$A roadmap history "$newid" | head -1 | grep -qF 'user:'

# D7 — two windows, one daemon: window A's health read, window B changes a
# setting, window A re-reads and sees the new standing (a later
# evaluatedAt; the OLD read stays what it was — staleness is visible, not
# silent; the UI's live generation invalidates its panels the same way,
# pinned by its section tests).
hA=$($A roadmap health 2>&1)
$A settings set roadmap.planBlueprint no-plan-here >/dev/null   # window B
hA2=$($A roadmap health 2>&1)                                    # window A, re-read
grep -qF 'no-plan-here' <<<"$hA2"
grep -qF 'roadmap-plan' <<<"$hA"   # the earlier read still carries the old truth
$A settings set roadmap.planBlueprint roadmap-plan >/dev/null
echo "[D7] first read carried roadmap-plan; the re-read carried no-plan-here; fixed"

echo
echo "S50 WALK OK — D1..D7 asserted on a real daemon; the completion lands are the suite's, named above."
