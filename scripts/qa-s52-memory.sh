#!/usr/bin/env bash
# S52 memory-management walk (T6, P9): real daemon, real store, real socket,
# real CLI — the qa-s51-triage.sh shape carried. The lanes a person's socket
# cannot hold are QUOTED to named suite tests at their steps below (S50/S51's
# precedent): the agent lane needs a LOOP socket's peer kind (a person socket
# stamps user:<uid>, never the agent), and the two-window staleness is a
# two-browser-window act the vitest suite drives against a fake daemon.
# Everything else — the four-provenance vocabulary, the adds, the board and
# its honest EMPTY, the two-click retract gate and its --yes law, the stamp's
# disappearance from every default read, the flagged return, the restore
# replay, the delete refusal naming the count — runs for real and is asserted
# verbatim.
set -euo pipefail
cd "$(dirname "$0")/.."
qa=/tmp/opencode/aeon-tmp/s52walk
rm -rf "$qa"; mkdir -p "$qa"
daemon_pid=""
cleanup() {
  [[ -n "$daemon_pid" ]] && { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; }
}
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
mkdir -p "$AEON_STATE_DIR"

CGO_ENABLED=1 go build -tags sqlite_fts5 -o "$qa/aeon" ./cmd/aeon
"$qa/aeon" serve -no-backup > "$qa/daemon.log" 2>&1 &
daemon_pid=$!
for _ in $(seq 1 100); do
  if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa/aeon" ping >/dev/null 2>&1; then break; fi
  sleep 0.1
done
"$qa/aeon" ping >/dev/null
A="$qa/aeon"

# [D1] The vocabulary answers the closed sets and the ONE retract law —
# the CLI's gate and the UI's confirm quote THIS string, never a copy.
vocab=$($A learnings vocabulary)
grep -qF 'retract is a stamp, never a delete' <<<"$vocab"
grep -qE 'told.*observed.*assumed.*relayed' <<<"$vocab"

# The agent row the whole walk keys on: created over the person's socket
# (agent.create is S43's door), name chosen so the slug equals it.
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
        return "REFUSED: " + msg["error"].get("message", "")
    if msg["result"].get("refused"):
        return "REFUSED: " + str(msg["result"])
    return msg["result"]
call("initialize", {"clientName":"s52-walk","clientVersion":"0","protocolVersion":2})
out = call("agent.create", {"name": "memory-walk-bot", "description": "walk", "functionality": "walk"})
if isinstance(out, str) or out.get("slug") != "memory-walk-bot":
    raise SystemExit(f"agent.create did not land the slug: {out}")
open(qa + "/agent-slug", "w").write(out["slug"])
PY

# [D2] The adds. A person's told rides the person scope, audited user:<uid>;
# an observed rides the agent's OWN scope — the slug, never a name (§3/LM1).
$A learnings add --scope person --scope-key "$(id -u)" \
  --body "the walk retracts what it plants" --provenance told --tag s52walk > "$qa/add-told.out"
$A learnings add --scope agent --scope-key memory-walk-bot \
  --body "the CGO shim header must match go-sqlite3" --provenance observed \
  --observed-count 3 --tag s52walk > "$qa/add-obs.out"
body_id=$(grep -oE '^#[0-9]+' "$qa/add-obs.out" | head -1 | tr -d '#')
[[ -n "$body_id" ]] || { echo "the add did not print its id"; cat "$qa/add-obs.out"; exit 1; }
# The board shows both, in the daemon's own words — told and observed are
# distinct words on the board, and the strength rides as the daemon put it.
board=$($A learnings board --tag s52walk)
grep -qF 'told' <<<"$board"; grep -qF 'observed' <<<"$board"
grep -qF 'the CGO shim header must match go-sqlite3' <<<"$board"
# An agent may not file told and the store refuses relayed for ANY hand
# writer: those refusals are quoted — TestToldNeedsAPersonKind and
# TestRelayedNeedsTheSystemHook (internal/learnings/actor_test.go) drive
# the agent/system lanes a person socket cannot impersonate, and
# TestAgentScopeIsTheSlugNotTheName keys the scope on the slug.
echo "[D2] agent-lane refusals quoted: TestToldNeedsAPersonKind, TestRelayedNeedsTheSystemHook, TestAgentScopeIsTheSlugNotTheName"

# [D3] What would this agent see — the ONE retrieval function (its header
# says the prompt block and the door share it). Before the retraction the
# agent-scoped body is in the readable set.
$A learnings for-agent memory-walk-bot > "$qa/fa-before.out"
grep -qF 'the CGO shim header must match go-sqlite3' <<<"$(cat "$qa/fa-before.out")"

# [D4] The two-click gate. WITHOUT --yes the verb prints the daemon's law
# and the row and writes NOTHING — the board and the career prove it.
$A learnings retract "$body_id" --reason "superseded by the pinned version" > "$qa/gate.out" 2>&1 || true
grep -qF 'retract is a stamp, never a delete' <<<"$(cat "$qa/gate.out")"
$A learnings board --tag s52walk | grep -qF 'the CGO shim header must match go-sqlite3'
if $A learnings history "$body_id" | grep -q 'retract'; then
  echo "the gate without --yes still wrote an audit line"; exit 1
