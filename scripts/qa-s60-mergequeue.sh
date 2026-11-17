#!/usr/bin/env bash
# S60 merge-queue walk (T12, P9): real daemon, real store, real socket, real
# git, real CLI — the qa-s50/qa-s51/qa-s53 shape carried. Everything the CLI
# lane can honestly prove runs for real and is asserted verbatim. The lanes
# that need the loop engine's runner or the forge's fake stand QUOTED to
# their named suite tests, never re-enacted by a stand-in (S53's precedent):
# the landing loop's lane-by-lane drive and land's APPLY side are
# TestHappyPathEndsOnBaseAndCleansWorktree (daemon, real git + forge.Fake);
# the kill-and-reopen rungs beside a live merge are TestRecoveryAtEveryState;
# the AGENT door's inert proposal is TestProposeEnqueueIsInert; the conflict
# ask/resolve answers are mergequeue_conflict_test.go; the behind-catch-up
# bump is TestBaseMovesTriggersCatchUpBounded. OBSERVED and recorded here as
# the shipped fail-closed default: on this serve the queue's own reactor cuts
# the integrate ref and runs the decision table, while the landing loop
# (blueprint land-feature, code-action lanes) parks at preflight awaiting the
# loop-runner host — so an item HOLDS at verifying ("no gate evidence yet")
# rather than landing on unevidenced green. That hold is D3's assertion.
set -euo pipefail
cd "$(dirname "$0")/.."
source /home/cjlapao/code/cjlapao/opencode-agents/.s52-env.sh
qa=/tmp/opencode/aeon-tmp/s60walk
rm -rf "$qa"; mkdir -p "$qa/state"
daemon_pid=""
cleanup() { if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi; }
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
db="$AEON_STATE_DIR/state.db"

go build -tags sqlite_fts5 -o "$qa/aeon" ./cmd/aeon
start_daemon() {
  "$qa/aeon" serve -no-backup > "$qa/daemon-$1.log" 2>&1 &
  daemon_pid=$!
  # ping alone is NOT ready: localrpc binds in Init, before seed and startup
  # recovery run (by design — the inspectable window). A walk that drives
  # state transitions must wait for "startup recovery complete" too, or it
  # races recovery reaping a loop the reactor started mid-Init (measured
  # 2026-11-04 under concurrent test load: the race is real; the walk is not
  # the place to drive items before the daemon finished opening).
  for _ in $(seq 1 600); do
    if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa/aeon" ping >/dev/null 2>&1 \
       && grep -q "startup recovery complete" "$qa/daemon-$1.log"; then return 0; fi
    sleep 0.1
  done
  echo "daemon $1 never finished starting"; tail -5 "$qa/daemon-$1.log"; return 1
}
stop_daemon() { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; daemon_pid=""; sleep 0.5; }
assert() { # assert <label> <needle> [file]
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
    echo "FAIL [$label]: must NOT appear: $needle"; exit 1
  fi
  echo "ok [$label]"
}
item_id() { # item_id <branch> — the OPEN item's id (status-filtered by caller)
  ( cd "$qa/proj" && "$qa/aeon" mergequeue list --json ) \
    | python3 -c "import json,sys; rows=json.load(sys.stdin); print(next(r['id'] for r in rows if r['branch']=='$1' and r['status'] not in ('removed','merged')))"
}

# ---- the repository pair (a bare origin and the person's clone) ----
git init --bare -q -b main "$qa/origin.git"
git clone -q "$qa/origin.git" "$qa/proj"
cd "$qa/proj"
git config user.name walk; git config user.email walk@aeon.test
echo "# base" > README.md && git add README.md && git commit -qm "seed" && git push -q -u origin main
git remote set-head origin main
echo ".worktrees/" >> .git/info/exclude   # the queue's integration worktrees are the daemon's, not the person's tree
start_daemon 1
"$qa/aeon" ping >/dev/null
"$qa/aeon" component create --name mq --slug mq --root "$qa/proj" >/dev/null

echo "== D0 vocabulary: the closed sets answer live =="
"$qa/aeon" mergequeue vocabulary | assert "vocabulary names the states" "catching_up"

echo "== D1 the settings seatbelt (P4): decline first, then press =="
rc=0; out=$("$qa/aeon" mergequeue settings --mode auto 2>&1) || rc=$?
[[ $rc -eq 2 ]] || { echo "FAIL: widening without --yes rc=$rc, want 2"; exit 1; }
assert "the refusal prints the daemon's own widening sentence" "AUTHORITY WIDENING" <<<"$out"
assert "the refusal sends the digest to resend" 'acknowledged "' <<<"$out"
assert "and says so: nothing changed" "Nothing changed" <<<"$out"
assert "the decline wrote nothing (mode still off)" "mode off" <("$qa/aeon" mergequeue status)
"$qa/aeon" mergequeue settings --mode auto --yes | assert "the press applies and stamps the actor" "by user/1000"
assert "status now reads auto" "mode auto" <("$qa/aeon" mergequeue status)

