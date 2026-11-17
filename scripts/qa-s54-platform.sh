#!/usr/bin/env bash
# S54 platform-status walk (T6, P9): real daemon, real store, real socket,
# real CLI — the qa-s52-memory.sh shape carried.
#
# Embeddings ride a LOCAL STUB route on purpose: the daemon refuses to
# reindex with no route at all (V19's no-provider state — the walk asserts
# that refusal FIRST, for real), then registers an OpenAI-compatible route
# pointed at a python stub on 127.0.0.1 with NO price, so the actual pass
# makes REAL calls and spends nothing on a route that prices nothing — the
# plan's `unknown` cost state and the pass's `unknown` cost answer agree,
# which is exactly "actual spend within the estimate's state".
#
# The lanes a scripted walk cannot hold are QUOTED to named tests at their
# steps (S50/S51/S52's precedent): the agent-cannot-operate lane needs a
# LOOP socket's peer kind (a person socket stamps user:<uid>, never agent —
# TestPlatformOperationsAreAHumanDoor holds the refusal), and the UI's
# confirm/refusal renders plus the second WINDOW's picture are the vitest
# suite (SystemSection.test.tsx) driven against a fake daemon. Everything
# else — the fresh store's honest panel, the V19 refusal, the plan, the
# actual pass, the flying pass naming its starter to a SECOND socket, the
# graph operations, the audited rows, the staleness after an edit — runs
# for real here and is asserted verbatim.
set -euo pipefail
cd "$(dirname "$0")/.."
qa=/tmp/opencode/aeon-tmp/s54walk
rm -rf "$qa"; mkdir -p "$qa"
daemon_pid=""; stub_pid=""
cleanup() {
  [[ -n "$daemon_pid" ]] && { kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; }
  [[ -n "$stub_pid" ]] && { kill "$stub_pid" 2>/dev/null || true; wait "$stub_pid" 2>/dev/null || true; }
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

repo="$qa/repo"; mkdir -p "$repo"
cat > "$repo/widget.go" <<'GO'
package widget

func Widget() int { return helper() + 1 }
GO
cat > "$repo/helper.go" <<'GO'
package widget

func helper() int { return 41 }
GO

# [D1] Fresh store: the composed panel says what IS. A wired subsystem with
# nothing indexed is `available` with an EMPTY component list (T2's rule:
# empty means empty; `unwired` is the ABSENT seam only, which this build is
# not — TestUnwiredExactlyWhenTheSeamIsAbsent; for the variant binary's
# genuinely absent retrieval seam, cmd/aeon/platform_guard_noretrieval_test.go's
# TestPlatformDoorMatrixPerVariant). No block pretends to have run, and no
# zero reads like an invoice of nothing.
$A platform status --json > "$qa/status-fresh.json"
python3 - "$qa/status-fresh.json" <<'PY'
import json, sys
v = json.load(open(sys.argv[1]))
WORDS = {"available", "stale", "building", "unwired", "error"}
assert v.get("evaluatedAt"), "the panel names no evaluation time"
for block in ("retrieval", "graph", "usage"):
    assert v[block]["state"] in WORDS, f"{block} state {v[block]['state']!r} is not the daemon's word"
assert not v["retrieval"].get("components"), "a fresh store has no corpora — the panel must show none"
assert not v["graph"].get("roots"), "a fresh store has no graph — the panel shows none"
u = v["usage"]
assert u.get("workspace") is None or u["workspace"]["recorded"] is False, \
    "a workspace that has run no loops is NOT recorded — a zero would read as an invoice"
print("D1 ok — fresh panel:", v["retrieval"]["state"], v["graph"]["state"], u["state"], "with nothing yet built")
PY

# [D2a] V19 first, for real: with no embedding route configured the pass
# REFUSES, naming the no-provider state. (The walk's own failure is the
# demo; a non-zero rc here is the EXPECTED outcome, so it is captured.)
refused=$($A index build --root "$repo" --corpus code 2>&1 || true)
grep -qi "no embedding route" <<<"$refused" || { echo "the route-less refusal did not arrive: $refused"; exit 1; }
grep -qi "not a call" <<<"$refused" || { echo "the refusal lost V19's sentence: $refused"; exit 1; }
echo "D2a ok — route-less reindex refused verbatim: $(head -1 <<<"$refused")"

# [D2b] Now the route: an OpenAI-compatible stub on 127.0.0.1, NO price
# (--no-price is the walk staying honest — the daemon may not invent a
# figure for a route nobody priced). The 2s pause per answer exists so D3
# can catch a flight over a two-file tree.
python3 - "$qa" <<'PY' > /dev/null 2>&1 &
import http.server, json, sys, time
qa = sys.argv[1]
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        inputs = body.get("input", [])
        time.sleep(2.0)
        data = [{"index": i, "embedding": [((hash(t) % 97) + i + 1) / 97.0] * 1 + [float(i)] * 7}
                for i, t in enumerate(inputs)]
        n = sum(len(t) // 4 + 1 for t in inputs)
        raw = json.dumps({"data": data, "usage": {"prompt_tokens": n, "total_tokens": n}}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw))); self.end_headers(); self.wfile.write(raw)
    def log_message(self, *a): pass
