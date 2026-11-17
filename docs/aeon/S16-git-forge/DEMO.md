# S16 Git & forge actions — the demo

**Stage 3 · Car, third of four.** *The first slice where Aeon does something other people can see.*

**You need a throwaway GitHub repository whose history does not matter**, and a token with the scopes to open and merge
pull requests in it. Everything below lands in that repository for real.

---

## 1. A loop branches, commits, and opens a real pull request

```
cd ~/throwaway-repo
aeon loop start --blueprint task-build "add a --version flag to cmd/hello"
```

Answer the plan gate. Then watch the build lane.

**You should see the loop reach `forge.openPR` and stop, asking:**

```
  ask    Open a pull request?

         forge.openPR
           head: task/4a17/t1-version-flag
           base: feature/4a17-version
           title: [T1] Add a --version flag to cmd/hello

         effect: outward — other people will see this

         [ approve ]  [ deny ]
```

**That prompt is the point of §3 and worth noticing now.** Approve it.

**You should see a real PR appear.** Open it in a browser.

**Three things to check on that page:**

- **The body reads like a person wrote it.** One authored paragraph explaining what changed and why, then the
  mechanical parts — the task id, the files, the test result. That paragraph came from the handler's handoff
  (`pr_summary`); everything else came from a template.
- **Every identifier is right.** The branch name, the base branch, the issue it references, the task id. **Code owns
  all of them** (L3), and that split exists because it removes an entire bug class — including the recorded
  branch-inheritance bug, where a model re-derived a branch name and got it subtly wrong.
- **No `{{slot}}` anywhere in the body.** An unresolved slot **fails the action** (L23); it never renders its own
  placeholder into something other people read.

**It failed if** the body is a wall of generated text with no authored paragraph, or an authored paragraph with no
evidence. The split is the design: prose from the model, identifiers from code.

---

## 2. Run it again. Nothing double-applies

This is the claim the whole slice rests on.

```
aeon loop start --blueprint task-build "add a --version flag to cmd/hello"
```

Approve everything again, and watch.

**You should see each action recognise its own prior work:**

```
  issue    ok   forge.openIssue     #14  (existing — reused)
  branch   ok   git.createBranch    task/…  (existing — reused)
  commit   ok   git.commitAndPush   nothing to commit
  pr       ok   forge.openPR        #15  (existing — reused)
```

**Then check the repository.** One issue. One PR. One commit. No duplicates of anything.

**Four specific things to verify, because each is a different failure:**

- **`nothing to commit` is a success, not a failure.** An empty `git status` means the work is already in — a naive
  implementation treats it as an error and fails a step that has nothing left to do.
- **The issue was found by searching open issues** for the exact title with the `dev-loop` label — not by remembering a
  number.
- **The PR was found by its `head` ref**, and an existing task branch was reused rather than recreated.
- **Nothing was hand-picked into the commit.** Confirm:

  ```
  git status --porcelain
  ```

  **Empty.** The rule is stage everything, then assert this is empty, and it exists because of a recorded incident:
  hand-picked files left behind and then **dropped by the squash merge**. Code has no excuse for hand-picking, so it is
  an assertion rather than advice — and it now has **no exclusion at all** (L24), because Aeon's store lives outside the
  repository and everything it writes into one belongs in the commit.

---

## 3. Merging is asked for — and refused when nobody is there

```
aeon loop start --blueprint task-build "add a --version flag" # to completion
```

**At the merge you should see:**

```
  ask    Merge this pull request?

         forge.merge  #15  →  feature/4a17-version

         effect: irreversible — this cannot be undone by another action

         [ approve ]  [ deny ]
```

Now the same thing unattended:

```
aeon loop start --attended=false --blueprint task-build "add a --version flag"
```
*(The real flag is `--attended=false` — attendedness is a DECLARATION pinned at start, D2; walked 2026-10-26 with a
local bare origin: `forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)`, verbatim,
and the same for `forge.openPR` with class `outward`.)*

**You should see it stop, refusing:**

```
loop 8c31  blocked
  forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)
```

**This closes a gap that was invisible until it was named.**

S11's evaluator guards `bash`. Code actions do not go through it. The corpus shows someone already thought about who may
merge — `github-steward` marks `gh pr merge*` as **`ask`**, and `gh pr merge*` carries `allow`, `ask` **and** `deny`
across different agents. But those are command-line patterns. **A blueprint naming `forge.merge` reaches the same
effect through a path with no guard at all.**

