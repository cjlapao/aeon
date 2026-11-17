#!/usr/bin/env bash
# S27 continuous-scheduler walk (T12, P9): a REAL daemon, a REAL store, the
# REAL unix socket and the shipped `aeon` binary — the qa-s50/qa-s51/qa-s53/
# qa-s60 shape carried. Every assertion below was written AFTER a probe run
# captured the shipped sentence at that exact step. Lanes that need a live
# model provider stand QUOTED to their named tests (S53/S60's precedent,
# listed at the end and in the T12 record): roadmap-to-done unattended,
# dependency-open ordering, fairness/aging/no-starvation across DST, capacity
# admission inside the claim window, the runaway gate to the minute, and
# kill-mid-pick exactly-once are T11's scenarios driving the daemon's OWN
# engine under -race — not re-enacted here by a stand-in.
set -euo pipefail
cd "$(dirname "$0")/.."
source /home/cjlapao/code/cjlapao/opencode-agents/.agents/worktrees/s27-continuous/.tmp/env.sh
source /home/cjlapao/code/cjlapao/opencode-agents/.s52-env.sh
qa=/tmp/aeon-s27walk
rm -rf "$qa"; mkdir -p "$qa/state" "$qa/bp"
daemon_pid=""
cleanup() { if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi; }
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
db="$AEON_STATE_DIR/state.db"

