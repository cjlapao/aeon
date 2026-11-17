#!/usr/bin/env bash
# S51 triage-management walk (T7, P9): real daemon, real store, real socket,
# real CLI — the qa-s50-roadmap.sh shape carried. The model lane is REPLAY
# (fixture-server behind AEON_ANTHROPIC_BASE_URL; no real provider, I7); the
# validation loop's COMPLETION and the agent lane are quoted to named suite
# tests at their steps below (S26/S50's precedent — replay's canned answers
# would fabricate the lands' evidence). Everything else — both doors, the
# gates, the refusals, the audit, the two state sets — runs for real and is
# asserted verbatim.
set -euo pipefail
cd "$(dirname "$0")/.."
qa=/tmp/opencode/aeon-tmp/s51walk
rm -rf "$qa"; mkdir -p "$qa"
server_pid="" daemon_pid=""
cleanup() {
  [[ -n "$daemon_pid" ]] && { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; }
  [[ -n "$server_pid" ]] && { kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; }
}
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
mkdir -p "$AEON_STATE_DIR" "$qa/pa"
printf 'Aeon S51 walk workspace A\n' > "$qa/pa/README.md"

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

# The agent lane on the PERSON's socket is named and refused (TM6): the
# filing twin does not file under a person — it points at triage.file, the
# person's own door. Same bytes the conformance fixture carries.
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
call("initialize", {"clientName":"s51-walk","clientVersion":"0","protocolVersion":2})
call("component.create", {"name":"walk-a","slug":"walk-a","root": f"{qa}/pa"})
r = call("triage.fileAs", {"componentRoot": f"{qa}/pa", "title": "should not pass", "body": "should not pass"})
assert isinstance(r, str) and "person" in r and "triage.file" in r, f"fileAs human refusal changed: {r}"
print(f"[gate] triage.fileAs over the person's socket — {r[:150]}")
PY

# D1 — a person files through the self-file door: the claim arrives
# `unvalidated`, priority `unknown`, filed_by 'person' — the daemon derived
# it from the peer; there is no --filed-by to lie with (S51 TM3).
out=$($A triage file --component "$R" --title "retry counter over-counts" --body "adds the attempt number, not one" 2>&1)
grep -qF '#1 · unvalidated · unknown' <<<"$out" && grep -qF '· filed by person' <<<"$out"
echo "[D1] person file: $out"

# D2 — the agent's twin files the SAME unvalidated shape under its slug.
# The agent lane is a loop's socket, not this one; the suite drives it with
# a real stamped peer: TestBugFileFilesAnUnvalidatedClaimWithOneAgentAuditRow
# (slug filer, one `file` audit row), TestSelfFiledBugIndistinguishableFrom-
# HumanFiled (the shape carries no hint of who filed), TestBugFileRefusesASmug-
# gledFieldAndAnUnknownAgent, TestBugFileIsRefusedToAPersonWithThePointerSentence.
$A triage history 1 | grep -qF '· file ·'   # the person's file line is stamped user:
echo "[D2] agent lane quoted to the named daemon tests (loop socket is not this socket's peer kind)"

# D3 — a person's edit, and the audit shows it: title/body only, and the
# history answers newest-first with the stamped actor.
$A triage update 1 --title "retry counter over-counts on the third attempt" >/dev/null
grep -qF 'on the third attempt' <<<"$($A triage show 1 2>&1)"
hist=$($A triage history 1 2>&1)
grep -qF 'update ·' <<<"$hist"; grep -qF 'user:' <<<"$hist"
echo "[D3] history after the edit (newest first):"; sed 's/^/       /' <<<"$hist"

# D4 — a duplicate is a person's irreversible act (D5/N3). A machine
# confirmer is refused BY NAME and writes nothing; a person's name lands it,
# audited, and the board says so in the daemon's own sentence.
out=$($A triage file --component "$R" --title "same over-count, one attempt later" --body "duplicate of bug 1" 2>&1)
grep -qF '#2 · unvalidated · unknown' <<<"$out"
h1_before=$($A triage history 1 2>&1 | wc -l)
if out=$($A triage confirm-duplicate 2 --component "$R" --of 1 --confirmed-by agent --yes 2>&1); then echo "an agent confirmer linked it — wrong"; exit 1; fi
grep -qF 'refused confirm-duplicate by "agent"' <<<"$out"; grep -qF 'only from a person' <<<"$out"
[[ "$($A triage history 1 2>&1 | wc -l)" == "$h1_before" ]]   # refusal wrote nothing
out=$($A triage confirm-duplicate 2 --component "$R" --of 1 --confirmed-by "Carlos" --yes 2>&1)
grep -qF '· dup of #1' <<<"$out"
grep -qF '· duplicate ·' <<<"$($A triage history 2 2>&1)"
echo "[D4] confirm-duplicate: machine refused BY NAME, person's name landed it — $out"

# The gate BEFORE any dispatch (TM9): validate without --yes prints the
# daemon's authority statement (the SAME string the UI quotes, TM8/RM9's
# one-string law) and the bug's own line, and writes nothing.
before=$($A triage history 1 2>&1 | wc -l)
if out=$($A triage validate 1 2>&1); then echo "validate without --yes moved something — wrong"; exit 1; fi
grep -qF 'This starts a validation attempt' <<<"$out"; grep -qF 're-run with --yes' <<<"$out"
grep -qF '#1 · unvalidated' <<<"$($A triage show 1 2>&1)"
[[ "$($A triage history 1 2>&1 | wc -l)" == "$before" ]]
echo "[gate] validate without --yes refused — statement printed, nothing written"