So prove both routes reach a human:

```
aeon policy seed-corpus --agent github-steward
aeon policy why --agent github-steward --command "gh pr merge 15" --type command_access   # → asked
aeon action check forge.merge                                                                  # → ask (irreversible)
```
*(Walked 2026-10-26: the policy route printed `asked: agent:github-steward · command_access "corpus.github-steward
.ec3539e98dc4" … (an allow exists: "corpus.github-steward.684888c0ebb1" · agent:github-steward — asked anyway, PL4)`
while `aeon action check forge.merge` printed `forge.merge · irreversible · ask — cannot be undone by another action —
a human decides`. Two routes, one human. `policy check` was the spec's idealized verb; `policy why` is the real one.)*

**Both must ask.** If the code action does not, expressing an action as a code action rather than a command launders it
past the guard (L22).

And notice what the unattended default means: **an unattended loop cannot open a PR or merge one unless someone opted
in at start.** That is the correct default for a thing other people will see.

---

## 4. The phantom-merge guard refuses a mis-based PR

Create a PR whose base is wrong — target the repository's default branch instead of the feature branch — and point the
loop at it.

**You should see it refuse before merging:**

```
  forge.merge refused: PR #16 has base.ref 'main', expected 'feature/4a17-version'
```

**That assertion is the phantom-merge root cause.** A PR merged into the wrong base looks merged, reports merged, and
its work is not where the loop believes it is.

Then merge a correct one and check the guard's other half:

```
git merge-base --is-ancestor <mergeSha> feature/4a17-version   # exits 0
git merge-base --is-ancestor <original-task-commit> feature/4a17-version   # NON-zero
```

**Both results are correct.** Verify the **squash SHA**, never the task branch's original commits — a squash rewrites
history, so the originals are never ancestors even after a perfectly good merge. A guard checking the original commits
would report every successful squash merge as a failure.

---

## 5. `Closes #n` does not fire, and something has to close the issue

Look at the task issue after the task PR merged into the **feature** branch.

**It should have been closed by `forge.closeIssue`** — and you should be able to see that it was closed by Aeon rather
than by GitHub.

**Why this matters:** GitHub's `Closes #n` keyword auto-closes only on a merge into the repository's **default**
branch. Task PRs target the feature branch, so **the keyword never fires.** A body containing `Closes #14` is a
comforting no-op.

So `forge.closeIssue` is the only thing that closes a task issue when its work lands, and it **reads the issue back**
to confirm. Test the failure:

*(No `aeon debug action` verb exists — that line was the spec's idealization. The failure is simulated honestly at
its own level: the fake forge's `CloseSilentlyFails` mode, driven by `TestCloseReadsTheIssueBack` (running the fake's `CloseSilentlyFails`)
(`internal/action/close_test.go`), is the walked proof; against the live API this is the §5 owed row below.)*

**You should see it fail loudly.** A silently-failed close leaves the issue open with nothing downstream noticing — the
loop believes the task is done and the tracker disagrees, indefinitely.

---

## 6. A crash between "PR opened" and "PR recorded"

The case receipts exist for.

*(No such env hook exists — idealized. The REAL crash walked 2026-10-26 is `kill -9` on the daemon while a loop
waits at an ask: after restart the loop re-references the SAME question row (count in the question table: 1, never 2)
and only the answer door re-drives it. The postReview variant of this crash needs a live PR — owed, with the exact
re-walk in the record below.)*

**You should see recovery reconcile without posting twice:**

```
recovering 1 step
  c41f8a  action 'forge.postReview' has an intent with no id — listing reviews
          reconciled to review 998812 (posted before the crash)
```

**`forge.postReview` is the only one of the eight that needs receipts**, and that is worth stating because a plan
applying one strategy uniformly gets it wrong in exactly one place. A review has **no natural business key** — same
body, same verdict, two reviews, and nothing distinguishes them.

**Reconciliation is by id, never by body text.** Two reviews with identical bodies are two reviews; matching on text
would either miss one or merge two.

Then try the same crash on `forge.openPR`:

**You should see it need no receipt at all.** A PR has a natural key — its `head` ref — so check-then-act finds it.