echo "== D2 positions, the slot, and the held row that names its holder =="
rc=0; out=$("$qa/aeon" mergequeue enqueue --branch feature/nowhere 2>&1) || rc=$?
[[ $rc -ne 0 ]] || { echo "FAIL: enqueue of an absent branch accepted"; exit 1; }
echo "ok [the absent-branch enqueue refused with rc=$rc: $(head -1 <<<"$out")]"
git checkout -q -b feature/one main && echo "one" > one.txt && git add one.txt && git commit -qm "feat one" && git push -q origin feature/one
git checkout -q -b feature/two main && echo "two" > two.txt && git add two.txt && git commit -qm "feat two" && git push -q origin feature/two
git checkout -q main
"$qa/aeon" mergequeue enqueue --branch feature/one | assert "row one enqueues attended, with a position" "waits at position"
"$qa/aeon" mergequeue enqueue --branch feature/two | assert "row two enqueues behind it" "waits at position"
for _ in $(seq 1 60); do "$qa/aeon" mergequeue list --json | grep -qE "catching_up|verifying|ready" && break; sleep 1; done
"$qa/aeon" mergequeue list --json | python3 -c "
import json,sys
rows=json.load(sys.stdin)
head=[r for r in rows if r['branch']=='feature/one'][0]
tail=[r for r in rows if r['branch']=='feature/two'][0]
assert head['status'] in ('catching_up','verifying','ready'), head['status']
assert tail['status']=='queued', tail['status']
print('ok [one slot admits one: head %s, second row held queued]' % head['status'])"
for _ in $(seq 1 10); do grep -q "holds the landing slot on main" "$qa/daemon-1.log" && break; sleep 1; done
assert "the held row names the holder (the daemon's sentence)" "holds the landing slot on main" "$qa/daemon-1.log"

echo "== D2b reorder: a QUEUED row moves by --before; a slot-holder cannot =="
git checkout -q -b feature/three main && echo "three" > three.txt && git add three.txt && git commit -qm "feat three" && git push -q origin feature/three
git checkout -q main
"$qa/aeon" mergequeue enqueue --branch feature/three | assert "row three enqueues last" "waits at position"
ID2b=$(item_id feature/two); ID3=$(item_id feature/three)
"$qa/aeon" mergequeue reorder "$ID3" --before "$ID2b" >/dev/null
"$qa/aeon" mergequeue list --json | python3 -c "
import json,sys
rows={r['branch']:r for r in json.load(sys.stdin)}
three, two = rows['feature/three'], rows['feature/two']
assert three['position'] < two['position'], (three['position'], two['position'])
assert three['status']=='queued' and two['status']=='queued'
print('ok [reorder swapped the queued rows: three %d slid two to %d]' % (three['position'], two['position']))"
ID1r=$(item_id feature/one)
rc=0; out=$("$qa/aeon" mergequeue reorder "$ID1r" --before "$ID3" 2>&1) || rc=$?
[[ $rc -ne 0 ]] || { echo "FAIL: reorder of a non-queued row accepted"; exit 1; }
echo "ok [reorder of the slot-holding row refused rc=$rc: $(head -1 <<<"$out")]"

echo "== D3 verification is bound, not vacuous: no gate evidence ⇒ the queue HOLDS =="
head_before=$(git rev-parse main); tree_before=$(git rev-parse 'main^{tree}')
for _ in $(seq 1 30); do grep -q "no gate evidence yet" "$qa/daemon-1.log" && break; sleep 1; done
assert "no green without gate evidence (fail-closed)" "no gate evidence yet" "$qa/daemon-1.log"
"$qa/aeon" mergequeue list --json | python3 -c "
import json,sys
rows=json.load(sys.stdin)
head=[r for r in rows if r['branch']=='feature/one'][0]
assert head['status'] in ('catching_up','verifying'), head['status']
print('ok [the item waits in %s rather than landing on unevidenced green]' % head['status'])"
assert "catch-up cut an integrate ref, never main" "refs/heads/integrate/mq-" <(git -C "$qa/proj" for-each-ref refs/heads/integrate/)

echo "== D4 the human door answers land only bound to the row shown =="
ID1=$(item_id feature/one)
rc=0; out=$("$qa/aeon" mergequeue land "$ID1" 2>&1) || rc=$?
[[ $rc -eq 2 ]] || { echo "FAIL: land without --yes rc=$rc, want 2"; exit 1; }
assert "land without --yes echoes the row it binds to" "status verifying" <<<"$out"
assert "and says nothing was landed" "nothing was landed" <<<"$out"

echo "== D5 gate authoring refuses outside the closed set =="
CID=$("$qa/aeon" component list --json | python3 -c "import json,sys; print(json.load(sys.stdin)['components'][0]['id'])")
rc=0; out=$("$qa/aeon" verify add --component "$CID" --name always-green --command "true" --requirement required --yes 2>&1) || rc=$?
assert "the closed timeout range refuses with its bounds" "closed range is 1 to 600" <<<"$out"
"$qa/aeon" verify add --component "$CID" --name always-green --command "true" --requirement required --timeout 30 --yes >/dev/null

