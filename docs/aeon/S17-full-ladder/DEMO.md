# S17 The full ladder — the demo

**Stage 3 · Car, fourth of four. This is the car.**

**This is the first demo in the plan that is a product claim rather than a capability.** Every earlier one showed a
mechanism behaving correctly. This one has to show the **method** working — and a ladder where nothing ever rejects has
not been demonstrated.

So it has a deliberate defect in it.

**You need a throwaway GitHub repository** with a small Go module and a token. Set aside an hour; most of it is waiting.

---

## 1. Give it a real feature and one gate

Something small and real. Not a toy:

> *Add a `--format {text,json}` flag to `cmd/report`. Default `text`, current behaviour unchanged. `json` emits the same
> data as a single JSON object. Cover both in tests.*

```
cd ~/throwaway-repo
aeon loop start --blueprint dev-loop "add a --format flag to cmd/report"
```

**You should see it work through the top of the ladder without you:**

```
loop 6d02  dev-loop   waiting
  clarify      ok           prompt.md                   $0.0104
  discovery    ok           discovery.md                $0.0870
  legal        not-needed   when: flags.legal_review
  plan         ok           plan.md, tasks/             $0.1832
  plan-gate    waiting      approve-plan
  build        pending      iterate: tasks (3)
  qa           pending      when: flags.integration_surface
```

**Read `plan.md` and `tasks/` before answering.** This is the one approval gate the method is built around — the
protocol's own summary is *"investigates → plans → (one approval gate) → builds task-by-task → verifies each task →
commits, running unattended."* Everything after this point runs without you unless something goes wrong.

**Check the task dependencies while you are here:**

```
aeon loop show 6d02 --tasks
```

**You should see them in dependency order**, and if the plan declared a `depends_on`, the dependent task is after the one
it needs. That order was settled at **load**, by topological sort — not discovered while running. Try it with a bad plan:

```
aeon plan check tasks/ --simulate-cycle
```

**You should see a refusal naming the cycle.** v1's reason is the right one: a bad `depends_on` produces *"a task that can
never complete, and catching it here names the plan instead of surfacing a mystery at merge time."*

Now approve.

---

## 2. Watch a task get caught being wrong

**This is the section the slice exists for.** The plan's second task carries a deliberate defect — the `json` output
omits a field the spec required. Nothing was told about it.

**You should see `verify` reject it:**

```
  build   iterate: tasks (3)
    T1    ok        3 steps                              $0.41
    T2    running
      issue      ok      #21
      implement  ok      cmd/report/format.go
      verify     REJECT  json output omits 'generated_at'  (retries 1/2)
      implement  ok      cmd/report/format.go   ← re-entered
      verify     ok
      security   ok
      push       ok
      pr         ok      #22
      review     ok
      merge      ok      squash a91f3c
    T3    pending
```

**Four things to check, and each is a different property:**

**The reject routed to `rework: implement`**, not to the top of the lane. A send-back re-enters the step that can fix it.

**The redo carried the reason.** Check:

```
aeon step show <the second implement step> --input
```

**You should see the rejection's text in the step's input.** A bare retry re-runs the same call that just failed and
spends a round reproducing the same output (L32) — so the reason travels with the redo.

**The counter went to 1 of 2, not to the cap.** One send-back is normal; the cap is for a task that cannot converge.

**And T1's and T3's counters are untouched.** Check:

```
aeon loop show 6d02 --counters
```

```
  task   retries  security_rounds  review_rounds
  T1     0/2      0/2              0/2
  T2     1/2      0/2              0/2
  T3     0/2      0/2              0/2
  loop   total_failed_gates 1   qa_rounds 0/2   splits_done 0
```

**Counters are two-level** (L28), and this table is why. A per-loop-only `review_rounds` would mean the **fifth task
cannot be reviewed at all**, because the first two spent the budget — and it presents as a loop escalating for no visible
reason on a task nobody reviewed once. The loop-level `total_failed_gates` exists for the opposite failure: it stops a
twenty-task loop spending forty rounds and calling it progress.

**It failed if** the reject did not happen. A ladder where `verify`, `security`, `review` and `qa` only ever pass has not
been demonstrated — they are decoration until one of them sends something back.

---

## 3. `unverified` is neither pass nor fail

Force the case the protocol names explicitly:

```
aeon loop start --blueprint dev-loop --limit-memory 64m "add a --format flag to cmd/report"
```

The linter gets OOM-killed. **You should see a third outcome:**

```
      verify     UNVERIFIED  golangci-lint OOM-killed (exit 137) at --concurrency=1;
                             lint UNVERIFIED  (retries 1/2)
      implement  ok          ← re-entered to make the check RUN
```

**`unverified` is not `pass` and not `fail`.** The protocol says it in one line: *"lint is unverified, which is not the
same as clean. Do not report `verification=pass` on an unverified gate."*

**Check the record:**

```
aeon step show <the verify step> --json | jq '.outcome, .reason'
```

**It must never read `pass`.** A linter that was killed found nothing **because it did not run**, and reporting that as
clean is the most plausible lie available — it looks like a green check.

**And notice the redo's instruction differs.** After a `fail`, the redo says *fix the finding*. After an `unverified`, it
says *make the check run*. Those are different work, and collapsing them wastes a round.

This is [P31](../ARCHITECTURE.md#8-product-decisions)'s **fifth** appearance — after cost, the `bash` receipt, blueprint
validation and connection liveness. Five separate places, five designs that refused to collapse an uncertain answer.

---

## 4. A task that cannot converge escalates — and its siblings do not

Take a task the model genuinely cannot get right and let it burn its retries.

**You should see it stop at the cap:**

```
    T2    ESCALATED   retries 2/2 exhausted at 'verify'
                      then: escalate
  loop  waiting   escalation: T2
```

Answer the escalation. **Then check T3:**

```
  T3    ok    3 steps
```

**T3 ran normally with its own full budget.** That is the two-level split earning its keep.

Now the same thing unattended:

```
aeon loop start --unattended --blueprint dev-loop "…"
```

**You should see the escalation become a block:**

```
loop 8f11  blocked
  T2 retries 2/2 exhausted at 'verify' — escalate collapsed to block (loop is unattended)
```

**Escalating to a human nobody can reach is a loop that stops without saying why** (L29). Same collapse as an
unattended `ask` becoming a deny, for the same reason — and it says which it was, so nobody debugs the wrong thing.

---

## 5. Review gates on state, not threads

Look at what the merge gate actually read:

```
aeon step show <the merge step> --gate
```

```
  merge gate
    checks:  green  (3 of 3)
    review:  APPROVED, no outstanding REQUEST_CHANGES
    threads: not queried — this forge reports no thread API
  deferred findings → docs/unresolved-pr-comments.md (2 entries)
```

**The gate is on review *state*.** Not every forge can resolve conversation threads, and gating on something a forge may
not implement makes the gate unreachable there. State works everywhere.

**Deferred non-blocking findings went to a durable backlog file**, not to merge-blocking threads. Read it: two entries,
each naming its PR. A finding worth recording and not worth blocking on needs somewhere to live, or it becomes a
blocker by default and the review lane becomes the thing people route around.

---

## 6. The feature is merged, and the record is in the repository

```
git log --oneline feature/6d02-format-flag | head -5
gh pr list --state merged
ls plan.md tasks/ discovery.md
go test ./... && go run ./cmd/report --format json | jq .
```

**You should see:** three squash commits on the feature branch, three merged PRs, three closed issues, **and the loop's
own artefacts committed** — `plan.md`, `tasks/`, `discovery.md`. The record of what it was asked to do lands with the
work, so a reader six months later can see the reasoning and not just the diff.

**And the feature works.** `--format json` emits the object, `--format text` is unchanged, both are covered.

---

## 7. Read the whole thing as a person

Open the feature branch's PRs and read them in order.

**You should be able to reconstruct what happened without Aeon**: what was asked, what was planned, which task did what,
what the reviewer said, what came back and why, and what was deferred. Each PR carries **one authored paragraph** and
correct identifiers throughout (S16's L3).

**This is the actual product claim.** Not that the mechanism ran — that a competent person reading this repository in a
year can tell what was decided and why. If they cannot, the method did not work regardless of what the loop reported.

---

## 8. Run it from the app — the Full ladder panel

Open the app: **Test bench → Demos → "Full ladder (S17)"**. The same sections, run from where they will be
consumed (AGENTS §4b) — as a **reader**, never a decider.

**Run ladder demo** sends only reads — `blueprint.validate`, `loop.show`, `step.show` (+ the page-wide
`routes.list` probe). The plan gate arrives on the `question.asked` event stream and renders **verbatim**,
`[approve, revise, abort]` shown as the text the daemon wrote. **No button on the panel answers anything** —
answering is `aeon answer <loopId> …`, typed by a person at the plan gate that exists precisely because a
person decides. `abort` included.

**The counters table is the D2 claim in pixels**: it renders from the daemon's `loop.show` rows — a task at
`1/2` beside two at `0/2` — and an `unverified` verify renders as `unverified`, never a green tick. A counters
table the page authored itself would be the prettiest lie in the bench.

**Run ladder replay** shows the suite-proven rows — the four refused `depends_on` classes, the five-task
budget, the two-clause block, the stored `unverified`, the engine brackets, the threadless merge gate, the
append-only backlog, `abort` landing `aborted` — each labeled REPLAY with its Go test. The model-call and
GitHub halves say **`owed`** whenever the box lacks the credential, and an owed row never wears a met pill.

### §8 record — walked 2026-10-26 (T10)

Rig: **vitest level against the fake daemon** — the same discipline the
S14/S15/S16 panels keep (`aeon/ui/test/bench-s17-ladder.test.tsx`,
**7 cases, written first and measured red 7/7 before the panel existed**,
then green 7/7; the S16 panel's 6 cases re-run green alongside). What
that means, precisely:

| Claim | Where it was actually seen |
|-------|---------------------------|
| validate row quotes the counts, `met` only at 0 invalid / 0 unverifiable | **LIVE through the fake daemon** (vitest, real component, real bridge) |
| the plan gate verbatim with `[approve, revise, abort]` as TEXT, **no button answering it** | **LIVE through the fake daemon** — `question.asked` (kind `gate`) emitted as the shell delivers it; `queryAllByRole("button", /^approve$|^revise$|^abort$|^answer$/i)` finds **nothing**; the row carries `aeon answer <loopId> approve\|revise\|abort` as text |
| counters render FROM the daemon rows (D2 in pixels) | **LIVE through the fake daemon** — `t2 · retries 1/2` beside `t1 · retries 0/2` / `t3 · retries 0/2`, `loop · total_failed_gates 1/10` — the caps ride the view (`loopCounterCaps`, filled from `loop.Table()` in Go, red-first `TestLoopCounterCapsRideTheView`); the page holds no ceiling constant |
| the redo’s reason is in the input; `unverified` never reads pass | **LIVE through the fake daemon** — the first user message from `step.show`'s transcript is rendered and asserted to carry `redo of "verify": retry 1 of 2 — …`; the starved check renders `stored outcome: unverified … (neither pass nor fail …)` |
| cold loop reads `not runnable here` naming this file §1 + the operator command | **LIVE through the fake daemon** (`store: not found`) |
| **no mutation ever leaves the page** | **LIVE** — methods after live + replay ⊆ {`blueprint.validate`, `loop.show`, `step.show`, `routes.list`}; the ban list (`question.answer`, `loop.start`, `loop.answer`, `gate.answer`, `step.continue`, …) asserted explicitly |
| every replay row labeled with a grep-verified Go citation | **LIVE at vitest** + the S16 T13 audit redone: **41 cited test names, 41 exist** (scripted grep over `internal/`/`cmd/`); the joined replay text matches all eight spec regexes; the owed row says `owed` and carries no verdict of its own |
| the real app window, the real daemon behind it | **OWED** — the Tauri shell-open: this box lacks Tauri's system libraries (`make -C ui check-deps`, the S14/S15/S16 precedent); owed pass = open Test bench → Demos, run both buttons against the T9 rig's real daemon, confirm the gate row renders the walked prompt byte-for-byte |

Two findings the panel earned on the way, both honest:

- **One listener per page.** The first cut added a second `onEvent`
  subscription for the gate kind and silently stole the S16 ask listener's
  callback (the fake models the bridge's single handler — the test caught
  it: S16's ask row went blank). Both row kinds now ride ONE listener.
  The trap generalizes: the bench's fake is honest about the bridge being
  single-handler; a page that treats it as a multiplexer breaks the panel
  that shipped before it.
- **A UI pin caught my own slice.** `blueprint-flow.test.tsx` still pinned
  the pre-T7 build ladder (`issue, implement, verify, security`); the
  re-authored unit cuts a per-task `branch` step, and the test had been
  red since T7 — the slice's own UI suite was not re-run at T7 and the
  miss surfaced here. The pin now tracks the shipped file (`issue, branch,
  implement, verify`), which is what a pin is for.

Residual reds on the working tree that are NOT this panel's: `port.test`
(ui-kit release pin) and `product.test` (keyboard user-menu) fail only
against THEIRS' uncommitted `ui/package.json`/kit work — both pass at
HEAD in a read-only worktree, verified 2026-10-26; the same
uncommitted-0026 situation the loop schema pin records. Full vitest
**470/472** with those two owed to their author's half-finished bump.

---

## Walk record — 2026-10-26 (T9)

Rig: throwaway `XDG_STATE_HOME` + `LOCALRPC_ENDPOINT_PATH` + one daemon;
real git against a **local bare origin**; the provider is the repo's own
replay technique (DEMO.md "How to run it"): a scripted handler on
127.0.0.1 answering per-agent SSE handoffs — the MODEL is the scripted
part, the daemon, protocol, store, policy, ladder engine and CLI are all
the real ones. **No GitHub token on this box** — the whole outward half
says so where it applies. Tasks were seeded by editing the loop's
`runtime_json` + `task` rows via `sqlite3` because **no production writer
exists** (F-α below); every row below says what was LIVE (executed, read
back from the store/git/process), REPLAY (a named Go test is the proof),
or OWED (not done — and the exact re-walk waits below).

| § | Claim | Status | What was actually seen |
|---|-------|--------|------------------------|
| 1 | the ladder runs the top without you and stops at the ONE gate | **LIVE** | `loop show` verbatim: `clarify ok prompt.md · discovery ok · legal not-needed (when: flags.legal_review eq true) · plan ok plan.md, tasks/ · plan-gate waiting`; question row `Approve the plan and start building?` + `["approve","revise","abort"]`; approve → `plan-gate ok` and the ladder continues |
| 1 | 3 tasks appear and the build lane iterates them | **OWED→rig-seeded** | `build pending iterate: tasks (0)` on the real loop — nothing seeds tasks (F-α); the ladder walks below ran with rig-seeded tasks (disclosed) |
| 1 | a real issue/PR appears on GitHub | **OWED** | no token; the boundary refuses honestly: after `approve` on the `forge.openIssue` ask (O1 row rendered with title/repository/`["approve","deny"]`), blocked `forge.openIssue: no forge wired` (local origin ⇒ nil Forge, F1 design) |
| 2 | the defect is caught, routed to `rework: implement`, not the lane top | **LIVE at mechanism level** + REPLAY | implement+verify ladder with the shipped lane shape (`rework: implement`, `on_reject: {counter: retries, max: 2, then: escalate}`): verify reject → rework ran `implement` again (not the lane top); `TestBuildLaneRoutesAllThreeVerifyOutcomes` pins the full ladder |
| 2 | the redo’s input contains the rejection’s reason | **LIVE** | `aeon step show 31 --input`: `redo of "verify": sent back — unverified: the linter was killed before it ran — could not determine` under `Task item: t1` — AND only because the walk caught F-δ: before the fix every live reject arrived reasonless and the door itself refused: `bare retry refused: … the redo of "verify" has no reason … (L32)` |
| 2 | counter 1 of 2, siblings untouched | **LIVE** | `aeon loop show --counters`: `t1 retries 1/2 · t2 0/2 · t3 0/2` + `loop total_failed_gates 1/10`; two levels visible in one table |
| 2 | no gate was loosened | **LIVE** | the fix (F-δ) moved the HANDOFF REASON to the engine; the L32 bare-retry refusal stayed and was observed firing — N8 honoured |
| 3 | the stored outcome is never `pass` for a check that did not run | **REPLAY** + LIVE plumbing | `TestBuildLaneRoutesAllThreeVerifyOutcomes` unverified arm (task counter uncharged — `t1 retries 0/2` seen LIVE — ceiling +1 seen LIVE); the stored-third-value invariant pinned in T6's `verify_test.go` |
| 4 | siblings keep their budgets while one exhausts | **LIVE** | lad8: `t1 retries 2/2 EXHAUSTED`, ladder waits; attended reason verbatim: `t1 retries 2/2 exhausted at 'verify' — then: escalate: raise the cap, or accept the task cannot converge` |
| 4 | unattended escalation BLOCKS naming cap + absent human | **LIVE** | `--attended=false`: `t1 retries 2/2 exhausted at 'verify' / then: escalate — collapsed to block (loop is unattended)` — both facts in the one sentence |
| 4 | …and a human can answer the escalation | **OWED** | the question row exists (`kind gate`, `["continue","abort"]`) and the ladder waits, but `aeon answer <id> continue` is refused `is waiting on a step question` — `AnswerEscalation` has no socket door (F-γ) |
| 5 | the merge gate reads review STATE, never needs threads, records what it read | **REPLAY** | `internal/action/gate_test.go` 9 tests: threadless-fake pass + evidence fields + outstanding-request-changes 4 arms + ordering (`MergePR` absent from the fake's calls, D5 probe at ZERO queries) + `TestThreadsAreNeverResolved`; LIVE half OWED (needs the real PR) |
| 6 | abort at the plan gate yields `aborted`, not `failed` | **LIVE + F-β** | composition with a benign `on_abort` lane: `aeon loop show` prints **`aborted`** with the handler lane `ok`; no-on_abort blueprint blocks `aborted by user; blueprint has no terminal handler` (deliberate — never `failed`); the SHIPPED dev-loop aborts through `git-abort`, which dies on O1 (`forge.closeIssue … missing: issue number`) when no issue was ever opened — **F-β**, blocked not aborted, recorded |
| 6 | merged PRs land on the feature branch, record in repo | **OWED** | GitHub half |
| 7 | a person reconstructs the run from the repository alone | **LIVE to the gate** | from the repo: `prompt.md`, `discovery.md`, `legal.md`, `plan.md` (Understanding + 3 tasks), `tasks/t1..t3.md`; from rows: gate question + answer, 6–9 step rows per walk with `attempt` counts, the counters table, the events. What the person CANNOT reconstruct live: any merge, any PR, any issue (all OWED) — the reconstruction claim is only earned by the GitHub re-walk |
| — | wall time + model-call count (the hybrid decision’s payoff) | **LIVE** | clean runs: §1 top-of-ladder **4 model calls, 14 s wall**; task-level ladder (3 tasks + one unverified round) **9 calls, 12 s**; full session incl. rig and re-runs: **36 provider calls** (16 verify, 12 implement, the rest top-of-ladder), 09:05–09:25 London |
| 8 | the app panel | **T10 — recorded** | `bench-s17-ladder.test.tsx` 7/7 (red 7/7 first), 41/41 citations, gate as text with no answering control; Tauri shell-open OWED — see the §8 record below |

**Findings — the walk’s yield:**

| # | Found walking | Status |
|---|---------------|--------|
| F-α | **No production writer for `st.Tasks` or the `task` rows.** `build`'s `iterate: tasks` iterates an empty list: the shipped `dev-loop` loop answers `approve` → `build not-needed  iterate: tasks (0)` → **`done`** with nothing built. `store.CreateTask` has no production caller; no socket method, no CLI verb, and no slice from S8 to S17 assigned the job. The ladder is proven composition (tests seed tasks); the product claim needs the plan-handoff→tasks seam | **RECORDED (F-α), not repaired** — inventing the mechanism unassigned is exactly what N1 forbids; it needs its own task and spec |
| F-β | The shipped ladder cannot abort before an issue exists: gate `abort` → `on_abort: git-abort` → `forge.closeIssue` ask refused by O1 (`missing: issue number`) → **blocked**, never `aborted` | **RECORDED** — early abort is exactly when the terminal matters most; the lane needs a state the action can dispatch without a number (or a when-guard whose grammar can express evidence presence — today it cannot) |
| F-γ | `AnswerEscalation` (continue/abort) has **no socket door** — `loop.answer` reaches `AnswerGate` only; the escalation question is visible and answerable in tests but not from a terminal | **RECORDED (F-γ), OWED** — the door routes by waiting state today; one branch, like S16's F2 did for asks |
| F-δ | **The handoff's reject REASON never reached the loop engine** — `internal/runtime/loop.go` called `st.complete(..., "")` with the reason hardcoded empty at BOTH handoff paths. Every live reject arrived reasonless and died at the L32 bare-retry refusal — the §2 property was structurally unreachable on a real daemon; engine tests injected `ModelResult` directly and could not see the seam | **FIXED** (red first: `internal/runtime/reason_test.go` failed on `""`, then green; runtime suite 186/0). The N8 route: the HANDLER was fixed — the guard never moved |
| F-ε | `step.taskid` is blank on loop-dispatched steps — the step rows do not carry the ladder item (the identity lives only in engine `StepStates` keys like `build/t1/verify`) | **RECORDED** — triage needs it; today the join is by position, not row |
| F-ζ | A gate `abort` with no `on_abort` handler BLOCKS with `aborted by user; blueprint has no terminal handler` — deliberate (never `failed`, never silently `done`), worth reading before someone "fixes" it into `aborted` | **RECORDED as by-design** |

**Owed rows — the exact re-walk, first box with a GitHub token** (`AEON_FORGE_TOKEN`, throwaway repo per §"You need" above):

```
cd <throwaway repo, origin = https://github.com/<owner>/<repo>.git, seeded go module>
export XDG_STATE_HOME=/tmp/opencode/s17-live AEON_FORGE_TOKEN=<token>
aeon routes set --id fake-anthropic --provider anthropic --model claude-sonnet-5 \
  --base-url http://127.0.0.1:18877/v1 --api-key-file /tmp/opencode/s17-walk/key   # or a real route
aeon loop start --blueprint dev-loop --route fake-anthropic --slug live1 --tech go \
  --with task=t1 --with task_title="Add --format flag to cmd/report" \
  --with pr_summary="Adds --format {text,json}; text unchanged." "walk §1-§7"      # F-α: seed tasks (sqlite3
                                                                                     # update loop_blueprint.runtime_json
                                                                                     #   json_set($.tasks,…) + task rows)
aeon answer <id> approve              # §1: ONE issue, ONE PR visible; §2: script the verifier to reject task 2 once —
aeon loop show <id> --counters        #   expect t1 1/2 siblings 0/2; aeon step show <redo> --input names the reason
aeon loop show <id> --json            # §3 stored outcome of the starved check: never "pass"
# §4: attended exhaustion waits with ["continue","abort"] — answering it still needs F-γ's door; unattended blocks naming both
# §5/§6: review posts APPROVED via the review lane; the merge gate must record review_state/review_by/review_at
#        + checks/threads N/A text in the step evidence (loop show --json); after merge: git log feature/… shows the squash;
#        abort variant: answer abort AFTER the issue exists → git-abort closes it → loop status aborted
```

*(§8 is walked at T10 with its own record; its owed rows point here until the GitHub list clears.)*

---

## Validation pass — 2026-10-26 (the user read their screen; the screen admitted two wrong sentences)

The operator ran the app against **their own daemon** and read the blueprint
screen plus the ladder panel, then asked why S13-era deferrals were still on
screen. Three verdicts:

| # | What they saw | Verdict |
| --- | --- | --- |
| V-1 | `dev-loop: 1 invalid` (`lanes[git-abort].step.action` — `unknown action`) | **Their daemon is stale.** Every blueprint in HEAD validates 0 invalid (measured all eight; `aeon blueprint validate dev-loop` on a fresh build reads **81 valid, 0 invalid, 0 unverifiable**) — the `git-abort` lane carried `forge.closeOpenIssuesAndPRs` until S16 T10 (D14) refused it and T7 pointed it at `forge.closeIssue`. A binary built before those commits validates the blueprint it embeds and says `unknown action`. Re-walk: `go build -o … ./cmd/aeon`, restart the daemon, revalidate. Not a code bug — recorded, not glossed. | **Amended 2026-10-26 by V-4 — the diagnosis here was wrong.** `make dev-daemon` is `go run … serve`: it recompiles on every launch, and the running process (measured: `/proc/<pid>`, started 10:39:53) post-dated every fix. The binary was never the stale thing; the FILE it validates was — and `go build`+restart would not have helped. The re-walk command is in V-4.
| V-2 | `Agent body is unavailable until S13` on triage/brief/proposal/self-check | **Half bug, half truth.** The truth: five bodies (brief-author, product-manager, project-manager, proposal-author, task-coordinator) exist in no corpus — S13 named them follow-ups and `loop.start` refuses those lanes; `unverifiable` is the honest state (P31). The bug: the label spoke of S13 as the *future* after S13 closed 2026-10-25. Fixed with the pair of red-first cases (QAC-94): label now reads `Agent body not shipped — S13 named it a follow-up`; the at-rest `action-registry` deferral says the list was not given, not that S16 is coming; the dead `resolver-registry` label was dropped, since a label for an unemittable diagnostic is a claim nobody tests. |
| V-3 | Every cold panel row printed `not runnable here …` **twice** | **Real bug, both panels.** The row-level `<pre>` and the unavailable block each rendered `v.text`. Fixed in both; the cold cases now assert the per-row occurrence count is exactly 1 (red first at 2). QAC-95. |
| V-4 | The `unknown action` survives `make dev-daemon`, restarts — everything binary-side | **The real bug, and V-1's correction.** `make dev-daemon` = `go run -tags aeondebug ./cmd/aeon serve` — **fresh compilation every launch**, so the binary cannot be stale at start (one caveat: a long-running `serve` holds its start-time code until restarted). The daemon validates `blueprint.Resolve("dev-loop")` = the **state-directory copy** (`~/.local/state/aeon/blueprints/`, materialized 2026-10-18). Measured: that copy was byte-identical to the committed embedded revision `923e9193` (sha256 `8af923d7…`, fixture kept at `internal/blueprint/testdata/dev-loop-pristine-923e9193.yaml`) — **pristine, never edited, and unable to catch up**: the upgrade hatch matched only two-step's recorded digests, so S16 T10 retiring `forge.closeOpenIssuesAndPRs` and S17 T7 re-authoring the ladder never reached it. A correct daemon honestly validating a stale file reads exactly like a stale daemon. Fix red-first (`TestPristineStaleDevLoopUpgradesButEditedCopyStaysPinned` — red, then green; package **39/0** dated 2026-10-26): `pristineUpgrades` is now a table of committed-revision digests (each `git show <rev>:…` piped to `sha256sum`, never a guess) — edited copies stay pinned, customs untouchable. Box fixed same day: stale copy renamed to `dev-loop.yaml.stale-923e9193.bak`, re-materialized from the current embed, and re-validated **through the running daemon's socket** (initialize → `blueprint.validate`): **81 valid, 0 invalid**. The test nearly didn't test: the shipped file legitimately MENTIONS the retired action in a comment (dev-loop.yaml:189), so the first green-killer was my own `strings.Contains` gate — equality with the shipped bytes is the assertion. QAC-96. |

**Findings recorded, not fixed (N1):** an at-rest `Validate` with
`Resolvers: nil` would call `by-tech-tag` an `unknown-agent` — `invalid`
where the honest three-state answer is *defer*. Every production call site
(daemon ×2, CLI) passes the resolver list, so nothing live is wrong; the
fix belongs to whoever first needs an at-rest validator, with its own
three-state test — inventing the deferral now would test a caller that
does not exist.

Measured on this pass: blueprint Go suite 38/0 (the red-first pair included);
vitest full run 470/473 — the three reds are THEIRS' uncommitted ui-kit bump
(`port.test` + `product.test`), which pass **63/63 at HEAD** under a clean
`npm ci` in a read-only worktree. tsc clean.

## What this demo does **not** show

- **No dogfooding.** Aeon does not run this ladder on its own repository yet. A loop that breaks the daemon running it is
  a bad first demonstration, and that question belongs after S23's first-run work.
- **No second protocol.** `proposal-brief`, `development-map` and `conversational-map` remain unexpressed. `hotfix` is now
  expressible; authoring it is S23's if wanted, and the format was already proven by two blueprints (S8's L10).
- **No cleanup.** Merged task branches remain. S16's N8 and N9 stand, and the recorded incident behind them is unchanged.
- **No retrieval.** S22. Every lane still finds its own context.
- **No provider breadth.** S18. This ran on one provider.

---

## The car

Stop and take stock, because the claim has changed shape.

**The bicycle was useful for a narrow job.** The car **builds software and opens pull requests** — specified, planned,
gated, built, verified, reviewed, merged, with send-backs bounded and a record a person can read.

And the thing worth noticing is not that it worked. It is that **it caught itself being wrong** in §2, refused to call an
unrun check clean in §3, and stopped rather than guessing in §4. Those three are the same disposition showing up in three
places, and they are what separate a method from a pipeline.

What is left is reach: other providers (S18), other clients (S19), memory (S22), and a stranger getting to a running loop
in five minutes (S23).