srv = http.server.HTTPServer(("127.0.0.1", 0), H)
open(qa + "/stub.port", "w").write(str(srv.server_address[1]))
srv.serve_forever()
PY
stub_pid=$!
for _ in $(seq 1 100); do [[ -s "$qa/stub.port" ]] && break; sleep 0.1; done
port=$(cat "$qa/stub.port")
# The route is declared over the socket with the PURPOSE "embedding" —
# the flag surface of `aeon routes set` does not reach the purpose field,
# but the human door's own params do (`SetRouteParams.capabilities`), and
# `tools.EmbedRoute` selects on it. Same door, same params the CLI sends;
# the walk just reaches the field the CLI has no flag for.
python3 - "$LOCALRPC_ENDPOINT_PATH" "$port" <<'PY'
import json, socket, sys
ep, port = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect(ep); f = s.makefile("rb")
def call(method, params, mid=[0]):
    mid[0] += 1
    s.sendall((json.dumps({"jsonrpc": "2.0", "id": mid[0], "method": method, "params": params}) + "\n").encode())
    msg = json.loads(f.readline())
    if "error" in msg: raise SystemExit(f"{method}: {msg['error']}")
    return msg["result"]
call("initialize", {"clientName": "s54-walk-routes", "clientVersion": "0", "protocolVersion": 2})
call("routes.set", {"id": "walk-stub", "provider": "openai", "model": "walk-stub-8",
                    "baseUrl": f"http://127.0.0.1:{port}", "apiKey": "walk-stub-key",
                    "capabilities": ["embedding"], "maxOutputTokens": 8192, "noPrice": True})
print("D2b route ok — walk-stub declared with the embedding purpose, unmetered on purpose")
PY

# The plan BEFORE the pass: counts, basis, and the cost WITH its state.
$A platform plan code --component "$repo" --json > "$qa/plan.json" 2>"$qa/plan.err" || {
  cat "$qa/plan.err"; exit 1; }
python3 - "$qa/plan.json" "$repo" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
assert p["corpus"] == "code" and p["componentRoot"] == sys.argv[2], "the plan misnamed itself"
assert p["estimatedInputTokens"] > 0, "a plan over a two-file tree answers zero tokens"
assert p["cost"]["state"] in {"known", "partial", "unknown"}, p["cost"]["state"]
assert p["cost"]["state"] == "unknown" and p["cost"].get("usd") is None, \
    "the route carries no price — a figure here would be an invention"
assert p["cost"].get("reason"), "unknown MUST ride the daemon's reason, never a bare word"
print("D2b plan ok —", p["files"], "files,", p["staleChunks"], "stale chunks, cost",
      p["cost"]["state"], "·", p["cost"]["reason"])
PY

# The actual pass (the CLI's --yes is the twin of the modal's click).
$A index build --root "$repo" --corpus code > "$qa/build.out" 2>&1
grep -qE "chunks written" "$qa/build.out" || { cat "$qa/build.out"; exit 1; }
grep -q "cost unknown" "$qa/build.out" || \
  { echo "the pass claimed a cost the plan refused to name:"; cat "$qa/build.out"; exit 1; }