echo "== D6 dequeue is the person's door: the row goes terminal, kept =="
"$qa/aeon" mergequeue dequeue "$ID1" | assert "the row goes removed" "status removed"
assert "the removed row is terminal and kept for the ledger" "removed-by-person" <("$qa/aeon" mergequeue list --all)
assert "row three now leads the queue (the reorder held)" "feature/three" <("$qa/aeon" mergequeue list)
ID2=$(item_id feature/two); ID3=$(item_id feature/three)
"$qa/aeon" mergequeue dequeue "$ID3" >/dev/null   # clear the lane for the conflict probe
"$qa/aeon" mergequeue dequeue "$ID2" >/dev/null
assert_absent "the open list is empty — removed rows keep only the ledger" "feature/" <("$qa/aeon" mergequeue list)

echo "== D7 the base moves; the planted conflict lands the row in conflicted, PATHS only =="
git clone -q "$qa/origin.git" "$qa/other"
git -C "$qa/other" config user.name other; git -C "$qa/other" config user.email other@aeon.test
printf '# base\n\nmoved\n' > "$qa/other/README.md"
git -C "$qa/other" add README.md && git -C "$qa/other" commit -qm "base moves" && git -C "$qa/other" push -q origin main
SEED=$(git rev-list --max-parents=0 main)
git checkout -q -b feature/conflict "$SEED"
printf '# base\n\nCONFLICTED with AKIAIOSFODNN7EXAMPLESENTINEL\n' > README.md
git add README.md && git commit -qm "feat conflict" && git push -q origin feature/conflict
git checkout -q main
"$qa/aeon" mergequeue enqueue --branch feature/conflict | assert "the conflicting row enqueues" "waits at position"
for _ in $(seq 1 45); do "$qa/aeon" mergequeue list --json | grep -q "conflicted" && break; sleep 2; done
IDC=$(item_id feature/conflict)
"$qa/aeon" mergequeue show "$IDC" | assert "the row is conflicted" "status conflicted"
"$qa/aeon" mergequeue show "$IDC" | assert "the ledger carries the paths and only the paths" '"conflict_paths":["README.md"]'

echo "== D8 SEC-033: the sentinel rides no row, no event, no log =="
assert_absent "the secret is not in the store" "AKIAIOSFODNN7EXAMPLESENTINEL" <(python3 -c "
import sqlite3
c=sqlite3.connect('$db')
for (t,) in c.execute(\"select name from sqlite_master where type='table'\"):
    for row in c.execute(f'select * from \"{t}\"'):
        print(row)")
assert_absent "the secret is not in the daemon's log" "AKIAIOSFODNN7EXAMPLESENTINEL" "$qa/daemon-1.log"
assert_absent "the secret is not in the item's show (paths and counts only)" "AKIAIOSFODNN7EXAMPLESENTINEL" <("$qa/aeon" mergequeue show "$IDC")

echo "== D9 the person's checkout never moved through any of it =="
head_after=$(git rev-parse main); tree_after=$(git rev-parse 'main^{tree}')
[[ "$head_before" == "$head_after" && "$tree_before" == "$tree_after" ]] \
  && echo "ok [main HEAD $head_after and tree unchanged through catch-up, conflict and dequeues]" \
  || { echo "FAIL: the person's checkout moved"; exit 1; }

echo "== D10 the offline door refuses with the daemon's words (EV18) =="
stop_daemon
rc=0; out=$("$qa/aeon" mergequeue list 2>&1) || rc=$?
[[ $rc -ne 0 ]] || { echo "FAIL: offline list succeeded"; exit 1; }
assert "the offline refusal names the daemon door" "no daemon at" <<<"$out"
assert "and names the way to fix it" "aeon serve" <<<"$out"

echo "== D11 restart: reconcile converges — no double work, no lost row =="
start_daemon 2
sleep 6
"$qa/aeon" mergequeue list --all --json | python3 -c "
import json,sys
rows=json.load(sys.stdin)
op=[r for r in rows if r['branch']=='feature/conflict' and r['status'] not in ('removed','merged')]
assert len(op)==1 and op[0]['status']=='conflicted', rows
print('ok [restart-reconcile kept the conflicted row conflicted — the ledger, not memory, owns the queue]')"
assert "the removed rows are terminal, kept" "removed-by-person" <("$qa/aeon" mergequeue list --all)

echo
echo "PASS — S60 merge-queue CLI-lane walk"
echo "QUOTED (not re-enacted, per S53's rule): land's APPLY side and the landing"
echo "loop's lane-by-lane drive = TestHappyPathEndsOnBaseAndCleansWorktree; the"
echo "kill-and-reopen rungs beside a live merge = TestRecoveryAtEveryState; the"
echo "AGENT door's inert propose = TestProposeEnqueueIsInert; conflict ask/resolve"
echo "answers = TestConflictAttendedAsksThenResolves; the behind-catch-up bump ="
echo "TestBaseMovesTriggersCatchUpBounded. On this serve the landing loop parked"
echo "at preflight (the loop-runner host belongs to the blueprint slices) and the"
echo "queue held the item fail-closed at verifying — D3 records that as shipped."
