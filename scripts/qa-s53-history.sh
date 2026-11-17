#!/usr/bin/env bash
# S53 history-views walk (T5, P9): real daemon, real store, real socket, real
# CLI — the qa-s50/qa-s51 shape carried. Everything the CLI lane can honestly
# prove runs for real and is asserted verbatim. The AGENT lane (D6/D7) is the
# loop's socket, not this walk's peer kind — QUOTED to its named suite tests,
# never re-enacted by a stand-in (S51's precedent). The UI half is the vitest
# gates, not a live click-through (S25's precedent, honored and named).
#
# Covers: D1 counts-equal-reality, D2 planted rot ⇒ cannot-say names the id
# while the view keeps rendering the rest, D3 vocabulary + lane switch, D4 the
# drill-down header-first, D5 export ≡ list (H3, the daemon's payload written
# client-side, never read back), D8 two windows byte-identical, plus the P5
# offline door and the restored `case "history":` dispatch that `b5620631`
# had deleted (T5's second restoration finding — the binary could not reach
# its own history verbs).
set -euo pipefail
cd "$(dirname "$0")/.."
source /home/cjlapao/code/cjlapao/opencode-agents/.s52-env.sh
qa=/tmp/opencode/aeon-tmp/s53walk
rm -rf "$qa"; mkdir -p "$qa/state" "$qa/proj"
daemon_pid=""
cleanup() { if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi; }
trap cleanup EXIT
export AEON_STATE_DIR="$qa/state"
export LOCALRPC_ENDPOINT_PATH="$qa/rpc.sock"
db="$AEON_STATE_DIR/state.db"

