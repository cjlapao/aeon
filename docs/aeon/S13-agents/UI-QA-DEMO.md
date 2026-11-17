# S13 UI QA — continuing the same daemon, database, route and workspace

> Purpose: the app-side half of the S13 demo — the twenty-five handlers, the tech-tag resolver and T10's explicit
> retry — walked in the product on the **same persistent context** as S9, S10, S11 and S12: the one daemon
> endpoint, the one state database, the configured provider route, and `$HOME/aeon-qa-workspace` (module
> `example.com/qaworkspace` — **as measured on disk**; S9's guide names `example.com/aeon-qa`, which is what the
> workspace was *told*, not what it is. It contains `cmd/hello`). Later stages append loops to this history; they do not create
> another database.
> Last walked: **2026-10-25 (S13 T11)** — see "What was walked today" below. The three-state honesty rule applies:
> each step says whether it was verified live, verified at the socket/daemon layer plus the UI's own test suite,
> or awaits a provider credential.

## Setup (once per machine; keep across stages)

| Field | Value | Notes |
| --- | --- | --- |
| Daemon | `aeon serve` with `AEON_STATE_DIR=<one persistent dir>` | The 2026-10-25 walk used `/tmp/aeon-s13-live/state` (throwaway by choice — see deviations) |
| Endpoint | `LOCALRPC_ENDPOINT_PATH=<dir>/aeon.sock` | The CLI, the app and any socket client must point at the SAME socket |
| Provider route | Test bench → Providers: a configured route with a key (the S13 walks used an `openai-luna` route in earlier slices) | **No credential exists on this box on 2026-10-25** — the daemon's own banner says `routes none — set ANTHROPIC_API_KEY or OPENAI_API_KEY`; every model step therefore blocks with `unknown route`, which is itself a checked state |
| QA workspace | `~/aeon-qa-workspace` (absolute path; a literal `~` in the form is not expanded) | Enter the absolute path when creating the loop |
| App | `make tauri-dev AEON_ENDPOINT=<same socket>` | The header must read **current** |

> **Shortcut added 2026-10-25:** Test bench → **Demos** runs steps 2–4 of this walk (registry facts, both
> verify sweeps, the two `tag-only` resolver runs) with the honest verdict states, and replays the MCP
> envelopes with their test citations. It is the scripted half of the walk; the clicks below remain the
> human half, and the credential-side rows still end at the same boundary with the same commands.

## The walk

1. **Reopen the S9 baseline.** Loops → the saved `two-step` loop (`#/loops/…`). Expected: `survey → approve →
   summarise`, the `approve` gate answered, `SURVEY.md` and `SUMMARY.md` still listed as produces.
   *Walked today:* **no S9-era loop exists on this box** — the pre-S13 state directories were cleared with the
   `/tmp` sandboxes between slices, and the S9 UI-QA guide's own table was never filled in. A **fresh baseline**
   was started instead: `aeon loop start --blueprint two-step --project /tmp/aeon-ws --slug two-step-baseline
   "survey the workspace"` → loop `8f2a4f32a4357671f34cabaefe681d91` (`two-step@2`), ladder `survey blocked /
   approve pending / summarise pending`, produces `SURVEY.md` / `SUMMARY.md` present, reason `aeon -31100:
   unknown route` (the no-credential state, honestly rendered). `two-step@2` is itself a checked fact: the
   second pinning of that blueprint (a `two-step` loop existed on this machine in an earlier slice).