# TM4 — the blueprint named to a missing name: health says `missing`, and
# the dispatch REFUSES NAMING the setting and the name — never a fallback.
$A settings set triage.validationBlueprint no-such-blueprint >/dev/null
h=$($A triage health 2>&1); grep -qiF 'missing' <<<"$h"; grep -qF 'no-such-blueprint' <<<"$h"
if out=$($A triage validate 1 --yes 2>&1); then echo "a missing blueprint dispatched — wrong"; exit 1; fi
grep -qF 'no-such-blueprint' <<<"$out"
grep -qF '#1 · unvalidated' <<<"$($A triage show 1 2>&1)"
echo "[TM4] missing blueprint — health says missing; dispatch refused by name:"; sed 's/^/       /' <<<"$out" | head -2

# The defaults: triage.validationBlueprint's shipped default IS brainstorm
# (TestTriageDefaultsReproduceTodaysValidate pins the dispatch byte for
# byte against S29's), validate --yes lands an ORDINARY loop, and the bug
# stays unvalidated — the reactor moves it on the loop's own terminal
# (internal/daemon's handleTriageTerminal tests drive the real emission
# path; the walk does not fake a completion through replay).
$A settings set triage.validationBlueprint brainstorm >/dev/null
out=$($A triage validate 1 --yes 2>&1); grep -qiF 'triage-validate-1' <<<"$out"
sleep 1
loops=$($A loop list --all 2>&1); grep -qF 'triage-validate-1' <<<"$loops"
grep -qF '#1 · unvalidated' <<<"$($A triage show 1 2>&1)"
grep -qF 'started a validation attempt on bug 1' <<<"$($A triage history 1 2>&1)"
echo "[D5] validate on defaults → ordinary loop triage-validate-1; the bug stays unvalidated by law"

# D6 — hand-off honesty: an unvalidated bug is refused; the success shape
# (validated + prioritized → roadmap item, provenance `triaged`, the board
# carrying its status) is the suite's — TestTriageHandoffCarriesTheStamped-
# ActorIntoTheSeam + the handoff conformance fixture (#7 → item 42).
if out=$($A triage handoff 1 --yes 2>&1); then echo "an unvalidated bug handed off — wrong"; exit 1; fi
grep -qF 'needs a validated bug' <<<"$out"
echo "[D6] handoff of an unvalidated bug refused by the DOOR — $(grep -m1 -oF "needs a validated bug" <<<"$out")"

# D7 — the two state sets render SEPARATELY (D2, property-tested; the walk
# reads the board's own JSON): validation and priority are two fields, two
# sentences, never one crossed word; bug 2 carries the daemon's duplicate
# sentence; attempts count the audit.
board=$($A triage board --component "$R" --json 2>&1)
python3 - "$board" <<'PY'
import json, sys
rows = {r["bug"]["id"]: r for r in json.loads(sys.argv[1])}
r1, r2 = rows[1], rows[2]
assert r1["bug"]["validation"] == "unvalidated" and r1["bug"]["priorityState"] == "unknown", r1["bug"]
assert r1["describe"]["validationSentence"] and r1["describe"]["prioritySentence"], r1["describe"]
assert r1["describe"]["validationSentence"] != r1["describe"]["prioritySentence"]
assert r2["describe"].get("duplicate"), r2["describe"]
assert r1["attempts"] >= 1 and r1.get("lastAttemptAt"), r1
print(f"[D7] board: #{r1['bug']['id']} {r1['describe']['validationSentence']!r} / {r1['describe']['prioritySentence']!r}; #{r2['bug']['id']} {r2['describe']['duplicate']!r}")
PY

# D8 — two windows, one daemon: window A's health read, window B changes the
# setting, window A's fresh read carries the new standing — staleness is an
# old answer, not a wrong one (the UI's generation-based invalidation is its
# section tests' T: a stale connection asks nothing at all).
hA=$($A triage health 2>&1); grep -qF 'brainstorm' <<<"$hA"
$A settings set triage.validationBlueprint no-plan-here >/dev/null    # window B
hA2=$($A triage health 2>&1); grep -qF 'no-plan-here' <<<"$hA2"
$A settings set triage.validationBlueprint brainstorm >/dev/null
echo "[D8] first read carried brainstorm; the re-read carried no-plan-here; fixed"

# D9 — the vocabulary answers the plane: the closed sets, the legal edges,
# and the two authority statements, from ONE door the CLI and the UI quote.
vocab=$($A triage vocabulary 2>&1)
grep -qF 'unvalidated · reproduced · not-reproduced · could-not-tell' <<<"$vocab"
grep -qF 'known · assumed · unknown' <<<"$vocab"
grep -qF 'validate: This starts a validation attempt' <<<"$vocab"
grep -qF 'handoff:' <<<"$vocab"
echo "[D9] vocabulary: 4+3 closed sets, legal edges, both authority statements — one source"

echo "S51 WALK OK — D1..D9 + both gates asserted on a real daemon"