echo "D2 ok — pass landed: $(head -1 "$qa/build.out")"

# And the ledger row it minted — stamped with the PEER, not with anything a
# caller typed (there is no --actor to lie with).
$A platform changes --json > "$qa/changes-1.json"
python3 - "$qa/changes-1.json" <<'PY'
import json, os, sys
rows = json.load(open(sys.argv[1]))
uid = str(os.getuid())
reindexes = [r for r in rows if r["op"] == "reindex"]
assert reindexes, f"no reindex in the ledger: {rows}"
r = reindexes[0]
assert r["actorKind"] == "user" and r["actorId"] == uid, \
    f"the audit names {r['actorKind']}:{r['actorId']}, not the peer {uid}"
print("D2 audit ok —", r["op"], r["subject"], "=", r["actorKind"] + ":" + r["actorId"])
PY

# [D3] Two windows, one pass — REAL this time: edit a file, launch a
# rebuild in the background (the stub pauses 2s per answer, so the flight
# is wide), then a SECOND socket asks for the same pass while it flies. The
# daemon joins the flight rather than double-spend, and the answer names
# the starter (T1's startedBy naming) — the UI prints exactly these words
# (SystemSection.test.tsx: "an already-flying pass's answer names its
# starter, verbatim on the panel").
echo '
func Bonus() int { return helper() }' >> "$repo/widget.go"
$A index rebuild --root "$repo" --corpus code --yes > "$qa/rebuild-d3.out" 2>&1 &
rebuild_pid=$!
sleep 1
python3 - "$LOCALRPC_ENDPOINT_PATH" "$repo" <<'PY'
import json, os, socket, sys
ep, repo = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect(ep); f = s.makefile("rb")
def call(method, params, mid=[0]):
    mid[0] += 1
    s.sendall((json.dumps({"jsonrpc": "2.0", "id": mid[0], "method": method, "params": params}) + "\n").encode())
    line = f.readline()
    if not line: raise SystemExit(f"{method}: daemon closed")
    msg = json.loads(line)
    if "error" in msg:
        raise SystemExit(f"D3 the second call was REFUSED, not joined: {msg['error']}")
    return msg["result"]
call("initialize", {"clientName": "s54-walk-d3", "clientVersion": "0", "protocolVersion": 2})
out = call("retrieval.reindex", {"componentRoot": repo, "corpus": "code"})
assert out.get("started") is False, f"the second caller STARTED a second pass: {out}"
assert out.get("startedBy") == f"user:{os.getuid()}", \
    f"the flying pass does not name its starter: {out}"
assert out.get("startedAt"), "the flying pass names a starter with no time"
print(f"D3 ok — joined the SAME flight {out['workerId']}, starter named: {out['startedBy']} at {out['startedAt']}")
PY
wait "$rebuild_pid"
grep -q "chunks written" "$qa/rebuild-d3.out" || { echo "the D3 pass did not land:"; cat "$qa/rebuild-d3.out"; exit 1; }

# [D4] Graph operations, audited: the build, a reparse replacing ONE
# file's derived rows, a delete removing the root's graph — three starts,
# three ledger rows, each stamped with the peer. Source files are touched
# by none of them (the checksums prove it).
sha_before=$(find "$repo" -name '*.go' -exec sha256sum {} \; | sort | sha256sum)
$A graph build --root "$repo" > "$qa/graph-build.out" 2>&1
$A graph status --root "$repo" > "$qa/graph-status.out" 2>&1
grep -qE "nodes" "$qa/graph-status.out"
$A graph reparse --root "$repo" --path widget.go --yes > "$qa/graph-reparse.out" 2>&1
$A graph delete --root "$repo" --yes > "$qa/graph-delete.out" 2>&1
sha_after=$(find "$repo" -name '*.go' -exec sha256sum {} \; | sort | sha256sum)
[[ "$sha_before" == "$sha_after" ]] || { echo "a graph act touched a SOURCE file"; exit 1; }
$A platform changes --json > "$qa/changes-2.json"
python3 - "$qa/changes-2.json" <<'PY'
import json, os, sys
rows = json.load(open(sys.argv[1]))
uid = str(os.getuid())
ops = {r["op"] for r in rows if r["op"].startswith("graph_")}
missing = {"graph_build", "graph_reparse", "graph_delete"} - ops
assert not missing, f"the ledger lacks {missing}: {[r['op'] for r in rows]}"
for r in rows:
    assert r["actorKind"] == "user" and r["actorId"] == uid, \
        f"row {r['id']} names {r['actorKind']}:{r['actorId']}, not the peer"