---

## 7. Eight actions, and no renderer

```
aeon action list
```

**You should see eight**, each with its effect class and idempotency strategy:

```
  git.createBranch    local          check-then-act
  git.commitAndPush   outward        check-then-act
  git.mergeBranch     local          check-then-act
  forge.openIssue     outward        check-then-act
  forge.openPR        outward        check-then-act
  forge.postReview    outward        receipt-first
  forge.merge         irreversible   check-then-act
  forge.closeIssue    outward        check-then-act
  audit               local          naturally idempotent
```

**That is eight plus `audit`, and no `render`.** v1 had a renderer writing `loop.md`, `events.jsonl` and `memory.md`.
It is retired (L25): S4 always said *"the mirror is disposable; the store is the source of truth"*, and S9's app plus
`aeon loop show` cover everyone who would have read it.

The registry is **closed** — a blueprint cannot name arbitrary code — which is what makes effect classes sufficient
rather than a first line of defence.

---

## 8. The nine unverifiable diagnostics are gone

```
aeon blueprint validate dev-loop
```

**You should see the code-action count drop to zero:**

```
  valid          43 checks
  unverifiable    0
  invalid         0

OK
```

Since S8, `dev-loop.yaml` has validated with **nine `unverifiable`** diagnostics, every one of them a code action, each
saying *"cannot verify — the code-action registry arrives at S16"*.

They are now real checks, and `--strict` passes for the first time. S8 committed the expected set exactly so that this
change would be **deliberate and visible**: the fixture diff is the evidence.

---

## 9. The demo runs from the app — Test bench → Demos

The bench's **Demos** tab carries a sixth feature panel: **Git · forge (S16)**. Open the app, Test bench →
Demos, and press the panel's run buttons.

**You should see**, through the same socket the CLI uses:

- **§8 running from the app** — `blueprint.validate` on `dev-loop` answers **zero `unverifiable`** and the
  row quotes the counts; this is the S8 fixture-diff evidence read live, not restated;
- **the ask prompt itself, verbatim, and no button that answers it** — `loop.show` and `question.asked`
  render the `forge.openPR` stop exactly as stored (action, head, base, title, *effect: outward — other
  people will see this*), with the operator's commands as greyed text. **`question.answer` never leaves
  this page**: an approve button on a demo tab would turn the irreversible thing into a click-through, and
  the ask exists precisely because a human must mean it (S9's answer path is elsewhere, on purpose);
- **§3's two routes side by side, verbatim** — the code action's `ask (irreversible)` beside the command's
  `ask`, and the unattended `deny` sentence whole; re-casing either is the second truth;
- **an unstarted loop honestly `not runnable here`** — the row names this DEMO's §1 as the remedy and
  carries the operator's `aeon loop start` command; the panel starts nothing (starting a loop whose lane
  pushes is the operator deciding, not a demo button);
- **labeled REPLAY rows** for everything no fake daemon can conjure — the eight-action table, the
  phantom-merge refusal and the squash-SHA ancestor pair, the closeIssue read-back failure, the crash
  reconciliation **by id**, the twice-is-no-op call-log evidence — each citing the Go test that proves its
  envelope;
- **the GitHub-API half saying `owed`, never `met`** — the real-PR rows carry the T12 owed-pass record
  with the exact re-walk commands, because a fabricated outward-effect is the one lie this panel exists
  to make impossible.

**Three states only — met / refused-as-data / not runnable here — with every replay row labeled as one.**
A `met` the daemon did not say, an owed row dressed as met, an answered ask, or a live-looking PR are all
the bugs this panel exists to avoid — the panel contract is the numbered case in
`docs/test_suites/aeon-loop.md` following T12's additions.

*(Walked at T13; what was verified through the fake daemon and what is owed a pass with the app open are
recorded as such in the QA record — S14's and S15's §11 precedent.)*

---

## Walk record — 2026-10-26 (T12)

Rig: throwaway `XDG_STATE_HOME` + `LOCALRPC_ENDPOINT_PATH`, real git against a **local bare origin**, no provider
credentials, **no GitHub token on the box** (gh unauthenticated). Everything below marked LIVE was executed and read
back from the store, the git repository, or the process's own output; everything marked OWED was not — and says so.

