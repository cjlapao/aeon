# S30 Code Graph — the demo

**Walked 2026-10-30 (T8).** A real `aeon serve` (throwaway `XDG_STATE_HOME`, own
endpoint), then the eight CLI verbs, then the raw-socket pair through socat — the
same doors, the same answers. The §Findings table at the bottom is the authoritative
version of what actually happened; read it before the predictions above.

Fixture project: a small multi-package Go service plus a TypeScript frontend, checked into
`testdata/fixtures/graph-demo/` — large enough to have a real call chain (at least four hops deep) and one
deliberately ambiguous name (two functions named `Validate` in different packages).

---

## 1. The graph builds itself — no separate command

```bash
aeon loop start --blueprint demo-touch-a-file --project testdata/fixtures/graph-demo
```

The loop's first step edits one file in the fixture. Nothing in this slice is invoked explicitly — the
write-signal hook (T4) fires on the loop's own write, and the graph for this project root now has rows.

**What this proves:** there is no `aeon graph build` command to remember to run, and no filesystem watcher
running in the background either (D5, N2) — the graph exists because a loop wrote something, not because
anything was watching.

## 2. Using the app: the `graph` tab, `explain`

Open the test bench (`/bench/graph`). The page matches `AskPage.tsx`'s and `PolicyPage.tsx`'s layout: a
project-root field (required — no default, matching the tool's own contract), an action selector
(`explain` / `path` / `query`), and a symbol field.

1. Project root: the fixture path.
2. Action: `explain`.
3. Symbol: `ProcessOrder` — a function four calls deep into the fixture's chain.
4. Submit.

**Expected result panel**, state `found`:

```text
ProcessOrder  (func)  internal/order/process.go:42–58

  calls →
    validateOrder     internal/order/validate.go:12   (extracted)
    chargePayment      internal/billing/charge.go:30   (extracted)

  called by ←
    HandleOrderRequest internal/api/handler.go:88      (extracted)
```

No prose summary anywhere in the panel — just the node and its direct edges, exactly as D7 and T5 require.
The agent (or the person at the keyboard) decides from this list which node, if any, is worth reading in
full.

## 3. `path` — the chain between two symbols

Same tab, action `path`, symbols `HandleOrderRequest` and `chargePayment`.

**Expected result**, state `found`:

```text
HandleOrderRequest → ProcessOrder → chargePayment
```

Three hops, within the bound T5 set. Trying a `path` between two symbols with no connecting chain at all
returns `not-found` — not an error, not an empty `found`.

## 4. `ambiguous` — two functions, one name

Action `explain`, symbol `Validate` — the fixture deliberately has two, in `internal/order/` and
`internal/billing/`.

**Expected result**, state `ambiguous`:

```text
"Validate" matches 2 nodes:
  internal/order/validate.go:12     func Validate
  internal/billing/validate.go:9    func Validate
```

No traversal happens past this point — the caller has to pick one and ask again by its fully-qualified
symbol. This is the result D8 exists to make possible: a caller cannot mistake "ambiguous" for "found
nothing."

## 5. `not-built` — a project nobody has touched yet

Switch the project-root field to a second fixture path that has never run a loop.

**Expected result**, state `not-built`:

```text
No graph exists yet for this project root.
```

Distinct, in the panel's own copy, from what §6 below shows for a genuine no-match — this is the case T7's
UI task is specifically required to render differently, and the case D8 was added (during brief-writing,
not the earlier discussion) specifically to make expressible at all.

## 6. Composition: chaining into S22's `search`

Back on the first fixture, take one node from §2's result — `chargePayment`,
`internal/billing/charge.go:30`. Switch to the `search` tab (S22, once built) and query for content scoped
to that exact file and line range.

**What this proves:** two separate tool calls, two separate tabs, no shared schema (D11). The graph told
the agent *where* to look; S22 tells it *what is there*. Nothing in this slice's response carries S22's
content, and nothing in S22's schema carries this slice's structure.

## 7. Edit one file — the graph updates, the rest of the project does not re-parse

```bash
echo "// a comment" >> testdata/fixtures/graph-demo/internal/order/process.go
aeon loop start --blueprint demo-touch-a-file --project testdata/fixtures/graph-demo
```

Re-run §2's `explain` on `ProcessOrder` — the result is unchanged (a comment adds no node or edge), but the
instrumentation T4's test relies on shows only `process.go`'s rows were touched, not the whole fixture's.

**What this proves:** D6's scaling bound, visibly — this is the same proof T4's test makes mechanically,
shown here by hand.

## 8. What dispatch sees: nothing

There is no step in this demo where any agent-selection decision consults the graph. This is deliberate
(D10) and not demonstrable as a positive action — the closest this demo can come is pointing at T6's
import-boundary test and confirming it still passes after every step above.

---

## What this demo does **not** show

- **The real grammar count beyond Go and TypeScript** — T3 measures that against the fixture's own
  languages only; a 40-language fixture is not practical to maintain by hand here.
- **A `path` query that exceeds the hop bound** — exercised by T5's own test, not walked here.
- **Behavior with S22 absent entirely** — this slice's own claim is that it does not need S22 to exist; a
  separate walk with S22's package deleted belongs to S22's own optionality proof (its D1), not this one.

---

## Walk record — 2026-10-30 (T8)

Environment: `./.tmp/aeon-s30 serve` with `XDG_STATE_HOME=.tmp/s30-walk/state` and
`LOCALRPC_ENDPOINT_PATH=.tmp/s30-walk/aeon.sock` (fresh state; endpoint `srw-------`).
Fixture root: `aeon/internal/graph/testdata/fixtures/graph-demo`. Raw-socket
transcript kept at `.tmp/s30-walk/socket2.jsonl` (disposable; the content is quoted below).

| Step | Prediction | What the run showed | Status |
|---|---|---|---|
| §1 builds itself | A built root stays current on loop writes, no manual step. | Not walkable without a running loop (needs a model); the two halves ARE pinned instead: the hook is chained at both StepRunner literals (`TestGraphWriteHookWiredAtBothStepRunners`) and it is async+isolated (`TestChainHooksIsolatesSubscribers`), with `StatsForRoot` as the one-query Build-vs-Rebuild gate (`TestGraphBuildsItselfViaStatusGate`). | Partial — pinned by tests, not clicked |
| §2 the tab explains | `explain` answers one symbol with edges both directions. | `found` + node + `→ uses/calls [extracted|inferred]` + `← calls/references`, live over the CLI. | Held |
| §3 the chain | `path` connects HandleOrderRequest→…→Validate. | `found`, three hops, each with kind + resolution tag. | Held |
| §4 ambiguous | both `Validate`s listed, none picked. | `ambiguous`, `candidate: billing.Validate` + `candidate: order.Validate`, "nothing is picked for you". Same answer over raw JSON-RPC (`"state":"ambiguous"` with both symbols). | Held |
| §5 not-built | a never-parsed root is its own state. | `NOT BUILT — no rows for this root (aeon graph build starts one)` before build; after `delete`, the same root returns to it and the OTHER root survives `graph.roots`. | Held |
| §6 chaining into search | graph answers carry references only. | The wire types have no content field to fill (`TestCG8NoProseFieldAnywhere`, reflection over every result type); the walk's outputs are all `symbol · kind · file:lines`. | Held |
| §7 one file, one re-parse | editing one file touches that file. | `reparse … charge.go: 13 rows touched` in a 30-node project; the re-linked edges arrive `[inferred]`, and the renamed symbol's vanished incoming edge DROPS rather than dangles. | Held — with a find |
| §8 dispatch sees nothing | structurally. | T6's both-directions AST import test passes as the last step of the walk. | Held |

**The find**: `graph reparse --path internal/billing/charge.go` — the documented,
project-relative spelling — was refused with "not under project root". The door
passed the relative name straight to the absolute-path `RebuildFile`. Fixed in the
same sitting (join under the normalized root; the `..`-escape stay refused) with
regression test `TestReparseAcceptsProjectRelativePath`. The absolute spelling (the
daemon hook's shape) was verified still accepted.

**Counters as spoken**: build `7 files (0 skipped), 30 nodes, 27 edges, 9 references
unresolved` — the unresolved ones are the fixture's external stdlib calls (fmt etc.),
reported, not dropped silently. Raw socket (`protocolVersion: 2`, since that is what
this daemon speaks): `graph.status → built:true, files:7, nodes:30, edges:27`.