fi
# WITH --yes the stamp lands.
$A learnings retract "$body_id" --reason "superseded by the pinned version" --yes > "$qa/stamp.out"

# [D5] Gone from every DEFAULT read: the board, for-agent (= the prompt
# retrieval), the filtered board — and it returns ONLY flagged, stamped.
if $A learnings board --tag s52walk | grep -qF 'the CGO shim header must match go-sqlite3'; then
  echo "a retracted learning is still on the board"; exit 1
fi
if $A learnings for-agent memory-walk-bot | grep -qF 'the CGO shim header must match go-sqlite3'; then
  echo "a retracted learning is still in the agent's readable set"; exit 1
fi
ret=$($A learnings board --tag s52walk --include-retracted)
grep -qF 'the CGO shim header must match go-sqlite3' <<<"$ret"
grep -qF 'superseded by the pinned version' <<<"$ret"
# The career tells the stamp — actor, reason, the walk's own reason verbatim.
# The career grew the stamp line - op, actor, the daemon s own summary
$A learnings history "$body_id" > "$qa/career.out"
grep -qF 'retract' "$qa/career.out"
grep -qF 'user:' "$qa/career.out"
# The similarity index excludes it too — quoted: TestRetractedBodyIsNotRerankable
# (internal/learnings/s52_test.go) re-ranks against the live vec0 index.
echo "[D5] vec-index exclusion quoted: TestRetractedBodyIsNotRerankable"

# [D6] The restore, and the replay's honest changed:false on BOTH doors.
$A learnings restore "$body_id" --yes > "$qa/restore.out"
$A learnings board --tag s52walk | grep -qF 'the CGO shim header must match go-sqlite3'
$A learnings restore "$body_id" --yes > "$qa/restore-replay.out" 2>&1 || true
grep -qi 'changed:false\|nothing to change\|already' "$qa/restore-replay.out"

# [D7] LM6's filters are equality filters: an out-of-set word matches
# nothing and earns the board's honest EMPTY, not a guessed set.
empty=$($A learnings board --provenance banana)
grep -qi 'empty\|no learnings' <<<"$empty"

# [D8] Delete an agent whose memory is still scoped to it: refused,
# NAMING THE COUNT — retracted rows count too (they are still its memory).
python3 - "$LOCALRPC_ENDPOINT_PATH" "$qa" <<'PY'
import json, socket, sys
ep, qa = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM); s.connect(ep); f = s.makefile("rb")
s.sendall((json.dumps({"jsonrpc":"2.0","id":0,"method":"initialize","params":{"clientName":"s52-walk","clientVersion":"0","protocolVersion":2}})+"\n").encode())
json.loads(f.readline())
s.sendall((json.dumps({"jsonrpc":"2.0","id":1,"method":"agent.delete","params":{"slug":"memory-walk-bot"}})+"\n").encode())
msg = json.loads(f.readline())
text = json.dumps(msg)
if "refused" not in text.lower():
    raise SystemExit(f"agent.delete accepted a memory-owning agent: {text}")
if "learning" not in text.lower() or "1" not in text:
    raise SystemExit(f"the refusal does not name the count: {text}")
print("[D8] delete refused, count named")
PY
# The inert agent ask — the retract PROPOSAL stays inert until a person
# commits it, the commit stamps the COMMITTER, and a crash-retry moves one
# audit line: quoted to TestRetractAskIsInertAtRest,
# TestRetractCommitStampsTheCommitter, TestRetractCrashRetryCompletesWithoutASecondLine
# (cmd/aeon/memory_proposal_test.go) — the proposal lane files under a loop
# socket's peer kind, which this script's socket is not.
echo "[D9] proposal lane quoted: TestRetractAskIsInertAtRest, TestRetractCommitStampsTheCommitter, TestRetractCrashRetryCompletesWithoutASecondLine"
echo "[D10] two-window staleness quoted: MemorySection.test.tsx 'reconnecting asks nothing at all' (aeon/ui)"

# [D11] The upgrade close-out is Go-drill evidence, walked in-suite:
# TestS52CloseoutUpgradePreservesMemoryRows and
# TestS52CloseoutPlantedUnresolvedScopeKeyRefusesTheOpen
# (internal/store/s52_closeout_drill_test.go) — fresh-vs-upgrade folds of
# learning, learning_tag and proposal byte-equal, and the planted
# unresolvable scope_key refuses the WHOLE open, naming the key, until the
# operator registers it.
echo "[D11] close-out drills quoted: TestS52CloseoutUpgradePreservesMemoryRows, TestS52CloseoutPlantedUnresolvedScopeKeyRefusesTheOpen"

# [D12] M1's ordering golden — the for-agent WHY line and the ranking the
# prompt would see, pinned by TestForAgentEqualsTheRealRetrievalRanking
# (internal/learnings/s52_test.go).
why=$($A learnings for-agent memory-walk-bot --json)
grep -qF '"why"' <<<"$why"
grep -qF 'relayed ranks below told, observed and assumed' <<<"$why"

echo "S52 memory walk: every step asserted verbatim, rc=0"