print("D4 ok — audited ops:", sorted({r["op"] for r in rows}), "; sources byte-identical")
PY

# [D5] An agent CANNOT operate: no tool, catalog spec or narrow door
# carries any of the FOUR operation doors (checked for real against the
# registry — no tool name and no spec method names `retrieval.reindex`,
# `graph.build`, `graph.reparse` or `graph.delete`; a read tool's
# DESCRIPTION may lawfully say the operations are a person's act — that is
# index_status's own law, not a door). The socket refusal of a non-person
# actor is T3's guard — TestPlatformOperationsAreAHumanDoor
# (cmd/aeon/platform_guard_test.go) — walked there, not claimable from a
# person's socket here.
$A tools list --json > "$qa/tools.json"
python3 - "$qa/tools.json" <<'PY'
import json, re, sys
doc = json.load(open(sys.argv[1]))
methods, names = set(), []
def walk(x):
    if isinstance(x, dict):
        for k, v in x.items():
            if k == "method" and isinstance(v, str):
                methods.add(v)
            elif k == "name" and isinstance(v, str):
                names.append(v)
            else:
                walk(v)
    elif isinstance(x, list):
        for v in x:
            walk(v)
walk(doc)
OP_DOORS = {"retrieval.reindex", "graph.build", "graph.reparse", "graph.delete"}
assert not (methods & OP_DOORS), f"an agent surface reaches an operation door: {methods & OP_DOORS}"
bad = [n for n in names if re.search(r"reindex|reparse|graph_(build|delete)|platform_change", n)]
assert not bad, f"an agent-surface tool NAMES an operation: {bad}"
print(f"D5 ok — {len(methods)} methods, {len(names)} names on the surface, zero operation doors; the actor refusal rides TestPlatformOperationsAreAHumanDoor")
PY

# [D6] index_status is the agent's honest EYE (read-only, registered): the
# same counts the panel prints, answering the same service.
$A tools show index_status > "$qa/tool-index-status.out" 2>&1
grep -qi "index" "$qa/tool-index-status.out" || { echo "index_status answers nothing"; exit 1; }
echo "D6 ok — $(head -1 "$qa/tool-index-status.out")"

# [D7] Staleness is the daemon's fact, answered fresh: edit a source file
# and ask the panel again — the corpus reports its stale chunks. (The
# second browser WINDOW opening the same read is the vitest suite's
# two-mount drive; the staleness itself is asserted for real here.)
# The corpus line of `aeon index status` is the daemon's own word: a
# walked corpus is `current` and a source edit moves it to `stale: N files`
# — measured, not invented (T2 law: the panel recomputes at the read;
# TestNoSecondStatusComputation keeps one source for the word). The BLOCK
# word stays `stale` here for a second, honest reason the walk records: the
# history corpus has zero archives to index, its pass wrote nothing, and a
# never-built corpus reads `index-empty` — the panel aggregates it as stale
# exactly as the graph block aggregates a listed-but-never-built root.
before=$($A index status --root "$repo" | grep -E "^\s+code ")
echo "$before" | grep -q "current" || { echo "the walked corpus was not current before the edit:"; echo "$before"; exit 1; }
echo "D7 before — code corpus: ${before}"
echo '
func Another() int { return helper() + 2 }' >> "$repo/widget.go"
after=$($A index status --root "$repo" | grep -E "^\s+code ")
echo "$after" | grep -qE "stale: [0-9]+ file" || { echo "an edited source file left the corpus at: $after"; exit 1; }
echo "D7 ok — the edit moved the corpus: current -> $(echo "$after" | grep -oE "stale: [0-9]+ file[s]?")"

echo "S54 walk rc=0 — real daemon, real socket, real CLI; the agent lane and the second window are named tests, quoted at their steps."