2. **Blueprints → `conversational-map`.** Expected (the slice's promise): its S9 missing-agent warnings are gone
   — four of the five specialists (`debugger-engineer`, `code-reviewer`, `security-engineer`, and the resolver
   `by-tech-tag`) are registered now. *Reality, corrected here:* the `triage` lane names `task-coordinator`,
   **which S13 did not ship** — starting it is refused, and the refusal now says so honestly:
   `unavailable-handler: lanes[triage].step.agent cannot run: agent body not shipped — S13 named it a follow-up
   (no body file in the registry)`. (The stale wording "arrives at S13" was caught by this walk and fixed in the
   same commit.) Record: **conversational-map remains start-blocked on `triage`** until the follow-up ships it.
3. **The retry surface, from the app.** T10 gives `loop.show` three daemon-computed fields — `retryEligible`,
   `retryAgent`, `retryRefusal` — and `loop.retry_handler_block` answers refusals as **data the app renders
   verbatim**. The UI never computes eligibility itself.
   - A loop blocked by an **unrelated cause** must show **no retry button** — only the refusal sentence.
     Walked live today: loop `64ad36053b538ec980d776c17f3884f7` (`tag-only`, `--tech elixir`) is `blocked` with
     reason `no handler for tech tag "elixir"` + the sorted `available:` tag list; `loop.show` carries
     `retryEligible: (absent)` and `retryRefusal.kind = "not-missing-handler"`; calling the op anyway returns
     the same refusal and changes nothing. In the app this is the panel with the sentence and no button.
   - A loop blocked **historically** on `unknown handler "X"` whose handler **now exists** shows
     `retryEligible: true` with `retryAgent`, and the button. **Historical retry not walked:** no such loop
     exists on this box (see step 1 — the pre-S13 database is gone), and per the task's own rule it must not be
     fabricated. The mechanism is test-verified: `TestHistoricalHandlerBlockRetry`,
     `TestHandlerBlockRetryIsSingleDispatch` (two windows, one dispatch, one `loop.retried` event), the six
     refusal cases byte-unchanged, plus the four UI tests in `ui/test/loop-retry.test.tsx` (eligibility renders
     from the daemon, refusal renders verbatim, no button on unflagged loops, the click posts
     `loop.retry_handler_block {id}`).
   - When a credential is available, produce the eligible case honestly: register a route, start the
     `two-step` baseline through to its gate, and any loop that blocked on a handler before it was registered
     (e.g. one pinned while a body file was missing) can be retried from the app; verify pinned source and
     position survive, and that reopening the loop in a second window cannot dispatch the step twice.
4. **A Go specialist build, same workspace and route** (needs the credential):
   `[QA-S13-GO] Add a --version flag to cmd/hello that prints aeon-qa 0.1.0 and exits 0. Add a Go test for the
   flag. Keep the existing default hello output. Run go test ./... and report the result in the step handoff.`
   Start with `aeon loop start --blueprint dev-loop --project "$HOME/aeon-qa-workspace" --tech go "<the goal>"`
   (flags BEFORE the goal — the parser stops at the first positional; a real CLI quirk found by this walk).
   Expected: the implement lane's `by-tech-tag` step dispatches as **`golang-engineer`** (the resolved name, in
   the `loop.step.dispatched` event and the ladder), its transcript and handoff appear, `OUT`/produced files
   exist in the workspace, and `go test ./...` passes **in the workspace**.
   *Walked today, as far as the credential allows:* `--tech go` on the single-lane `tag-only` blueprint
   (loop `b2211dd559bcfc09b98c60348da9a526`) blocked with `aeon -31100: unknown route` — **not**
   `unknown handler`: the resolver ran, `golang-engineer` resolved, and the refusal to dispatch a generic
   fallback is proven both ways (the `elixir` loop left **no file at all** in the workspace, `produces:`
   unsatisfied and never `ok`). The model-written Go itself remains unverified: **awaiting credential**.
5. **Save the URLs for S14.** Baseline: `#/loops/8f2a4f32a4357671f34cabaefe681d91`; the two resolver loops above;
   and the two model loops (baseline gate answered + QA-S13-GO build) once a credential exists.

## What was walked today (2026-10-25)

- Live on a running `aeon serve` (own socket, own state dir; the S31 team's dev daemon and Tauri app were
  running and were not touched): §2 both directions with the resolver, the unrelated-block refusal (op +
  `loop.show` fields), the conversational-map follow-up refusal, the fresh `two-step` baseline ladder and the
  no-credential blocked state rendered honestly.
- Test-verified: the retry surface end to end (Go) and in the app (vitest, 4 cases), the produces post-condition
  (`TestMissingProducedFileRejectsStep`), all resolver/tag/registry closures (95 handler-package tests).
- **Not walked — awaiting a provider credential:** model-written Go (§1/§4b), the answered gate transcript,
  the handoff `--handoff` view of a completed step, and the live eligible-retry case. The commands above are the
  exact continuation for whoever holds a key.
