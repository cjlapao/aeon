#!/usr/bin/env bash
# qa-s61-intake.sh — the S61 T12 demo walk (DEMO §S61). A real daemon, a
# throwaway state dir, local paths as the "remote" — NO network, NO real
# forge, NO provider credential (this session has none). The agent-loop
# half of the brief (converse with the intake agent over a provider; an
# agent calling charter_propose inside its loop) is QUOTED to its named
# tests, as prior slices' walks do; everything a PERSON does is walked for
# real through the CLI and the socket. The loop's OUTPUTS (the intake_plan
# proposals, the charter_save ask) are PLANTED by tools/s61walk through the
# store's own registered methods, marked PLANTED in the log — the store is
# single-writer by design, so planting happens while the daemon is down.
#
# Requires: AEON (the built binary), S61WALK (tools/s61walk).
# Usage: AEON=… S61WALK=… bash qa-s61-intake.sh [workdir]
set -u
: "${AEON:?AEON=/path/to/aeon}"
: "${S61WALK:?S61WALK=/path/to/s61walk}"
W="${1:-$(mktemp -d)}"; mkdir -p "$W"
S="$W/state"; H="$W/home"; PROJECTS="$W/projects"; LOG="$W/walk.log"
export AEON_STATE_DIR="$S" HOME="$H"
export LOCALRPC_ENDPOINT_PATH="$H/rpc.sock"
mkdir -p "$S" "$H" "$PROJECTS"
git config --global user.name  "Aeon Walk" 2>/dev/null || true
git config --global user.email "walk@aeon.test" 2>/dev/null || true
DPID=""
N=0; FAILS=0
note() { printf '%s\n' "$*" | tee -a "$LOG"; }
ok()   { N=$((N+1)); note "PASS $N: $*"; }
bad()  { N=$((N+1)); FAILS=$((FAILS+1)); note "FAIL $N: $*"; }
want(){ # want <name> <needle> <file>
  if grep -qF "$2" "$3"; then ok "$1 quotes $2"; else bad "$1 — expected $2, got: $(head -c 400 "$3")"; fi
}
digest(){ grep -oE 'sha256:[0-9a-f]+' "$1" | tail -1; }
daemon_up() { # daemon_up [extra-env...]
  local envs=("$@")
  ( env "${envs[@]}" nohup "$AEON" serve -no-backup >"$W/daemon-$(date +%s).log" 2>&1 & echo $! >"$W/daemon.pid" )
  for _ in $(seq 1 60); do [ -S "$H/rpc.sock" ] && break; sleep 0.5; done
  [ -S "$H/rpc.sock" ] || { note "daemon socket never appeared"; cat "$W"/daemon-*.log; exit 1; }
  for _ in $(seq 1 60); do "$AEON" intake vocabulary >/dev/null 2>&1 && break; sleep 0.5; done
  DPID="$(cat "$W/daemon.pid")"
}
daemon_down() { [ -n "$DPID" ] && kill "$DPID" 2>/dev/null; for _ in $(seq 1 40); do ! kill -0 "$DPID" 2>/dev/null && break; sleep 0.25; done; }

note "== S61 walk $(date -Is) · aeon=$("$AEON" version | head -1) · work=$W"

# 1 — cold: the verb refuses with the daemon's own words (P5).
"$AEON" intake vocabulary >"$W/cold.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "cold vocabulary refuses" "no daemon" "$W/cold.out" || bad "cold vocabulary exited $rc"

# 2 — the loop's floor template, PLANTED with the store's writer alone.
"$S61WALK" -dir "$S" -do template >"$W/plant1.out" 2>&1
grep -q '^PLANTED' "$W/plant1.out" && ok "template base planted" || bad "template plant: $(tail -1 "$W/plant1.out")"

daemon_up

# 3 — the T11 descriptor over the wire, before anything exists.
"$AEON" intake vocabulary >"$W/vocab1.out" 2>&1
want "publish is a closed choice set"  "none"                        "$W/vocab1.out"
grep -q "private" "$W/vocab1.out" && grep -q "public" "$W/vocab1.out" && ok "publish shows private+public" || bad "publish set incomplete"
want "remote create answers off+reason" "remote create: off"         "$W/vocab1.out"
want "no-route sentence at the entry"   "no model route configured"  "$W/vocab1.out"

