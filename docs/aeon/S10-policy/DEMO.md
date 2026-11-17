# S10 Policy — the demo

**Stage 3 · Car.** *First of eight. This is where Aeon starts being governed.*

The bicycle could run a loop. It could not be told *"not here."* Everything in this demo is one sentence
the system now understands.

> **Header note (loop `aeon-s10-policy`, T16 — 2026-10-20).** This document is the slice's **prediction**:
> it was written before the demo was ever walked. The walked, cumulative record lives at
> [`docs/aeon/DEMO.md`](../DEMO.md), section **S10 — Policy** — every output there is quoted from real
> runs on the checkout. T17's Findings table records where this prediction was wrong; the per-step
> deltas it harvests are pre-listed in the T16 Notes block of [`tasks/T12.md`](tasks/T12.md).
>
> **The walk happened (loop `aeon-s10-policy`, T17 — 2026-10-21).** All eight sections were run on this
> checkout against a throwaway store; the record is [§Findings](#findings-t17-walk-2026-10-21)
> below, and the cumulative record with the real quoted output is
> [`docs/aeon/DEMO.md`](../DEMO.md) §S10. **Every command block in §1–§8 above stays exactly as predicted**
> — a prediction is evidence about the design, and rewriting it after the fact destroys that. Where a
> command does not run as written, the finding says so; the working form lives in the cumulative record.
>
> **The two behaviour gaps were closed under this loop's deferred-findings gate (2026-10-21, T18–T23), and the
> rows were amended the same day (T24).** **F-13** (the cap never shown enforcing) and **F-14** (a
> filtered-empty audit listing explains nothing) are closed by **T22 `6ff60715`** (PR #116) and **T23
> `e6418cc0`** (PR #118); the re-walk that proved it, with this checkout's own output, is the pair of
> amendments under [§Findings](#findings-t17-walk-2026-10-21), and the QA re-run's exact replacement wording is
> recorded there too (the suite itself stays `qa-engineer-loop`'s). §1–§8 are still untouched.

---

## 1. There are no policies, and that means nothing is allowed

```
aeon policy list
```

**You should see** an empty list, said as a fact rather than as an absence:

```
no policies.  default is DENY (PL5) — nothing is granted until something grants it.
```

**It failed if** it prints nothing at all, or `0 policies` with no mention of the default. A guard whose
empty state reads as "fine" is the one failure this slice cannot have.

Now prove it:

```
aeon policy why --agent golang-engineer-loop --tool bash
```

**You should see:**

```
DENIED  bash
  no allow matched · default deny (PL5)
```

---

## 2. Grant the tool, and watch it work

```
aeon policy add --type ToolAccess --target agent:golang-engineer-loop \
                --action allow --spec '{"toolName":"bash","state":"allow"}' \
                --id grant-bash-to-go
aeon policy why --agent golang-engineer-loop --tool bash
```

**You should see:**

```
ALLOWED bash
  by ToolAccess "grant-bash-to-go" · target agent:golang-engineer-loop · source operator · no expiry
```

**`no expiry` is printed, not omitted** (PL7). A policy with no lifetime and a policy whose lifetime nobody
checked look identical when the field is silent.

---

## 3. Deny it system-wide, and watch the grant lose

This is the slice's whole thesis.

```
aeon policy add --type ToolAccess --target all-agents \
                --action deny --spec '{"toolName":"bash","state":"deny"}' \
                --id no-shell-anywhere
aeon policy why --agent golang-engineer-loop --tool bash
```

**You should see the grant from step 2 still there, and lose anyway:**

```
DENIED  bash
  by ToolAccess "no-shell-anywhere" · target all-agents · source operator · no expiry
  (an allow exists: "grant-bash-to-go" · agent:golang-engineer-loop — denied anyway, PL4)
```

**It failed if** the more specific allow wins. *"Most specific wins"* is the intuitive rule and it is the
wrong one — it is exactly the union-of-permissions escalation this design closes. **Deny is absolute at
every level, and the verdict says so by naming the allow it overrode.**

Now watch it in a real step:

```
aeon loop create --slug policy-demo
aeon loop run policy-demo --blueprint dev-loop
```

**You should see** a step attempt `bash` and be refused, **with the policy named in the transcript** — and
a `Client` spy proving the command never spawned (PL9: the runtime enforces).

---

## 4. The agent was told, and the telling is recorded separately

```
aeon step show <step-id> --manifest | grep -A5 policies
```

**You should see** the policies that were in force **written into the step's `context_manifest`**:

```
policies:
  - ToolAccess no-shell-anywhere   all-agents   deny
  - ToolAccess grant-bash-to-go    agent:…      allow
```

**Why both halves exist** (PL9): the runtime refused the call, *and* the agent was told in its context. A
model told not to do something may still try — so the instruction is an optimisation, never the control.

Now the part that is telemetry rather than audit:

```
aeon policy events --kind disagreement
```

**You should see** the attempt recorded:

```
disagreement  step 4f2a1c  agent golang-engineer-loop  tried bash  refused by no-shell-anywhere
```

**A rising count here means the policy text is not landing in the model's context** — a failure you would
otherwise never see, because the system keeps working.

---

## 5. An agent may deny, and may never allow

This is PL1, and it is what makes a plugin safe without trusting it.

```
aeon policy add --type ToolAccess --target agent:reddit-agent \
                --action deny --source agent --id reddit-self-restraint \
                --spec '{"toolName":"webfetch","state":"deny"}'
```

**You should see** it accepted. An agent narrowing itself is always fine.

```
aeon policy add --type ToolAccess --target agent:reddit-agent \
                --action allow --source agent --id reddit-self-grant \
                --spec '{"toolName":"bash","state":"allow"}'
```

**You should see it refused at write time**, with the source named:

```
REFUSED  an Allow may only be authored by an operator (PL1)
         source: agent · id: reddit-self-grant
         a Deny from this source would be accepted.
```

**It failed if** it is stored and merely ignored at evaluation. **The rule is enforced at the write
boundary**, in one place — because a rule enforced in two places is a rule with a gap, and a stored allow
that "does not count" is one refactor away from counting.

---

## 6. Expiry, at the boundary

```
aeon policy add --type ToolAccess --target all-agents --action deny \
                --spec '{"toolName":"webfetch","state":"deny"}' \
                --id no-fetch-today --not-after '2026-10-15T00:00:00Z'

aeon policy why --tool webfetch --at '2026-10-14T23:59:59Z'
aeon policy why --tool webfetch --at '2026-10-15T00:00:00Z'
```

**You should see** denied, then **not denied by that policy**:

```
DENIED   webfetch   by ToolAccess "no-fetch-today" … expires 2026-10-15T00:00:00Z
DENIED   webfetch   no allow matched · default deny (PL5)
         ("no-fetch-today" expired at 2026-10-15T00:00:00Z and no longer applies)
```

**Note the second one is still denied** — by the *default*, not by the expired policy. An expired deny does
not become an allow; it stops existing, and PL5 takes over.

**`--at` is the whole reason this is testable.** The clock is a parameter (PL6), so the boundary is exact
and no test sleeps.

---

## 7. Which policies is this agent under, and did it know?

```
aeon policy in-force --agent golang-engineer-loop
```

**You should see three distinct states**, never two:

```
ToolAccess no-shell-anywhere   enforced · acknowledged 2026-10-14T09:12:04Z
ToolAccess grant-bash-to-go    enforced · NOT ACKNOWLEDGED
RouteAccess local-only         could not determine — no evaluator for this type yet
```

**It failed if** unacknowledged renders the same as acknowledged, or if `could not determine` renders as
either. This is **P31's eighth instance**: a fleet that never acknowledged its policies is working but
degraded, and nothing reveals that unless something asks.

**And acknowledgement is not a gate** (PL10) — the middle row is still *enforced*. If it gated enforcement,
a plugin could evade policy by declining to accept it.

---

## 8. The log does not grow forever

```
aeon policy events --depth
```

**You should see** the depth against its cap:

```
policy_event  1,284 rows  ·  cap 90 days  ·  oldest 2026-07-16
```

**It failed if** there is no cap. Every refusal, forever, is an unbounded table — decided now rather than
discovered at 2 GB (PL11).

---

## Findings (T17 walk, 2026-10-21)

All eight sections were walked on this checkout. **No failure below is softened**: where a predicted
command does not run, the row says it failed and names what runs instead. Nothing here was repaired
silently — the corrections to this document are marked as amendments, and the commands in §1–§8 stay as
predicted.

**How the walk ran.** A throwaway store (`XDG_STATE_HOME` to a temp dir) and a private socket
(`LOCALRPC_ENDPOINT_PATH`), the `aeon` binary built from this checkout, and a replay model endpoint serving
the repo's own recorded SSE fixtures from `internal/model/testdata/anthropic/` — so runtime, gate, dispatcher
and store are the real code and only the "model" is replayed. (The 2026-10-20 walk used a local replayer; the
2026-10-21 re-run used the repo's own `aeon/scripts/fixture-server`, which is the form documented in
[`docs/aeon/DEMO.md`](../DEMO.md) §S10 step 3.) **App halves were not exercised: headless box, no
DISPLAY/webkit2gtk** (AGENTS §4a — *say what you did not verify*). `policy` verbs open the store directly and
need no daemon; `loop`, `step` and `routes` verbs do.

| # | § | Predicted | Actual, on this checkout | Sev | Origin |
| --- | --- | --- | --- | --- | --- |
| F-1 | §1 §2 §3 §6 | verdicts render as two-line blocks (`ALLOWED bash` / `DENIED bash`, then an indented `by …` line) | one line, `Verdict.Sentence()`: `allowed: <subject> · <type> "<id>" · target … · source … · no expiry` / `refused: …` — the whole verdict is a single sentence, and `policy why`, the tool_result in a transcript and the socket's `sentence` field are the same string (D9) | Med | T16 |
| F-2 | §1 | `no policies.  default is DENY` — two spaces after the period | one space; the pinned constant `emptyPolicyListLine` (cmd/aeon/policy.go) has one. The sentence itself is verbatim | Low | new |
| F-3 | §2 §3 §5 §6 | `--type ToolAccess` | refused by name, exit 1: `aeon: unknown policy type "ToolAccess"`. The closed vocabulary is snake_case: `--type tool_access` | Med | T16 |
| F-4 | §3 | `aeon loop create --slug policy-demo` then `aeon loop run policy-demo --blueprint dev-loop` | `loop create` only files a preflight row (and needs a running daemon, bound to the current directory's project root); `loop run` answers ``aeon: unknown `loop` subcommand "run"``. The real verb is `aeon loop start --slug S --blueprint B --route R --project DIR "GOAL"`. `--blueprint dev-loop` was never exercised — the recorded fixtures are the two-step lane | Med | new detail on a T16 flag |
| F-5 | §2 §3 | granting and asking as `agent:golang-engineer-loop` shows the runtime refusal naming the overridden allow | a step asks as `agent:default` (the blueprint lane's handler name), so as written the transcript shows the plain default deny only; the "(an allow exists … — denied anyway, PL4)" clause appears only when the grant targets the identity the step carries | Med | new |
| F-6 | §4 | ``aeon step show <step-id> --manifest \| grep -A5 policies`` shows the frozen policy set | the grep finds nothing: `--manifest` prints iterations and deadline prose. The set and its hash are in the plain ``aeon step show <id>`` context JSON (`"policies":[…],"policiesHash":"…"`), and the model-side telling is the `## Policy` digest in the opening USER message — which renders an all-agents row as `all-agents:` with a trailing empty id (runtime/context.go:232 formats `%s · %s:%s · %s` unconditionally) | Med | T16 + new detail |
| F-7 | §4 | `aeon policy events --kind disagreement` | no `--kind` flag (exit 1: `flag provided but not defined: -kind`); the real flags are `--subject KIND:ID` and `--depth N` | Med | T16 |
| F-8 | §5 | a three-line `REFUSED` block naming the source and the policy id | one sentence, exit 1: `aeon: refused: Allow authored by source "agent" requires an operator (PL1) — a Deny from "agent" would be accepted`. It **omits the policy id** — the predicted `id: reddit-self-grant` is not printed | Low | new |
| F-9 | §6 | the after-expiry line parenthesises the expired rule (`("no-fetch-today" expired at … and no longer applies)`) | absent; the default-deny sentence does not name the policy that expired. The direction is still right: expired deny → default, never → allow (D7) | Low | T16 |
| F-10 | §6 | `why` called without `--agent` renders a subject | it renders an empty one: `refused: agent: · …` — types.go:214 prints `kind + ":" + id` with no `all-agents`-style special case. Pass `--agent` (or document the empty form) | Low | new |
| F-11 | §7 | `enforced · acknowledged <instant>` / `enforced · NOT ACKNOWLEDGED` / `could not determine — no evaluator for this type yet`, the third row a `RouteAccess` | `InForceAnswer.Render()` rows, type and policy id in columns: `in force · acknowledged by agent:<id> since <instant>` / `in force · unacknowledged — still enforced (PL10)` / `could not determine · no evaluator for <type> yet (D10)`. The third state here came from a **cost_cap** row; any stub type produces it — `RouteAccess` need not exist | Low | T16 + new detail |
| F-12 | §8 | `aeon policy events --depth` (no value), header `policy_event  1,284 rows  ·  cap 90 days  ·  oldest 2026-07-16` | `--depth` requires a value (default 20); the footer is `depth <n> · cap 90 days · oldest <RFC3339 instant>` — an instant, not a date | Low | T16 |
| F-13 | §8 | the age cap shown enforcing | **nothing in the walk ages a row past the cap, so PL11 is never shown enforcing.** To see it, one `policy_event` row was inserted directly into the throwaway store with `at = now − 100 days`; `policy events` — which is itself the on-demand trim trigger (DEF-030) — then printed `trim    1 row(s) past the age cap removed` and the row was gone. Re-run and reproduced 2026-10-21. **Amended below (T24 re-walk, 2026-10-21)** — PL11 is demonstrated now: the QA case [QAPL-18](../../test_suites/aeon-policy.md) plants a past-cap row and the verb prints the `trim` line | Med | new |
| F-14 | §8 | the audit's third state is reachable whenever a listing comes back empty | the honesty line fires only at a **true depth 0**. `report.Depth` counts the whole table, so a filtered-empty listing (`--subject` matching nothing) prints the footer alone — `depth 1 · cap 90 days · oldest …` — with no rows and no explanation. Separately, the `unaudited  the ad-hoc step actor … cannot be an audit subject` line never printed: `runtime.DefaultHandlerName` is `step-ad-hoc`, which the identifier policy **accepts**, so `agent:step-ad-hoc` is a valid subject — this contradicts the caveat T16's record carried. **Amended below (T24, 2026-10-21): the two claims come apart — the filtered-empty half is closed (T22 + T23), the `unaudited` half stands as measured, and T16's caveat was wrong** | Med | new |
| F-15 | does-not-show | "Four of the seven types are schema-only until S14, S18, S17 and S20" | `spec_stubs.go`'s `init()` registers **six** stubs — CommandAccess (S11), SkillAccess (S14), EffectClass (S16), CostCap (S17), RouteAccess (S18), TriggerAccess (S20) — and EffectClass/S16 appeared nowhere in this document. Amended below | Low | new |
| F-16 | does-not-show | "No policy editor. The CLI is enough to demonstrate this; the app is an S9 amendment" | stale: `policy` is one of the nine bench tabs (`aeon/ui/src/app/routes.ts:25`) and `PolicyPage.tsx` calls exactly the four read methods — a read-only window, no form, no authoring path (21 `policy.test.tsx` cases pin it, including "the page has no authoring affordance"). Amended below | Low | new |
| F-17 | §3 | "a `Client` spy proving the command never spawned (PL9)" | no such spy exists in a walk. What proves it: the workspace stayed empty (`ls -A` shows no `PROOF-BASH-RAN`, no `SURVEY.md` — nothing spawned) and the transcript carries the refusal sentence; the ladder then caught the step's self-declared `ok` | Low | new |
| F-18 | wire | (T16) `policy.why` requires `type` on the socket | reproduced on a live AF_UNIX probe: omitted `type` → `-31400 the policy read was refused · unknown policy type ""` (the CLI's `tool_access` default does not carry to the wire); any call before `initialize` → `-31001 connection is not initialized`; `policy.add` / `policy.remove` → `-32601 … is not registered` | Med | T16, re-verified in the 2026-10-21 walk |
| F-19 | build | `make build` / `make dev` as the record's entry point | both fail on this box unless `CGO_CFLAGS` points at a local `sqlite3.h` shim (`CGO_CFLAGS=-I$PWD/../.tmp/cgo-shim`, from this checkout's `aeon/`): otherwise `sqlite-vec.h:7:10: fatal error: sqlite3.h`. `make -C ui check-deps` reports webkit2gtk-4.1, gtk+-3.0 and librsvg-2.0 missing, which is why the app halves are marked unexercised | Low (environment) | new |
| F-20 | §8 | (not predicted — surfaced by the F-14 re-walk, 2026-10-21) | the filtered-empty sentence is chosen by **`subject != ""`**, not by whether the table holds anything: on a **genuinely empty** store, `aeon policy events --subject agent:nobody` prints `depth 0 · cap 90 days · oldest (none)` **plus** `honesty  no policy_event rows matched "agent:nobody" …` and never the `eventsEmptyHonesty` log sentence. Both readings are true ("this filter matched nothing" is the more specific fact about the question that was asked), and the unfiltered run on the same store prints the log sentence verbatim — so the log's warning is unreachable only while a filter is in force. Recorded, not fixed: it is T23's spec'd gate, and the CLI is not lying | Low | new (T24 re-walk) |

**Severity triage.** 19 rows: **0 high**, 9 medium, 10 low — nothing here goes to the loop's final report
as a high finding. Three of the mediums came from this walk — **F-5** (the grant/why identity never matches the
one a step carries), **F-13** (the 90-day cap is never shown enforcing) and **F-14** (a filtered-empty audit
listing explains nothing) — and **F-13 and F-14 are the two the verifier should look at**, because they are
behaviour gaps, not wording gaps: the demo text can state both, only a code change closes F-14's filtered
`Depth` and makes F-13's `trim` reachable without a hand-inserted row. The other six mediums (F-1, F-3, F-4,
F-6, F-7, F-18) are the T16-carried wording and verb-form deltas — F-4 newly detailed here — each re-confirmed
against the checkout rather than inherited. *(T24, 2026-10-21: the table is **20 rows** now — the F-14 re-walk
added F-20, a Low — so the triage reads **0 high, 9 medium, 11 low**. No high finding, so nothing goes to the
loop's final report as a high.)*

**Amended 2026-10-21, the deferred-findings gate (loop `aeon-s10-policy`, T18–T23).** The user picked F-13 and
F-14 at that gate and the code has moved under them: **T22 `6ff60715`** (PR #116 — `PolicyEventStats` honours
the `--subject` filter) and **T23 `e6418cc0`** (PR #118 — a filtered-empty listing prints its own honesty
sentence). The two rows keep their original text, because that is what the prediction got wrong and it is the
point of the table; the amendments below record what changed and what this checkout's binary printed. Nothing
else in the table moved. One row was **added** by the re-walk (F-20) — a finding that contradicts the doc is a
row, never an edit to the prose.

### The F-13 amendment — PL11, now demonstrated (T24 re-walk, 2026-10-21)

**The cap is shown enforcing, and what demonstrates it is a case rather than a paragraph.** QA case
[**QAPL-18**](../../test_suites/aeon-policy.md) plants one `policy_event` row at `at = now − 100 days` — a
disclosed manual write, since no surface authors a past-cap row — and `aeon policy events`, which is itself the
on-demand trim trigger (DEF-030), prints the `trim` line and the row is gone. The QA run of 2026-10-21 executed
it as **pass**: "first `events` prints `trim    1 row(s) past the age cap removed`, row gone (sqlite truth),
depth drops, oldest moves inside cap".

**Re-run on this checkout** (`HEAD e6418cc`, Go 1.26.2; binary built from `aeon/` with
`GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS=-I$ROOT/.tmp/cgo-shim`; `XDG_STATE_HOME` exported to
a throwaway directory before every binary run; the four runtime rows below are a live `aeon loop start` on the
repo's own fixture replay — **the fifth row was inserted by hand**, `agent:walk` / `refusal` /
`at = 2026-07-13T17:27:03.000000000Z`, keeping the forced nine-digit fractional layout that keeps TEXT
comparison chronological):

```text
$ sqlite3 "$XDG_STATE_HOME/aeon/state.db" "SELECT count(*) FROM policy_event;"      # 4 runtime + 1 planted
5

$ aeon policy events                                                                # the verb IS the trigger
trim    1 row(s) past the age cap removed
4      2026-10-21T17:26:51.245746331Z  disagreement  agent:default  -                         refused: agent:default · no allow matched · default deny (PL5)
3      2026-10-21T17:26:51.245746331Z  refusal       agent:default  -                         refused: agent:default · no allow matched · default deny (PL5)
2      2026-10-21T17:26:51.245746331Z  disagreement  agent:default  "no-shell-anywhere"       refused: agent:default · tool_access "no-shell-anywhere" · target all-agents · source operator · no expiry (an allow exists: "grant-bash-to-default" · agent:default — denied anyway, PL4)
1      2026-10-21T17:26:51.245746331Z  refusal       agent:default  "no-shell-anywhere"       refused: agent:default · tool_access "no-shell-anywhere" · target all-agents · source operator · no expiry (an allow exists: "grant-bash-to-default" · agent:default — denied anyway, PL4)
depth 4 · cap 90 days · oldest 2026-10-21T17:26:51Z                                        exit 0

$ aeon policy events                                     # again — the trim is done, so no `trim` line
depth 4 · cap 90 days · oldest 2026-10-21T17:26:51Z      (the same four rows, no trim line)  exit 0

$ sqlite3 "$XDG_STATE_HOME/aeon/state.db" "SELECT count(*), min(at) FROM policy_event;"
4|2026-10-21T17:26:51.245746331Z                          # the planted row is gone; oldest is inside the cap

$ aeon policy events --subject agent:walk                  # the trimmed subject, asked by name
depth 0 · cap 90 days · oldest (none)
honesty  no policy_event rows matched "agent:walk" — that is the filter's answer, not the log's: run aeon policy events without --subject for every subject
```

The footer's `cap 90 days` and the rule that deleted the row are the same constant
(`policy.PolicyEventAgeCap`, rendered through the verb's `capLabel()`), which is what makes the line evidence
rather than decoration. **F-13 is closed as a demonstration gap.** Its row above stays as written: "nothing in
the walk ages a row past the cap" was true of the walk *as predicted*; the walked record
([`docs/aeon/DEMO.md`](../DEMO.md) §S10 step 7) now has a step that does, and an executable case that re-runs
it.

### The F-14 amendment — the row's two claims come apart (T24 re-walk, 2026-10-21)

**Claim one — "a filtered-empty listing prints the footer alone, with no explanation" — is CLOSED**, by two
merged tasks:

- **T22 `6ff60715`** (PR #116, issue #115) — `PolicyEventStats(ctx, subject)` reads the depth and the oldest
  instant **for the listing that was asked for**, so `depth` stopped being a neighbour's number.
- **T23 `e6418cc0`** (PR #118, issue #117) — `eventsFilteredEmptyHonesty(subject)` in `cmd/aeon/policy.go`: a
  listing empty *under a filter* prints its own sentence, and `eventsEmptyHonesty` is reserved for a genuinely
  empty **unfiltered** table.

What the same binary prints on this checkout (first pair: two `agent:bob` rows **inserted by hand** into a
second throwaway store, in QAPL-18's disclosed form, so that one subject's answer differs from the table's —
the table holds 5 rows there, bob holds 2):

```text
$ aeon policy events --subject agent:bob                      # the table's depth is 5; bob's is 2
4      2026-10-20T08:00:00.000000001Z  disagreement  agent:bob  -   refused: agent:bob · no allow matched · default deny (PL5)
3      2026-10-20T08:00:00.000000000Z  refusal       agent:bob  -   refused: agent:bob · no allow matched · default deny (PL5)
depth 2 · cap 90 days · oldest 2026-10-20T08:00:00Z                                    exit 0

$ aeon policy events --subject agent:nobody                   # rows exist — for other subjects
depth 0 · cap 90 days · oldest (none)
honesty  no policy_event rows matched "agent:nobody" — that is the filter's answer, not the log's: run aeon policy events without --subject for every subject
                                                                                                                  exit 0

$ aeon policy events --subject agent:bob --depth 1             # --depth bounds the ROWS, not the answer
4      2026-10-20T08:00:00.000000001Z  disagreement  agent:bob  -   refused: agent:bob · no allow matched · default deny (PL5)
depth 2 · cap 90 days · oldest 2026-10-20T08:00:00Z                                    exit 0
```

The silence the row described is gone, and two properties of the sentence are worth pinning. It **fabricates no
count**: the only number in that run is the footer's measured `depth 0`, and the unfiltered total is *pointed
at* ("run … without `--subject`") rather than invented beside a run that never asked for it. And the subject
rides `%q` (the `eventsUnauditedAdHoc` house pattern, SEC-033's class), so caller-supplied text cannot put raw
control bytes on an operator's terminal.

**Claim two — the `unaudited` line — stands exactly as it was measured, and this row now says plainly that
T16's caveat was wrong.** `runtime.DefaultHandlerName` is `step-ad-hoc` (a hyphen), so the probe subject the CLI
wires, `agent:step-ad-hoc`, **is** identifier-valid; the boundary never refuses it, and the
`unaudited  the ad-hoc step actor … cannot be an audit subject` line therefore **never prints through the CLI**.
The re-walk grepped zero occurrences across every run — empty store, seeded store, live loop. The line is
conditional by design, not a dead branch: it prints for an actor the boundary cannot record, and that is a name
the runtime **does not mint** — which is where DEF-041's blind-zero story actually belongs. T16's record carried
the opposite caveat (an actor named `step:ad-hoc`, with a colon, refused as a subject); T17 measured the mint and
found the caveat false, and T23 pinned the boundary's verdict with a comment at the probe plus two `unaudited`
subtests that pass unedited.

**Scope, said so nobody reads "closed" too widely.** Both sentences are the **CLI verb**. Over the socket,
`handlePolicyEvents` still answers a filtered-empty request with `{rows: [], depth: 0, oldest: null}` and no
honesty analogue (the JSON is machine-readable and the caller holds the filter it asked for), and the app's
Events panel is still headless-blocked on this machine (QAPL-16/26). Recorded in the review ledger as **DEF-086**
and left where it belongs — a presentation-side gap on the daemon/app path, not a claim that F-14's CLI half
ever printed the log sentence for a filter.

### The QA hand-off — recorded here, executed by `qa-engineer-loop`

`docs/test_suites/aeon-policy.md` is QA's artifact: `qa-engineer-loop` (and `qa-test-spec-loop` in a POC loop)
own it, and the `QAPL` ids are stable — append, never renumber. **This task did not edit it; it is
byte-unchanged in this diff.** Two cases now pin behaviour that no longer exists, and four behaviours this gate
added have no case at all, so the exact replacements are recorded here for the QA dispatch that reads this
table:

| Case | Says today (now stale) | Must say |
| --- | --- | --- |
| **QAPL-17** (~318–321) | "`depth` is the **whole table's** row count, not the page size (`--depth N` limits only the listing, default 20)" | "`depth` is the depth **of the listing asked for** — the whole table's when no `--subject` is given, the one subject's when one is; `--depth N` bounds how many rows print (default 20)" |
| **QAPL-24** (~407–425), expectation (2) | "**the gap, measured:** the footer alone — `depth 1 · cap 90 days · oldest <instant>` — with no rows, no `honesty` line and no 'none matched'. `report.Depth` counts the **whole table**" | the closed behaviour: `depth 0 · cap 90 days · oldest (none)` **followed by** `honesty  no policy_event rows matched "agent:nobody" — that is the filter's answer, not the log's: run aeon policy events without --subject for every subject`, with `eventsEmptyHonesty` reserved for a genuinely empty **unfiltered** table. Keep unchanged the statement that the `unaudited` line stays unreachable through the CLI because `step-ad-hoc` is identifier-valid |
| **Coverage table** (~490, "The walk's two behaviour-gap mediums (F-13, F-14)") | "QAPL-18, QAPL-24 — carried as cases, not folklore" | **both closed by this gate**: F-13 proven by **QAPL-18** (the `trim` line, from a planted past-cap row), F-14's filtered half by **QAPL-24 (2)** (footer + the filter-naming sentence) and by the new `--depth`-versus-`depth` assertion folded into **QAPL-17** |

**New cases to append from `QAPL-29` upward** (ids are QA's to assign), one per behaviour this gate added:

1. **The database refuses an `allow` from a non-operator source at write** (SEC-022, migration **0018**),
   proven the way QAPL-18 proves the cap — a disclosed manual `INSERT`: `allow` + `source='agent'` fails with
   `CHECK constraint failed`; `deny` from each of the four sources still lands; and a store left holding a
   pre-0018 forged row **refuses the open** and rolls back whole.
2. **A hand-half-updated frozen manifest is refused on replay**, with the digest-mismatch line in the daemon
   log (DEF-039): edit one policy inside a stored `context_manifest` and leave `policiesHash` alone — the
   replay must refuse, never evaluate the tampered set.
3. **A zero-policy step still refuses after an operator `allow` is added during its suspension** (DEF-042): the
   set is FROZEN, not re-read, so the resumed step answers with the verdict its frozen set produces.
4. **`EXPLAIN QUERY PLAN` shows the per-target acceptance read using `idx_policy_acceptance_target`** (DEF-027),
   with the control that a `WHERE` on an unindexed column must **not** name that index — so the assertion
   measures the planner instead of trusting it.

### The §3 deny-wins check (4-way)

§3 is the slice's thesis, so it got an explicit check rather than a tick: **is the refusal in §3 a real deny
beating a real allow, or a mis-authored allow that nothing was ever going to grant?** Four independent
propositions were verified — same query, and three controls that differ in agent, tool and authoring order.
All four were re-run on 2026-10-21 on a fresh throwaway store; every line below is that run's output.

1. **Same query, before and after the deny** (the §1–§3 sequence itself). `policy why
   --agent golang-engineer-loop --tool bash` answers `allowed:` naming `grant-bash-to-go`; after
   `no-shell-anywhere` is stored, the *same command* refuses and names the allow it overrode. Both rows are
   still in `policy list` — the allow was never deleted, never ignored.
2. **Control A, allow authored first.** `ctrl-a-allow` (agent:ctrl-a, grep) answers `allowed:` with no deny
   in the world; adding `ctrl-a-deny` for the same agent and tool flips it to a refusal naming
   `"ctrl-a-deny" … (an allow exists: "ctrl-a-allow" · agent:ctrl-a — denied anyway, PL4)`.
3. **Control B, deny authored first.** `ctrl-b-deny` stored before `ctrl-b-allow`, same agent and tool →
   the identical refusal. Two authoring orders converge on one answer, so the result is not an ordering
   accident and not a stored-row artifact.
4. **The identity a step actually carries** (F-5's control). `default-allow-grep` (agent:default, grep)
   answers `allowed:`; then `no-grep-anywhere` (all-agents deny) refuses it naming the overridden allow.
   This is the class a running step asks as, and it is why the cumulative record's step 2 grants to
   `agent:default` instead of the predicted `agent:golang-engineer-loop`.

```text
$ aeon policy why --agent ctrl-b --tool grep          # control B: deny stored FIRST, then the allow
refused: agent:ctrl-b · tool_access "ctrl-b-deny" · target agent:ctrl-b · source operator · no expiry (an allow exists: "ctrl-b-allow" · agent:ctrl-b — denied anyway, PL4)

$ aeon policy why --agent default --tool grep         # the runtime-agent class, after the all-agents deny
refused: agent:default · tool_access "no-grep-anywhere" · target all-agents · source operator · no expiry (an allow exists: "default-allow-grep" · agent:default — denied anyway, PL4)
```

**Runtime half (PL9), from the walk's live loop:** a step under the same broad deny attempted `bash` and
then `write_file`; both `tool_result`s carried that same sentence byte-for-byte (one formatter, D9), the
workspace stayed empty — the command never spawned — and the ladder refused the step's self-reported `ok`
because the declared artifact `SURVEY.md` was not written.

---

## What this demo does **not** show

- **No `bash` pattern matching.** `CommandAccess` has its schema and nothing else here. **S11** brings the
  366-rule corpus and the matcher, because the matcher is a measurement over real patterns.
- **No tool namespaces.** `ToolAccess` governs names; S12 decides which tools *exist* for whom.
- **No agent skills, routes, cost caps or triggers.** Six of the seven types are schema-only until their
  evaluators land — CommandAccess (S11), SkillAccess (S14), EffectClass (S16), CostCap (S17), RouteAccess
  (S18), TriggerAccess (S20) — and each one **refuses rather than permits** until then.
  *(Amended by T17's walk, 2026-10-21. As predicted this read "Four of the seven types are schema-only until
  S14, S18, S17 and S20"; the registry in `spec_stubs.go` registers six stubs and named no EffectClass/S16
  anywhere in this document. Finding F-15.)*
- **No policy editor.** The CLI is the only authoring surface. The app has a **read-only** window onto policy
  state since T16 — `policy` is one of the nine bench tabs (`aeon/ui/src/app/routes.ts:25`) and
  `PolicyPage.tsx` calls exactly the four read methods, with no form and no authoring path (ruling R4).
  *(Amended by T17's walk, 2026-10-21. As predicted this read "No policy editor. The CLI is enough to
  demonstrate this; the app is an S9 amendment" — the app window has since shipped, so "the app is an S9
  amendment" was stale. What remains true is the half that mattered: no editor, no authoring outside the CLI.
  Finding F-16.)*
- **No authoring over HTTP or MCP**, and there never will be (PL15).
- **The 90-day age cap: shown enforcing since the findings gate, and §8 as written still does not show it.**
  §8 prints `cap 90 days`, and the printed promise and the enforced one share one constant
  (`policy.PolicyEventAgeCap`, retention.go, through the verb's `capLabel()`), but nothing in §8 as written ages
  a row past the cap — until a row is inserted past it, no `trim` line prints. *(Added by T17's walk, 2026-10-21;
  finding F-13. The demonstrated form, with the insert and the
  `trim 1 row(s) past the age cap removed` output, is step 7 of
  [`docs/aeon/DEMO.md`](../DEMO.md) §S10.)* *(Amended by the deferred-findings gate, 2026-10-21 — T24's record.
  The half that was true stays: **no step of this prediction shows it**, and no surface authors a past-cap row,
  so the insert is and remains a disclosed manual write. What is no longer true is the heading: PL11 is now
  **demonstrated**, by [QAPL-18](../../test_suites/aeon-policy.md) and by the run quoted in the F-13 amendment
  above, which printed `trim    1 row(s) past the age cap removed` and left the table with
  `4|2026-10-21T17:26:51.245746331Z` — the planted row gone, `oldest` inside the cap, no `trim` line on the
  second run.)*
- **A filtered-empty audit listing is not the same as an empty log — and the CLI now says which you have.**
  The honesty line prints only at a true depth 0; `--subject` matching nothing prints the whole-table footer
  and nothing else. *(Added by T17's walk, 2026-10-21; finding F-14.)* *(Amended by the deferred-findings gate,
  2026-10-21 — T24's record: **closed**, by T22 `6ff60715` (the footer's `depth` is the listing's, so a
  filtered run answers `depth 2` where its table holds 5) and T23 `e6418cc0` (a filtered-empty listing prints
  `depth 0 · cap 90 days · oldest (none)` **and** its own sentence, `honesty  no policy_event rows matched
  "agent:nobody" — that is the filter's answer, not the log's: run aeon policy events without --subject for
  every subject`). What is unchanged is the second half of the original row: the `unaudited  the ad-hoc step
  actor …` line never prints through the CLI, because the minted actor `agent:step-ad-hoc` **is**
  identifier-valid — T16's caveat was wrong, and the row above says so.)*

The car's first wheel is on. **S11 gives it the measured corpus; S12 gives agents tools of their own.**