go build -tags sqlite_fts5 -o "$qa/aeon" ./cmd/aeon
# The dispatch restoration guard first: the BINARY answers history, not just
# the tests (this is the check b5620631's deletion would have failed).
help_text=$("$qa/aeon" --help 2>&1 || true)
grep -qF "history           list, show, export, coverage, vocabulary" <<<"$help_text" || { echo "FAIL: main.go usage lost the history line"; exit 1; }
echo "ok [dispatch restoration: the usage advertises all five verbs]"
start_daemon() {
  "$qa/aeon" serve -no-backup > "$qa/daemon-$1.log" 2>&1 &
  daemon_pid=$!
  for _ in $(seq 1 100); do
    if [[ -S "$LOCALRPC_ENDPOINT_PATH" ]] && "$qa/aeon" ping >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  echo "daemon $1 never answered ping"; tail -5 "$qa/daemon-$1.log"; return 1
}
stop_daemon() { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; daemon_pid=""; sleep 0.5; }
assert() { # assert <label> <needle> [file] — the haystack is stdin, or FILE
  local label="$1" needle="$2" hay
  if [[ $# -ge 3 ]]; then hay="$(cat "$3")"; else hay="$(cat)"; fi
  if ! grep -qF -- "$needle" <<<"$hay"; then
    echo "FAIL [$label]: wanted to find: $needle"; echo "--- got ---"; echo "$hay"; exit 1
  fi
  echo "ok [$label]"
}

start_daemon 1
"$qa/aeon" ping >/dev/null

echo "== seed (D1's project: one live loop, one loop with a document that will be archived) =="
"$qa/aeon" component create --name walk --slug walk --root $qa/proj
"$qa/aeon" loop create --slug alpha --component $qa/proj --goal "live one"
"$qa/aeon" loop create --slug beta  --component $qa/proj --goal "to be archived"
"$qa/aeon" loop write beta brief.md --component $qa/proj --content "# the brief" >/dev/null
"$qa/aeon" loop list --all | assert "loop list shows both" "beta"
beta_live_id=$(sqlite3 "$db" "SELECT id FROM loop WHERE slug='beta';")
"$qa/aeon" loop archive --note "cold" "$beta_live_id" \
  | assert "archive names the cold copy" "in cold storage as"

echo "== D1 — the banner counts equal reality =="
"$qa/aeon" history coverage --root $qa/proj > "$qa/cov1.txt"
cat "$qa/cov1.txt"
live_reality=$(sqlite3 "$db" "SELECT COUNT(*) FROM loop WHERE component_root='$qa/proj';")
arch_reality=$(sqlite3 "$db" "SELECT COUNT(*) FROM loop_archive WHERE component_root='$qa/proj';")
assert "D1 live count = the loop table" "shown       live: $live_reality loop(s)" "$qa/cov1.txt"
assert "D1 archived count = the archive table" "shown       archived: $arch_reality archive(s)" "$qa/cov1.txt"
assert "D1 healthy store has no cannot-say" "cannot say  none" "$qa/cov1.txt"
assert "D1 the floor statement rides verbatim" "floor       A cost is a floor, never a bare number" "$qa/cov1.txt"

echo "== D3 — vocabulary is the closed set, the lane switch is the daemon's word =="
"$qa/aeon" history vocabulary > "$qa/voc.txt"; cat "$qa/voc.txt"
assert "D3 lanes served, not hardcoded" "lanes:     [loop phase task agent]" "$qa/voc.txt"
assert "D3 outcomes served" "outcomes:  [running waiting blocked done idle aborted failed]" "$qa/voc.txt"
"$qa/aeon" history list --project-root $qa/proj \
  | assert "D3 loop lane shows the archived header too" "beta"
"$qa/aeon" history list --project-root $qa/proj --group-by agent > "$qa/lane-agent.txt"
if grep -q "beta" "$qa/lane-agent.txt"; then echo "FAIL [D3]: the agent lane regrouped an ARCHIVE — an archive is not a lane (F-2's law)"; exit 1; fi
echo "ok [D3 agent lane leaves archives out — the not-shown, by the lane's nature]"

echo "== D4 — the drill-down is header-first =="
aid=$(sqlite3 "$db" "SELECT id FROM loop_archive WHERE slug='beta';")
"$qa/aeon" history show "$aid" | assert "D4 the archived drill prints its header line" "— beta (archived, cold-stored)"

echo "== D5 — export ≡ list, client-side from the daemon payload (H3) =="
"$qa/aeon" history export --project-root $qa/proj > "$qa/x.ndjson"
assert "D5 export carries both rows as NDJSON" '"kind":"archived"' "$qa/x.ndjson"
assert "D5 the live row rides too" '"kind":"live"' "$qa/x.ndjson"
assert "D5 the row carries the daemon's describe" '"describe"' "$qa/x.ndjson"
if [[ "$(sqlite3 "$db" "SELECT COUNT(*) FROM loop_archive WHERE component_root='$qa/proj';")" != "$(grep -c '"kind":"archived"' "$qa/x.ndjson")" ]]; then
  echo "FAIL [D5]: export's archived rows ≠ the archive table"; exit 1
fi
echo "ok [D5 export row counts equal the tables]"

echo "== D8 — two windows, byte-identical =="
"$qa/aeon" history list --project-root $qa/proj > "$qa/w1"
"$qa/aeon" history list --project-root $qa/proj > "$qa/w2"
diff -u "$qa/w1" "$qa/w2" && echo "ok [D8 two windows are byte-identical]"

echo "== D2 — plant the rot, restart, the view says CANNOT SAY and names the id =="
stop_daemon
planted=$(sqlite3 "$db" "UPDATE loop_archive_file SET content=X'000102' WHERE archive_id='$aid'; SELECT changes();")
[[ "$planted" == "1" ]] || { echo "FAIL [D2]: the plant hit $planted rows — the fixture broke"; exit 1; }
start_daemon 2
"$qa/aeon" history coverage --root $qa/proj > "$qa/cov2.txt"; cat "$qa/cov2.txt"
assert "D2 cannot-say names the planted id" "cannot say  1 archive(s) whose content could not be read — $aid" "$qa/cov2.txt"
assert "D2 the header still shows (T1: coverage never hides the row)" "shown       archived: 1 archive(s)" "$qa/cov2.txt"
assert "D2 the rest still renders: the live count is untouched" "shown       live: $live_reality loop(s)" "$qa/cov2.txt"
"$qa/aeon" history list --project-root $qa/proj | assert "D2 the rotten row is still LISTED by id" "$aid"

stop_daemon
echo "== the P5 offline door — same numbers, daemon down, notice first =="
"$qa/aeon" history coverage --root $qa/proj > "$qa/cov3.txt" 2> "$qa/notice.txt"
assert "P5 the fixed notice" "daemon not running — applied directly to the store" "$qa/notice.txt"
[[ "$(cat "$qa/notice.txt")" == "daemon not running — applied directly to the store" ]] || { echo "FAIL [P5]: the notice must stand alone on stderr"; exit 1; }
assert "P5 offline counts equal the socket counts" "cannot say  1 archive(s) whose content could not be read — $aid" "$qa/cov3.txt"

echo "== D6/D7 — the agent lane: QUOTED, not re-enacted (the loop's socket is not this walk's peer) =="
echo "D6 (an agent calls history_list): the agent's answer is byte-identical to the socket door it mirrors —"
echo "   internal/daemon/records_history_test.go: TestHistoryListAnswersTheDoorRenderingByteForByte,"
echo "   TestHistoryShowAnswersTheDoorRenderingByteForByte; the surface is exactly the two reads —"
echo "   TestHistoryCatalogSpecsAreExactlyTheTwoReads; and they point persons at the socket doors —"
echo "   TestHistoryToolsPointPersonsAtTheSocketDoors."
echo "D7 (history.export as an agent): IMPOSSIBLE by surface — cmd/aeon/history_guard_test.go:"
echo "   TestHistoryExportIsPersonOnly and TestNoHistoryMethodIsOnTheNarrowSurface (HV5)."

echo
echo "S53 WALK: all assertions green — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