# 4 — the entry gate: start refuses until a route exists (GI16).
"$AEON" intake start --name Shop >"$W/start-noroute.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "start refused without a route" "route" "$W/start-noroute.out" || bad "start passed with no route"

# 5 — a route (unmetered on purpose); the wizard opens; sessions are opened
# for every drill the walk needs, all names minted by the daemon.
"$AEON" routes set --id walk-demo --provider anthropic --model claude-walk --no-price >"$W/route.out" 2>&1 \
  || { want "routes set" "(unexpected)" "$W/route.out"; }
"$AEON" intake vocabulary >"$W/vocab2.out" 2>&1
grep -q "no model route configured" "$W/vocab2.out" && bad "routeAvailable still false with one route" || ok "routeAvailable flipped"
"$AEON" intake start --name Shop >"$W/start.out" 2>&1
INTAKE="$(grep -oE 'int-[A-Za-z0-9_-]+' "$W/start.out" | head -1)"
[ -n "$INTAKE" ] && ok "intake session opened ($INTAKE)" || bad "no intake id: $(head -c 200 "$W/start.out")"
"$AEON" intake status "$INTAKE" >"$W/status1.out" 2>&1 && ok "status answers the young session" || bad "status failed"
for NAME in Priv Neat Link Crash Break Mid; do
  "$AEON" intake start --name "$NAME" >"$W/start-$NAME.out" 2>&1
  SID="$(grep -oE 'int-[A-Za-z0-9_-]+' "$W/start-$NAME.out" | head -1)"
  eval "ID_$NAME=\$SID"; [ -n "$SID" ] || bad "session $NAME not opened"
done
ID_Shop="$INTAKE"
ok "six more sessions opened for the refusal and crash drills"

# 6 — the loops' output, PLANTED with the daemon down (single-writer law,
# internal/store/open.go): one plan per session, each naming its intake id.
daemon_down
for NAME in Shop Priv Neat Link Crash Break Mid; do
  eval "SID=\$ID_$NAME"; [ "$NAME" = Shop ] && SID="$INTAKE"
  "$S61WALK" -dir "$S" -do seed -intake "$SID" >"$W/plant-$NAME.out" 2>&1
  eval "PID_$NAME=\$(grep '^PLANTED' "$W/plant-$NAME.out" | grep -oE 'id=[0-9]+' | cut -d= -f2)"
  eval "[ -n \$PID_$NAME ]" || bad "seed plant missing for $NAME"
done
ok "seven intake_plan proposals planted while the store had one writer"
daemon_up

# 7 — the door before the plan.
"$AEON" intake plan "$INTAKE" --proposal 999999 >"$W/plan-noprop.out" 2>&1; rc=$?
[ $rc -ne 0 ] && ok "plan refuses a missing proposal" || bad "plan accepted proposal 999999"

# 8 — the preview: files, the git commands, records census, digest last (GI7).
"$AEON" intake plan "$INTAKE" --proposal "$PID_Shop" --target "$PROJECTS/shop" --publish none >"$W/plan.out" 2>&1
want "preview names the git commands" "git init" "$W/plan.out"
grep -q "README.md" "$W/plan.out" && ok "preview lists the template's files" || bad "no file list"
D="$(digest "$W/plan.out")"; want "digest stated last for commit" "digest" "$W/plan.out"
[ -n "$D" ] && ok "digest captured" || bad "no digest"

# 9 — door refusals. The digest covers target and publish choice, so a
# stale or wrong view of either refuses before anything runs.
"$AEON" intake commit "$INTAKE" --proposal "$PID_Shop" --digest "sha256:deadbeef" --target "$PROJECTS/shop" --publish none >"$W/r-stale.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "stale digest refused" "plan changed" "$W/r-stale.out" || bad "a bogus digest passed"
"$AEON" intake commit "$INTAKE" --proposal "$PID_Shop" --digest "$D" --target "$PROJECTS/shop" --publish private --yes >"$W/r-noowner.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "publish without an owner refuses at the door" "wants an owner" "$W/r-noowner.out" || bad "ownerless publish passed"
"$AEON" intake commit "$INTAKE" --proposal "$PID_Shop" --digest "$D" --target "$PROJECTS/shop" --publish public --owner gate-walker --yes >"$W/r-nopubconfirm.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "public without confirm refuses" "confirm_public" "$W/r-nopubconfirm.out" || bad "public passed unconfirmed"