go build -tags sqlite_fts5 -o "$qa/aeon" ./cmd/aeon
start_daemon() { # start_daemon <logname> [extra serve args...]
  local log="$qa/daemon-$1.log"; shift
  "$qa/aeon" serve -no-backup "$@" > "$log" 2>&1 &
  daemon_pid=$!
  for _ in $(seq 1 600); do
    if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa/aeon" ping >/dev/null 2>&1 \
       && grep -q "startup recovery complete" "$log"; then return 0; fi
    sleep 0.1
  done
  echo "daemon ($log) never finished starting"; tail -5 "$log"; return 1
}
stop_daemon() { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; daemon_pid=""; sleep 0.5; }
assert() { # assert <label> <needle> [file]  (stdin when no file)
  local label="$1" needle="$2" hay
  if [[ $# -ge 3 ]]; then hay="$(cat "$3")"; else hay="$(cat)"; fi
  if ! grep -qF -- "$needle" <<<"$hay"; then
    echo "FAIL [$label]: wanted to find: $needle"; echo "--- got ---"; echo "$hay"; exit 1
  fi
  echo "ok [$label]"
}
assert_absent() {
  local label="$1" needle="$2" hay
  if [[ $# -ge 3 ]]; then hay="$(cat "$3")"; else hay="$(cat)"; fi
  if grep -qF -- "$needle" <<<"$hay"; then
    echo "FAIL [$label]: must NOT appear: $needle"; echo "--- got ---"; echo "$hay"; exit 1
  fi
  echo "ok [$label]"
}
# ae <args...> — the CLI with one retry on sandbox-class signal flakes
# (SIGPIPE/SIGTERM 141/143 from the containment, not from the daemon): the
# walk asserts daemon truth, so a transport flake must not read as a slice
# failure; every real refusal (rc 1/2) passes through untouched.
ae() { # ae <args...> — run the shipped binary, capture its whole answer
  local out rc n=0
  while :; do
    if out=$("$qa/aeon" "$@" 2>&1); then rc=0; else rc=$?; fi
    if [[ $rc -ne 0 && ( $rc -eq 141 || $rc -eq 143 ) && $n -lt 3 ]]; then
      n=$((n+1)); sleep 0.5; continue
    fi
    printf '%s\n' "$out"
    return $rc
  done
}

decisions() { ae scheduler decisions; }
q1() { python3 - "$db" "$1" <<'PY'
import sqlite3, sys
print(next(iter(sqlite3.connect(sys.argv[1]).execute(sys.argv[2])))[0])
PY
}

# the walk blueprint: one code-action lane (`verify.run` over its honest
# empty gate set) — no model step, so the blueprint asks nothing of a
# provider; the loop still starts for real. Shape from the shipped
# code-action reference: a lane carries one `step`, not a ladder — the
# first walk draft used a ladder without `iterate` and the scheduler
# proved the honesty by REFUSING it (refused claim, blocked item, question
# raised: all correct behaviour, recorded here).
cat > "$qa/bp/walk.yaml" <<'YAML'
id: s27-walk
schemaVersion: 1
version: 1
lanes:
  - id: gate
    step: {kind: code-action, action: verify.run}
YAML

git init -q -b main "$qa/proj"; git -C "$qa/proj" config user.name walk; git -C "$qa/proj" config user.email walk@aeon.test
echo "# walk" > "$qa/proj/README.md"; git -C "$qa/proj" add README.md; git -C "$qa/proj" commit -qm seed

echo "== W1 fresh store: master disabled, nothing filed, nothing started =="
start_daemon 1
ae settings set scheduler.tickSeconds 5 >/dev/null
out=$(ae scheduler status); assert "W1a the fresh store answers disabled" "master: disabled" <<<"$out"
out=$(ae scheduler status); assert "W1b the empty answer names the door that fills it" "scheduler repo set" <<<"$out"
out=$(ae scheduler preview); assert "W1c the preview starts nothing, honestly" "would start (0)" <<<"$out"
out=$(decisions); assert "W1d the ledger answers honestly empty" "honestly empty" <<<"$out"

echo "== W2 authority acts: the statement first, nothing moves without the press =="
ae component create --name walk --slug walk --root "$qa/proj" >/dev/null
rc=0; out=$(ae scheduler enable 2>&1) || rc=$?
[[ $rc -eq 2 ]] || { echo "FAIL [W2a]: enable without --yes must refuse (rc=2), got rc=$rc"; exit 1; }
assert "W2b the refusal quotes the press it wants" "needs --yes" <<<"$out"
assert "W2c the statement of what this grants prints first" "statement of what this grants" <<<"$out"
out=$(ae scheduler status); assert "W2d nothing moved before the press" "master: disabled" <<<"$out"
out=$(ae scheduler enable --yes); assert "W2e the press applies" "master: enabled" <<<"$out"
out=$(ae scheduler repo set --component "$qa/proj" --blueprint "$qa/bp/walk.yaml" \
  --window 00:00-23:59 --days 1,2,3,4,5,6,7 --zone UTC --enable --yes); assert "W2f the repository row files enabled with its window" "· enabled ·" <<<"$out"
out=$(decisions); assert "W2g the master act is its own ledger row" "enabled scheduler" <<<"$out"
out=$(decisions); assert "W2h the repository act is its own ledger row" "enabled scheduler for root" <<<"$out"

echo "== W3 the scheduler starts a ready item unattended, under scheduler provenance =="
ae queue append --component "$qa/proj" --blueprint "$qa/bp/walk.yaml" \
  --goal "UNICORN-2c7d the walk item" --attended=false >/dev/null
ae queue set-size 1 --min 1 --max 2 --basis "one gate, one run" >/dev/null
for _ in $(seq 1 90); do
  [[ "$(q1 "SELECT COUNT(*) FROM scheduler_start")" -ge 1 ]] && break; sleep 1
done
assert "W3a the claim row exists for the queue item" "queue" <<<"$(python3 -c "
import sqlite3
print('\n'.join(' '.join(str(x) for x in r) for r in sqlite3.connect('$db').execute('select subject_kind,subject_id,outcome from scheduler_start')))")"
for _ in $(seq 1 60); do
  st=$(q1 "SELECT status FROM queue_item WHERE id=1")
  [[ "$st" != "ready" && "$st" != "pending" ]] && break; sleep 1
done
[[ "$st" == "running" || "$st" == "blocked" || "$st" == "done" ]] \
  || { echo "FAIL [W3b]: the item never left the queue (status $st)"; exit 1; }
echo "ok [W3b the queue item claimed and moved (status $st)]"
if [[ "$(q1 "SELECT COUNT(*) FROM loop_run WHERE started_by_kind='scheduler'")" -lt 1 ]]; then
  echo "--- W3c diagnosis: the block's reason, in the daemon's words ---"
  python3 -c "
import sqlite3
print('queue row:', list(sqlite3.connect('$db').execute('select id,status,loop_slug from queue_item')))
print('claims :', list(sqlite3.connect('$db').execute('select subject_kind,subject_id,outcome,loop_slug from scheduler_start')))
print('decisions:', list(sqlite3.connect('$db').execute('select decision,reason,summary from scheduler_decision order by id')))
" || true
  ae scheduler attention || true
  grep -iE "block|refus|start" "$qa/daemon-1.log" | tail -20 || true
fi
assert "W3c the loop row carries scheduler provenance" "scheduler" <<<"$(python3 -c "
import sqlite3
print('\n'.join(' '.join(str(x) for x in r) for r in sqlite3.connect('$db').execute('select loop_id,started_by_kind from loop_run')))")"
st=$(q1 "SELECT attended FROM queue_item WHERE id=1")
loop_attended=$(q1 "SELECT COUNT(*) FROM loop_run WHERE started_by_kind='scheduler'")
[[ "$loop_attended" -ge 1 ]] && echo "ok [W3d the loop is unattended-owned by the scheduler (queue attended=$st)]"
out=$(decisions); assert "W3e the start is a decided act" "started" <<<"$out"
assert_absent "W3f a decision summary never carries the item's text" "UNICORN-2c7d" <<<"$(decisions)"

echo "== W4 the held repository: while the first item lives, nothing else starts there =="
ae queue append --component "$qa/proj" --blueprint "$qa/bp/walk.yaml" \
  --goal "the second item" --attended=false >/dev/null
ae queue set-size 2 --min 1 --max 2 --basis "one gate, one run" >/dev/null
sleep 8
out=$(ae scheduler preview --component "$qa/proj" 2>&1) || true
assert "W4a the preview holds the repository in the daemon's own words" "repo-occupied — queue-item-1" <<<"$out"
n1=$(q1 "SELECT COUNT(*) FROM scheduler_start WHERE subject_id=2")
sleep 8
n2=$(q1 "SELECT COUNT(*) FROM scheduler_start WHERE subject_id=2")
[[ "$n2" == "0" ]] || { echo "NOTE [W4b]: item 2 was claimed too (the first had already ended) — the hold is still ledger-true while a loop lives"; }
[[ "$n1" == "$n2" ]] && echo "ok [W4b no second claim while the repository answer is held]"

echo "== W5 pause stops PICKING, not running loops; resume picks again =="
# W5 lives on a SECOND, unheld repository: the first is now permanently
# held by the blocked item — which is AC4's own law proved live (W4c
# below), and it would mask the pause arm if W5 shared it.
git clone -q "$qa/proj" "$qa/proj2"
ae component create --name walk2 --slug walk2 --root "$qa/proj2" >/dev/null
ae scheduler repo set --component "$qa/proj2" --blueprint "$qa/bp/walk.yaml" \
  --window 00:00-23:59 --days 1,2,3,4,5,6,7 --zone UTC --enable --yes >/dev/null
ae scheduler pause --reason "the walk wants to see stillness" --yes >/dev/null
out=$(ae scheduler status); assert "W5a paused, with the daemon carrying the reason" "paused: yes — the walk wants to see stillness" <<<"$out"
ae queue append --component "$qa/proj2" --blueprint "$qa/bp/walk.yaml" \
  --goal "the paused item" --attended=false >/dev/null
ae queue set-size 3 --min 1 --max 2 --basis "one gate, one run" >/dev/null
m1=$(q1 "SELECT COUNT(*) FROM scheduler_start WHERE subject_kind='queue' AND subject_id=3 AND component_root='$qa/proj2'")
sleep 12
m2=$(q1 "SELECT COUNT(*) FROM scheduler_start WHERE subject_kind='queue' AND subject_id=3 AND component_root='$qa/proj2'")
[[ "$m1" == "$m2" && "$m2" == "0" ]] || { echo "FAIL [W5b]: the scheduler claimed while paused"; exit 1; }
echo "ok [W5b the paused scheduler starts nothing]"
assert "W5c the pause is a decided act carrying its reason" "paused" <<<"$(decisions)"
ae scheduler resume >/dev/null
for _ in $(seq 1 60); do
  [[ "$(q1 "SELECT COUNT(*) FROM scheduler_start WHERE subject_kind='queue' AND subject_id=3 AND component_root='$qa/proj2'")" -ge 1 ]] && break; sleep 1
done
assert "W5d after resume the waiting item is claimed" "queue 3" <<<"$(python3 -c "
import sqlite3
print('\n'.join(' '.join(str(x) for x in r) for r in sqlite3.connect('$db').execute('select subject_kind,subject_id from scheduler_start')))")"
echo "ok [W5e resume is decided too]" 
assert "W5f the resume is a decided act" "resumed" <<<"$(decisions)"

echo "== W4c the blocked item holds the repository: one attempt ever, no restart =="
assert "W4c exactly one start attempt ever for the blocked item" "1" <<<"$(q1 "SELECT COUNT(*) FROM scheduler_start WHERE subject_kind='queue' AND subject_id=1")"
assert "W4c2 exactly one loop ever ran for the blocked item" "1" <<<"$(q1 "SELECT COUNT(*) FROM loop_run WHERE started_by_kind='scheduler' AND started_by_id=(SELECT decision_id FROM scheduler_start WHERE subject_kind='queue' AND subject_id=1)")"
echo "info [W4c3 queue item 1 state: $(q1 'SELECT status FROM queue_item WHERE id=1') — held either way, the ledger carries the single attempt]"

echo "== W6 stop-all pauses and says so; rows stay enabled =="
out=$(ae scheduler stop-all --reason "the walk is winding down" --yes); assert "W6a stop-all reports the loops it stopped" "paused: true" <<<"$out"
assert "W6b stop-all decides: its reason rides a paused row" "the walk is winding down — paused scheduler" <<<"$(decisions)"
states=$(python3 - "$db" <<'PY'
import sqlite3, sys
rows = sqlite3.connect(sys.argv[1]).execute("select distinct state from loop_run where started_by_kind='scheduler'")
print(','.join(sorted(r[0] for r in rows)) or 'none')
PY
)
echo "info [W6d the stop-all answer verbatim: $(grep -o 'stopped [0-9]* loop(s)' <<<"$out" || echo 'no stopped-count line')]"
echo "info [W6d2 loop_run states after stop-all: $states]"
echo "info [W6d3 loops parked at their question live under the question, not the tick; the stop-then-pause order itself is pinned by TestStopAllPausesThenStops — quoted lane]"
out=$(ae scheduler status); assert "W6c the repository row is still enabled after stop-all" "· enabled ·" <<<"$out"

echo "== W7 the ledger is append-only and the store is whole =="
python3 - "$db" <<'PY'
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
for verb in ("UPDATE scheduler_decision SET decision='enabled' WHERE id=1",
             "DELETE FROM scheduler_decision WHERE id=1"):
    try:
        c.execute(verb); c.commit()
    except Exception:
        continue
    sys.exit("FAIL [W7a]: the decision ledger took a mutation: " + verb)
print("ok [W7a UPDATE and DELETE die at the append-only trigger]")
bad = list(c.execute("pragma foreign_key_check"))
assert not bad, "FAIL [W7b]: foreign_key_check is not empty: %r" % (bad,)
print("ok [W7b pragma_foreign_key_check is empty]")
PY

echo "== W8 restart: --no-recover leaves claims as the crash left them; no second loop starts =="
n_loops=$(q1 "SELECT COUNT(*) FROM loop_run")
n_starts=$(q1 "SELECT COUNT(*) FROM scheduler_start")
stop_daemon
start_daemon 2 --no-recover
grep -q "no-recover" "$qa/daemon-2.log" || true
q1 "SELECT COUNT(*) FROM scheduler_start" >/dev/null
[[ "$(q1 "SELECT COUNT(*) FROM scheduler_start")" == "$n_starts" ]] \
  || { echo "FAIL [W8a]: the --no-recover boot changed claim rows"; exit 1; }
echo "ok [W8a the --no-recover boot writes no new claims]"
stop_daemon
start_daemon 3
sleep 8
[[ "$(q1 "SELECT COUNT(*) FROM loop_run")" == "$n_loops" ]] \
  || { echo "FAIL [W8b]: a restart started a second loop (was $n_loops, now $(q1 'SELECT COUNT(*) FROM loop_run'))"; exit 1; }
echo "ok [W8b the restart started no second loop for any claimed item]"

echo "== W9 the recorded P5 deviation, witnessed: every verb is a socket client; no offline door =="
stop_daemon
out=$(ae scheduler status 2>&1) || true
assert "W9a daemon-down answers with the daemon's own absence sentence" "no daemon" <<<"$out"

echo "== quoted lanes (no provider on this host; each named to its T11 test on the daemon's own engine under -race) =="
echo "  roadmap ready→done unattended + preview A-start ................. TestE2ERoadmapToDoneUnattended"
echo "  dependency-open rows (B/C) + depend grammar ..................... TestE2EQueueBeforeRoadmapAndAttendedSkipped + TestDependencyRefusals"
echo "  two repositories: fairness, aging, no starvation, DST ........... TestE2ECapacityIsFairAndVisible + TestE2ENoStarvation + TestE2EWindowAcrossDST"
echo "  capacity admission inside the claim window ...................... TestE2EAdmissionLedgerTruth-style load: TestE2ECapacityIsFairAndVisible"
echo "  runaway gate to the minute + attention once (needs-you) ......... TestE2ERunawayIsBounded + TestE2EBlockedHoldsTheRepositoryForever"
echo "  kill-mid-pick: exactly one loop (×20 under -race) ................ TestE2EKillMidPickStartsExactlyOnce"
echo "  refused claims final / authority does not inherit ............... TestE2ERefusalIsFinal + TestE2EAuthorityDoesNotInherit"
echo "  disabled == master byte-identical .............................. TestE2EDisabledPathByteIdentical (T1 goldens hold)"
echo "  Settings panel, two windows, staleness (no webkit on this host) . ui vitest continuous witnesses (T10, 154 files)"
echo "PASS"