| § | What was claimed | Status | What was actually seen |
|---|------------------|--------|------------------------|
| 1 | ask names head/base/title + effect + closed `approve\|deny` | **LIVE** | the question row: `Open a pull request?` + `forge.openPR  head: task/walk-ask2/4a17  base: feature/walk-ask2  title: Add version endpoint … effect: outward — other people will see this`, options `["approve","deny"]` |
| 1 | a real issue/PR appears on GitHub | **OWED** | no token on this box — see re-walk below |
| 2 | rerun double-applies nothing (git half) | **LIVE** | second `createBranch` call `ok`, one branch, one commit; `git status --porcelain` **empty** after `commitAndPush` (nothing excluded — the DoD check); `task/walk-ok/4a17` visible in `git ls-remote origin` |
| 2 | issue/PR reuse by exact-title search; `nothing to commit` = success | **OWED** | GitHub half — pinned meanwhile by T4/T5's real-git and fake-forge tests |
| 3 | policy route asks | **LIVE** | `asked: … command_access "corpus.github-steward.ec3539e98dc4" … (an allow exists … — asked anyway, PL4)` |
| 3 | action route asks | **LIVE** | `forge.merge · irreversible · ask — cannot be undone by another action — a human decides` |
| 3 | unattended deny, verbatim | **LIVE ×2** | `forge.openPR denied — effect class 'outward' resolves to deny (loop is unattended)`; `forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)` |
| 3 | approve → dispatch → refuses naming missing data | **LIVE** | after `aeon answer … approve`: `forge.openPR refuses: required argument "pr_summary" (supplied by handoff) is missing` — no model step on this box to produce one; the forge was never called |
| 4 | wrong-base PR refused pre-merge | **OWED** | needs a live PR |
| 4 | squash SHA is ancestor, originals are not | **LIVE** | raw git, scratch squash repo: `--is-ancestor <squashSha> feature/4a17-version` → exit 0; `--is-ancestor <originalSha> …` → exit 1 |
| 5 | closeIssue refuses a silent failure | **REPLAY** | no `aeon debug` verb; `TestCloseReadsTheIssueBack` (running the fake's `CloseSilentlyFails`) is the proof; live variant OWED |
| 6 | crash re-references the same question | **LIVE** | `kill -9` mid-ask → restart → same question id, table count 1, no second ask; door-noticed junk answer `Approve` → `denied — the answer "Approve" is not the word approve (junk refuses, never executes)` and the tree stayed dirty |
| 6 | postReview crash reconciles by id | **OWED** | GitHub half — `TestReconcilesByIdNeverByBody` pins it meanwhile |
| 7 | `aeon action list`: nine, no render | **LIVE** | nine rows, columns ACTION/CLASS/STRATEGY/ARGS, no `render` anywhere |
| 8 | validate + `--strict` pass; registry closed | **LIVE** | `dev-loop … 98 valid checks, 0 invalid, 0 unverifiable` (exit 0 strict); smuggling attempt: `unknown code action "git.smuggle" — the registry is closed (D14)` exit 1 |
| 9 | the app panel: asks read verbatim, never answered; owed rows say owed | **LIVE (vitest)** — Tauri open **OWED** | `test/bench-gitforge-s16.test.tsx` 6/6, full suite 457/457 (40 files) 2026-10-26: `98/0/0` quoted; ask body with head/base/title + `effect: outward — other people will see this` verbatim, zero answer buttons; both §3 verdicts in one row, no `error (` anywhere; cold row names this document §1 + `aeon loop start`; methods sent ⊆ {`blueprint.validate`, `loop.show`, `question.asked`, `routes.list`} — `question.answer`/`loop.start`/`loop.answer` never; every replay row labeled + cited (all 15 Go names grep-verified); owed rows say `owed`. Tauri shell open OWED — this machine lacks Tauri's system libraries (`make -C ui check-deps`), S14/S15 §11 precedent; the vitest run drives the real page through the real generic `rpc()` bridge |

**Findings — the walk's yield (each fixed, tested, committed as `0619d55` and `fd40882`):**

| # | Found walking | Fix |
|---|---------------|-----|
| F1 | **No task wired the daemon's `Deps.Actions`** — T10 built the seam, T11 built the client, nothing joined them; every code action would have blocked `action-runtime-missing` forever | `internal/daemon/actions.go`: per-loop-root services, forge identity resolved from the loop's own `remote.origin.url`, non-GitHub origins get a nil Forge, `forge.DefaultBaseURL` named once |
| F2 | The action-ask door (`AnswerActionAsk`) had **no socket route** — an ask nobody could answer | the daemon routes `loop.answer` by waiting STATE: gate→`AnswerGate`, ask→`AnswerActionAsk`; one verb, two doors, neither answerable through the other |
| F3 | The ask did not name head/base/title — O1 refused it closed (blocked `missing: head, base, title`) | the ask target is built from CODE (evidence first, branch-name derivation, code-owned args); the question row holds the whole rendering |
| F4 | `base` was never supplied — `forge.openPR` could not dispatch anywhere | derived from T3's names (`feature/<loop>`), never defaulted |
| F5 | `which` had no channel — the feature lane and task lane of one loop were indistinguishable | step `params:` carry authored constants; the validator's params rule scoped to model steps (T0 unchanged); `BranchWhich` validates at the action |
| F6 | Task identity had no channel before S17's ladder | `--with key=value` at start → durable with map (`with["task"]`); absent is absent — the registry refuses naming the argument |
| F7 | The door refused an out-of-band answered row as "a race" — stranding the chat-answer design | the door NOTICES (recorded answer is the record) and re-drives; the answered path stays the only arbiter |
| F8 | Answered asks kept printing `question is waiting` on blocked loops | releasing the pending ask retires `st.QuestionID` with it |
| F9 | DEMO commands idealized (`policy check`, `aeon debug action`, `AEON_DEBUG_CRASH_AFTER_ACTION`, `--unattended`) | grounded above, each with what was really walked or what replays it |
| F10 | A walk-misreading almost filed a bug: `git.mergeBranch` merges **feature into task** (sync), per T4's table row — it does not "land" the task | no code change; recorded so the next reader does not "fix" the direction |
| F11 | THEIRS' Q1 stream-throughput test flaked (fail/pass/pass) | recorded, not chased — theirs, timing-sensitive, unrelated |

**Owed rows — the exact re-walk, for the first box with a token** (scope table in `aeon/docs/FORGE.md`; supply via
`AEON_FORGE_TOKEN`):

```
# §1/§2/§3-live: real repository, real PR
cd <repo with origin = https://github.com/<owner>/<repo>.git>
aeon loop start --blueprint openpr.yaml --with task=4a17 --with task_title="Add version endpoint" \
    --with pr_summary="Adds the flag; tests included" "walk"
aeon answer <loopId> approve        # then: ONE issue, ONE PR, visible on GitHub; rerun → reused
aeon loop start --blueprint merge.yaml --attended=false … # sentence above must appear verbatim
# §4: open a PR whose base.ref is the default branch, point the loop at it →
#     `forge.merge refused: PR #n has base.ref 'main', expected 'feature/…'`
# §5: after the task PR merged into feature: `aeon action check forge.closeIssue` → run → issue closed
#     with closedBy == the token's identity (Aeon), NOT GitHub's `Closes #n` — then re-close it and rerun.
# §6: AEON-side: kill -9 the daemon after ListReviews returned and before RecordIntent saved;
#     restart → resume → the review table gains nothing (intent row + ListReviews id decide), exactly one new review id.
```

*(The §9 panel is walked at T13 with its own record; the GitHub rows there say `owed` until this page's owed list is
cleared.)*

---

## What this demo does **not** show

- **No full ladder.** **S17.** The actions work; sequencing them into phases with redo edges and round caps is next.
- **No second forge.** GitHub only. The typed interface keeps another possible — *a typed interface with per-forge
  implementations beats discovering a tool by function and hoping.*
- **No renderer.** Retired (L25).
- **No approve-flow UI.** The app **reads** the demo (§9 — validate, the ask rendered verbatim, refusals as
  data, labeled replay, no answer ever sent); answering asks, approving merges, and the gates' control
  surface are S9's answer path, exercised where a human means it.
- **No merge queues or protected branches.** A repository that refuses the merge produces an honest failure, not a
  workaround.
- **No policy authoring for effect classes.** They are code, deliberately: a config-defined class is a way to downgrade
  `irreversible` to `local`.

Next: **S17** sequences all of this into the full ladder — and its demo is a product claim rather than a capability.