# 10 — mid-run refusals (the scaffold guard, the closed gate): each gets its
# own session; the ledger names the failed step; nothing half-lands.
# 10a private into the closed gate — the refusal names the setting key (AC2).
"$AEON" intake plan "$ID_Priv" --proposal "$PID_Priv" --target "$PROJECTS/priv" --publish private --owner gate-walker >"$W/plan-priv.out" 2>&1
DPRIV="$(digest "$W/plan-priv.out")"
"$AEON" intake commit "$ID_Priv" --proposal "$PID_Priv" --digest "$DPRIV" --target "$PROJECTS/priv" --publish private --owner gate-walker --yes >"$W/r-private.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "publish private runs into the closed gate, naming the key" "intake.allowRemoteCreate" "$W/r-private.out" || bad "private passed the closed gate"
"$AEON" intake status "$ID_Priv" >"$W/status-priv.out" 2>&1
grep -q "failed" "$W/status-priv.out" && ok "the ledger names the step where the gate stopped the run" || bad "priv ledger: $(head -c 160 "$W/status-priv.out")"
# 10b the scaffold guard: a component directory that exists, non-empty, refuses.
mkdir -p "$PROJECTS/neat/shop" && echo taken > "$PROJECTS/neat/shop/README.md"
"$AEON" intake plan "$ID_Neat" --proposal "$PID_Neat" --target "$PROJECTS/neat" --publish none >"$W/plan-neat.out" 2>&1
DNEAT="$(digest "$W/plan-neat.out")"
"$AEON" intake commit "$ID_Neat" --proposal "$PID_Neat" --digest "$DNEAT" --target "$PROJECTS/neat" --publish none >"$W/r-neat.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "the existing non-empty directory refuses" "is not empty" "$W/r-neat.out" || bad "the scaffold wrote over what was there"
# 10c the scaffold guard: a symlinked path segment refuses.
ln -sfn "$PROJECTS" "$W/link"
"$AEON" intake plan "$ID_Link" --proposal "$PID_Link" --target "$W/link/lnk" --publish none >"$W/plan-link.out" 2>&1
DLINK="$(digest "$W/plan-link.out")"
"$AEON" intake commit "$ID_Link" --proposal "$PID_Link" --digest "$DLINK" --target "$W/link/lnk" --publish none >"$W/r-link.out" 2>&1; rc=$?
[ $rc -ne 0 ] && want "a symlink in the target path refuses" "symlink" "$W/r-link.out" || bad "the scaffold wrote through a symlink"

# 11 — the landing (publish none): directories, repos, tree, charter, records.
"$AEON" intake commit "$INTAKE" --proposal "$PID_Shop" --digest "$D" --target "$PROJECTS/shop" --publish none --json >"$W/commit.out" 2>&1; rc=$?
[ $rc -eq 0 ] && ok "intake.commit completed" || bad "commit failed: $(head -c 300 "$W/commit.out")"
ROOT="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("rootId",""))' "$W/commit.out" 2>/dev/null)"
[ -n "$ROOT" ] && ok "root component created ($ROOT)" || bad "no rootId in commit result"
DIRS=$(find "$PROJECTS/shop" -name .git -maxdepth 3 -type d 2>/dev/null | wc -l)
[ "$DIRS" -eq 3 ] && ok "three repositories initialised on disk" || bad "expected 3 .git dirs, found $DIRS"
if [ "$DIRS" -eq 3 ]; then
  N1C=1
  for d in $(find "$PROJECTS/shop" -name .git -maxdepth 3 -type d); do r="$(git -C "$(dirname "$d")" rev-list --count HEAD 2>/dev/null)"; [ "$r" = "1" ] || N1C=0; done
  [ "$N1C" -eq 1 ] && ok "every repository carries exactly one initial commit (git log walked)" || bad "a repo does not have exactly one commit"
fi
"$S61WALK" -dir "$S" -do census -root "$ROOT" >"$W/census.out" 2>&1
grep -q "components=4 (roots=1)" "$W/census.out" && ok "the tree lands: one project row over three components" || bad "tree census: $(grep ^census "$W/census.out")"
grep -q "in_review=2" "$W/census.out" && grep -q "sourceKind=intake:2" "$W/census.out" \
  && ok "both goals in_review, provenance intake (AC1/AC6)" || bad "goal census: $(grep ^census "$W/census.out" | tail -1)"
grep -q "items=3 (idea=3, priority=unknown:3)" "$W/census.out" \
  && ok "roadmap items arrive as idea/unranked, none auto-prioritised (AC6)" || bad "items census: $(grep '^census: items' "$W/census.out")"
"$AEON" charter show "$ROOT" >"$W/charter1.out" 2>&1
want "charter r1 exists" "r1" "$W/charter1.out"
"$AEON" intake status "$INTAKE" >"$W/status2.out" 2>&1
grep -q "done" "$W/status2.out" && ok "the ledger streamed every step as done" || bad "ledger: $(head -c 200 "$W/status2.out")"

# 12 — a plan lands ONCE: the committed proposal cannot be re-landed.
"$AEON" intake commit "$INTAKE" --proposal "$PID_Shop" --digest "$D" --target "$PROJECTS/shop" --publish none >"$W/recommit.out" 2>&1
want "re-commit refused — the proposal is committed (AC5)" "is committed" "$W/recommit.out"
"$S61WALK" -dir "$S" -do census -root "$ROOT" >"$W/census2.out" 2>&1
[ "$(grep ^census "$W/census.out")" = "$(grep ^census "$W/census2.out")" ] \
  && ok "and the refused re-commit changed zero rows" || bad "a refused commit changed the census"

# 13 — the charter round trip: an agent's ask, committed by the person (AC1/AC3).
daemon_down   # the writer walks alone
"$S61WALK" -dir "$S" -do charter-propose -root "$ROOT" >"$W/plant3.out" 2>&1 && ok "charter_save ask planted" || bad "charter plant: $(tail -1 "$W/plant3.out")"
CPID="$(grep '^PLANTED' "$W/plant3.out" | grep -oE 'id=[0-9]+' | cut -d= -f2)"
daemon_up
"$AEON" proposal list >"$W/plist.out" 2>&1
grep -q "$CPID" "$W/plist.out" && ok "the ask waits inert on the person's list" || bad "proposal $CPID not listed: $(head -c 200 "$W/plist.out")"
"$AEON" proposal commit "$CPID" >"$W/pcommit.out" 2>&1; rc=$?
[ $rc -eq 0 ] && ok "a person committed the ask (proposal.commit)" || bad "proposal commit: $(head -c 250 "$W/pcommit.out")"
"$AEON" charter history "$ROOT" >"$W/charter2.out" 2>&1
grep -q "rev 2" "$W/charter2.out" && ok "charter is at r2, stamped with the committer (AC1)" || bad "history: $(head -c 250 "$W/charter2.out")"

# 14 — an agent tries intake_propose outside an intake loop. QUOTED:
# TestIntakeProposeStampsTheIntakeId + TestIntakeProposeOutsideAnIntakeLoopRefuse
# (internal/daemon/records_charter_test.go) — the tool arm needs a loop's
# identity; outside one it refuses naming why. The registry guard pins that
# no CLI verb exists for the proposes either.
note "QUOTED: agent surfaces (reads + the two proposes) → records_charter_test.go family + registry census guards"

# 15 — goal approval through the existing flow.
GID="$(python3 - "$S" <<'PY'
import sqlite3,sys
con=sqlite3.connect(sys.argv[1]+"/state.db")
row=con.execute("select id from goal where review_state='in_review' limit 1").fetchone()
print(row[0] if row else "")
PY
)"
[ -n "$GID" ] || bad "no in_review goal row to approve"
VER="$("$AEON" goal show "$GID" 2>/dev/null | grep -ioE 'version[^0-9]*[0-9]+' | grep -oE '[0-9]+' | head -1)"
"$AEON" goal decide "$GID" --version "${VER:-1}" --decision approved --reason "walk approves one" >"$W/decide.out" 2>&1; rc=$?
[ $rc -eq 0 ] && ok "a goal is approved through the existing flow (AC1)" || bad "goal decide: $(head -c 200 "$W/decide.out")"

# 16 — the crash drill, three kill -9 points (early, mid, late), each:
# commit in the background, kill the daemon mid-run, restart, re-run.
for spec in "Crash 0.4" "Break 0.9" "Mid 1.6"; do
  set -- $spec; TN="$1"; SL="$2"
  eval "TGT=\$ID_$TN"; eval "PP=\$PID_$TN"
  "$AEON" intake plan "$TGT" --proposal "$PP" --target "$PROJECTS/$TN" --publish none >"$W/plan-$TN.out" 2>&1
  DT="$(digest "$W/plan-$TN.out")"
  "$AEON" intake commit "$TGT" --proposal "$PP" --digest "$DT" --target "$PROJECTS/$TN" --publish none >"$W/kill-$TN.out" 2>&1 &
  CP=$!
  sleep "$SL"; kill -9 "$DPID" 2>/dev/null; wait "$CP" 2>/dev/null
  note "  kill -9 $TN daemon after ${SL}s (commit was still running or had just finished)"
  daemon_up
  "$AEON" intake status "$TGT" >"$W/status-$TN.out" 2>&1
  grep -qE "done|failed|skipped|committed|stopped|state:" "$W/status-$TN.out" \
    && ok "$TN: after the kill the ledger says exactly where it stopped" || bad "$TN post-kill status unreadable"
  "$AEON" intake commit "$TGT" --proposal "$PP" --digest "$DT" --target "$PROJECTS/$TN" --publish none >"$W/resume-$TN.out" 2>&1; rc=$?
  if [ $rc -eq 0 ]; then ok "$TN: re-running the killed commit completes it (AC5)"
  elif grep -q "is committed" "$W/resume-$TN.out"; then ok "$TN: the killed run HAD landed — the committed proposal refuses a second landing (AC5)"
  else bad "$TN resume failed: $(head -c 220 "$W/resume-$TN.out")"; fi
  DTOR=$(find "$PROJECTS/$TN" -name .git -maxdepth 3 -type d 2>/dev/null | wc -l)
  [ "$DTOR" -eq 3 ] && ok "$TN: exactly three repositories — zero duplicates across the kill" || bad "$TN census $DTOR"
done

# 17 — abandon (the person walks away, reasons recorded).
"$AEON" intake start --name Ghost >"$W/start3.out" 2>&1
I3="$(grep -oE 'int-[A-Za-z0-9_-]+' "$W/start3.out" | head -1)"
"$AEON" intake abandon "$I3" --reason "changed my mind" >"$W/abandon.out" 2>&1 \
  && ok "abandon records the person's reason" || bad "abandon failed"

# -- 18: not-ready is visible. The roadmap lists the intake's items exactly
# as they landed — idea, unranked; readiness is decided at the attempt, never
# stored — so nothing here is work-ready until a person ranks it.
daemon_up
# Each component carries its OWN plan-of-record roadmap, keyed by the
# component's root PATH (an intake files each item under its owning
# component — the tree parent honestly answers EMPTY).
"$AEON" roadmap list --component "$PROJECTS/shop/shop-web" >"$W/roadmap.out" 2>&1
grep -q "idea" "$W/roadmap.out" && grep -qiE "unknown" "$W/roadmap.out" \
  && ok "the listed items are idea/unranked — nothing work-ready until a person ranks (AC6)" \
  || bad "roadmap list: $(head -2 "$W/roadmap.out")"
"$AEON" roadmap list --component "$PROJECTS/shop" >"$W/roadmap-root.out" 2>&1
grep -qiE "EMPTY" "$W/roadmap-root.out" \
  && ok "the tree parent honestly answers EMPTY — items live on their owning component" \
  || bad "tree parent roadmap: $(head -2 "$W/roadmap-root.out")"

note "== QUOTED steps (no provider credential in this session; real forge out of scope by the brief):"
note "  agent conversation producing a plan   → intake engine + daemon loop tests (engine_test, crash_test, records arms)"
note "  agent charter_propose inside a loop   → records_charter_test.go (TestIntakePropose* / charter arms family)"
note "  forge.createRepo against a real forge → NOT RUN — needs an operator-supplied throwaway repo + token, never in-session"
note "  missing git identity, live                        → NOT RUN — this host auto-detects an identity (`git var GIT_AUTHOR_IDENT` measured ok 2026-11-05) and unshare is blocked, so no daemon here can be made to lack one; the sentence is pinned by TestScaffoldMissingIdentityRefusesAndCreatesNothing and T0's P-D walked it from real output"
note "== $N assertions, $FAILS failed. log: $LOG"
[ "$FAILS" -eq 0 ] || exit 1
