# Aeon — where we are

> **Completion tracking moved to [SLICES.md](SLICES.md).** This document preserves historical planning and evidence. Its progress claims may be stale. Update task Notes and SLICES.md for new work; do not mirror completion here.

> **S23 T9 · 2026-10-29 (loop, Stage 4 "First run & polish" — the last task):** the timed measurement — **DoD not met, recorded as not met, not as a pass.** The gate is a **human** measurement (*"a stranger reaches a running loop in under five minutes, recorded"*) — the only DoD in the project a machine cannot evaluate, and the only task whose gate is a person rather than a test. **This slice is not done, and it is recorded as not done, not as a pass:** the ruling (C14, C2 of the self-check) is honoured by recording the absence rather than inventing a number — an author who has written the onboarding believes it is clear, and a tester who has read the design knows which flag exists, so their five minutes are not a stranger's (*if no such person is available, the slice is not done; you cannot self-certify a first-run experience*). **The automatable parts, done:** `FIRSTRUN-LOG.md` written with the log structure (the tester's exact words O1, every question, what they typed, where they hesitated, the clock time — recorded even on a pass O2) and the honest "not self-certified, therefore not done" status; `test_suites/aeon-cli.md` **amended, not rewritten** (S23 cases QAC2-10–QAC2-16 already present; **QAC2-17** added — `doctor` works with retrieval absent, S22's D1 "not built" not an error; the C7/C8 cases marked corrected, naming what superseded them — U1 by S9's Tauri shell, the runner by P29's daemon-owned execution — not deleted); the slice's `DEMO.md` (the §2–§7 walk, §5 `doctor` read carefully) exists. **T9 does not measure the first run — it cannot, and pretending otherwise is the failure the rule exists to stop; it builds the measurement's harness (the log, the QA cases, the demo walk) and records the one part a machine cannot run as not done, so the next person with a stranger knows exactly what is outstanding and does not paper over it with a number.** The DoD D1 is not met and is recorded as not met, not as a pass — the slice is not done until a stranger who has not read the design reaches a running loop in under five minutes, per provider, recorded in `FIRSTRUN-LOG.md`.

> **S23 T8 · 2026-10-29 (loop, Stage 4 "First run & polish"):** `doctor` — every state in its own states (C15, D10), where the whole product's honesty converges. `internal/doctor` is created: the report (cost per route, capability ×3 per route, retrieval availability ×4, staleness, mountings, credential presence, forge, the bash policy) + a `Render` that flattens nothing — no "ok" / "all good" / "healthy" over an uncertain value, no green mark over a declared-but-unprobed capability, no $0.00 over an unmetered route, no "available" over a stale corpus, and a not-listening mounting is shown, not absent (`TestDoctorFlattensNothing`); `TestGoldenRender` pins the full render against a golden file (D10), so a change to the rendering is a reviewable diff, not a silent flatten. **It renders; it decides nothing (I4):** every value comes from the slice that owns it. **No credential is printed (D6, C17):** presence and source, never the value (T2's `Source`), and "no credential is printed here" is on the screen deliberately (`TestNoCredentialIsPrinted`). **It works with retrieval absent (S22's D1):** "not built", not missing, not an error (`TestDoctorWorksWithRetrievalAbsent`); and it says what is fine (O3). **Q3 finding:** the three/multi-state values `doctor` reports (cost known/partial/unknown, capability ×3, retrieval ×4, staleness, mountings, credential presence, forge, bash policy) — all in the report, each in its own states. **Recorded deviation:** `protocol/doctor.go` + the `doctor.check` method (`method.go`) are the daemon-surface wiring (D16/I10/P34), but `method.go` is dirty (another agent), so it is a follow-up — `TestDoctorCheckSocketAndCLIReadSameStates` pinned against the report (the render is a pure function of it, so the socket and CLI cannot drift), not a live socket call; `cmd/aeon/doctor.go` + the registration are the follow-up. `doctor` is **5/5 green in-package** (the four gates + the golden); the full-suite run is blocked by another agent's in-flight edits, so the count is measured in-package (the base `de0374ec` builds clean).

> **S23 T7 · 2026-10-29 (loop, Stage 4 "First run & polish"):** the README and the four-step path. `aeon/README.md` + `aeon/docs/QUICKSTART.md` written: the path is exactly four steps (install, `aeon init`, `cd <a repo>`, `aeon step "…"` or `aeon loop start "…") — `TestREADMEPathIsFourSteps`, `TestQuickstartHoldsTheSameFourSteps` (the two cannot drift). No step requires reading another document (`TestNoStepRequiresReadingAnotherDocument`: no link / "see …" inside the path — a path whose step three says "see CONFIGURATION.md" is a path that takes twenty minutes). Every command extracted (inline + fenced) and checked for form (`TestEveryCommandInREADMEWorks`). The no-telemetry statement is in the README, not only PRIVACY.md (`TestNoTelemetryStatementIsPresent` — a stranger deciding whether to install a thing that reads their source code is deciding about that); the local-model path ("works with no key at all", S18) is mentioned (`TestLocalModelPathIsMentioned` — a stranger will not guess it); the architecture is linked (`docs/aeon/`), not summarised, with a length bound (`TestREADMEDoesNotSummariseTheArchitecture`). **Recorded deviation:** the task's `files_expected` is `README.md` (root), but the repo root `README.md` is the OpenCode Agents README (a different system); the Aeon README is written at `aeon/README.md` (parallel to `aeon/docs/QUICKSTART.md`) so the gates read it — overwriting the root would destroy the OpenCode Agents README; and `TestEveryCommandInREADMEWorks` checks commands for form, not execution (running them needs the built binary, `cmd/aeon` dirty, so execution is a follow-up). `readme` is **7/7 green in-package**; the full-suite run is blocked by another agent's in-flight edits, so the count is measured in-package (the base `de0374ec` builds clean).

> **S23 T6 · 2026-10-29 (loop, Stage 4 "First run & polish"):** no telemetry, asserted (C11, C19: a claim like that is worth nothing unstated, so a sentence in a README becomes a test that fails when it stops being true). `internal/nettest` is created: a `Recorder` observes outbound connections and asserts only the configured hosts; a transport-level `recordingRoundTripper` records the host of every Go-level HTTP call and returns an offline response (no real connection leaves the machine). `TestFullLoopMakesNoUnexpectedConnection` runs a loop (model call + a forge action) and asserts only configured hosts connected (transport level). **Permanent (N1):** `TestNoFlagEnablesTelemetry` greps for telemetry flags (none — not opt-in, recorded in a comment). **Process-level (the case the transport cannot catch):** `TestNoAnalyticsDependency` checks the dependency tree for phone-home packages (sentry/bugsnag/crashlytics/mixpanel/segment.io/datadog/newrelic/rollbar). **Recorded finding:** the tree contains `go.opentelemetry.io/*` (the framework's tracing service, defaults off) — observability tracing, not C11's phone-home telemetry, so the check names phone-home packages, not the substring "telemetry" (which would match opentelemetry). `TestFirstRunMakesNoConnection` (init before config connects to nothing — init is where a version check would feel most natural), `TestErrorPathsMakeNoConnection` (a failing call must not phone home — a crash reporter is the classic case), `TestNoVersionCheck` (a version check is telemetry with a friendly name), and `TestPrivacyDocMatchesTheAllowedList` (PRIVACY.md ↔ the allowed list, both directions). `aeon/docs/PRIVACY.md` written (your prompts and code go to the provider you configured, and nothing goes anywhere else; what Aeon stores and where; permanent); `.github/workflows/ci.yml` written with the nettest trace (D13: a network assertion nobody runs is a network assertion that stops being true) — `TestTraceRunsInCI`. **Recorded deviation:** the full-loop trace is bounded to the transport level (a live process-level observer is a follow-up); `cmd/aeon`/`protocol` are dirty (another agent), so the trace runs against the harness, not the live loop. `nettest` is **8/8 green in-package**; the full-suite run is blocked by another agent's in-flight edits, so the count is measured in-package (the base `de0374ec` builds clean).

> **S23 T5 · 2026-10-29 (loop, Stage 4 "First run & polish"):** blueprints shipped and overridable. The blueprints ship (S8/S17's `//go:embed` set + `internal/blueprint/blueprints/*.yaml` — `dev-loop` + `two-step` among the eleven; `TestShippedBlueprintsValidateInCI` validates them in CI, S8's D13). **Added to `internal/blueprint/load.go`:** `ResolveWithProject(name, projectDir)` — the project's own `./aeon/blueprints/<name>.yaml` wins over the shipped by name, with `ProvenanceProject`/`ProvenanceShipped` on the result; `ListWithProvenance` lists them with the provenance (D14: on the screen and the `blueprint.list` response, the same resolution, not a second one) — `TestProjectBlueprintOverridesShipped`, `TestListShowsWhichIsWhich`. `ErrUnknownBlueprint` carries the three parts (T1) with `Do` = `aeon blueprint list` — `TestUnknownBlueprintErrorListsAvailable`. `TestOverrideDoesNotAffectARunningLoop` pins the foundation a running loop relies on (the resolution is deterministic, so the content captured at loop start is what the pin holds — S8's L6). **Recorded deviations:** (1) the shipped blueprints are `//go:embed`'d, so `TestNoBlueprintIsCompiledIn` is a contradiction to resolve (the override is added via `ResolveWithProject`; a full no-embed is a follow-up); (2) `cmd/aeon/blueprint.go` + the `blueprint.list` socket provenance (D14/P34) are the wiring, recorded as a follow-up (the resolution is the same for CLI + socket, can't drift); (3) `--strict` for `dev-loop` is S8's CI check. `blueprint` is **green in-package**; the full-suite run is blocked by another agent's in-flight edits, so the count is measured in-package, not from a full-suite the tree could not build (the base `de0374ec` builds clean).

> **S23 T4 · 2026-10-29 (loop, Stage 4 "First run & polish"):** presets and `aeon init` (one task, R6: a preset with no init is data nobody reads, and an init with no presets asks a stranger about base URLs). `internal/presets` is created with the three templates init reads and the daemon surface (presets.list) shows — the identical data, not a parallel description (D16/I10/P34): Anthropic, OpenAI, **OpenAI-compatible** (Ollama, vLLM, LiteLLM, OpenRouter — a stranger picks a provider, not a wire format). A preset produces a route (S18's M12), not a second config path. **Fully scriptable** (C5/D7): every prompt has a flag, so the whole flow runs with no TTY — `TestEveryPromptHasAFlag`. init **probes** what it configured (a reachable server verifies; an unreachable one leaves `Declared` and says so — a probe failure does not fail init, O6 — `TestInitProbesWhatItConfigured`), **does not overwrite** (existing config reported, `--force` required — `TestInitDoesNotOverwrite`), and **obeys T2's credential rule** (key via T2's one reader, never into config, source reported not the value — `TestInitObeysTheCredentialRule`). `testdata/servers.json` records the servers + versions tried (O7/D9). **Recorded deviation:** `cmd/aeon/init.go` + the `presets.list` method (`protocol/method.go`) are the wiring, but `main.go`/`method.go` are dirty (another agent), so it is a follow-up — `TestPresetsListSocketReturnsTheSameTemplates` pinned against `presets.List()` (init + a socket reader both read it, so they cannot drift). `presets` is **5/5 green in-package**; the full-suite run is blocked by another agent's in-flight edits, so the count is measured in-package, not from a full-suite the tree could not build (the base `de0374ec` builds clean).

> **S23 T3 · 2026-10-29 (loop, Stage 4 "First run & polish"):** the exit-code table a CI job branches on (C10, D12) is created. `internal/exitcode`: six codes — `OK(0) · Failed(1) · Blocked(2) · Waiting(3) · Usage(4) · Config(5)` — one table, one mapping (`CodeFor(event, attended)`), so a code means the same thing everywhere. `Waiting` earns the table (a gate is `3`, a failed step is `1`); a CI-triggered loop is unattended, so a gate produces `Blocked(2)` with the ask denied (S19, G7/L22), not `Waiting` — both codes exist because both situations do. An aborted loop is `OK(0)` (S17's T7: a plan read and declined is a working system). `Result` carries the code and the three-part message together (T1: a code with no message is a CI job with nothing to read). `EXITCODES.md` created with the six codes + the attended/unattended table. **Q5 finding:** `main.go` has bare `os.Exit(1/2)` with no waiting/blocked; routing it through the table is the D12 point but `main.go` is dirty (another agent), so it is a recorded follow-up — `TestEveryCommandUsesTheTable`/`TestCodesAreDocumented` scoped to the package + `EXITCODES.md`, the `--help` direction and `main.go` routing recorded as follow-ups. `exitcode` is **7/7 green in-package**; the full-suite run is blocked by another agent's in-flight edits, so the count is measured in-package, not from a full-suite the tree could not build (the base `de0374ec` builds clean).

> **S23 T2 · 2026-10-29 (loop, Stage 4 "First run & polish"):** the rules `init` and `doctor` must obey come first. `internal/config` is created: the five layers `defaults → user file → project file → environment → flags` (a later layer wins, a flag wins for one run); the **one** credential reader `Credential(name, keyFile)` — environment first, then a dedicated key file, never a config file, never argv, never a log — and a credential-shaped key in a config file is a **load-time error, not a warning** (C4: a config file gets committed). `Source` reports where a credential came from, never the value (D6); validation names the key **and** the file via T1's three-part `errs.Error` (C3/D3: a fix, not "invalid configuration"). **Q6 finding:** `narrow/auth.go`, `daemon/routes.go`, `secrets/cursor.go` are pre-existing readers; `config.Credential` is the canonical one and routing them is a recorded follow-up (I1's full enforcement is a deviation). `config` is **10/10 green in-package**; the full-suite run is blocked by another agent's in-flight, non-T2 `protocol/method.go` edit (undefined `handleHistory*`; the base `de0374ec` builds clean), so the count is measured in-package, not from a full-suite the tree could not build.

> **S23 T1 · 2026-10-29 (loop, Stage 4 "First run & polish"):** the three-part error shape is structurally required, not a convention. `internal/errs.Error` has three unexported fields and one constructor (`New`); a literal missing the third (`Do`) does not compile, pinned by `TestErrorMissingDoDoesNotCompile` running `go build` on a `testdata/` file (S11's forged-token shape). Adopted in `narrow.Error` and `forge.ForgeError` (embedded `errs.Error`, `New`/`NewMsg`, `What/Why/Do`) so S16's five forge kinds and S19's four narrow kinds survive and stay branchable; `Do` is a kind-derived action, never "check your configuration" (O4). **Q2 finding:** S11's policy verdicts and S22's retrieval availability are discriminated *results*, not errors (AGENTS §5/DB19) — recorded, not forced. Verified in a clean scratch worktree (another agent had truncated `stdio.go` in the shared tree); **3144 PASS**, the only reds a pre-existing mounting flake and a transient worktree-corruption build-fail, neither T1's.

> **Model management · 2026-10-26:** Models is now in the user avatar Menu, with `/models`, `/models/new`, and `/models/:id/edit`, plus All models/Add model in the secondary menu. Persisted route metadata adds display name, description, bounded PNG/JPEG/WebP icon, seven declared capabilities, and a per-model effort subset (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`). Provider slug stays on `model`; route IDs remain separate and fixed during edits. Migration **0026_route_metadata** preserves existing rows, and unspecified versus empty arrays survive storage and socket round trips. Defaults and per-call requests reject efforts outside declared subsets; legacy routes retain their behavior. Attachment capabilities are recorded metadata, not new attachment transport.
>
> Verified: **479 UI tests in 43 files passed**, followed by **87 focused model/step/chat tests** after the final selector and route-ID refinements; UI typecheck and production build passed; `make -C aeon check-protogen` passed. Fresh Go tests passed for model, adapters, protocol, store, and runtime; the new daemon route-store reload test passed with both metadata and credential restored; five CLI route persistence/credential regressions passed. A broader daemon suite run encountered unrelated concurrent handler-registry assertions (30 bodies versus 29, and project-manager already shipped where an older test expects it absent); these were not changed by this work. Native desktop appearance and live provider calls were not verified. Design: [model management](../superpowers/specs/2026-10-26-model-management-design.md).

> **Daemon footer · 2026-10-26:** Every secondary menu now has the UI kit detail footer with daemon connection state, negotiated version, and protocol. It reuses the shared session, labels retained reconnecting values “last confirmed,” and shows Unknown when negotiation details are unavailable. Verified: `make -C aeon/ui test` — **465 tests in 41 files passed**; `make -C aeon/ui typecheck` passed. Coverage includes all connection states, missing retained values, protocol zero, navigation persistence, and the mobile drawer. Native desktop appearance was not visually inspected. Design: [daemon footer](../superpowers/specs/2026-10-26-daemon-footer-design.md).

> **User menu · 2026-10-26:** The primary rail now pins a standard User avatar in its footer. It opens the UI kit `Menu` popup containing Settings; Settings moved out of primary navigation while `/settings` remains available. Selection closes the popup and mobile drawer. Verified: **461 UI tests in 40 files passed**, including keyboard opening, focus restoration on Escape, outside-click dismissal, mobile selection, and direct Settings URLs; `make -C aeon/ui typecheck` passed. Native desktop appearance was not visually inspected. Design: [user menu](../superpowers/specs/2026-10-26-user-menu-design.md).

> **UI maintenance · 2026-10-26:** Test bench navigation now uses the joined shell's secondary menu, with all twelve former tabs opening addressable pages. Existing `/bench` URLs, connection ownership, access checks, and page error boundaries are preserved. Unknown bench URLs show not-found without selecting Status. Verified: `make -C aeon/ui test` — **459 tests in 40 files passed**; `make -C aeon/ui typecheck` passed. Native desktop appearance was not visually inspected. Design: [test bench menu](../superpowers/specs/2026-10-26-test-bench-menu-design.md).

> **S11 · Permissions & the tools complete · 2026-10-23 (loop `aeon-s11-permissions`).** Stage 3's second wheel
> is on: **13/13 specced tasks built, walked as 20 loop tasks (T1–T20), plus a post-QA cleanup the
> deferred-findings gate added (T21 — SEC-059, closed 2026-10-24, PR #163 @ `2d4a752`)**, and the slice's migration is
> **`0019_policy_action_ask_and_loop_attended`** — so `Migrations()` is **19**, the `policy` action CHECK admits
> `ask`, and `loop.attended` is declared at pin time. What it got you: **`ask` as a third action**
> (`Allow · Deny · Ask`), folded **deny over ask over allow**, so an `ask` outranks a broad `allow` and the
> sentence names the allow it looked past (PL4) · `CommandAccess` filled in with a real validator and evaluator
> over the **measured corpus** (`internal/policy/testdata/corpus.json`, extracted from this repository's own
> agent definitions by `tools/corpusgen`: 38 blocks · 367 rules · 51 distinct patterns · 245 deny / 115 allow /
> 7 `ask`, re-measured 2026-10-23 — **no test asserts a count**, that is a rule not a style, so these numbers
> are a measurement with a date, not an assertion) · `aeon policy seed-corpus`, explicit, audited, idempotent,
> never automatic (measured: run twice, `seeded 7 · unchanged 0` then `seeded 0 · unchanged 7`) · the unforgeable
> `AllowToken`: `Verdict.Token(command)` is the only mint and `tools.Spawn` checks it as its first statement,
> so **verdict → receipt → spawn** in that order and a token for `git push` does not authorise
> `git push --force` · `write_file` renamed to **`write`** with a hidden accepted alias for pinned bytes ·
> **ten** registered tools (`bash, edit, glob, grep, list, question, read, todo, webfetch, write`, pinned
> name-for-name by `TestRegistryExactNames`) with a default grant of `read, list, write` — no shell, no egress ·
> and `--allow-bash` **retired its warning and its apology**: measured `0` bytes on stderr.
> **The slice was walked, not just tested.** The cumulative demo gained its S11 section
> ([`DEMO.md`](DEMO.md) §S11, 13 numbered steps, every output quoted from a run on this checkout — throwaway
> `XDG_STATE_HOME`, private socket, model lane replaying fixtures so only the "model" is fake). **12 findings,
> 0 high, 3 medium, 9 low.** The mediums, not buried: **F-21** — the slice DEMO predicted *"approve → the
> command runs"*; measured, `approve` becomes the blocked call's **tool result**, the model re-issues, the frozen
> `ask` asks again, and **nothing spawns** (running it takes an `allow`, and that is walked too, with its
> `tool_receipt` row); **F-3** — `policy why` defaults to `--type tool_access`, so a command question answers a
> plausible `default deny (PL5)` for a command that has an `ask` rule (S10's F-18 class); **F-4** — a loop step's
> grants come from the handler registry, which holds only `default` and `read-only` and grants `bash` to nobody,
> so the slice's headline "a step granted bash" is unreachable via `loop start` until **S13**. F-21, F-3 and F-4
> are behaviour and surface gaps for their owning successors, not wording, and this record pass changed no
> source file.
> **Not claimed:** `scripts/qa-s8-blueprints.sh` **fails at baseline** on this checkout (pre-existing, found
> during T18's verification; the QA phase owns it) and `docs/test_suites/aeon-loop.md` still cites it as passing;
> no webfetch DEMO step is staged, because **SEC-T16-01's host-scoping seam is open** and the grant-then-dial
> precondition is recorded verbatim instead of being walked around; the app halves are headless-blocked (no
> `DISPLAY`, no `webkit2gtk-4.1`/`gtk+-3.0`/`librsvg-2.0`). Carried as recorded improvements, not fixed here:
> **`ContextManifest.FrozenSet()`** (a shared helper the three frozen-set readers should ask, instead of
> `recover.go`, `answer.go` and `daemon.frozenManifest` each shaping it) and **SEC-T16-09** (toolchain ≥
> go1.26.10 before webfetch becomes grantable).
> **The QA suites were amended in place, never renumbered:**
> [`test_suites/aeon-permissions.md`](../test_suites/aeon-permissions.md) and
> [`test_suites/aeon-tools.md`](../test_suites/aeon-tools.md) carry the `ask` rendering, the seeding
> idempotence, migration 0019's three-state attendedness, the CLI `--action ask`/`--command` sugar and the ten
> registered names.
> **Follow-up 2026-10-24 — loop T21, the deferred-findings gate's one fix (SEC-059, PR #163 @ `2d4a752`):**
> ToolAccess `Match` now canonicalizes **both sides** at match time — seam `SetToolNameCanonicalizer`, wired from
> `internal/tools` at init (`nil` = identity; one definition + one production call, grep-pinned by
> `TestTheCanonicalizerWiringAppearsInExactlyOnePlace`) — so the **alias-authored row decides**: `allow write` +
> `deny write_file` now **denies** (PL4 restored), an `ask` under the alias routes to a human, and the gate,
> `aeon policy why` and the socket's `policy.why` answer both write spellings identically because all three judge
> through the one `ev.Match`; `Store.Add` **stays permissive** so legacy pinned rows keep loading. The record
> lives in [`S11-permissions/tasks/T12.md`](S11-permissions/tasks/T12.md)'s T21 Notes, the live case is
> **QAP-21** in [`test_suites/aeon-permissions.md`](../test_suites/aeon-permissions.md), and the seam itself is
> in AGENTS §4d.
> **Measured 2026-10-24 (the T21 record sync), not recalled** (AGENTS §4a): from `aeon/`,
> `GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS="-I$ROOT/.tmp/cgo-shim" timeout 1500 go test ./... -v -count=1`
> → **1436** top-level PASS lines, **4092** subtests (**5528** in all), **0 FAIL**, **22 packages `ok`**; the
> per-package split is in the Tests column below.

> **S10 · Policy complete · 2026-10-21 (loop `aeon-s10-policy`).** Stage 3's first wheel is on: **12/12
> specced tasks built**, plus four follow-ups the slice needed and the plan did not name (the four read-only
> `policy.*` socket methods, `PolicyPage`, the §4b cumulative-demo rule, the seed of this page's
> [cumulative walkthrough](DEMO.md)), **plus the seven the deferred-findings gate added the same day
> (T18–T24) — the slice ran 24 tasks in all, and the two migrations it carries are `0016_policy_governance`
> and `0018_policy_authorship_net`.** What it got you: seven policy types where **every type has either an
> evaluator or a refusing stub that refuses rather than permits** (D10) · deny-wins, and the refusal **names
> the allow it overrode** (PL4) · one verdict sentence rendered identically by the CLI, the transcript and the
> socket (D9) · an `Allow` authored by anyone but an operator **refused at the write boundary**, naming the
> source, while a `Deny` from any of the four sources is accepted (PL1) · expiry that falls to the **default**,
> never to an allow (D7) · acceptance recorded and **never a gate** (PL10) · a 90-day event cap whose printed
> promise and enforced rule are one constant (D15/PL11) · the three-state in-force answer, none collapsing
> (T11) · authoring **CLI-only**, structurally: the socket registers four reads and answers `-32601` for
> `policy.add`/`policy.remove` (R4c/PL15), and `PolicyPage` has no form.
> **The slice was walked, not just tested:** all eight sections of
> [`S10-policy/DEMO.md`](S10-policy/DEMO.md) ran on this checkout on 2026-10-21 (throwaway store, private
> socket, model lane replaying the repo's own SSE fixtures — no credential, no spend). **19 findings: 0 high,
> 9 medium, 10 low.** Mediums, not buried: F-1 (the predicted two-line verdict block versus the one-sentence
> render), F-3 (`--type ToolAccess` refused; the vocabulary is snake_case), F-4 (the real loop verb is
> `loop start`, `loop run` does not exist), F-5 (a step asks as `agent:default`, so granting another name
> never shows the PL4 clause), F-6 (the frozen policy set is in `step show`'s context JSON, not `--manifest`),
> F-7 (`policy events` has `--subject`/`--depth`, no `--kind`), **F-13** (nothing in the demo aged a row past
> the cap, so PL11 was never shown enforcing until a row was inserted past it), **F-14** (a *filtered*-empty
> `policy events` prints the whole-table footer with no `honesty` line, and the `unaudited` branch is
> unreachable because `step-ad-hoc` is identifier-valid) and F-18 (the wire requires `type` on `policy.why`;
> the CLI default does not carry). F-13 and F-14 are **behaviour gaps, not wording gaps** — they are carried as
> QA cases [QAPL-18 and QAPL-24](../test_suites/aeon-policy.md), not as folklore.
> **The deferred-findings gate then closed both of those, and five security/defect findings with them
> (2026-10-21, same loop — T18–T23).** The user picked **SEC-022**, **SEC-042**, **DEF-039**, **DEF-040**,
> **DEF-027**, **DEF-042**, **F-13** and **F-14** at that gate; each landed as its own task. What changed:
> `policy` gained its database-level authorship net — **migration `0018_policy_authorship_net`**, so the slice
> now carries **`0016` *and* `0018`** and `Migrations()` is **18** — with `CHECK (action <> 'allow' OR
> source = 'operator')` plus `idx_policy_acceptance_target` (T18, PR #108); the authorship pair is now checked
> **at load**, not only at write, so a stored or forged `allow` from a non-operator source refuses the whole
> `List` by id instead of being honoured (T19, PR #110); a frozen manifest's digest is **compared** at replay
> and a mismatch denies everything rather than trusting the set (T20, PR #112); a zero-policy step is
> **FROZEN**, not pre-S10, so an `allow` added during a suspension cannot widen it (T21, PR #114); the audit
> footer's `depth` is **the listing's**, honouring `--subject` (T22, PR #116); and a filtered-empty listing
> prints its own honesty sentence instead of a bare footer (T23, PR #118). **F-13 and F-14 are therefore
> closed** — the walked record ([`DEMO.md`](DEMO.md) §S10 step 7 and the amendments in
> [`S10-policy/DEMO.md`](S10-policy/DEMO.md)) quotes the `trim` line and both honesty sentences from a run on
> this checkout, and the QA suite's replacement wording for QAPL-17/QAPL-24 plus four new cases from QAPL-29
> are recorded in that §Findings table for `qa-engineer-loop` to execute — the suite itself is QA's, and was
> not edited here. One row was **added** by the re-walk (F-20, Low), so the table reads **20 rows: 0 high, 9
> medium, 11 low**. Carried forward, not fixed here: **SEC-064**'s code half (the 0018 recovery playbook in
> `internal/store/schema.go` — copy the `policy_event` rows out before deleting a forged rule, because
> `policy_id` goes NULL; the docs half is recorded in `S10-policy/tasks/T1.md`) and **DEF-086** (the socket's
> `policy.events` has no filtered-empty analogue of the CLI's sentence).
> **The QA registry now exists:** [`test_suites/aeon-policy.md`](../test_suites/aeon-policy.md) — `draft`,
> `QAPL-01`–`QAPL-28` plus `PRE-01`, one case per row with intent/steps/expected executable by
> `verifier-engineer-loop`, and **properties, not counts** throughout (S11's rule). **App halves are
> headless-blocked on the walk machine** (no `DISPLAY`, no `webkit2gtk-4.1`/`gtk+-3.0`/`librsvg-2.0`, no Tauri
> runtime): QAPL-16 and QAPL-26 owe two screenshots from a desktop box; `ui/test/policy.test.tsx` is
> corroboration only, never the evidence.
> **Measured this dispatch, not recalled:** `make ci` **exit 0** (fmt-check, vet, guard, check-protogen, test,
> build; the Rust arm skipped **with its named reason** — cargo/webkit2gtk missing);
> `timeout 300 go test ./... -v -timeout 120s | grep -c '^--- PASS'` from `aeon/` = **1170**;
> `timeout 600 npm run test -- --run` from `aeon/ui/` = **250 tests in 22 files**; zero new dependencies and no
> `helper/` path in the slice's diff (legal constraints 4 and 5).
> **Re-measured 2026-10-21 by the findings-gate record (T24, `HEAD e6418cc`), not incremented:** the same
> `timeout 300 go test ./... -v -timeout 120s | grep -c '^--- PASS'` from `aeon/` (run bounded at `timeout 900`
> with `-count=1`, `GOPRIVATE='github.com/cjlapao/*' CGO_ENABLED=1 CGO_CFLAGS=-I$ROOT/.tmp/cgo-shim`,
> `XDG_STATE_HOME` at a throwaway directory) = **1195** top-level PASS lines, 0 FAIL, 20 packages `ok`; the
> un-`-v` form `timeout 300 go test ./... -timeout 120s -count=1` exits **0**; and `timeout 900 npm run test --
> --run` from `aeon/ui/` = **250 tests in 22 files, all passed** — unchanged from close, because T18–T23 are
> Go-side fixes and added no UI case. ~~**S11 is next: the measured corpus, and the
> matcher that `CommandAccess` is a schema for.**~~ **Superseded 2026-10-23: S11 is built (the banner above
> this one); S12 · MCP client is next.** **Superseded 2026-10-24: S12 is built (§S12 of DEMO.md, walked);
> S13 · Agents is next — it is the slice that gives the registry a production consumer.**
> **Project form side panel · 2026-10-20.** New project, Save as project, and Edit project now open the same ui-kit 0.2.6 `SidePanel` from the right instead of inserting a form in the page. The panel overlays the app with a backdrop, scrollable fields, and a fixed action footer; its left edge supports pointer and keyboard resizing. Create/edit validation, image icons, immutable project root on edit, and save behavior remain in place. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks opening, resizing, dismissal, and persistence on the same QA database. UI production build, 59 focused product tests, and `git diff --check` passed; desktop visual confirmation remains manual. Built-task totals remain 92/233.

> **White light-theme canvas · 2026-10-20.** Desktop review showed the light Controls-colored glow still tinted the gaps around panels after the base changed to white. The light app canvas and root now use solid white throughout; dark mode retains its existing base and radial glow. Panel surfaces continue to use the selected Surfaces tone. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks the gaps between menu, header, and panels in both themes. UI production build and `git diff --check` passed; desktop visual confirmation remains manual. Built-task totals remain 92/233.

> **Project stat tiles and responsive metrics · 2026-10-20.** The Projects dashboard now uses ui-kit 0.2.6 `StatCard` for its five metrics, with icons, compact values, and supporting text. The grid fits five tiles across when its content area is wide enough and wraps them as the app narrows. Workspaces, active loops, approval requests, and daemon state follow the selected Controls tone; blocked loops stays red. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks the row, wrapping, and tone change. UI build, 58 focused product tests including tile count and tone assertions, and `git diff --check` passed; a live desktop visual check remains. Built-task totals remain 92/233.

> **Projects overview dashboard · 2026-10-20.** The Projects overview now uses ui-kit 0.2.6 `InfiniteScrollPanel` for a searchable, filterable, sortable project grid, with saved and recorded workspaces, loop counts, and project links. Summary cards show workspaces, active loops, approval requests, blocked loops, and daemon state. Every running, waiting, or suspended loop gets a current activity card with the ui-kit workflow rail and step details; waiting gates offer the same answer path as the loop page. The activity section disappears when there are no active loops. The old model disclosure was removed. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) covers the persistent project, filters, scrolling, and multiple active loops. UI build and 58 focused product tests passed; a live desktop visual walk remains. Built-task totals remain 92/233.

> **Single-column blueprint ladders · 2026-10-20.** Each ladder step now occupies its own row inside the lane's `ConnectionFlow` chain, so `dev-loop`'s build steps read in one vertical column. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks this layout and the existing return branch. Focused blueprint tests, UI build, and `git diff --check` passed; a live desktop visual check remains. Built-task totals remain 92/233.

> **Unified blueprint chain flow · 2026-10-20.** Blueprints now show one ui-kit `ConnectionFlow` per blueprint. A ladder lane uses `childLayout: "chain"` and ordered rows of step cards inside its lane container; the separate selected-ladder flow was removed. Explicit lane and ladder `on_reject.goto` targets become visible failure branches, while conditions, retry limits, and fallback outcomes remain labelled on the cards. The flow fits on load and retains pan/zoom controls for large blueprints. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks `two-step`'s survey return and `dev-loop`'s build chain on the existing daemon. Focused blueprint mapping and layout tests plus the UI production build passed; a live desktop visual check remains. Built-task totals remain 92/233.

> **Project workspace file browser · 2026-10-20.** The saved project detail page now has an exploratory read-only **Project files** panel using ui-kit's `Tree` with folder expansion and selected-file preview. Two Rust shell commands list directories and read files under the selected canonical workspace root, reject traversal and symlink escapes, cap directory listings at 500 entries, and cap text/image previews at 1/8 MiB. Markdown uses the existing renderer, common source/config files use monospace text, PNG/JPEG/GIF/WebP/BMP use image previews, and unknown or binary files explain why they cannot open. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) reuses the persistent QA project and describes reproducible code, Markdown, image, and binary checks. Focused UI and Rust tests plus the UI production build passed; a live desktop visual check remains. Built-task totals remain 92/233.

> **Blueprint connection flow · 2026-10-20.** The Blueprints page now maps its selected daemon YAML into ui-kit's `ConnectionFlow`: ordered lanes on the overview track, direct steps on their lane cards, and a second sequential track for the selected ladder. Cards label gate questions, agents, actions, outputs, conditions, iteration, and rework targets. Reject targets are labelled because the kit's forward connectors do not express backward routing; malformed or incomplete source shows a flow error while source and diagnostics stay available. The flow and YAML parser load only when the page needs them. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks `two-step` and `dev-loop` on the same daemon. The UI build, 61 focused tests including shipped blueprint mapping, and `git diff --check` passed; built-task totals remain 92/233. A live visual check remains to be done.

> **Aeon workspace menu · 2026-10-20.** The side menu now uses the ui-kit `topItem` workspace dropdown with the bundled Aeon icon and app title. Loops, Projects, and Blueprints remain in the main rail; System, Settings, and Test bench moved into the top dropdown, with the same routes and mobile-close behavior. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) describes the new navigation. UI build, 57 focused product tests covering dropdown destinations and mobile navigation, and `git diff --check` passed; built-task totals remain 92/233.

> **Approval gate labels · 2026-10-20.** Gate-only workflow lanes and nested gate rows now show **Approval gate** in place of model cost and step time. The selected gate inspector shows status and recorded decision without Cost or Step time fields; mixed lanes still report their model work, with gate spans excluded from measured model time. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) and [tracker integration guide](UI-WORKFLOW-TRACKER.md) cover the display. UI build, 57 focused product tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Loop header typography and alignment · 2026-10-20.** The blueprint type now begins at the back button's left edge, while the **Blueprint workflow** label beside the button uses a slightly larger font. The right status/path group starts at the top of the header, aligning its pill with the button. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) and [tracker integration guide](UI-WORKFLOW-TRACKER.md) reflect the revised placement. UI build, 57 focused product tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Workspace path alignment correction · 2026-10-20.** Desktop review showed that responsive grid span classes placed the path in the narrow back-icon column, wrapping it one character at a time. The loop header now uses separate left identity and right status/path groups, keeping the unlabelled path right aligned beneath the status pill; the left-caret button remains beside the blueprint label. The product test checks that the path belongs to the right group. UI build, 57 focused product tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Loop header alignment and back control · 2026-10-20.** The loop workflow header now places an icon-only ui-kit left-caret button beside **Blueprint workflow**; its tooltip names **Project loops**, and it returns to the loop's project list. The blueprint type aligns below the label, while the unlabelled workspace path sits on that same row beneath the single status pill. On narrow windows the path moves below the blueprint type. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) and [tracker integration guide](UI-WORKFLOW-TRACKER.md) describe the layout. UI build, 57 focused product tests including back navigation, and `git diff --check` passed; built-task totals remain 92/233.

> **Full-width loop goal · 2026-10-20.** The workflow header now places the workspace folder in the same top row as the blueprint name, aligned to the right on wide windows, with the single status pill. The read-only three-line goal Textarea occupies the full row below; narrow windows stack the top-row fields. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) and [tracker integration guide](UI-WORKFLOW-TRACKER.md) describe the placement. UI build, 57 focused product tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Compact loop header · 2026-10-20.** The loop goal and workspace root moved into the full-width workflow header, alongside the blueprint/slug, one semantic status pill, and run totals. The goal is a read-only ui-kit `Textarea` fixed to three visible lines with internal scrolling, so long prompts no longer consume the whole page. The separate oversized goal heading and duplicate status pill above the workflow were removed. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) and [tracker integration guide](UI-WORKFLOW-TRACKER.md) describe the new header. UI build, 57 focused product tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Workflow run header restored · 2026-10-20.** The loop workflow again has a full-width ui-kit `Panel` above its rail and two right-hand cards. It shows blueprint name, semantic loop status, total model cost, wall elapsed time, and recorded step time. The lower `WorkflowTrackerDetails` card now holds only selected step artifacts and transcript; switching lanes leaves the run totals visible in the header. The [integration guide](UI-WORKFLOW-TRACKER.md) and [S9 UI QA walk](S9-app/UI-QA-DEMO.md) describe the restored hierarchy. UI build, focused product tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Side-menu app icon · 2026-10-20.** The transparent 256px Aeon app mark is now bundled as a UI asset and passed to the kit `SideMenu` through `logoIcon`, replacing the letter placeholder in expanded, collapsed, and mobile menus. Vite's asset type declaration supports the typed PNG import. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks the mark in Light and Dark modes and after collapsing the menu. UI build, 15 app-shell tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Workflow parts as the single loop layout · 2026-10-20.** The saved loop page now always places ui-kit's `WorkflowTrackerRail` in the left column, with `WorkflowTrackerStepDetails` above `WorkflowTrackerDetails` on the right; narrow windows stack the cards. The all-in-one tracker, Overview/Split/Focus switch, and focus height measurement were removed from the app. The three parts continue to share one `WorkflowData` adapter, parent-owned selection, gate answer handler, glass tones, and the kit's own card surfaces. Files and the thread remain below the workflow. The [integration guide](UI-WORKFLOW-TRACKER.md) and [S9 UI QA walk](S9-app/UI-QA-DEMO.md) now describe this one layout. UI build, all 224 UI tests, and `git diff --check` passed; a live desktop and narrow-window walkthrough remains manual. Built-task totals remain 92/233.

> **Workflow tracker family integration · 2026-10-20 (superseded layout).** This work added ui-kit 0.2.4's gate actions, details content, and three composable parts. The waiting gate's rail and card actions share one answer path with the thread; pending actions disable together, and read-only sessions omit them. The unused bespoke Ladder renderer was removed, while its data types and blocked-reason helper remain. The composite and focus layouts from this iteration were removed by the single-layout change above. Built-task totals remain 92/233.

> **Floating menu dark-theme fix · 2026-10-20.** The ui-kit stylesheet was already imported, but Aeon's Tailwind output loaded after it. Both sheets define `bg-white` with equal specificity, so Aeon's later light rule overrode the kit's `dark:bg-neutral-900` on the floating SideMenu body and logo header. The entry now loads Aeon's CSS first and the kit stylesheet last. Dark and Light Settings screenshots from the running local Vite app show the floating menu's body, header, border, text, and active row changing together. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) now checks this explicitly. UI build, all 221 UI tests, and `git diff --check` passed; built-task totals remain 92/233.

> **Control-colored app gradient · 2026-10-20.** After desktop review found ui-kit's `GlassBackground` too intense, the app returned to its original upper-right radial gradient across the whole app root. The gradient now follows the selected **Controls** tone through the shared settings service and uses a pale shade in Light mode and a deep shade in Dark mode, including OS changes under System. The unused page-glass CSS and scroll-layer positioning were removed. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks the color change, theme contrast, and route stability. UI build, all 221 UI tests, and `git diff --check` passed. Built-task totals remain 92/233.

> **Glass page backdrop experiment · 2026-10-20.** ui-kit `GlassBackground` was tried behind the scroll viewport, with the selected surface tone driving the gradient. The operator found it too intense and removed it; the control-colored app gradient above is the current design. The experiment's UI build, all 221 UI tests, and `git diff --check` passed. Built-task totals remain 92/233.

> **Owned app page layout · 2026-10-20.** `ui/src/app/PageLayout.tsx` now owns the glass side menu, fixed header, responsive mobile menu, single main landmark, and route-keyed scroll viewport. `App.tsx` supplies liveness and routed content while retaining its one daemon session; it no longer composes `SideMenuLayout`, `PageHeader`, and `ScrollArea` itself. The source layout guide names this boundary, and the existing [S9 UI QA walk](S9-app/UI-QA-DEMO.md) covers desktop and narrow-window scrolling. UI build, 221 UI tests including mobile-menu navigation, and `git diff --check` passed. Built-task totals remain 92/233.

> **UI routing and source layout · 2026-10-20.** The product now uses React Router with one route registry for paths and side-menu entries, an explicit route-access decision point for future page protection, saved hash URLs for product pages and test-bench tabs, and a shell that retains one daemon session while pages change. Product and test-bench screens are grouped under `ui/src/features/`; `ui/src/app/` owns the shell and router, and [the source layout](../../ui/src/README.md) maps the boundaries. Unknown URLs show an in-shell recovery link. The [continuing S9 QA walk](S9-app/UI-QA-DEMO.md) checks deep links and tab URLs on the existing daemon. The UI build and 220 UI tests passed; built-task totals remain 92/233.

> **Global app appearance settings · 2026-10-20.** The product now has a **Settings** page in the side menu. The Light/Dark/System selector moved from the header. ui-kit panels, the segmented theme control, and selectable `ColorSwatches` configure default control, text, and surface tones with a live preview. A single `appSettings` service persists the choices locally, migrates the prior `aeon-theme` value, follows OS appearance in System mode, and updates other open app windows through storage events. Its defaults feed ui-kit buttons, inputs, panels, toggles, the side menu, workflow tracker, and shared text/background tokens across the product and test bench; semantic blocked/approval status colors remain distinct. The page links to existing provider, system, and project configuration. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks page-to-page changes and restart persistence. UI build, all 218 UI tests, and `git diff --check` passed. This is app-local appearance configuration; no daemon schema or provider route changes. Built-task totals remain 92/233.

> **Project editing and icons · 2026-10-20.** Saved projects can now be edited from their detail page. The `project.update` daemon method changes name, slug, remote URL, owner, identity keys, tags, and icon while keeping the project ID and primary workspace root fixed so recorded loops stay attached. Migration 16 adds an optional icon value to existing databases. The UI accepts emoji or a bounded PNG/JPEG/WebP/GIF upload (256 KiB maximum), with initials from the name or slug when the icon is empty; it shows the icon on project cards and the detail page. The continuing [S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks edit, persistence, upload, and fallback in the same QA database. Focused store/protocol/daemon Go suites, the generated contract check, UI build, all 216 UI tests, and `git diff --check` passed. The task total remains 92/233; a live desktop walkthrough remains manual.

> **Saved projects in the product UI · 2026-10-20.** Projects now has a **New project** form for the existing project model: name, slug, an existing workspace root, optional remote URL, owner, tags, remote key, and repository key. New `project.create` and `project.list` daemon methods persist and return the metadata through the current state database. The page lists saved projects even before their first loop, still shows older loop-only workspaces, groups loops by root, and exposes the stored fields and current model limits. The [continuing S9 UI QA walk](S9-app/UI-QA-DEMO.md) saves the same workspace used by later loops. Focused store/protocol/daemon Go tests, generated contract check, UI build, all 214 UI tests, and `git diff --check` passed. This product follow-up does not change the 92/233 built-task count. A real daemon and provider walkthrough remains manual.

> **Linux dev dock icon · 2026-10-20.** The generated bundle icons alone left `make tauri-dev` with GNOME's generic gear on Wayland. The Rust shell now sets a stable dev window program name without enabling GTK's single-instance app ID, preserving the two-window QA walk. `make tauri-dev` registers a matching hidden per-user desktop entry pointing to the generated PNG; the current user's entry has been installed. The launcher passed `desktop-file-validate`, the Rust shell built offline, and an eight-second live Wayland run reported `xdg_toplevel.set_app_id("dev.cjlapao.aeon.Dev")`, matching the desktop filename. The [S9 UI QA walk](S9-app/UI-QA-DEMO.md) now calls for closing an older window and rerunning dev. A dock screenshot after restart remains to be checked. Built-task totals stay 92/233.

> **Desktop app icon · 2026-10-20.** The existing transparent Aeon mark is preserved as `ui/src-tauri/icons/source.png`. Tauri CLI generated a square 512px PNG plus 32px, 128px, 256px, Windows ICO, and macOS ICNS desktop assets with transparent padding, and the bundle config now lists them. File signatures, dimensions, transparency, configuration JSON, and `git diff --check` were verified. The icon has not yet been inspected in a built desktop bundle. Built-task totals stay 92/233.

> **Direct step versus ladder distinction · 2026-10-20.** The daemon now includes `structure: direct | ladder | iterate` in each `loop.show` lane, preserving the pinned blueprint's `step` versus `ladder` declaration in live snapshots. The product tracker no longer invents nested substeps for `two-step`'s direct `survey`, `approve`, and `summarise` lanes. Selecting a direct model step opens its transcript and artifacts immediately; a direct gate instead shows its decision context and thread link. Genuine ladder and iterated steps remain nested, including a one-step ladder. The continuing [S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks both shapes. Focused daemon/protocol Go tests, generated protocol check, UI build, all 213 UI tests, and `git diff --check` passed. A full Go suite attempt produced no output for over two minutes and was stopped; it remains unverified for this change. Built-task totals stay 92/233.

> **Workflow tracker selection fix · 2026-10-19.** The loop page now follows the ui-kit demo's controlled `activeStepId` selection and uses the tracker's own timeline and selected-lane detail card. The CSS that hid the kit detail card and the duplicate external lane pane were removed. Clicking a substep opens its artifact, cost, and transcript section below the tracker; choosing another lane clears that selection. The continuing [S9 UI QA walk](S9-app/UI-QA-DEMO.md) checks this interaction on the saved loop. The earlier `loop.files` error was confirmed to be an unrestarted daemon build, so no database reset is needed. The UI production build, all 212 UI tests, and `git diff --check` passed; a live visual walk remains open. Built-task totals stay 92/233.

> **File reader compatibility fix · 2026-10-19.** A QA window showed blueprint outputs but `loop.files` returned “method is not registered.” The current daemon registers `loop.files` and `loop.file.read`; daemon and protocol tests pass, so that response identifies an older daemon process on the saved socket. The product file panel and workflow artifact preview now explain the required same-state daemon restart, and the file panel no longer reports **No visible files here** when listing failed. The [continuing QA walk](S9-app/UI-QA-DEMO.md) includes the recovery step. All 212 UI tests, focused daemon/protocol Go tests, the UI build, and `git diff --check` passed; task totals remain 92/233.

> **Theme and workflow inspection · 2026-10-19.** The product header now has persistent Light, Dark, and System modes, following the ui-kit React demo's root-class pattern with an Aeon-specific preference. Aeon kit surfaces default to `glass`; explicit variants still override that default. The loop workflow initially used a separate lane details pane; the selection fix above replaces it with the kit's built-in detail card. Substep selection loads saved `step.show` transcript and lets the user read recorded write paths or declared lane outputs through `loop.file.read`, with change notes and current read errors. [S9 UI QA](S9-app/UI-QA-DEMO.md) walks theme and selection in the continuing daemon history. A live visual walk remains open. Built-task totals stay 92/233.

> **Product page shell · 2026-10-19.** The S9 product uses the kit's `SideMenuLayout` with a matching `PageHeader` and a bounded `ScrollArea` for every route, including the managed test bench. The menu and connection header stay fixed while the page body scrolls; the bench no longer adds its own full-screen height or nested main landmark. The continuing [UI QA walk](S9-app/UI-QA-DEMO.md) includes desktop and narrow-window shell checks. All 209 UI tests and the production UI build passed; a live visual walk remains to be done. Built-task totals stay 92/233.

> **Workflow tracker correction · 2026-10-19.** A blocked loop could show its current gate lane as green `ok` after a saved `revise` answer, even though continuation failed; the screenshot exposed this mismatch. `loop.show` and event snapshots now report the active lane as blocked, waiting, or running from the durable loop position while preserving completed substep history. The product tracker adds a visible blocked reason or approval request with a link to the decision card. It shows total and lane model cost, recorded model step time, and wall elapsed time with explicit labels; step time uses stored wall milliseconds or the persisted start/end timestamps and may include a wait for a step answer. The [continuing QA walk](S9-app/UI-QA-DEMO.md) checks both the waiting gate and the older blocked `revise` case. UI build, 209 UI tests, focused Go tests and generated contract check passed. Task totals remain 92/233.

> **Project and file UI follow-up · 2026-10-19.** The product now groups recorded loops by workspace root under **Projects**, lets a new loop reuse that root, and shows each loop's lanes through `@cjlapao/ui-kit@0.2.3` WorkflowTracker. A loop file panel browses generated workspace documents and stored loop documents, rendering Markdown on demand. New `loop.files` and `loop.file.read` RPCs resolve paths from the pinned loop root, reject traversal and symlink escapes, and limit reads to UTF-8 text files of at most 1 MiB. **Test bench → Files** exercises the same calls; the continuing [S9 UI QA walk](S9-app/UI-QA-DEMO.md) describes the shared daemon and workspace check. The UI build, all 206 UI tests, focused Go tests, and generated contract check passed; this is a follow-up surface and does not change the 92/233 task count. Projects with no recorded loops are not listed yet; named project management remains a later stage.

**The one page that says what is built, what is specced, and what is next.**
Everything else in this directory is reference; this is the index.

> **Updated 2026-10-18 — a defect, not a task.** The desktop app lost its daemon connection after about ten
> idle minutes: *"not connected — read from the daemon: Resource temporarily unavailable (os error 11)"*.
> The shell's reader thread had **one** 600s value doing two jobs — the socket's read *timeout* and the
> connection's *lifetime* — and `BufReader::lines()` handed that timeout back as `Err(WouldBlock)`, which the
> loop treated as a fatal read error. **An idle connection was being read as a dead one**, so the symptom
> only ever arrived on a window nobody was touching, and no test could reach it because waiting ten minutes
> was not on offer. `ui/src-tauri/src/rpc.rs` now splits the two: `rpc::Timing` makes the read timeout a 1s
> *tick*, and liveness is answered by a `ping` **probe** (20s of silence, 10s per answer, two strikes) — so
> silence stays current per T3's D6, while a genuinely wedged daemon is called dead in ~40s with a reason
> that says what to run. `Session::drop` now retires its reader thread and closes the socket, which also
> removes the second, leaked `client connected` a single window used to leave in the daemon's log. On the web
> side the window asked for **two** sockets, because `main.tsx` renders `App` in `<StrictMode>` and the mount
> effect connected while its cleanup disconnected: now a `useRef` guard gives one connect per window (a ref,
> because the question — *has this window begun its one connect?* — lives exactly as long as the instance that
> owns the session; not a `live` flag, which would be wrong in the production render), the release moved off
> the effect cleanup to `pagehide`, and a **Reconnect** button means a lost daemon no longer means restarting
> the app. New: `ui/src-tauri/tests/liveness.rs` — ten tests plus one live-daemon test — and `app.test.tsx`'s
> two. **Verified by mutation**, because a test that passes on both the broken and the fixed code proves
> nothing: treating the tick as fatal again fails 5; gutting `Drop` fails 2; letting inbound bytes stop
> standing the probe down fails 3; letting activity reset the liveness clock fails 5; and on the web side,
> removing the ref guard, the button, its `disabled`, and the `pagehide` release each fail their own test.
> The two that matter most are `a_slow_answer_is_not_a_dead_connection` (a 900ms answer, three times> `dead_after()`, while pings keep being answered — a probe that mistook an outstanding `ask` for a dead
> daemon would have been a *worse* bug than the one being fixed) and `a_request_awaiting_an_answer_is_not_
> liveness` (outbound traffic must not read as liveness, or a wedged daemon stays "connected" forever).
> Checked against a real `aeon serve` too: one `client connected` per window, and the disconnect arrives the
> moment the session is dropped rather than ten minutes later.
> **Not verified, in plain terms:** (1) the probe crossing an `ask` against a **live provider** — the 900ms
> test uses a fake that answers late while continuing to read, mirroring the framework's per-request
> goroutines (`localrpc/service/conn.go:175`), but no real model call has been watched; (2) whether wry
> fires `pagehide` when Tauri destroys a window — the release is best-effort, and `Session::drop` is the
> guarantee, but nobody has closed a real window and read the daemon's log.
>
> **Updated 2026-10-19.** S9 T1–T11 are built under the requested replay-only acceptance scope; Stage 2 is complete. `@cjlapao/ui-kit@0.2.3` stays pinned. The list and ladder consume additive, durable loop snapshots from events without an event-triggered refetch; reconnect still rereads. Iterated item states and step, item, lane and total costs are visible. Resume observations mark changed/missing files without rewriting their original hashes. Step live activity grows from events, while final transcript data comes from `step.show`. Case B keeps the interrupted charge unknown in the total and records the separate reissued cost. Migration 15 stores both components. Headless browser measurements at 1024px found no horizontal overflow on Loops, Loop, Step, Blueprints, System or the bench. T5's v1 inventory is complete, with an automated guard against HTTP and direct filesystem access; v1's unsafe platform and incompatible phase/task schema are explicitly excluded. `make ci` passed Go, vet, guards, generated contract, 203 UI tests, Rust and build.
> **S9 follow-up · 2026-10-19.** An old `conversational-map` loop blocked at `triage` with `unknown handler "task-coordinator"`. The cause remained in current code: only `default` and `read-only` are executable before S13, but the standard validator treated future agents as `unverifiable` and `loop.start` still pinned the loop. Start now refuses any unavailable S13 agent or resolver before writing a loop, naming the first affected lane in blueprint order and S13. The New loop form disables those blueprints and explains why. The validator retains its three-state report; this is an execution preflight, not a change from `unverifiable` to `invalid`. Existing blocked loops remain recorded. The shipped `conversational-map.yaml` and UI form have regression tests. `make ci` passed with 203 UI tests, Go and Rust suites, vet, contract check and build. The S9 task count stays 11/11.
> **S9 desktop replay · 2026-10-19.** Two Tauri windows on one disposable step question converged on one durable `new-table` answer, then one gate approval advanced both to `done` with both artifacts. The losing click created no optimistic row; the event may have removed its control before dispatch, so a visible refusal in the physical losing window remains unproven. A deterministic two-window UI replay now proves the losing window renders a gate refusal while its stale question remains visible, and a step question JSON-RPC refusal leaves its choices available. A separate real SIGKILL after a `bash` receipt produced a recovered case C report in System. The window exposed a misleading case C label, corrected to "tool receipt reconciliation" with the resolved outcome shown separately. A waiting gate's step state was corrected from `pending` to `waiting`. The replay-only §1–§8 verdict is recorded in `S9-app/DEMO.md`; the physical losing-window click did not itself show a refusal, and the deterministic two-window UI test covers that timing case.
> **Continuing UI QA guide · 2026-10-19.** [S9's UI QA walkthrough](S9-app/UI-QA-DEMO.md) now uses one existing daemon, state database, configured provider route, and persistent Go workspace. It supplies a concrete `two-step` goal, app clicks, expected gate and artifacts, and saved loop URLs to carry into later stages. The `conversational-map` start refusal is the fresh-history S9 checkpoint; an older loop already blocked by `task-coordinator` is retained as an optional S13 handoff. [S13 T10](S13-agents/tasks/T10.md) now specifies a narrow, explicit retry after the handler arrives, and [S13's demo](S13-agents/DEMO.md) requires its UI walk in the same history. These are documentation and future-task changes: no live provider walk or S13 retry implementation was performed here. Built-task totals are unchanged.
> **S9 workspace path fix · 2026-10-19.** The reported directory existed. The saved live loop revealed the exact cause: its root was `/home/cjlapao/code/aeon-qa-workspace ` with a **trailing space**, so `list(".")` tried to resolve a different path and the model blocked. The New loop form and daemon now trim surrounding workspace whitespace before storing the root. `loop.start` also resolves and lists the absolute directory from the daemon's filesystem before creating a loop or calling a provider, so a genuinely missing or unmounted path is refused immediately. Regression tests cover the trimmed path and missing-root no-write behavior; daemon, loop and tools Go packages, 38 product UI tests, UI typecheck and `git diff --check` passed. Existing blocked loops remain recorded; a corrected run starts a new loop in the same database. Task totals stay unchanged.
> **S9 revise-gate fix · 2026-10-19.** The next live `two-step` loop reached the approval gate, but `revise` blocked with “reject has no rework target.” The saved pinned YAML and the state-directory `two-step.yaml` both lacked `on_reject`, although the repository's shipped source had the edge. Standard blueprints are materialized only on first use, so the old state copy stayed stale. The shipped `two-step` is now version 2; resolving it upgrades **only** the two known pristine version 1 copies by hash and preserves edited copies and historical pins. Validation now rejects a gate that offers `revise` without a rework target, so edited copies expose the problem before a model call. The S9 UI guide checks the edge before a provider call and walks `revise → survey → approve` on one loop ID. `go test ./... -count=1` passed. Existing blocked loops stay recorded; a new run uses the upgraded blueprint after updating the daemon. Task totals stay unchanged.
> **S9 QA progress · 2026-10-18.** The blueprint library validates every listed blueprint; its diagnostic explanations come from structured kinds, not daemon prose. Confirmed step and gate answers appear as durable, sequence-ordered `You` rows from additive fields on existing events. Step and gate question prompts remain in the thread after answering; the active gate prompt also survives a pruned waiting event through `loop.show`. A total with no measured cost stays unknown instead of reading as a zero floor. `docs/test_suites/aeon-ui.md` describes the desktop bridge and replay walk. An isolated Tauri window against the disposable S8 replay state was resized to **1024px**; Loops, Blueprints, System, Loop ladder, and Step were visually checked after fixing the cramped loop list and form contrast. Killing that daemon made the Step window show stale with an increasing age, reconnecting cause and socket path; restarting it returned the window to current. A disposable replay showed a step question and gate answered in the same thread, reaching `done` with both artifacts. That walk caught three snapshot/continuation defects now fixed: a resumed step left its next lane stuck `waiting`, daemon `loop.show` omitted gate options for a normalized single-step lane, and CLI `loop show --json` dropped the root and gate choices. A fresh replay window showed both historical prompts and both sequenced answers. Another disposable window stayed `current` at a gate after 305 seconds of silence; `lsof` confirmed it held no Aeon store file open. The genuine two-window answer, measured horizontal-overflow and full demo walks, plus the remaining data gaps in T6–T8 and T10, remain.
> **CI note · 2026-10-18.** The S4 store suite's real-state isolation assertion now takes a pre-suite baseline, so an operator's existing WAL file does not fail S9 CI. The latest `make ci` run passed Go tests, vet, guards, the generated contract check, 192 UI tests, Rust tests, and the build after the S9 Step compaction-cost view. The operator's state file was not changed. The full S9 demo remains unwalked.
> **117 of 233 tasks built — 50.2%.** S1, S2, S3, S4 and **S5
> complete — Stage 1 · SKATEBOARD is finished.** **S6, S7 and S8 complete** under the requested replay-only validation scope; **S9 complete — Stage 2 · BICYCLE is finished under replay-only acceptance.**
> **S10 complete — Stage 3 · CAR is started:** the policy engine, deny-wins, the authorship rule, the age cap,
> the three-state in-force answer, and authoring that exists only on the CLI.
> **S11 complete — Stage 3 · CAR's second wheel:** the guard built from the measured corpus — `ask` as a third
> action, the command matcher, the token that binds a spawn to one exact command string, and the six tools that
> make a step able to work.
>
> **What you can do now:** With a configured route, the product window lists and starts blueprint loops, shows the ladder and pinned YAML on each loop, and answers both step questions and gates inline. The **Test bench** remains available for slice-level checks, and its **policy** tab reads the governance state — the list with its expiry, the three in-force states, the refusal log with its cap — never writes it. `aeon policy add|remove|seed-corpus|why|in-force|events|list` is the only authoring and interrogation surface (**seven verbs since S11**, and `why` gained `--command`). A command a step asks to run is now judged before it starts: unruled denies, ruled-`ask` suspends for a human on an attended loop, allowed runs under a token bound to its exact string — walk it in [`DEMO.md`](DEMO.md) §S11. The replay QA walk exercises a disposable two-step loop without a provider key.
>
> ⚠️ ~~**`--allow-bash` grants an unrestricted shell.** There is no command policy until S11: the command runs
> as given, in the workspace, with the daemon's environment, bounded by a timeout and the iteration limit and
> nothing else. It is off by default and it warns on stderr every single use.~~
>
> ✅ **Amended 2026-10-23 (S11, loop `aeon-s11-permissions`): this warning is retired, and so is the apology it
> carried.** `--allow-bash` still grants the shell and nothing else — the grant list remains the only thing that
> decides what a step can call — but the command is no longer unguarded. Every command is judged by
> `command_access` over the rules **frozen in the step's manifest**: an unruled command **denies before it
> starts**, a ruled `ask` **suspends for a human answer** (attended loops only — see the S11 demo section), and
> a command that runs is **token-bound to its exact string**. Enforcement now says that **at the moment it
> acts**, in the refusal sentence and the ask body, so the every-use stderr warning was removed together with
> the flag help's apology (T19; measured `0` bytes on stderr, `DEMO.md` §S11 step 10). The limits worth reading
> **before** the decision live where they are read: the flag's own description and the tool spec the model sees
> (`tools.BashHonesty`, embedded verbatim in both).
>
> ### Amended: a slice extends the cumulative demo
>
> **Superseded 2026-10-19, at the user's direction (S10).** The 2026-10-15 rule — each slice adds a tab
> to a **test bench** in the app, labelled as one in the UI — is replaced: every slice now **extends the
> single iterable, cumulative `docs/aeon/DEMO.md`** — run the app, it loads what the latest slice built,
> here are the numbered steps; a slice **appends a section; it never restarts or replaces the document**.
> The means changed, the intent did not — *"this is to test what we need in the place where it is
> consumed"*. **S9's scope is unchanged**: it builds the real app, with chat and the ladder and a designed
> experience. The standing rule is [`AGENTS.md` §4b](../../AGENTS.md); S2's and S3's N8 remain
> superseded.
>
> ### This page is updated by hand, by whoever finishes a task
>
> **Nothing updates it automatically.** Aeon is not building Aeon yet: these tasks are done by different
> agents, in different sessions, on different days, and none of them shares a memory. No CI job reads a
> checkbox here and no daemon watches the tree — **the only thing that knows a task is done is whoever just
> finished it.**
>
> So if you finished something, update this page in the same commit as the work.
> [`aeon/AGENTS.md` §4a](../../AGENTS.md) says exactly what to touch and why. An un-updated record is
> not untidiness: it is the next person re-reading a task spec to work out whether it was already built,
> finding code that half-matches, and guessing — which is how a task gets done twice, or skipped because it
> *looks* done.

---

## Progress

```
Stage 0 · GARAGE        ███████████████████████  7/7    S1
Stage 1 · SKATEBOARD    ███████████████████████  45/45  S2 ✅  S3 ✅  S4 ✅  S5 ✅
Stage 2 · BICYCLE       ███████████████████████  40/40  S6 ✅  S7 ✅  S8 ✅  S9 ✅
Stage 3 · CAR           ███████████████████████  91/91  S10 ✅  S11 ✅  S12 ✅  S13 ✅  S14 ✅  S15 ✅  S16 ✅  S17 ✅
Stage 4 · SHUTTLE       ░░░░░░░░░░░░░░░░░░░░░░░░ 25/54   S18 ✅  S19 ✅  S20 ✅ … S23
                                                 ─────
                                                 210/237
```

| | Spec | Built | Tests |
| --- | :--: | :--: | --- |
| **Slices** | 23 of 23 | **13** | — |
| **Tasks** | 233 | **136** | **S12, re-measured 2026-10-24 at slice close** from `aeon/` with the §6 cgo preamble: `timeout 900 go test ./... -v -timeout 300s -count=1 | grep -c '^--- PASS'` = **1698** top-level PASS lines, **0** FAIL, all packages `ok`; `make ci` exit **0** (fmt, vet, guard, check-protogen, check-corpusgen, tests, build); `make test-race` clean except one **foreign** flake — `internal/daemon`'s `TestASteerQueuedMidCall…` (someone else's uncommitted work; passes isolated at `-race -count=1`, fails only under full-suite parallelism, and no package outside `cmd/aeon` imports `internal/mcp` — `TestNoUnconditionalImportOfMCP` pins it). `go test ./internal/mcp/ -race -v -count=1` = **92** top-level PASS, **0** `DATA RACE`. The whole module also passes with **no MCP configuration** (`AEON_MCP_CONFIG` at a nonexistent path, empty `XDG_CONFIG_HOME`) — MC11 measured, T9's notes carry the reproducible command. **S31's measured baseline — 1598 top-level, `make ci` exit 0, `make test-race` clean, UI 275 in 27 files, Rust 28 — stands as recorded at its close**; S12 added `internal/mcp` (92 top-level of its own, fake included) and the `aeon mcp` verbs. The 127/233 chart counts specced slices' tasks as before: S12's ten built, making **13 built slices / 136 built tasks**

The S8 Go count uses `go test ./... -count=1 -v | grep -c '^--- PASS'` from `aeon/`;
subtests are excluded. S9's 203 UI tests and Rust bridge suite passed with ui-kit 0.2.3 in the full `make ci` run.
S10's numbers above are that same command, re-measured on 2026-10-21 in the loop that closed the slice
(`GOPRIVATE='github.com/cjlapao/*' CGO_CFLAGS=-I$PWD/../.tmp/cgo-shim`) — not carried forward from an earlier
slice — and re-measured again the same day after the deferred-findings gate landed (T24, HEAD `e6418cc`, the
exact bounded command and its output quoted in the banner above: **1195** PASS lines, 0 FAIL, 20 packages
`ok`; UI unchanged at **250 in 22 files**). The 117/233 totals still track **specced** tasks: S10's gate's
seven tasks and five unnamed follow-ups ran on top of its twelve (which is why its banner counts 24), and S11
specced **13** and ran **21** (`T1–T21`, the extra eight being the corpus loader split, the `ask` migration,
the `every-corpus-rule-evaluates` pass, the `write_file` → `write` rename, the registration audit, the
handler-policy attachment, the record pass, and the post-QA `SEC-059` cleanup) — so S11 adds 13 to the counter,
not 21, on exactly the rule S10 used. **S11's own line is a fresh measurement, not carried forward**: the
1436 / 4092 / 5528 figures above came from one bounded `go test ./... -v -count=1` run in the record sync that
wrote them, the run itself taken 2026-10-24 after T21 merged (the command is AGENTS §4a's, exported with the
cgo preamble §6 requires — without `CGO_CFLAGS` ten
packages fail to build on this box with a C error, `sqlite-vec.h:7:10: fatal error: sqlite3.h`, which is why
the preamble is quoted rather than assumed).

---

## Stage 0 · GARAGE — *you can build it and it starts*

| Slice | Tasks | State | What it got you |
| --- | :--: | --- | --- |
| **[S1 · Bootstrap](S1-bootstrap/)** | 7 | ✅ **BUILT** · [demo walked](S1-bootstrap/DEMO.md) · [test spec](S1-bootstrap/TESTSPEC.md) | One binary that boots, binds a local endpoint, refuses a second instance, reclaims a stale one, and opens a window |

---

## Stage 1 · SKATEBOARD — *it talks to a model, does one piece of work, and remembers*

| Slice | Tasks | State | What it is for |
| --- | :--: | --- | --- |
| **[S2 · Protocol & streaming](S2-protocol/)** | 8 | ✅ **BUILT** · [demo walked](S2-protocol/DEMO.md) | The product boundary: JSON-RPC, versioned, with filtered subscriptions. Six methods, 23 conformance fixtures |
| **[S3 · Model layer](S3-model-layer/)** | 13 | ✅ **BUILT** · [demo walked](S3-model-layer/DEMO.md) | The canonical message format, two provider adapters, three-state cost — **and the test bench** |
| **[S4 · Store](S4-store/)** | 12 | ✅ **BUILT** · [demo walked](S4-store/DEMO.md) | The schema, migrations, transcripts, questions, two pools — and `seq` that survives a restart. **Through the framework's database contracts** |
| **[S5 · One step](S5-one-step/)** | 12 | ✅ **BUILT** · [demo walked](S5-one-step/DEMO.md) | One bounded model step against a real repository, with `list`, `read`, `write_file` (renamed **`write`** at S11, alias still accepted), and opt in `bash` — **and the step streams into the app** |

**S3 shipped:** the canonical format with two structural tag guards · transcript validation where a
*suspended* transcript is valid · an in-house SSE reader, parity asserted at every chunk offset · three-state
cost at 100% coverage · two adapters behind one registry (a third provider is one file plus one line) · ten
labelled fixtures, every one hand-written and saying what was not verified · a client with a retry boundary
that never duplicates a delta · `aeon ask`, streaming through S2's subscriptions.

**And `scripts/fixture-server`**, which replays a fixture as a provider — so S3's demo and its CLI tests
run with **no credential and no spend**. That was the finding that mattered most: a walk requiring a key is a
walk nobody does.

**T13 — the test bench.** Three tabs in the app: **Status** (the connection and the negotiated protocol
version), **Providers** (routes, base URLs and an API key that goes in and never comes out), **Ask** (a
prompt, streaming, with the cost in three states). Through one generic `rpc(method, params)` bridge, so a
slice that adds a protocol method adds no Rust. Labelled a test bench in the UI, because **S9 builds the
real app**.

**S4 shipped:** 17 tables and 6 numbered migrations through the **framework's** migration runner and its
`_migrations` ledger · a schema fingerprint that compares a FRESH database against an UPGRADED one, because
two hand-maintained DDL copies agree with each other and still break an upgraded install · the status
machine ported from v1 with its `poc` overlay, tested by enumerating the table rather than listing cases ·
transcripts asserted byte-identical **on the bytes on disk** and **across a process boundary** · three cost
states that stay distinguishable when two of them hold the same number · eleven document-path refusals, each
proven by attempt and none normalised into range · `seq` on the `event` rowid, with S2's doc comment and
demo both updated.

**The measurement that shaped it.** The framework's own SQLite arm opens `modernc.org/sqlite`, a pure-Go
translation that **cannot host a C extension**: `select vec_version()` answers *no such function*, and its
composed DSN runs without WAL and without foreign keys. So the store is registered through
`database.WithDB` — a published construction mode — and Aeon owns exactly one thing the framework cannot do:
the connection. The config contract, the ledger, the schema lock and the `Store` lifecycle stay the
framework's. Four probes, before any code; T1's notes hold the output.

**Two defects found after the slice was reported done**, both by the user.
**Routes still did not persist** — S3 promised it and named S4, and S4 shipped without it (the same shape
T8 fixed for `seq`, missed here). Migration `0007_route` adds the table, `MutableRoutes` gains a
write-through seam, and `aeon routes list|set|delete` makes it checkable from a terminal. **The credential
is not in the database**: C17 allows the environment or a dedicated file path, so a key goes to
`$STATE/aeon/credentials/<id>.key` at 0600 — which also lets S14 encrypt a file rather than migrate a live
store full of secrets. Guarded by a raw-byte scan of the database, a forbidden-column-name check, and a
full-state-directory walk after a restart.

The second, by opening the tab: `store.info` sent
`"pending": null` — Go marshals a nil slice as `null`, and `pending` is empty on every healthy store, so it
was `null` every time and the page crashed on `.length`. The conformance fixture said `[]`, a shape nothing
produced, which is exactly why no test caught it. Fixed at the handler, guarded by a test that marshals real
handler output through `Dispatch`, and every tab now sits in a `TabBoundary` that **shows** a render error
rather than blanking the window. S4's T12 notes carry it.

**T11/T13 — the test bench grew a Store tab.** The path (copyable), `foreign_keys` read from the connection,
the pool shape, the ledger, the derived awaiting query **as SQL**, and a document-name box so a refusal can
be seen where it will be read.

**S5 shipped:** a bounded loop that **persists nothing** (so S6 can suspend by exiting, S7 can replay a
crashed step's rows, and S9 can own the bookkeeping) · the three handoff strategies ported verbatim with a
scanner that respects string literals, because a regex counting braces finds the wrong end for
`{"summary":"fixed the {weird} case"}` · exactly one tool-free re-ask, asserted **on the request object**
rather than inferred from behaviour · four outcomes where `blocked` names the bound AND its value and
`reject` is not a failure · one path resolver with six refusals, the symlink check running after resolution
and the root resolved too · a shell with a mandatory timeout, stdin closed, output capped and the true size
reported · `aeon step`, streaming through S2's subscriptions, and a **Step** tab that shows the same events.

**The allow-list is the slice's only safety boundary, and it is proven by attempt.** `BuildToolset(["read"],
…)` constructs no bash tool at all, so there is no code path from a model's request to a process. Three tests
— at the toolset, through the real loop with a scripted model, and in the demo — have the model ask for
`bash` with a `touch` command and then look for the file. Each has a control that grants `bash` and asserts
the file DOES appear, because without it the refusal test passes against a `Call` that refuses everything.

**The two claims most easily faked are measured.** `oomKilled` comes from a real `kill -9 $$` (exit `-1`,
signal `killed`) and from `exit 137` (exit `137`, no signal) — the two are indistinguishable from outside,
which is why the field is a heuristic and the rendered message says so rather than diagnosing an OOM. Group
termination is proven by a **freed TCP port**: a grandchild holds a real port, the timeout fires, and the
test waits for the port to become bindable — with a negative control that kills only the parent and asserts
the port is still held. Without that control the first test passes against an implementation that kills one
process, because the shell dies either way.

**Four defects the demo walk found**, all fixed in the slice: `aeon step` subscribed to `step.*` alone, which
dropped `model.delta` — the step's actual answer — and printed nothing between "started" and "ok"; the frozen
manifest lost `maxIterations` and every file's `bytes` by passing through the store's narrower view on the
way to the column, and again on the way back, so `step show --manifest` read `iterations 2 of 0`; a tool
event's summary repeated the tool's name; and `step.started` reported "0 context section(s)" for a step that
had a prompt. **No test could have caught the manifest one**, because every test wrote through the store's own
type, which cannot lose a field it declares — the guard now writes BYTES carrying a field the store has never
heard of.

**And the demo is walkable with no credential.** `scripts/fixture-server` gained a repeatable `-fixture`
flag — one fixture per request, the last repeating — because a step is a conversation and a single replayed
fixture can only demonstrate the iteration bound. Four fixtures were added, each derived from an existing
one's frames with only the tool name and arguments changed, and each labelled `hand-written` with what was
not verified.

**T13 — the test bench grew a Step tab.** The stream as it happens, the grant as a decision (`bash` is a
checkbox that is off, with the consequence beside it), and four outcomes that look different — `reject` is
deliberately **not** red: a handler that considered the question and answered no did its job.

**Found while running it, after the slice was reported done.** A step against an OpenAI reasoning model
failed with `Function tools with reasoning_effort are not supported for gpt-6-luna in
/v1/chat/completions` — Aeon sent no `reasoning_effort` at all, so the model's own default applied and the
tools were refused. Every step offers tools, so that route could answer `ask` and no step. `Route` gained a
first-class `reasoningEffort` (`none|minimal|low|medium|high`), carried through the store (migration
`0008`), `routes.set`, `aeon routes set --reasoning-effort` and the Providers tab, with the value validated
where it is typed rather than forwarded to become a provider 400. It is **not** defaulted: a model with no
such dial answers `400 Unrecognized request argument`, and matching model names to decide is folklore with
an expiry date — so the refusal itself now names the remedy, with the route's id in it.

That hint then found a second defect, by being read back: the command it suggested would have **cleared the
route's base URL**, because `routes.set` was a full replace for everything except the credential. It is now
*set what you name, keep what you do not* — the rule the key always had.

**Three more rounds, from actually using it.**

**The effort needed to be per-CALL, not only per-route.** The same route serves two different needs: a chat
wants the model to think, a step on `/v1/chat/completions` cannot have it thinking at all (that endpoint
refuses function tools while reasoning is on, and every step offers tools). A route stuck at one setting made
somebody keep two routes for one model. `ModelRequest.ReasoningEffort` now overrides the route's for a
single call — `aeon ask --reasoning-effort`, `aeon step --reasoning-effort`, and a per-tab Select in each UI
page — with `EffectiveReasoningEffort()` as the one place "the call wins" is decided, so the adapter, the
error message and the tests cannot disagree about it. A step with no override still lowers a reasoning route
to `none` by itself (`reasoningFor` in `compile.go`), and SAYS SO on `step.started` — found necessary because
leaving a silent decision in place is indistinguishable from a bug. The tool-free retry carries the SAME
effort as the call it is retrying; an early version rebuilt the retry request field by field and dropped it,
which would have retried straight into the 400 it was retrying away from. A test with the client's recorded
requests catches it.

**Then a provider added a second API, and the fix above was still only half the story.** `/v1/responses`
accepts function tools and reasoning together — which the restriction above exists only to work around. So
`Route.OpenAIEndpoint` (`chat|responses`) selects between two full adapter implementations
(`openai.go`/`openai_responses.go`) behind ONE provider id and ONE dispatcher, because the provider,
credential and prices are identical and splitting it into two provider ids would make a transport detail
wear a provider's clothes. The two wire formats share almost nothing — `input` items instead of `messages`,
top-level `function_call`/`function_call_output` instead of nested tool calls, `response.*` events instead of
`choices[].delta`, `reasoning: {"effort": …}` instead of a scalar `reasoning_effort`. Three new fixtures,
labelled UNVERIFIED like every fixture in this slice, because this endpoint has never been called with a
credential. The lowering rule above learned to ask the ROUTE (`Route.ToolsRefuseReasoning()`) rather than
decide for itself — caught by its own first test run, which lowered an **Anthropic** route that has no such
restriction at all, and would have lowered a `responses` route that does not need it either.

**And the duplicate-subscription bug, found by running four steps in the test bench.** Both the Ask and Step
tabs subscribed on every call and never released the subscription. The daemon fans each event out to EVERY
matching subscription on a connection — S2's own design, and correct — so four calls meant every event
delivered four times, which read as the model or the tool repeating itself. One subscription per page
lifetime now, reused across calls; the regression test runs THREE calls and asserts exactly one
`subscribe`, because a test that only ever made one call could not have caught this.

**Ordering that is load-bearing:** S3 before S4 (the store persists canonical messages, so their shape
settles first), and S4 before S5 (nothing to build a runtime against until the tables exist).

**What S4 left for its successors, deliberately:** `Ask`/`Answer`/`PendingQuestion` are built and tested and
nothing calls them (S6) · `StepsByStatus(running)` is the recovery query and nothing reads it on startup
(S7) · the three policy tables exist with their CHECKs and nothing touches them (S10) · `sqlite-vec` answers
and no vector table exists (S22). Each is a shape created early because a table is free now and a migration
on a live store later.

**What S5 left for its successors, deliberately:** `StepResult` carries a `suspended` variant that nothing
produces, asserted by exhausting seven scripted paths (S6) · `Rehash` compares the manifest's hashes against
disk and nothing calls it (S6) · the step row is written with `status='running'` **before** the work starts
and nothing reads those rows on startup (S7) · `bash` has no policy at all, which is stated in the tool's own
description to the model rather than implied (S11) · the S5 follow-up added `list` and `write_file`, so the
older S6 brief's claim that a step cannot change a file is superseded.

**S6 T1 completed:** `question` is in the grant vocabulary but absent from the default grant. Its schema
accepts free-form and option-based questions, validates kinds, and its execute path panics with K18. The
description tells the model that asking suspends the step and should be reserved for decisions the user owns.
Focused tool tests pass.

**S6 T2 completed:** the shared loop intercepts one granted `question`, executes and records sibling tool
results first, and returns a suspended result carrying the question, transcript and manifest. The question
never reaches the toolset, and a second question in the same turn fails with a named reason. Runtime tests
check the final transcript shape, S3 validation, iteration use, and the single loop declaration.

**S6 T3 completed:** `ResumeStep` checks the stored transcript and pending question, compares frozen file
hashes, adds a user-role note with changed and missing paths capped separately, appends the human's answer,
and enters the same loop with the persisted bounds and accounting. Tests cover an empty answer, repeated
suspension, unchanged original content, no repeated tool call, and no context assembly.

**S6 T4 completed:** routes now carry a context window. The uncalibrated estimate uses four bytes per token,
eight tokens per message, and a 25% safety margin. The selector protects opening context, the last assistant
turn, and complete tool-use/result pairs. A deterministic 500-transcript generated corpus checks these
properties, and a refused range costs zero model calls.

**S6 T5 completed:** compaction buys a bounded summary through the injected client, replaces a safe middle
range with a user message, records separate usage and cost, and emits `step.compacted`. The shared loop
compacts before a model iteration, and returns `blocked` if it cannot do so. Tests cover cost attribution,
an oversized summary, no admissible range, and no compaction purchase after the iteration bound is spent.
T6 is next.

**S6 T6 completed:** the keystone tests reconstruct `ResumeRequest` from JSON in a helper that sees only
serialized bytes, assert independent message storage, and compare resumed work with a pre-answered
continuation. They compare transcript, outcome, handoff, manifest, usage, cost and compaction records;
only event timestamps and the monotonic component of Go's deadline clock are excluded. Variants cover
plain continuation, compaction on both paths, and two suspensions exhausting the original bound.

**S6 T7 completed:** `step` saves the transcript and snapshot before `Ask`, then marks the step suspended and
emits `question.asked`. `answer` uses S4's transactional answer refusal, rebuilds the saved request and route,
marks the step running, and queues continuation beyond the request's lifetime. The new error codes have
three-part conformance fixtures. Protocol tests cover write order, failed manifest persistence, both answer
refusals, and event delivery. A real store test closes and reopens the database with a fresh daemon, answers
the stored question, and verifies the resumed transcript and final status.

**S6 T8 completed:** `aeon step --allow-question` grants the question tool explicitly; suspension prints the
question, options and a copyable `aeon answer STEP_ID` command. `aeon answer` validates choices, accepts an
empty free-form answer, subscribes before answering, streams the continuation, shows changed-file notes and
the resumed cost with compaction spend. `step show --compactions` renders saved ranges and summaries. A
replay-provider test ran the shipped CLI against a real temporary daemon and store; the full CLI suite passed
with local socket access. Routes now accept and persist `--context-window` through migration 0010 so
compaction can be configured outside tests.

**S6 T9 completed (replay-only validation):** the demo uses the shipped decimal step IDs, explicit `--allow-question`,
`--context-files`, `routes set --context-window`, canonical transcript block queries, and an explicit walk
record. The runtime and tools QA suites activate the S6 question and resume cases while preserving their
`QAC-` IDs. The replay-provider CLI walk passes `pgrep` (no step client), a `jq` sibling-result count of 1,
question use/result counts before and after the answer, and second-answer refusal. `make ci` passed with an
isolated HOME, the existing Rust toolchain paths, and local socket access (Go, UI, Rust, vet, guard, and build).
At the user's direction, S6 completion uses replay tests only. A live provider walk, physical machine reboot,
and answer after at least one hour remain unverified release QA checks; no provider key was configured here.

**S6 test bench follow-up completed:** the Step tab now grants `question` explicitly, freezes declared context
files, reopens a saved step by ID, answers a pending choice or free-form question, and refreshes an asynchronous
continuation from the store. It shows question use/result counts, changed-file notes, compaction summaries and
cost, the canonical transcript, and `step.verify`. Providers exposes the route context window. The
[UI QA demo](S6-suspend/UI-QA-DEMO.md) records the manual desktop, live provider, reboot, and one-hour checks
as pending. `make ci` passed with 125 UI tests. This was a follow-up to S6, not an additional planned task.

**S7 T1 completed:** migration 0011 adds `tool_receipt` with a unique `(step_id, tool_use_id)` pair and
nullable completion fields. `ArgDigest` hashes tool name, a NUL separator, and exact JSON arguments. Store
tests cover round-trip including empty output, incomplete receipts, duplicate begin, cascade deletion, and
fresh-versus-upgraded schema equivalence. The full store suite passes with an isolated HOME.

**S7 T2 completed:** every current tool declares a nonempty re-run class; `bash` alone is unsafe.
The committed list covers eleven names: five current tools and six pending legacy names, reflecting S5's
added `list` and `write_file`. The full tool suite passed with localhost listener access.

**S7 T3 completed:** running steps now checkpoint their transcript, frozen request, manifest and cost during
execution. Unsafe tool calls begin a receipt before execution and capture the result before a transcript
checkpoint. The runtime, daemon, protocol and store test suites pass with an isolated HOME.

**S7 T4 completed:** pure `DetermineCase` classifies complete tails, lost model calls, unanswered tool calls
and corrupt records. Its constructed-input table covers all three receipt states, digest mismatch and
unclassifiable inputs.

**S7 T5 completed:** `RecoverStep` resumes Case A from a committed tail, reissues Case B, and either recovers a captured Case C result or fails an incomplete or mismatched unsafe receipt. A structured failure is persisted.

**S7 T6 completed:** startup opens the socket before scanning `running` steps, commits each outcome atomically, skips suspended steps, offers `--no-recover`, and continues after a per-step failure.

**S7 T7 completed:** four recovery events, `step.show`, `aeon debug receipts`, and the Recovery tab make the saved decision and live progress inspectable.

**S7 T8 completed (replay-only validation):** `scripts/qa-s7-recovery.sh` passed seven disposable fixture cases with real `SIGKILL`, including a crash during recovery. `make ci` passed Go, vet, guards, 128 UI tests, 10 Rust tests, and a release build. The [test bench QA walk](S7-recovery/UI-QA-DEMO.md) records the manual desktop checks; no desktop window or live provider was used here.

The S7 brief expected `ResumeStep` to stay unmodified, but its question-only entry path could not resume a running crashed step, so a recovery mode was added. A model stream killed before usage arrives cannot yield its exact first-call cost: that step records `unknown(stream-aborted-early)` with later measured usage as a lower bound. The [T5 notes](S7-recovery/tasks/T5.md) record both deviations.

---

## Stage 2 · BICYCLE — *it asks me things, survives being killed, and I can watch it from an app*

| Slice | Tasks | State | What it is for |
| --- | :--: | --- | --- |
| **[S6 · Suspend & question](S6-suspend/)** | 9 | **9/9 built** · UI bench added; replay validation passed; live release QA pending | The `question` tool, suspend by exiting, resume cold |
| **[S7 · Crash recovery](S7-recovery/)** | 8 | **8/8 built** · seven real crash replay cases passed; desktop/live QA pending | Three crash cases, re-run classes, the `bash` receipt, Recovery tab |
| **[S8 · Blueprints](S8-blueprints/)** | 12 | **12/12 built** · replay walk and CI passed; desktop/live QA pending | The blueprint format, its validator, `decide`, a two-step loop, and the Blueprints test bench |
| **[S9 · The app](S9-app/)** | 11 | **11/11 built** · replay-only demo walked; ui-kit 0.2.6 pinned | Loops, the ladder, the thread, questions inline, honest cost |

S6/S7 and S8 may run concurrently in separate worktrees. **S9's demo is this stage's gate.**

---

## Stage 3 · CAR — *it builds real software and opens real pull requests*

| Slice | Tasks | State | What it is for |
| --- | :--: | --- | --- |
| **[S10 · Policy](S10-policy/)** | 12 | ✅ **BUILT** · deferred-findings gate closed 2026-10-21 (T18–T24) · [demo walked](S10-policy/DEMO.md) · [QA suite](../test_suites/aeon-policy.md) `draft` — re-run owed; QAPL-17/24 rewording + four proposed cases recorded in the slice DEMO's QA hand-off | Governance: seven policy types (**one real evaluator when the slice closed; two today — `ToolAccess` and, since S11, `CommandAccess`, with five stubs that refuse rather than permit**, corrected 2026-10-23 from `internal/policy/spec_stubs.go`), deny-wins that names the allow it overrode, the authorship rule enforced at the write **and now netted in the schema itself** (`0018_policy_authorship_net`'s `CHECK`, SEC-022), the acceptance record that is never a gate (its per-target read indexed by `0016`'s `idx_policy_acceptance_target`, DEF-027), the 90-day refusal log whose audit footer prices **the listing it was asked for** (F-13/F-14 closed by T22/T23) |
| **[S11 · Permissions & the tools](S11-permissions/)** | 13 | ✅ **BUILT** · loop `aeon-s11-permissions`, 20 tasks (T1–T20) closed 2026-10-23, post-QA cleanup **T21** closed 2026-10-24 (**SEC-059** — the alias row decides; gate, `policy why` and the socket answer both write spellings; **QAP-21**) · [demo walked](DEMO.md) — §S11, 13 steps, **12 findings (0 high / 3 medium / 9 low)** · QA suites [permissions](../test_suites/aeon-permissions.md) + [tools](../test_suites/aeon-tools.md) amended in place | The guard the bicycle was missing, built from **this repository's own agent definitions**: the corpus extractor and its committed `corpus.json` (no count assertion anywhere), the matcher (`*` anywhere, exact without, everything else lite), **segment splitting where any denied segment denies the whole**, `ask` as a third action with migration `0019` and `loop.attended` pinned at start, the **unforgeable `AllowToken`** making the order verdict → receipt → spawn structural, `aeon policy seed-corpus`, and the tools that made a step able to work: `write`/`edit` behind **one** path-enforcement point, `glob`/`grep` with bounded output and no escape through results, `webfetch` GET-only with no credential header ever, and `todo` that provably never reaches the store. Ten registered tools; `--allow-bash` says nothing any more |
| **[S12 · MCP client](S12-mcp-client/)** | 10 | ✅ **BUILT** · solo pass T1–T10 closed 2026-10-24 · [demo walked](DEMO.md) — §S12, 8 steps, **12 findings (0 high / 4 medium / 8 low)** · QA suite [aeon-mcp.md](../test_suites/aeon-mcp.md) amended (S12 section + QAN-19–QAN-26; **QAN-10 open — no test has met a real MCP server**) | Three tool tiers; private namespaces peers cannot see. Hand-rolled MCP client (stdio + HTTP, spec-literal fake in `testdata/`); classes configured never guessed — server annotations may **raise, never lower**, and `classify.go` is the one raise site (grep-pinned); descriptions **and** results wrapped in per-label `crypto/rand` boundary regions; grants are `ToolAccess` policies and **nothing else** (no struct holds one, `TestExactlyOneWayToRevoke` scans every file); outward/irreversible **asks at use under any allow** (L22); the 30s bound renders `timeout:` as a fact, never an empty result; the CLI seam in `cmd/aeon` is the layer's **only** consumer — `absent_test.go` greps the runtime for imports and a feature flag, and the whole module passes with no configuration |
| **[S13 · Agents](S13-agents/)** | 11 | ✅ **BUILT** · solo pass T1–T11 closed 2026-10-25 · [demo walked](S13-agents/DEMO.md) (2026-10-25; model-dependent claims marked awaiting a credential) · UI walk [S13-agents/UI-QA-DEMO.md](S13-agents/UI-QA-DEMO.md) · QA suite [aeon-agents.md](../test_suites/aeon-agents.md) amended (QAC-22/QAC-23; QAC-09 **superseded with reason**, retired-tool cases **not applicable, not deleted**) | The handler registry: 25 hand-rewritten bodies (no ritual, no retired tool, no fallback), their drop records (git-loop's 372 lines fully dispositioned), the **closed** `by-tech-tag` resolver wired at dispatch — an unknown tag stops the loop naming the tag and the available tags — and the one narrow historical retry of a missing-handler block, decided by the daemon and rendered verbatim by the app |
| **[S14 · Agent services](S14-agent-services/)** | 12 | ✅ **BUILT** · solo pass T0–T12 closed 2026-10-25 · [demo walked](S14-agent-services/DEMO.md) (11 findings F-1…F-11, two fixed by the walk) · QA suite [aeon-agents.md](../test_suites/aeon-agents.md) amended (QAC-25 panel contract)  | A2A cards, typed skills, Tasks, cost rolled up the chain |
| **[S15 · Agent plugins](S15-agent-plugins/)** | 10 | ✅ **BUILT** · solo pass T1–T10 closed 2026-10-25 · [demo walked](S15-agent-plugins/DEMO.md) (13 findings, two fixed; §6 raced live — measured 413 ms chain) · QA suite amended (QAC-26–33)  | Importing agents we did not write, with a blocklist check |
| **[S16 · Git & forge](S16-git-forge/)** | 13 | ✅ **BUILT** · solo pass T1–T13 closed 2026-10-26 · [demo walked](S16-git-forge/DEMO.md) — LIVE against real git with bare origins (11 findings, six fixed; **GitHub-API half OWED** with its exact re-walk commands) · QA suite [aeon-loop.md](../test_suites/aeon-loop.md) amended (QAC-70–79)  | Eight idempotent code actions, governed by effect class |
| **[S17 · The full ladder](S17-full-ladder/)** | 10 | ✅ **BUILT** · solo pass T1–T10 closed 2026-10-26 · [demo walked](S17-full-ladder/DEMO.md) — LIVE against the real daemon: the ladder runs the top of itself without you and stops at the ONE gate (6 findings F-α…F-ζ, one fixed by the walk — F-δ, the handoff reject's reason never reached the engine; **the model/GitHub halves OWED** with exact re-walk commands) · QA suite [aeon-loop.md](../test_suites/aeon-loop.md) amended — QAC-05–09 re-checked, QAC-80–93  | Phases, two-level caps, redo edges, three verify outcomes |

---

## Stage 4 · SPACE SHUTTLE — *anyone can use it, from anywhere, with anything*

| Slice | Tasks | State | What it is for |
| --- | :--: | --- | --- |
| **[S18 · Provider breadth](S18-providers/)** | 10 | ✅ **BUILT** · solo pass T1–T10 closed 2026-10-26 · [demo walked](S18-providers/DEMO.md) — revised 2026-10-26 (P34 audit `routes.probe`/D16/M32 + reality alignment: adapters move, `WireCapabilities` split from the model-management vocabulary, D10 as a file-list pin — plan.md §Reality alignment). **T1 ✅ 2026-10-26** — provenance per assistant turn (`Message.Provenance`, migration 0027, the rule write-gated at `AppendMessages` so pre-M26 rows stay recoverable; all six `step_message` writers carry the column). **T2 ✅ 2026-10-26** — Q8's five wire flags + the three states in `internal/model/capability.go` (absence table as data, self-checking), migration 0028 (`route.wire_capabilities`, `route.no_price`), O8 refused at both write gates (store `UpsertRoute` + socket, pre-0028 rows grandfathered, env routes exempt), `--capabilities`/`--no-price` on the CLI, no-price checkbox in both editors. **T3 ✅ 2026-10-26** — `routes.probe` on the socket + `aeon routes probe` as its thin client, ONE minimal request per route (`internal/model/probe.go`, Q9 answered as the verdict table), verdicts cached in migration `0029_route_probe` (`declared` = absence of a row, the schema refuses a `declared` write), `routes.list` carries the three states with timestamp + probed model, and a contradiction refuses at probe **and** at use (`ask` + `StepRunner.Run`, sentence verbatim, unreadable cache fails closed). **T4 ✅ 2026-10-26** — `runtime.Check` is the first statement of `RunStep`: a step needing `tools`/`thinking` against a route that **declared** it lacks them (declared-none `[]` refuses; undeclared nil never does — I5 both ways) returns complete/failed with `*CapabilityRefusal`'s sentence — capability, route, "(no model call was made)" — asserted on the client spy at zero, no events, no checkpoints; distinct from T3's contradiction, opposite fixes. **T5 ✅ 2026-10-26** — thinking goes only to its producer (M27): anthropic replays its OWN turns' blocks byte-identical with signatures (M17), both openai wires drop everyone's (no field exists), an anthropic route DECLARING thinking still refuses a foreign signature (`TestRuleKeysOnProducerNotTargetCapability` — the rule keys on the producer, never the target, O6), the transcript is unmutated (asserted on bytes), and every drop is counted into its own number that reaches the record — `StreamResult.ThinkingDropped` → `StepResult` → migration `0030_step_thinking_dropped` (ask-save too), with the handoff-retry turn finally carrying the T1 stamp it was missing. **T6 ✅ 2026-10-26** — the compat path is the openai adapter plus flags, and the claim is a file-list pin: the adapters directory holds exactly four non-test files and the registry exactly two provider ids (`TestNoThirdAdapterFile`). A declared route gates four absences as absence-of-key — no `tools` key (never `tools: []`), no `stream_options`, the system text folded into the first user message with the canonical turn intact (M1), `parallel-tools` documented as the standing never-send; undeclared routes keep S3’s exact bytes (I5). Q11 answered **handled**: absent ids synthesise as `aeon-synthesized-<n>` — the id records the synthesis — duplicates suffix, never merge. The difference a flag cannot express — Ollama 0.18.0 streams thinking as `delta.reasoning` — is a Findings entry absorbed by the one adapter, read safely because T5 guarantees none of it returns. CAPABILITIES.md names **Ollama 0.18.0 with `qwen3.5:latest`, measured 2026-10-26** from the run that happened (O7), three `recorded` fixtures behind it, and names the second-server gap instead of filling it from memory. **T7 ✅ 2026-10-26** — the cost ledger: unmetered is `unknown` with a reason naming the route (`unknown — route ‘local’ is unmetered (no price declared)`), never `$0.00` (M30, D9); attribution groups by route–provider–model from the LAST assistant turn’s provenance (billing follows the answer, M26), the total over an unknown reads as the floor it is — “at least $0.0592” (D5, P32); one computation (`model.AttributeCosts` fed by `store.LoopCostRows`) rides `LoopView.costAttribution` for the app and prints through `writeCostLedger` in the CLI — two renderers, never two sums (D16, P34). Q5 answered from the code: the compute path was honest, the zero lived in the renderers — `formatUSD(0)` printed the literal `$0.00` and sub-cent figures began “$0.00…”; all Go money now goes through one `MoneyString` (sub-cent scientific, zero spelled out) and two tests that had pinned the forbidden look evolved with it. **T8 ✅ 2026-10-26** — the A-to-B-to-A round trip is an integration test, not a claim: suspend on Anthropic, resume on OpenAI, return — the signed thinking block read back byte-identical from the store after every leg (D2, C3), no thinking key and every drop counted on the openai wire, the own signature replayed verbatim on anthropic’s (M17, O6), provenance speaking anthropic·openai·anthropic (M26) and the cost ledger splitting by route over one transcript (D16). Q6 answered from the code — the assumption existed twice (answer door resolving the saved route name; recovery rebuilding from snapshot bytes) and was lifted both times: the loop’s current route governs resume and recovery, an unresolvable current route fails recovery loudly. A mid-step switch is refused, naming the step, deferring through `StepResult.PendingRoute` to `loop.route.applied` (O5, N9); effort alone stays immediate. CAPABILITIES.md records the round trip and names the live-foreign-transcript gap instead of filling it from memory. **T9 ✅ 2026-10-26** — the walk: §1/§2/§5/§6 LIVE against Ollama 0.18.0 with keys genuinely unset (`unknown (no-price-for-route)` everywhere, the probe-failure and pre-call-refusal sentences captured verbatim), §3 **live-owed** (no cloud keys on the box) and carried by its gates; the DEMO rewritten to verbs that exist (nine findings, F-6 the process one), and the walk **caught a real T8 defect** — a deferred route switch lost when its step suspended instead of completed — fixed with `loop.TestDeferredRouteSurvivesASuspendingStep` (F-9); QA suite amended at QAM-35..46; CAPABILITIES.md confirmed complete, M13 survived. **T10 ✅ 2026-10-26** — the bench half: capability pills in the three states on the ProvidersPage (P34 — `routes.probe` now reachable where humans look), the one operator-clicked **Probe** per route with the M32 lock asserted (mount, refresh and save fire ZERO probes; a second click mid-flight sends nothing), contradictions and probe-failures rendered **verbatim**, the earned `verified` pill carrying its timestamp and probed model, the S18 run added to the bench demo runner where the A→B→A row is `not runnable here` **naming its Go test** and no row may leave localhost or `fetch` (asserted with fetch stubbed). The bench walk caught two real defects, both fixed with it: `DemosPage` read `routes.list` as a bare array so the model-demo gate could never open against a real daemon; and the TS money renderer printed `$0.00` for a known zero and `$0.000111` for sub-cent costs — `money()` now mirrors `MoneyString`'s forms exactly (spelled zero, padded-exponent scientific, five decimals), with the three tests that had pinned the forbidden look evolved beside it. Vitest 485/489 (the four failures proven foreign against a clean-HEAD worktree), tsc clean for this task's files, vite build green | the refusal and the ledger, honest, in the app |
| **[S19 · The narrow surface](S19-narrow-surface/)** | 8 | ✅ **BUILT** · solo pass T1–T8 closed 2026-10-27 · [demo walked](S19-narrow-surface/DEMO.md) — reality-aligned 2026-10-27 (plan.md §Reality alignment F-1…F-6: framework v0.10.1 answered Q3/Q5 — the `api` service is registered and pinned off, so the slice's HTTP work is the **inversion by explicit flag**, RouteContributor and a four-method TokenValidator, not a listener; S2 already exposes `PublicMethods()` "for surfaces like this"; `aeon mcp` exists as the client — `serve` joins it; the boundary statement names the **app + socket**, never a phantom `aeon loop stop` (F-1, S18's F-6 recurring). **T1 ✅ 2026-10-27** — `internal/narrow/actions.go` is the only registration point: six entries, each naming its projected socket method **typed as protocol.MethodName** and asserted against S2's own `PublicMethods()` (N13, C4 — the table S2 built for this day), each stating its `Cannot` (emptiness fails); Kinds closed to read/trigger; exactly one trigger, pinned one-way. The operational-control check is **enumeration over poisoned tables** — a synonym trigger (`aeon_loop_terminate`) and an operational method smuggled as a read (`step.stop`) must both fail, and they do; the fixture `testdata/actions.json` must match both directions (O7); the boundary statement is RENDERED from the same data (no hand-copied drift), names the reason and only doors that exist, and `aeon/docs/NARROW.md` is its human mirror. Q1 answered from the table: no read wanted a method S2 lacks — **zero methods added** (N8). **T2 ✅ 2026-10-27** — the key is out of band or nowhere: `--http-key-file` (a PATH — no flag takes a value; asserted by grepping the CLI's own source) or `AEON_HTTP_KEY`, file precedence stated, a missing requested file an error naming the path it tried and **no silent env fallback**, both empties refused at start. `Key` is a type whose `String`/`GoString`/`MarshalJSON` answer `<redacted>` — §4c's two-types rule on the server side; eleven error paths exercised with both key values asserted absent. Q5 from the framework's source: the multi-tenant trio stays unregistered and the four-method `TokenValidator` seam carries the one-key-one-user check — constant-time on content, `auth` discriminated from `not-found` and `transport`, the audit-safe `KeyPrefix` carrying §4c's **suffix** hint, `ValidateToken` a second refusing door. Both serve flags arrive with their validator (deviation stated); nothing binds yet. **T3 ✅ 2026-10-27** — `aeon mcp serve` mounts the table as MCP tools over stdio (`claude mcp add aeon -- aeon mcp serve`, no port, no auth: the pipe's parent owns the transport, the socket behind is 0600 + peer credentials, P26). The boundary statement rides `initialize.instructions` — the field the spec feeds the model — verbatim, and the one-line hint rides every description; `Cannot` reaches the model that reads tool listings; the emitted list is enumeration-checked and set-equal to the table both directions; the mapping is mechanical by construction (the server takes the table; a fixture entry appears with no mounting change; schemas derive). Per-request goroutines: a trigger answers under a second behind a 2-second read, daemon-side peak concurrency ≥3 asserted — a slow client holds nothing. Malformed input answers with JSON-RPC's own codes and survives; an unknown tool's message redirects to the boundary. End-to-end against the real daemon through the socket: six tools, the boundary's first line, and the awaiting SQL (DB6) answering through the whole chain. **T4 ✅ 2026-10-27** — the port is typed or it does not open: `--http <address>` + `--http-key-file` (or the env key), and the pin keeps its strength with a reviewed exception — `Pins(opts...)`/`WithHTTP()` inverts ForceOff→ForceOn only behind the typed address AND a resolved key, and register_test now pins the flip as hard as the off (only api moves; the default survives; the framework's Enabled:true is answered by a Force in both directions, so config can never open it). Q3: the scaffolding's listener is **reused** — `RouteContributor` on a `narrow` service (golden +1, B11 same-commit), validator attached via `WithNarrow`; no `net.Listen` anywhere in narrow, machine-pinned. The env quirk measured: bind is `SERVER_BIND_ADDRESS` (the framework's explicit tag), port `SERVER_API_PORT`. Routes project the table per KIND (reads GET at their name-turned-path, the trigger `POST /v1/loops`), asserted on the mux both directions plus behaviourally (a stop-shaped path 404s the hinted not-found without touching the daemon — on the live server an unmounted path gets the framework's bare 404, improvement carried to T7). The dispatcher is the socket's own `Table.Dispatch` under one synthetic connection with initialize mandatory and first (F-7, from the inside) — the daemon validates its own params (the table's `-31105` scope refusal earned honestly in the live test). Live: startup sentence + `aeon doctor` both say the address and the fact of the key, greppable-secret key nowhere; `narrow list` diffs byte-empty across mountings; HTTP and stdio answered the same daemon the same instant. 34/34 packages ok. **T5 ✅ 2026-10-27** — the surface can read a question and can never answer or create one, and it is **permanent, not deferred**: the sentence lives where the flag-hunter looks (questions.go opens with it; the question reads' cannot-lines carry it into `tools/list`; a test greps for the argument's own words). The read needed a method and N8 was satisfied honestly — `question.pending` joined the table with its slice (N2): one conformance fixture, the handshake pin extended with its comment, the daemon seam over the store's own `PendingQuestion`, protogen carrying it into `contract.ts` in the same commit — id, kind, prompt and the declared answer set, live (`{"pending":false}` from a real store; the unscoped read earns the table's own `-31105`). The answering/creating nets are **enumerations over poisoned tables on all three emitted surfaces** (table, stdio list, HTTP paths) — a plausible `aeon_question_answer` read is caught everywhere, and the nets are wider than T1's (the creation family is provenance, N6, not operational control), with the noun/verb guard keeping the legitimate question reads unflagged. Keyed attempts (auth passes — that is what a key is for) answer 404 with the RIGHT sentence per shape: answering verbs get "requires a person", posting shapes get "never posted", each with the alternative (O3); stdio carries the same pair inside the JSON-RPC error, verified LIVE through `aeon mcp serve`; the daemon is never consulted (0 dispatches). Kind visible to a monitor with the U14 distinction argued in code and pinned to exist. The smoke found a real defect and it died: stdio `Serve` used to return at EOF with replies in flight — `Server.inflight` drains now, pinned by a delayed-call EOF test. **T6 ✅ 2026-10-27** — the composition is real because it is DRIVEN, not read: Q4 answered (attended arrives only on loop.start's params, one struct, grep-pinned; the narrow trigger takes it out of the caller's hands — `attended` is **forced false** in the dispatch on BOTH mountings through ONE transform (`TriggerParamsFor`/`TriggerParamsJSON`, grep-pinned: the literal appears once in non-test sources, one call site per mounting), and the pin is WRITTEN false, not omitted, so never-declared cannot become the trigger's default; `TestAttendednessCannotBeInferredFromPolling` keeps G14 true on the loop side (100 polls change nothing; no UPDATE touches attended after creation). `allowOutward` is per loop, at start, one column (migration **0031**, NOT NULL DEFAULT 0 — an absent permission reads deny-leaning, the deliberate asymmetry against attendedness's NULL, and the loop package's own migration pin moved to 31 saying so): read at the decision site beside attendedness, failing toward deny on error. The denial gates DRIVE loops to `forge.openPR` with the forge fake as witness (blocked, class + unattended named, zero fake calls); the opt-in's positive half drives too (fake SAW the action); `allowOutward` does not touch `irreversible` — `forge.merge` with the flag declared blocks anyway and the block reason NAMES the flag's insufficiency. Two loops, one key, two outcomes (`TestAllowOutwardIsPerLoop`) — a grant that leaks would fail it. `TestNoKeyLevelGrantExists` reflects every exported narrow type and greps non-test sources for standing permission vocabulary; the key stays `struct{ secret []byte }`. tools/list carries the lever (unattended always, allowOutward is the one, no key-level grant, irreversible not on offer) — N14 for a CI author. LoopStartParams' new field reached the app's type the honest way: a third fixture (`loop-start-outward.json`, the attended-fixture precedent — protogen is fixture-derived), contract.ts same commit. Live: three triggers through the real daemon — two HTTP + one stdio, all claiming `attended:true` — every row pinned `attended=0`; allow_outward 1/0/1 as declared; the opted-in row's block reason is *past* the deny (forge dispatch attempted; no forge wired on the smoke daemon), the non-opted-in row carries T2's sentence verbatim, proving the deny fires where it should and nowhere it shouldn't. (Deviation stated: `internal/loop/attended.go` in files_expected does not exist — attendedness lives in store/pin/execute; the wiring went there.) 34/34 packages ok. **T7 ✅ 2026-10-27** — the four kinds are real because each has its OWN answer: `errors.go` holds the table — auth 401, not-found 404, **refused 409** (new: understood-and-declined must not merge into absence, or T5's N14 result is erased), **transport 503** (was BadGateway; transport PROMISES a retry is correct, and only 503 says that honestly — with the word `retry` in the body where the kind implies it). Classification is by **type identity, not string resemblance** — T4's in-process dispatch means `errors.As`/`errors.Is` read the daemon's ORIGINAL errors: `store.ErrNotFound` → 404, `-31107` → 404, `ErrStoreUnavailable` → 503+retry, every other typed `*protocol.Error` → 409 carrying the reason AND the Todo's alternative (O3), untyped → 503. That last bucket is honest only if untyped REFUSALS do not live in it — and one did: the flood that spawned this test answered a missing workspace with 503-"retry", where resending changes nothing. So T7 typed the daemon's own start refusals AT THE SOURCE: registry codes **-31109** (workspace unusable) and **-31110** (mode unsupported), three-part data each, **conformance fixtures** each (D9 demands one per code — TestEveryErrorCodeHasAFixture proved the registry is enumeration-enforced, not decorative), the two daemon workspace tests evolved from prose assertions to **typed-identity** ones (strictly stronger: the code is pinned too). Every handler has a timeout because there is ONE dispatch site: caller in a goroutine, `select` on the bound, late outcome lands on a buffered channel and the goroutine exits — never a second write; the enumeration test walks `Entries()` and no entry may answer slower than the bound. Abandoned connections are reaped through ONE response door (`writeJSON`): marshal fully first (headers then body, no partials), write under a bound, **hijack-and-close** a stalled wire to release the blocked writer — asserted against a real listener with a body big enough to outrun every buffer in the path (the 2 MiB first version passed VACUOUSLY — loopback's kernel absorb finished the write; 32 MiB is not subtlety, it is certainty) plus the shape a killed CI job actually leaves: request half-sent, RST, no handler ever started, goroutines return to baseline. The bound is for ABANDONED readers, not slow ones — `TestSlowReaderStillGetsItsBytes` keeps the reaper from being a slow-client axe. The stdio drain T5 left open gets its answer: bounded by the same threshold HTTP uses to call a dispatch wedged (`TestHungStdioCallerDoesNotPreventShutdown` — EOF always returns). Scoping (Q11 answered): a read resolves its id **within this daemon's store** and a miss is not-found — behavioural half (miss → 404, id dispatched verbatim, the surface resolved nothing itself) and structural half (reflection: the Mux holds no store/SQL/file/conn handle — one Caller seam, nowhere "further" to reach; grep: the package imports no data source). `TestNoErrorBodyCarriesAKey` enumerates SEVEN error paths with a sentinel key configured — absent from every body (I6's empirical half beside T2's structural `<redacted>`). Live on a real daemon: four half-readers + six abandoned conns leave loop timing UNCHANGED (4.4 ms → 4.0 ms, `TestSlowCallerNeverDelaysALoop`), sixteen concurrent polite readers never starve the loop's writer (`TestNoHandlerHoldsAStoreLock`), and the smoke walked 401 / 404-missing-loop / 409-refused-relative-workspace / 409-refused-junk-mode (What+Todo in the body) / 409-malformed-body / 200-trigger-still-pinning-attended=0 from the running process. contract.ts gained one honest line (`"mode"?: string` on loop.start — the mode fixture exposed it; protogen is fixture-derived). **T8 ✅ 2026-10-27** — the walk ran §1/§3/§4/§5/§6 live on this checkout and the DEMO now quotes what happened, not what was predicted: zero listeners + exit-7 before, `refused: --http requires a key…` exit=1, `listening on … (narrow surface, key configured)` after; the outward denial byte-identical to the prediction; `allowOutward:true` moving the reason past the class denial; `diff` byte-empty across mountings; the keyed answer attempt 404-ing the **person sentence + the alternative**, and the alternative ran the loop to `done` (`aeon answer … approve`). **The walk killed two defects the suite structurally could not see** (both live where the package tests *are* the router's caller): F-1 — the live answering attempt got the framework's bare `404 page not found`, so T4's carried case is finally done (six catch-all **refusal routes**, not actions — listings unchanged; `TestLiveRefusalsAnswerWithReason` pins the wire); F-2 — the daemon's **typed** refusals (`ErrBadParams`, `PinRefusal`) rode the transport bucket promising retry a resend cannot change (type identity now). The suite gained **QAN-27** ("a surface's error shape is only proven at its own socket"); QAN-11…18 carry walked status, and **§2's model-display leg is LIVE-OWED** — the real Claude Code 2.1.311 connected (`✔ Connected`) but the weekly quota closed the kill-request exchange; the boundary fields captured protocol-side (`initialize.instructions`, `tools/list[].description`), re-run command named (F-4). Five more findings corrected the plan's own DEMO (verbs, params, the `mcp serve` subcommand, bare-key header, a gate blueprint needing `on_reject`). README S19 → BUILT. 34/34 packages ok. | | the boundary, on both doors, with the reason |
| **[S20 · Triggers](S20-triggers/)** | 9 | ✅ **BUILT** — reality-aligned 2026-10-27: thirteen findings (R-1…R-13, [`plan.md`](S20-triggers/plan.md)) — none changes a decision, all change what the tasks type: `TriggerAccessSpec` ships as `{trigger,state}` not `{kind,state}` (R-1), today's evaluator is the D10 refusing-to-decide stub whose replacement follows CommandAccess's precedent (R-2), loop provenance needs migration 0033 (R-3), the webhook route authenticates by HMAC under `ApiKeySecurityLevelNone` — the one exception on the surface (R-5) — and the signature rides a param so stdio parity survives (R-6), and the demo's `advance the host clock` becomes genuinely-down-for-real-minutes (R-7). **T1 ✅ 2026-10-27** — `internal/trigger/schedule.go` + migration **0032** (`trigger_schedule` + `trigger_fire`, one migration — a schedule without its fire-record is half a promise): TR1 made physical (blueprint NOT NULL; a field whitelist tested as two readings that must agree, poisoned copies included); zone required IANA — offset, `Local` and bare abbreviations each refused with the sentence naming what was typed; expressions validated **at write**, robfig's own parse error quoted verbatim, and the sub-minute check measured over two successive fires (the first draft false-fired on `*/1`); window pair-or-absent with reasons D7 can print; webhook kind is the same row with a different cause — secret named by env var, never stored. **Q1 answered**: five-field + named descriptors + `@every` ≥ 1m; seconds and `@reboot` refused. The ledger count-pins moved 31→32 by design (loop `counters_test`, store policy/schema pins — they exist to be moved by whoever needs the column). 35/35 packages ok, 2439 PASS. **T2 ✅ 2026-10-27** — the clock is injectable from the first line (AST-pin: `clock.go` holds exactly ONE `time.Now`, every tick path zero; the schedule validator keeps its one write-time never-fires probe and only that), the fire's only door is `Starter.StartFromTrigger(ctx, Fire)` with **no field on Fire that can speak attendedness** (D1 as a signature, reflection-tested), and the fired loop walks the human's own start path with the daemon authoring the hard `attended=false` beside the pin. **Migration 0033** (R-15) gives the fire its workspace + goal and the loop its `started_by`/`trigger_id` — provenance in columns owned APART from attendedness (R-3), the surface table naming all nine new methods, the count-pins moving 32→33 as designed. Unwired gates announce themselves (`WiredReport`: overlap/access NOT wired until T3/T7 — P31's third state printed, never inferred). **R-14 settled by measurement before it could be softened:** the design's 'suspends at its first outward action' is deny-at-once under S11 — the LIVE test (real `aeon serve`, seeded row, real tick) proves fired/attended-0/started_by=trigger/blocked-with-the-S10-sentence/zero-receipts. `loop list --started-by` filters server-side and prints the provenance sentence. **T3 ✅ 2026-10-27** — TR5 as a store-backed gate: the previous fired loop's LIVE row answers per schedule (skip + record naming the loop, never `did_not_fire`, never an absence — and nothing is queued, AST-asserted: no `queue/pending/backlog/retry` identifier can exist in the package). **Q2 re-answered on the real vocabulary before its first commit** (R-16): the design's 'suspended' is STEP-speak — S6 suspends the step and the LOOP row says `waiting`; blocking on the literal word would have blocked nothing. The ruling as shipped: `running | waiting | blocked` own the one-at-a-time slot; `done | aborted` release it; unknown statuses (step-words on a loop row, typos) fail open with the loud log elsewhere — a dead run must not silence a schedule forever. The daemon tests copy the daemon's OWN wired hook (they cannot pass by wiring a friendlier one), and `awaitSettled` learned the runner's last act is an emit that reads the store (measured nil-pointer under full-suite load; settle = terminal row + terminal steps + stability across polls + grace beat). 35/35 packages ok, **2464 PASS** (+7). **T4 ✅ 2026-10-27** — the burst and the silence, both answered: startup `Reconcile` (before the ticker, beside the reaper) walks the missed instants through the **same `fireAt` a tick takes** — one definition of fire, no second path past the gates — catches up exactly ONE (the newest: a week of dailies starts one loop, the design's own 10,079-minute week starts one, measured `firedCount==1` against a genuinely-down process with a 3-minute seeded silence), and records the middle as ONE range+count row — migration **0034** `trigger_gap`, the shape stated because row-per-instant would make the record the slow part of the outage and a bare count could not answer 'when were you silent?'. **Q3 answered as the design answered it**: a PAUSED schedule records NOTHING — no fire, no gap, the frontier stands (enforced in `Reconcile` itself, not only the store filter — defence in depth). The three answers are a closed value (enumeration-pinned; the two-booleans' fourth combination has no representation) and never collapse in rendering (five distinct sentences, tested not conserved). The walk is ONE shared bounded function (`walkInstants`, cap 20,000 — set ABOVE the largest honest outage; past it the frontier is corrupt data and the schedule is left untouched, loudly, for a human). Count-pins moved 33→34 by design. 35/35 packages ok, **2474 PASS** (+10). **T5 ✅ 2026-10-27** — the two days a year, measured before handled (R-18): Go's `time.Date` resolves the missing London hour FORWARD (01:30 → 02:30, no error) and the doubled hour to the **GMT** pass, while Santiago's missing hour resolves **BACKWARD** (Sep 6 00:30 → Sep 5 23:30) and its doubled one to the **first** pass — opposite directions across zones, so which-one-Go-picks is not a rule to build on; robfig hands BOTH doubled passes (the naive duplicate, live in both hemispheres) and SKIPS missing days silently. The handling owns both edges: the walk collapses doubled walls to the first chronological pass (`skipped` + already-fired note per dropped pass — 60 notes for the fold under a minutely schedule), and a pinned wall that vanished between consecutive instants is probed by CALENDAR FIELDS (`p.AddDate` normalizes through the instant and walked a probe back out of the southern gap — measured, then fixed) and recorded `does not exist in Europe/London`, the only message that is useful. Southern hemisphere covered both ways (Chile folds in April and springs on the first Saturday of September at 24:00 — the task file's London-02:30 example is the US shape; London's missing hour is 01:30), zones asserted real (`Zone()` differs January vs July — a FixedZone would pass every test while proving nothing), tzdata facts guarded with re-measure skips. 35/35 packages ok, **2482 PASS** (+8). **T6 ✅ 2026-10-27** — the untrusted door, mounted not built (TR11): `webhook.deliver` is a registered protocol method with a conformance fixture and an action-table entry — the trigger package ASTs itself to zero listeners, and the surface keeps exactly ONE none-level route (R-5 pinned in the rendering test: POST /v1/webhooks, named; everything else stays apikey). Order signature→replay→overlap→governance→start (TR9: the verified delivery still faces the gates — `Gate.Access("webhook")` asserted); HMAC-SHA256 constant-time over the EXACT bytes (the body is read exactly once — the first mount drained it and the payload arrived empty, measured); replay refused by delivery-id-or-digest, 4,096-key bound and ±5-minute timestamp window (mechanism STATED); the secret is only ever `os.Getenv` — ONE call site counted from outside, argv/config grep-absent, value absent from every record a delivery leaves; the payload enters `with` carrying S12's region label IN THE VALUE (Q4 — same mechanism as W23/MC7/AS12, no fifth written; the random boundary survives a payload printing the closer verbatim). Zone belongs to the expression, so a webhook row carries NONE — T1's validator inherited the zone from a schedule helper and nobody had a webhook-shaped consumer to notice (R-19: zoneless accepted, zone-carrying webhook refused; body read exactly once; row filters moved to the consumers — a paused webhook ANSWERS 409 with a record, TR12's off is not a 404, unknown ids get one sentence that discloses nothing). 35/35 packages ok, **2506 PASS** (+24). **T7 ✅ 2026-10-27** — the governance switch is a policy TABLE, not a hardcode: `TriggerAccess`'s S10 refusing stub became a real evaluator (`spec_triggeraccess.go`, the SkillAccess-at-S14 precedent followed to the letter — shape stays at the boundary, decision arrives with the slice), and BOTH fire paths answer to it — clock tick and verified webhook, because a check on only one is the gap somebody finds later. TR9 made executable: signature first, policy second, measured by hook-call-count (a stranger at the door never reaches the committee), and the verified-but-denied refusal is byte-exact against `Verdict.Sentence()` plus the earned parenthesis "(signature verified — authentication is not authorization)" — S10's own one-formatter guard caught tests re-assembling the sentence and made them call the formatter. **The loud fact: a fresh install fires NOTHING** — PL5's default deny answers every tick until an operator authors the allow (asserted against the daemon a user starts; the suite itself now seeds allows, governance wired-not-decorative). The trigger is the verdict's SUBJECT (`refused: agent:ship · …`) so one trigger can be denied without killing the kind. Measured same-day: a closed store PANICS through the policy repo callbacks — the governance hook recovers into fail-closed ("refusing, not guessing"; a hook that crashes takes every schedule down with it), the store-side root cause named OPEN. Asked refuses (no gate exists at fire time — residual, stated). 35/35 packages ok, **2521 PASS** (+15). **T8 ✅ 2026-10-27** — the two off switches, kept apart with teeth (TR12): `aeon trigger list|pause|resume|fire-now|history` and the five socket methods behind them are ONE control plane — the CLI dials, the daemon seam calls `control.go`, and `TestTriggerSocketAndCLICallSameControlFunctions` scans the three files' SOURCE (comments stripped: a scan that punishes explanation selects for silence) to make a second implementation impossible (P34). fire-now is an honest convenience: it walks the SAME `fireAt` a tick walks (window, overlap, governance, the starter's hard `unattended` — asserted from the LOOP ROW over the socket, TR2 with no attended exception for a watching human), a PAUSED trigger answers it `did_not_fire` with the TR12 sentence RECORDED (Q3's silence governs the automatic paths, not a person pressing a button), and a WEBHOOK row REFUSES — the signature is its entire authentication and the convenience never becomes the bypass. The listing prints PAUSED and ONLY that (no policy word may appear where governance cannot be evaluated — `TestPauseAndTriggerAccessAreDistinct` pins the vocabularies apart), history shows the five answers AND 0034's gap ranges (TR4 complete). `manual` stays policy vocabulary: 0032's CHECK gates the row and no migration was invented to flatter a test (fireAt asks governance for `string(s.Kind)`, so the closed three will mean something when the producer arrives). The forbidden-word guard's blind spot was measured before fixed: `trigger.fireNow` tokenized as ONE word — camelCase is a boundary now, poison-tested. S27's sketch stands narrowed — clock, firing, unattendedness recorded as S20's with the pointer; selection, ready, caps and the sharpened open questions preserved; `TestS27SketchIsSelectionOnly` fails if the pre-S20 framing returns. 35/35 packages ok, **2535 PASS** (+14). **T9 ✅ 2026-10-27 (BUILT)** — the walk that earned its keep: nine sections on real binaries, real `curl`, 190 genuinely-down seconds, both DST cases driven through the daemon's own Reconcile over system tzdata (2026-03-29's missing 01:30 `skipped — never fires a wall time that never arrives`; 2026-10-25's doubled 01:30 `fired once (TR6)` with its recorded second pass), the fresh-install refusal seen at the ticked minutes, §2's outward stop read from the loop row (`blocked`, reason spoken, **no question row** — S11's unattended rule replaces the sketch's suspended question, F-1), §3's thundering-herd check measured (`loops: 1`), the 403 carrying its parenthesis live. **Eleven findings, five code bugs behind green unit suites**: `trigger add` never existed (the slice shipped every door but the one that adds — F-3, built in T9: socket method + CLI verb + fixture); governance matched the trigger's KIND, not its identity, and the fixtures mirrored the bug (F-4 — `TestKindNamedRowsUnlockNothing` pins it); fire-now's minute-truncation collided with the tick's UNIQUE key and blind-sided its own overlap query (F-5 — exact asked-instant, `TestSecondFireNowWhileTheLoopRunsIsSkippedAndRecorded`); a brand-new schedule was silent forever — empty frontier parses as zero time (fixed: `Add` seeds the frontier at registration; `TestAddSeedsTheReconciliationFrontier`); and the forbidden-word scanner read `trigger.fireNow` as one token (camelCase is a boundary now, poison-tested). QA suite drafted: [`docs/test_suites/aeon-triggers.md`](../test_suites/aeon-triggers.md), QATR-01..21 as properties, not counts. [Demo walked end to end](S20-triggers/DEMO.md). 35/35 packages ok, **2539 PASS** (+4). |
| **[S21 · Remote agents](S21-remote-agents/)** | 9 | ✅ **BUILT** — instructed 2026-10-28; sequential T1–T9, the first slice that talks to a machine it does not own. **T1 ✅ 2026-10-28** — `internal/remote/peer.go` arrives as a **reference, not a value**: `Peer`/`Pin`/`KeyRef` hold no `[]byte` anywhere (reflect-walked), the decoder is strict so literal key bytes under ANY name (`literal`, `value`, top-level `argvKey`) are refused at the door (C17), `%v`/`%#v` print only `key:env:NAME`, and `LoadKey` — the mcp injectable-getenv seam — is use-time only, never reached by Validate. RA9's three doors all refuse at **configuration** time (address, pin AND key), and the unpinned refusal carries RA2's whole argument — *a CA certificate proves TLS, not identity*. **Schemeless addresses: `http://` refused with D1's sentence, `https://` refused too** (the scheme is the transport's decision, T3's). `TestNothingIsDiscovered` greps peer.go's own bytes — banned tokens count even in comments — and asserts the positive (`DisallowUnknownFields`) so an over-eager ban list cannot pass by deleting the strictness; `Config{}.OutboundState()` states *"nothing outbound is reachable…"* byte-exactly (D13 stated, not implied). 13/13 green, package wall 0.003 s; **official 2925 PASS / 0 FAIL rc 0** (2912 at S40's close + exactly these 13; attempt 12 — attempts 1–11 met the recorded flake families verbatim: the closed-store panic, `TestASteerQueuedMidCall…`, `TestSlowCallerNeverDelaysALoop`, `TestNoHandlerHoldsAStoreLock`, all daemon-timing siblings T8's run weathered the same way). **T2 ✅ 2026-10-28** — `trust.go` is RA2 as code, written **before the dial exists** (the plan's one ordering): the pin is `VerifyPeerCertificate` mounted ALONGSIDE full normal verification — chain, expiry, hostname all still run (scope 5 as executable fact: pin matches + expired ⇒ still refused, refusal says *expired*; wrong hostname + matching pin ⇒ still refused), and `InsecureSkipVerify` is banned by grep over every `.go` in the package including tests, token assembled so the hunting file cannot contain what it hunts, floor assertion so an empty scan cannot pass. **Q1: SPKI** — certificates renew every ninety days and a certificate pin breaks on the first renewal, which looks exactly like an attack; that is how pinning dies in month four, and the SPKI pin rides renewal because the key is reused (both halves tested: rotated key refused saying *\"pin no longer matches\"* with **both hex values printed**, same-key renewal accepted). Rotation has an answer, so the pin is permanent not temporary: re-key fires the pin, the refusal is CORRECT, re-read after out-of-band confirmation — never an automatic re-pin (self-repinning on first encounter is TOFU, and TOFU's first encounter is the attacker's best day). **Q2: leaf pins only** — a private CA widens trust to that CA's whole issuance policy; the cost at ten peers is stated (ten config edits per coordinated rotation) and the mitigation is the kind choice, never a store that trusts more. Tests mint certificates in memory (only that way can an *expired* cert be had to order); `testdata/certs/gencerts.go` is committed and regenerates the ~10-year demo PKI + `PINS.md`, with `peer-rotated.pem` the rotation made bytes — D2's real-CA-valid-right-host refusal and the CA-set-alone gap (identical handshake accepted without the hook, refused with it) . **T3 ✅ 2026-10-28** — `dial.go`: the first bytes Aeon ever puts to a machine it does not own, and the file is three refusals to be helpful. **One path and it verifies** — grep: one `tls.Client(`, one `tls.Config{`, one `VerifyPeerCertificate:`, one `func dialVerified(`, zero `tls.Dial(`; behaviour: a pin-less struct-literal peer is refused with the raw-dial counter at **zero**, so the unverified path has never existed to persist (the plan's ordering risk, closed by observation). **Q3 as numbers: connect 5 s, call 30 s (MCP's number for the same shape of thing), async Task expiry 1 h** — not a connection timeout (RA7 removed the connection; expiry is stamped at suspension and evaluated at completion or sweep); both caller-shortenable never lengthenable, measured both directions. **No plaintext mode:** the grep bans assemble their own tokens so the hunter can't contain the hunted, and the loopback ban is on the STRING `"localhost"` — prose may name the host, code may not branch on it. **Three outcomes, one struct, one formatter:** the deadline sentence says *did not answer*, the empty-result sentence owns *the peer reported nothing*, and the two may not borrow each other's words (asserted literally — D8 three ways); `DegradeNote` speaks the absence (peer, reason, consequence — RA6/MC9; a live peer degrades nothing). **No retry is counted, not promised:** refused port → 1 attempt, hung peer → 1, two calls → 2. One connection per call measured from the server side: 2 calls, 2 conns accepted, 2 closed — RA7 on the wire. **Measured lesson:** the first hung-peer fake reused T2's polite helper; its `Close()` sent close_notify and the hung tests returned *result* — *zero bytes, sincerely, the peer* is an answer, not an outage; the real fake blocks forever unclosed, and that distinction IS D8. 33/33 green, wall 1.03 s (the wall is the degraded calls themselves, which is the property); **. **T4 ✅ 2026-10-28** — `egress.go`: the slice's one genuinely new risk, and its test is an ORDER, not a behaviour. **RouteAccess before assembly** — the assembler is a named interface the tests spy on, the files it would read are counted, the socket is counted at the raw-dial seam, and a denial's proof is **three zeroes** (assembler 0, fs reads 0, dials 0), every denial test carrying an allow-control (1/1/1) so the zeroes can't come from a broken spy. The refused path never reaches the assembler in code either: `Admit` returns before it is reachable, and the refusal is `policy.Evaluate(...).Sentence()` **concatenated, never re-worded** (one formatter) + the guarantee line operators learn to look for: *\"the request was not assembled. no file was read.\"*** **The subject is extended, no type arrives (RA4)**: `route_access` gains `remote:` route values (`remote:docs-peer`, `remote:*` — single-trailing-star only, mid-name stars refuse to decide, the prefix IS the namespace); `TestNoNewPolicyTypeWasAdded` greps S10's `types.go` for any `egress` constant and asserts the closed set still holds seven. **The fill is the house template verbatim** (S14 SkillAccess, S20 TriggerAccess): schema/validator stay in spec_stubs, the evaluator arrives in `spec_routeaccess.go`, the stub-case row leaves with an absence comment, the in-force could-not-determine pin moves to `effect_class` (S11 moved it off command_access; pins follow the placeholders or they pin a dead sentence), and the two hand-pinned decided lists gain the type in the same diff — the tests exist to notice the move. **Per-invocation, not per-configuration**: rows read at call time through `g.Rows`, clock a seam, and a row's `NotAfter` falling between two `Admit`s on the SAME gate flips allow to deny — a cached verdict is a permission that outlived its grant. **Unreadable policy fails closed** with the guarantee line still attached. remote 41/41; policy/protocol/cmd re-greened whole (official **2953 PASS / 0 FAIL rc 0**, 2945 + exactly 8 — the removed stub-case row emitted no PASS lines of its own (a table loop, not t.Run subtests): . **T5 ✅ 2026-10-28** — remote results are untrusted, always (RA5), in ~60 lines added to `dial.go` (the task's file list — T3's left-over seams): `Outcome.Labelled()` = **S14's mechanism imported, never echoed** — `label.Region` is the one grammar W23/MC7/AS12/TR10 share, and the test holds both ends (boundary literals banned from dial.go, `label.Region(` mandatory — a fifth implementation is the failure the label package exists to prevent). The label NAMES the peer (two peers in one transcript stay readable) and carries RA5's own sentence. **Deadline and error render unlabelled on purpose** — they are Aeon's own words about a peer that never spoke; wrapping them hands the label back to the text that replaces the stranger (S15's AP7, one layer up). **D6 by grep**: `IsFirstParty(`/`SetFirstParty(`/`firstParty`/`TrustedPeer`/`trust-peers`/`skipLabel` banned across all four source files (identifiers, assembled tokens) — the predicate is never even consulted on this path, which beats consulting it and hard-coding false. **Peer claiming first-party: ignored, visibly** — `Provenance()` reports *"peer claimed first-party — ignored"* for every spelling, over-reporting deliberately (P31: suppressing the note to dodge false positives is one call away from suppressing it for the true ones). **Anti-spoofing holds**: forged opener+closer inside the payload, exactly one genuine closer at the end, token unguessable, stranger's words verbatim inside (labelling is not filtering). Recorded gap: `--provenance`'s CLI mount has no task in this slice — the socket-ready string exists, T9's walk prices it. remote 46/46; **official 2958 PASS / 0 FAIL rc 0** . **T6 ✅ 2026-10-28** — `callback.go`: the slice's sharpest security task, and **the order is the file** — TR9's discipline one layer up: `signature → replay → lookup → awaiting → callee → land`. **Verification proves WHO; binding proves WHAT THEY WERE ASKED** (RA8): the three binding refusals land DEMO-verbatim (*"signature verified · no such task was created by this daemon"* — the verified identity is exactly when the sentence can afford to inform; *"came from docs-peer, which was not its callee"* — asserted leaving the task untouched), and a bad signature **provably costs no store read** (the fake TaskBook counts its touches; every bad-sig case asserts the counter never moved). **Replay got two guards that say different things**: the byte-identical resend of a LANDED completion meets a bounded in-process landed-signature cache (TR9's shape and its honesty argument); a DIFFERENT completion for a terminal task meets the store's own *"already complete, or never async"* — both refuse, collapsing them would blur two facts (P31). **The store's silence is `could_not_say`, never `refused`** (third outcome; rendering inability-to-answer as a verdict claims a decision the store refused to make; still recorded). **Unknown peer ≡ wrong signature, byte-identical** — the endpoint cannot enumerate configured peers. **The canonical is LENGTH-PREFIXED** (result is free text; an unprefixed join lets a delimiter-forgery re-claim the HMAC; injectivity tested with a deliberate field-shift); `SignCompletion` exported so the fake peer signs through the SAME canonicaliser. **The mount is the full S20 T6 chain, recorded as the task's under-list** (files_expected missed protocol/daemon/cmd, like T5 missed the CLI): public `remote.complete` + success fixture (N2), `KindCallback` — kinds CLOSED-still, 3→4 same-diff — R-5's exemption a NAMED function with the keyless set ENUMERATED (webhooks + completions, both POST collections, both HMAC-first), S20's gate name kept and its claim rewritten; daemon holds only the `remoteCallbacks` seam (agentsvc never imported by the daemon — AS11 untouched; the adapter `remoteTaskBook` lives in cmd beside its red line: `Awaiting` = `TaskWaiting && TaskAsync`, `Land` maps `Reject.Field` — status/mode is "not there to land", anything else the store failing to ANSWER). **Landing IS `TaskStore.Complete`** — S14's first-record-wins CAS + cold `Resume`: the row IS the suspension (RA7 asserted by NumGoroutine pinned across 100 deliveries + a reflection sweep finding nothing in Callbacks that could hold anything open). `AEON_REMOTE_PEERS` unset = unwired answering the absence by name; set-but-unreadable fails boot loudly (a pin that silently stops enforcing is the failure). protogen regen rides the user's unstaged ui/ tree. remote 59/59 (13 new); narrow/protocol/daemon/cmd each green whole; **official 2971 PASS / 0 FAIL rc 0** . **T7 ✅ 2026-10-28** — the chain through a remote peer, files_expected honoured **exactly** (dial.go + chain_test.go — the slice's first complete list). **Q4 answered: a peer's internal fan-out does NOT count toward AS7's depth — the reason is observability, not preference** (invisible fan-out cannot be seen, recorded, or bounded; an uncheckable bound is decoration that fails open where it matters), so **AS7 bounds OUR delegation, not the transitive closure** and the boundary is STATED: VisibilityNote rides every rendered remote edge; refusing on a peer's *claim* of an internal cycle is guessing dressed as caution (tested both ways — an observable call-back row closes a cycle → refused before edge/wire; a self-reported cycle with no rows → proceeds). **The bound reads rows, not claims — enforced by the type system**: CheckAsk(caller, remoteCallee) receives two agent names and NO payload (a peer-reported internalDepth of 9 cannot reach a bound; the depth gate passes at our-4-hops over the claim and the rows under remote: nodes stay empty). **The walker is injected** (StoreChain.Check) — remote must not import agentsvc **even in tests** (sweeps count test binaries, plugin's precedent); the production walker arrives from the entry point; **T9 carries the 4-line ChainCheck export + mount**, recorded there. **The tests caught a real defect pre-commit**: Ask lacked S14's spine check — CheckAsk judged the POLICY agent while edges join through the STEP row (an ask filed under another agent's step makes the walk diagnose someone else's loop); now the step's agent must equal the asking agent, S14's verbatim refusal, structural via stepIdentityReader where rows are reachable. Second red was PL5 doing its job: a depth chain needs the egress allow to name the deeper agents too. **Order: Admit → spine → CheckAsk → RecordEdge → OpenTask → Assemble → dial** — the task is minted BEFORE the dial (a completion must never arrive for a task that does not exist yet — the order that makes RA8's refusal mean what it says), edge before task (crash-between = recorded attempt), and a dead dial keeps both (AS6 records attempts, not successes — tested against a dead address). Every pre-assembly refusal carries T4's guarantee line. Restart is one sentence wider than S14's: remote edge = the same agent_chain table — close the directory, reopen, re-walk, identical. remote **63/63** (4 new); gofmt/vet clean; **official 2975 PASS / 0 FAIL rc 0** (2971 + exactly 4; attempt 4 — load spiked to 6.3 mid-slice and the recorded live-boot flake families ran hot: TestASteer / TestBothMountings / TestStartupAndDoctor / TestNoHandlerHoldsAStoreLock + the panic family, . **T8 ✅ 2026-10-28** — cost.go: unknown stays unknown (RA10/P31/P32), the task's two files plus the claim's own carrier recorded as fill paperwork (Completion.Cost rides the SIGNED canonical — a number not bound to the signature is not a claim; Land gains the cost beside the result entering the same first-record-wins CAS; params + daemon map + cmd adapter to CompleteWithCost). **The best state a peer's claim can buy is PARTIAL, never known** — a known parent resting on a stranger's number is P32's lie with a signature; the peer's total becomes OUR floor (USDAtLeast), Missing names remote-cost-unverified:<peer>, Reason quotes the peer — the record says whose number it was (scope 3), and a sweep pins NO construction path produces known. **Two silences, two names**: landed-no-report = the new token peer-reported-no-usage (mirrors provider-omitted-usage); not-yet-answered stays S14's cost-not-reported (scope 2). **Zero is the look of free and OMITS** — both number fields stay zero so the JSON has no usd key (M9); RenderCost's unknown branch is digit-free by construction (kebab-word reasons → ContainsAny checkable); inside a total only the measured FLOOR may render (S3's honest at-least) and the total still says unknown in words. Foreign-currency claim: named, not counted, never converted; negative → a claim no ledger can hold. **No second rollup proven grep-style** (S14's no-package-var precedent): ban-list over comment-stripped CODE (prose legitimately cites what it extends), assembled tokens (the hunter cannot contain the hunted), width floor, and the citation requirement the other way. P32 propagation stays S14's gate (TestUnknownChildMakesParentNotKnown); T8 pins the remote child ARRIVES in the fold as exactly the unknown-shape (contract fields + the table run through model.Add). **Two fixtures, one truth** (S20 T7's lesson): complete-with-cost.json beside the original so protogen teaches "cost"? optional — one fixture without it leaves the field un-described, one with it would teach a required field. Land's Reject.Field: cost falls to the error side — our own CostFor refused by validateCost is OUR bug, answers could-not-say (P31). remote **71/71** (8 new); protocol/narrow/daemon/cmd green whole; **official 2983 PASS / 0 FAIL rc 0** (2975 + exactly 8; attempt 2 — attempt 1 met the recorded TestRunReconcilesThenDrains… tick flake). **T9 ✅ 2026-10-28 (BUILT)** — the walk that earned its keep again. §§1–9 walked against the committed fake-peer PKI and LIVE daemons (`aeon serve --http` + `AEON_REMOTE_PEERS`, key by path): §7's unknown-task refusal measured **verbatim** through the narrow surface with a peer-keyed HMAC over the length-prefixed canonical (external byte-equivalent signer — the server agreed), one-hex tamper → 401 with the store provably untouched, unknown peer ≡ bad signature (no enumeration), stale timestamp refused AFTER verification (the order observed); **two listeners, not three** (unix 0600 + one TCP); `debug goroutines` → 0 remote waiters. **§2's cert validity confirmed out-of-band, not on trust** (`openssl verify -CAfile ca.pem peer.pem` → OK; not expired until 2036; cert/SPKI/rotated hashes recorded — the rotation refusal is checkable bytes, not folklore). **F-5 was the walk's catch: `narrow/errors.go` dropped `Data.What` on the store-unavailable branch — T6's unwired-by-name promise and BOTH S20 wiring answers reached peers as a generic retry-me store outage; fixed at the mount, measured verbatim after, pinned by `TestStoreUnavailableNamesWhatIsAbsent`** (the package tests pinned the handler's error and could not see the mount's rendering — a refusal must name the thing refused at EVERY surface). **F-3 priced honestly: no task built the outbound invoke mounting** (no `aeon remote`/`aeon skill invoke` verbs; `agent.invoke` refuses a `remote:` callee — AS2 holds; no production caller of `EgressGate.Ask`), and the design's own gate keeps S14's `invoke.go`/`chain.go` unedited here — the 4-line `ChainCheck` export that T7's note promised "T9 carries" **ships with its first consumer, not as dead code** (recorded reversal, T7's note corrected at the source). Eight findings, nothing softened (F-1/F-2/F-4/F-7/F-8 document the missing CLI verbs and the live legs that need the caller task — package gates stand as witnesses; §1's absence promise kept at the surface a peer actually sees). "Does not show" verified after T1–T8: **Aeon-as-remote-agent still out** (the inbound door receives completions ONLY — N2), no discovery/mesh/federation/CA, closed set still seven. QA suite drafted: [`docs/test_suites/aeon-remote.md`](../test_suites/aeon-remote.md), QARA-01…15 as **properties, not counts**, LIVE legs cited to the walk. [Demo walked with the eight-row ledger](S21-remote-agents/DEMO.md); cumulative DEMO carries the section. ****official 2984 PASS / 0 FAIL rc 0** (2983 + exactly the mount gate; attempt 6 — attempts 1–5 met the recorded families: the closed-store panic ×4 and `TestASteerQueuedMidCall…`, none in T9's files) — **S21 BUILT: T1–T9 ✅**. Next: none — the slice stops here awaiting instruction. | Agents that run somewhere else |
| **[S22 · Retrieval](S22-retrieval/)** | 9 | 🔨 **building** — instructed 2026-10-28; sequential T1–T9; THE optional slice: everything here must survive deletion (D1). **T1 ✅ 2026-10-28** — `internal/retrieval` arrives as interface+no-op (untagged, ALWAYS compile) + tagged index/availability; migration **0040** = `retrieval_corpus` + `retrieval_chunk` + FTS5 with its three sync triggers — the **kind is a COLUMN** (Stage 5's concluded corpus is a row and a validator, tested as data), its `provenance`/`observation_count` reserved NULLable with a CHECK that makes a belief-without-provenance **unwritable the day the kind ships**, and `CreateCorpus` refuses `concluded` naming Stage 5 (S11's named-value refusal; a comment cannot reject). **Q1 measured**: the ledger accepts the virtual tables and S4's fresh-vs-upgraded equivalence survives FTS5 shadows + `vec0` — no exception needed, none granted (A7). `vec0` is NOT in the migration by design: dimension is DATA (V6), so `EnsureVecTable` creates `retrieval_vec_<corpus>_d<dim>_<sha256(root)[:16]>` on demand (corpus gate `^[a-z]{1,32}$` — identifiers cannot be bound; the root hash is why a worktree is a distinct index, V12). **The slice's biggest measured fact: FTS5 needs `-tags sqlite_fts5`** (vec0 answers untagged via DB18; fts5 dies `no such module: fts5` same binary): the Makefile gained `GO_TAGS` on EVERY go invocation (build/test/vet/race/run/dev/vectors/pin), CI spells the tag in its own steps + a **no-retrieval test step and build step (D1 as a property, O1)**, delve got `buildFlags` (same trap its `CGO_ENABLED` comment already names), untagged opens fail LOUD naming fts5 — the honest shape, since D3's four reasons have no fts5-missing state. `Availability` keeps the three questions apart (`Available` answers a query, `KeywordOnly` says WHAT you get (D4), `Reasons` answer WHY NOT (V19's four: `no-provider`/`index-empty`/`model-mismatch`/`dimension-mismatch`, all eight table rows reachable; both mismatches name BOTH); route-present-nothing-embedded is keyword-only **without a reason** — the closed four have no not-yet-embedded word and `no-provider` would lie. `ErrNotBuilt` at the Noop seam: absence is never an empty slice (V13); one `Backend`-only consumer compiles both backends. Self-reference handled S21-style: the ban-scan assembles its tokens and strips its own gate name, floors on both greps (≥5 files, ≥300 files). **The no-retrieval suite caught two landmines before the official run**: exec'd CLI binary built untagged (`AEON_GO_TAGS` now rides the integration build) and a fifth count pin in `internal/loop` (39→40 there, ×3 policy, ×1 schema — moved by design); plus `automation_schema_test`'s `[:len(Migrations())-1]` vintage silently moved when 0040 landed → pinned `[:38]` per the `[:36]` precedent. 12/12 gates; store suite green whole; whole suite green at the noretrieval tag; vet clean both configs. **T2 ✅ 2026-10-28** — `chunk.go` + `stale.go`: **Q9 stated in the file header** — brace families close at a column-0 `}` followed by blank-or-declaration (V11 from outside a parser, deliberately string-blind), indent families at column-0 return (py/rb/ex/yaml), **everything else the 120/20-overlap window as the NORMAL path** (no degraded flag exists — prose and unknown languages are most of a repository, and a fallback treated as exceptional is one badly tuned). Found by the tests: **trailing blank padding was a chunk** — deleting a function promoted a leftover blank to a phantom write (`TestStalePlanDeletesVanishedChunks` caught it; `bounded` trims trailing blanks, interior stay); and the honest long-line shape: V4 ranges cannot name a column, so a 200 KB line is **skipped and reported** (path/line/bytes/64 KiB-reason) while the file keeps its chunks — one loud skip, never a silent blob or a cancelled bundle. Binary = NUL-in-8 KiB (git's rule, stated); skips returned never silent. **`PlanFile` = comparison not work** (Write/Delete/Present/Unchanged/Skips from stored hashes — `RetrievalChunkRow` carries no Body, V4 at the seams); `ApplyChunkPlan` upserts + **except-prunes, never wipe-then-write** (a wiped file answers empty mid-reindex). Store pair added the house way, surface row extended same-diff; vec0-delete seam recorded for T3 at the site. 12 new gates green (24/24 package). Official **3008 PASS / 0 FAIL rc 0** (2996 + exactly the 12; attempt 1)  **T3 ✅ 2026-10-29** — embedding IS a route (V16): **Q5 — the route type needed no generalising** (kind/provider/model/base-URL/prices/NoPrice apply to an embedding model unchanged; M16's required `MaxOutputTokens` lives at `ModelRequest`/call time, so an embedding route honestly stores 0) — what was chat-shaped was the CALLER, so the fix is `Embed` on the SAME `model.Client` (one `httpClient`, one adapter registry, one Bearer line, **cost at the same single `ComputeCost` site** — V10's three states INHERITED not rewritten, unmetered = `unknown`+reason by S18's own door (M30)) plus the purpose-vocabulary word `embedding` (metadata.go) and an OPTIONAL `Embedder` adapter interface (anthropic refused at the door NAMING the provider before any dial; `ModelClientEmbedder` IS the S18 client — I8 made literal, `TestNoSecondProviderPath` greps retrieval for no-HTTP/no-key/no-price with ban tables EXTERNALISED to testdata (assembled tokens still live in the scanner's bytes — hunter/hunted third occurrence; my own comments' `APIKey`/`HTTP` fired the ban: a ban is a ban in a comment). **O5 is mechanical**: resume state = the rows (`VecChunkIDs`; missing vec0 table answers nothing-embedded, not error); the flake test COUNTS the retry's calls — re-sends exactly the failed chunks. Bounds 96/256 KiB, either cuts; partial geometry refuses ONE chunk and the batch-mates land. **The test caught a real bug pre-commit**: dimension from `vectors[0]` stamped the whole corpus at a corrupt first vector's length (11 failures where 1 was owed) → `modalDimension`, the length the batch AGREES on; row stamped BEFORE the first insert so a half index still answers Availability truthfully. New reason `call-not-attempted` (a pre-wire refusal is not a pricing unknown — the states stay three). vec0 v0.1.6 semantics MEASURED before writing the doors (metadata-column JSON inserts ✓, rowid-insert without chunk_id dies, delete-by-chunk_id ✓ — upsert = delete-then-insert, two vectors for one chunk would let it retrieve twice for the same code). **Two committed S18 guards ADMITTED, not rewritten** (O1 cost-construction guard admits `embed.go` — same client's other method, excluding it would hide the property; strict-leaf guard skips `*_test.go` — external test pkg is no dependency edge). Files_expected justified: `internal/model/route.go` does not exist (route = types.go + store/routes.go, neither needed a change — that IS Q5). Foreign recorded: user's live `loop_transcript` WIP (05:33/05:37) fails 2 own tests + surface guard — never staged. 14 new gates (retrieval 33/33); vet clean both configs. **Official: 3022 PASS / 0 FAIL rc 0** (3008 + exactly these 14; clean-tree rehearsal `git diff HEAD --cached` onto HEAD, -buildvcs=false + `AEON_NO_BUILDVCS` harness flag) — in-TREE full run rc≠0 ONLY on the named foreign live WIP: the user's `loop_transcript` trio + `TestTheMethodTableHasExactlyTheExpectedMethods` (`loop.transcript` registered un-pinned) + `TestRegeneratingProducesNoDiff` (their contract.ts WIP) + the recorded flake families; `TestSuitePassesWithoutThePackage` fails ONLY on the rehearsal tree where S41's uncommitted notify WIP does not compile — passes in-tree (S40's uncommitted-foreign-tests precedent applies) **T4 ✅ 2026-10-29** V3/D13 as code: RRF over ranks only (source scores never enter the sum — scan + behavioral); FTS5 AND-semantics + vec0 subselect-join MEASURED first; skips = S11's walker + stated size rule, vanished pruned and NAMED; V4 property at the type level + planted token; query-time degradation to keyword-only stays visible (D4). 11 new gates (hybrid 8 + code 3; package 43→44). **Official: 3032 PASS / 0 FAIL rc 1** (attempt 3 of this task's clean-tree runs; 3008 + 11 new top-level gates + 13 PASS-lines my python attribution showed as load-shedding between `-v` runs — v1/v2 measured 2907/2842 under the same tree and the same sole foreign failure, cmd/aeon+retrieval re-counted alone at full strength); failures ⊆ named foreign set: `TestRegeneratingProducesNoDiff` (committed protogen drift at HEAD — `remote.complete` in HEAD method.go missing from HEAD contract.ts; T1's regeneration post-dates S21's method addition — foreign, named not fixed) + the recorded flake families (`TestBothMountingsCanRunAtOnce`/`TestASteer…`/`TestStoppingARunningStep…`/`TestConnectionCloseDetachesButTheShellLives`/ `TestStartupAndDoctorSayItIsOpen`, each green alone); rehearsal tree = HEAD + staged diff + named user-WIP compile copies (lifecycle.go, notify_test.go, protocol/types.go — HEAD does not compile the daemon without them) + two worktree-only rehearsal guards (store AppendEvent nil-w, EventBacking nil-store — the WIP panics their own doc comment forbids) + AEON_NO_BUILDVCS honoured by cmd/aeon's two exec'd builds (same discipline as AEON_GO_TAGS) **T5 ✅ 2026-10-29** V15 as a property with TWO enforcement points (contents + input-grep, ban table externalised); Q8 answered in code+CORPUS.md tested between them; N6 permanence grepped (no flag/tag/env can enable); the corpus wall moved to the WRITE door (`.md` skipped by name in the code walk — caught by the test); `redactSecrets` before every door. 10 new gates (retrieval 44→54). **Official: 3041 PASS / 0 FAIL, sole failure `TestRegeneratingProducesNoDiff`** (attempts 1, 2, 4 deterministic-clean; attempt 5 = 2917 under load-shedding load; `TestNoHandlerHoldsAStoreLock` once flaked (green alone, recorded family) — 3041 = the 3032 T4 baseline + 9 top-level PASS-lines net: 10 new Test funcs, the copied-WIP `transcript.go`'s `TestReadLoopTranscriptPageSpansSteps` pinning the user's WIP +2… measured, not assumed); rehearsal tree = HEAD + staged diff + named user-WIP compile copies (lifecycle.go, notify_test.go, protocol types/method-independent memory.go+loop_transcript.go, store/transcript.go — HEAD does not compile their daemon/memory path without them) + worktree-only rehearsal guards (AppendEvent nil-w, EventBacking nil-store, `ReadLoopTranscriptPage` named in the surface table — the user's WIP door) + AEON_NO_BUILDVCS honoured by cmd/aeon builds; foreign failures named not fixed (protogen drift at HEAD, per S40 precedent) **T6 ✅ 2026-10-29** — the tenth tool + `search.query` socket twin over **one resolver function value** (D15/P34): registry gains `search` only while wired (sole caller = entry-point `cmd/aeon/retrieval_link.go`, `!noretrieval`; off file no-op), noretrieval absence asserted BY the tagged build via subprocess inside the amended `TestSearchAbsentWithoutThePackage`; empty ≠ absent three layers (`index-empty` verbatim through both doors / wired-empty exists-and-reports / unwired absent-from-registry); Description carries the complement-grep rule verbatim (O7); corpus required naming both options, `ValidCorpus` the authority (O8); `search.SearchHit` has **no text field** (D8 as type, rendering adds none back); keyword-only speaks in the RESULT (V9). **Type home forced by the guards** — `protocol↮tools` and no retrieval import anywhere but the walk exception → new leaf `internal/search/types.go` (pure data + one function type); `RegisterSearchQuery()` registers post-wire (seams are assigned after `NewTable`). Class fixture `search`=`safe` + **`retrieval_only`** marker keeps the committed list honest in both builds (40→41 DELIBERATE); `TestRegistryExactNames` extends `want` only while wired. Path guard keeps teeth: `search.go` bans `os.Open/ReadFile/Getenv/Getwd/filepath./exec.` outright (root = row KEY). Conformance fixtures + `contract.ts` regenerated SAME commit — protogen's coverage gate caught my own gap (attempt 2), and the regeneration swept the S21-era committed `remote.complete` drift (both named-foreign protogen failures closed as a consequence of this task's fixture work; partial regen not possible); `cli_integration_test.go`/`ask_test.go` exec'd builds now honour `AEON_NO_BUILDVCS` (attempt 1 died on the gap T4's record claimed closed — audited). Embedding is a route (V16) — first by sorted id whose Capabilities carry `embedding`; clean box → honest keyword-only. Registered-not-granted; UI = T7. **14 new gates. Official: **3051 PASS / 0 FAIL, rc 0** (attempts 1–5 audited: 1 = cmd/aeon exec-build died on the AEON_NO_BUILDVCS gap the T4 record claimed closed — fixed by honouring it in both exec'd builds; 2 = 3049/2 — `TestGeneratorCoverageIsStated` named MY gap, `search.query` without a fixture (closed by the two conformance fixtures), `TestRegeneratingProducesNoDiff` the S21-era committed drift (both closed by the same-commit regeneration); 3 = 3050/1 and 5 = 3050/1 — `TestASteerQueuedMidCallSkipsTheTurnsToolsAndTheLoopCarriesOn`, the recorded flaky family: reproduces on a HEAD-only tree at the same rate (3 FAIL of 8 solo runs on the HEAD copy-set vs 1 of 5 on mine — pre-existing timing flake, not my diff); 4 = 2926 under load average 7.6 — answer-path nil-mutex panic family (daemon green solo in 54 s). Delta over T5 measured **+10** net top-level PASS lines (all **14/14 new gates confirmed PASS by name** in the official log); rehearsal tree = T5's named WIP copies + `loop_transcript.go` + worktree-only surface-table guard; noretrieval tag: builds + vet clean, absence subprocess green inside the retrieval suite)** **T7 ✅ 2026-10-29** — V7\'s differentiator as code: **Q4 — ONE context path** (resume and recovery share `ResumeStep`, recovery its `Recovery:true` variant that skips even rehash); retrieval resolves in `AssembleContext` ALONE (N7; `resolve.go` deviation recorded — a second file is a second assembly path), `TestResumeDoesNotReResolve` walks BOTH paths with spies at zero + byte-identity on the frozen provenance (counter bump exempted honestly); seam = closure over root (`ContextDeps.Search` over the T6 `internal/search` leaf — runtime imports neither hunted nor doors, guards green unamended; `Compile` gained `ctx` — cancellable, no ambient background). Entries carry `resolution`/query/score (V14/O2/C4 omitempty-structural); model-visible annotation on the policy-section channel (\u0060similarity-matched, not declared; verify with `grep`\u0060) asserted on the opening user message; three states kept apart — never-asked / asked-nothing-found / asked-refused (resolver\'s sentence in the record, O6); unreadable hits counted (index lags the tree), retrieval NEVER fails a step (V7 optional stays true). Step door `contextRetrievals` refuses unbuildable declarations by name (V13/O8); `step/with-retrieval.json` + `contract.ts` regenerated same commit (swept `contextFiles` in too — Go had it, fixtures never did); daemon wiring test proves three doors on ONE closure behaviourally (D15). **V20 measured: 4 `grep`/`read` without resolved context, 0 with** (rigged-fixture guard inside the test; live-model number named not-verified). **15 new gates. Official: **3062 PASS / 4 FAIL, rc 1** — all four named foreign rehearsal artefacts (HEAD-pinned table tests against the user's live agent/mcp methods copied ONLY so their WIP compiles: `TestTheMethodTableHasExactlyTheExpectedMethods`, `TestMethodTableInOnePlace`, `TestAgentFixturesMatchWhatTheHandlersReturn`; plus the regeneration drift their un-protogen'd methods carry in `TestRegeneratingProducesNoDiff`). Attempts: 1 = 3061/5 (the four + Success/Coverage fixture gaps — closed by copying the user's own conformance JSON); 2 = protocol build-death from an over-copy (their `handshake_test` referencing the stripped `MethodLoopTranscript` — rehearsal error, withdrawn); 3 = 3061/5 (the steer flake family alongside the four); 4 = HEADLINE clean of flakes. Delta over T6 measured **+15** — exactly the new gates, and **15/15 confirmed PASS by name in every official log**; noretrieval build + vet clean; the recorded flake family green in the headline run; rehearsal tree = HEAD + staged + the named WIP compile copies (lifecycle, daemon memory, protocol memory/types/agent_roster/mcp_trust/loop_transcript, store transcript, user method.go minus the loop.transcript registration) + worktree surface-table guard + user conformance fixtures** **T8 ✅ 2026-10-29** — V17/V18/V21 + O3/O4/D9/D10/D15: mismatch **refuses (naming both models), prices (labelled estimate with basis), reindexes NOTHING** (spy = chunk COUNT + embedder CALLS); `force` is the V17 restatement — wipe (chunks + vec table + recorded embedding, `ResetRetrievalCorpusEmbedding` added because the setter's positive-dim check correctly refuses to record "nothing") then rebuild under the named route. **Q7: the trigger is the tool RESULT (`write`/`edit`/`write_file` with a `Path`, no error) at `toolCaller.Call` — the ONE conversion point**; `bash` cannot trigger, `git.commitAndPush` refused as trigger BY NAME (a commit never changes the worktree). **Q10: estimate = the SAME walk as the pass** (`reconcileCode apply:false`), tokens=bytes/4 priced through `model.ComputeCost` at the embedding route's input price (my direct `USDPerMTok` read hunted by `TestNoSecondProviderPath` — the ban table catching its own slice); unmetered → **no figure + the words**, never $0.00 (M30). Async reindex: single-flight + 30 s debounce tracker, step NEVER delayed — asserted on step TIMINGS with the pass held open (V18/D10: timings, not lock checks); status merges running+percent from the tracker ("running"@0% ≠ "0%"). Staleness **per corpus by construction** (history never consults the tree — code churn cannot move its figures) and a half-done pass SAYS so (`status` figures + `SearchAnswer.Staleness` from the cheap `PendingChunkCount` — one store fact, one source). **V21 against a SPY**: status/stale-query/boot fire ZERO passes; only `retrieval.reindex` or `aeon index rebuild` — the CLI verbs are **socket clients** (grep-locked: no `retrieval.New(`/`store.Open(`/retrieval import in `cmd/aeon/index.go`, D15/P34); socket carries optional `embedRoute`/`force` (the To-proceed flow needs them; automatic paths can never say them), progress nil (id-first). 18 new gates. **Official: 3084 PASS / 0 FAIL, rc 0 — first S22 run on HEAD + staged ONLY, zero user-WIP copies** (candidate compiles standalone; attempts: 1 = 3080/4 — provider-path ban named MY price-field read + surface-table naming + same-commit `contract.ts` regen (+30, two methods) + narrow flake; 2 = 2958/1 startup flake under load; 3 = HEADLINE; **18/18 confirmed PASS by name**; both tag configs build+vet clean). **Rehearsal also found the T7 leak:** T7's staged `daemon.go` had carried the user's `notifyHintFor`/`PublishNotify` block — HEAD did not compile standalone; T8's blob restores the plain `d.publish` call (user's live tree untouched). noretrieval: the doors are absent-not-refusing (V13) and the slice still survives deletion (D1) **T9 ✅ 2026-10-29 — SLICE COMPLETE 9/9.** The demo walked on a clean box and got **corrected, not obeyed** (7-finding walk table in `DEMO.md`): the phantom `aeon tools list` named-not-built (the registry-lock tests carry its claim), routes verbs fixed, `aeon index status`/`index build` lines replaced with MEASURED outputs (the no-provider refusal and both corpus-naming refusals quoted verbatim), `GO_TAGS` both-tags fixed against T1's fts5 fact, and the `transcripts SKIPPED` prediction retired for the stronger truth — V15 enforced at the INPUT (nothing offered, nothing skipped). **Built this task:** `aeon search` — the human's client of the SAME `search.query` door (locations-never-text, keyword-only + staleness notes riding IN the answer), and `index status`'s honest "not built yet" ("current" beside `index-empty` was the claim T9 exists to catch). QA suite `aeon-retrieval.md` **amended never rewritten** — extension-loading corrected naming S4's DB18/S25 and KEPT, `tools list` steps corrected-not-deleted, QAV-10…16 carry V13–V21 by continuing the sequence; first-PASS curation recorded. README: S22 → **BUILT**. Named-not-verified (each suite-asserted at its gate): §5 priced mismatch, §6 timings/running-merge, §3 manifest identity on resume, all money figures. Official: noretrieval **rc 0 (39/39)**, fts5 **3084 PASS / 0 FAIL, rc 0** (attempt 3; attempts 1–2 = the two recorded flake families + the nil-mutex-on-close panic family at load 4.9, all green solo; delta over T8 = 0 new tests, the count held measured). **Next: S23** (first run & polish) — or S24 on the user's word. |
| **[S23 · First run & polish](S23-first-run/)** | 9 | specced | Install, first run, code signing, notarisation |

---

## Stage 5 · FLEET — S24, S25, S26, S28, S29, S30, S34, S35, S36 and S37 fully planned; S33 **built 2026-10-30**; S27 specced; the rest sketched

**[S38 · Organization layers](S38-org-layers/)** — 🔨 **building** (opened 2026-10-27): the records slice
the author's Oct-27 spec asked for, first of three (S39 chat-runs, S40-automation are shaped behind it).
**T0 ✅ 2026-10-27** — the investigation ran rather than read: Q1 found no blueprint-less row-kind (mode,
not row; `loop_blueprint` pins at start — `store/blueprint.go:157`) and a **CLI verb that never existed**
(`unknown command "project"` — T4 adds `component`/`project`); Q2 wrote the split thresholds before
measuring (1,600 total / 1,000 column-half) and came in under them (≈423 + ≈732) — **Phase A stays one
merge-commit `(S38 T1–T4)`**; Q4 read the terminal rule live (`decide.go:40`/`continue.go:40` refuse
`done|aborted`) making the **`waiting`-park** the slice's engine delta (S39 keeps the composer), and found
`step_steer` has **no author column** — OL15's "in 0027/0028" invalid (immutable), `source` rides **0036**;
Q7 executed OL1's DDL + the 0016-pattern NOT NULL rebuild on a populated store copy (integrity ok, FK
empty) — `project_id NOT NULL` lands inside **0035**; Q6 retired `work_item` (every reader of the loop key
is five `Scope`-taking store functions — surrogate `id` absorbs it); Q9 counted the legacy MCP at 25 names
and fixed the catalog at **33 tools** (14 legacy mutations are engine-owned P29, `loop_render` is dead;
registry pin 10 → 43); Q8 proved `DecideBrief` joins `AnswerGate`'s transaction (claimed-FROM, S35 TD1's
shape). Every provisional OL rule closed with a date; eleven reality findings (R-1…R-11) in plan.md; the
five spec gaps handed up (investigation Q10). **T1–T4 ✅ 2026-10-27, the rename, as one commit** —
migration `0035_org_layers` (renames, the group table, the implicit backfill, the NOT NULL rebuild)
plus store, wire, CLI and UI in one phase-A diff. The rebuild's `DROP TABLE component` fired 0005's
CASCADE and emptied `component_root` **under passing integrity checks** — snapshot/restore inside
0035 + row-COUNT tests now hold it (investigation Q7 addendum); the version moved **1 → 2** on the
argued BREAKING line (`MinSupportedVersion` with it — nothing shipped v1); two production clients
still spoke literal version 1 (`narrow.go`, `mcp serve`'s `dial(1)`) and the real-socket repro caught
what `make ci` could not; `aeon project` verbs exist at last (T4), and the app has two tier pages —
`/components` (what `/projects` was) and a new `/projects` for the groups, with OL2's fold as a pure
`visibleLayers` function. **Measured:** `go test ./... -v -count=1` **2552 PASS / 0 FAIL**
(`make fmt`/`guard`/`protogen` green); `ui`: typecheck ✓, build ✓, vitest **488/493** — the 5 reds
reproduce identically at the pre-slice `438b9e19` (they ride the foreign dirty `ui/package.json` on
this box, untouched on purpose); native desktop not re-walked (no Tauri libs here). **T6 ✅ 2026-10-27, the records entity store** (`goals.go` etc: goals pass `in_review`, decisions are
claimed FROM in one transaction by a human only, briefs version per loop, attachments pin document revs
and report currency — 15 new tests). **T5 ✅ 2026-10-27, `0036_records`** (seven tables, loop's goal
columns, the task surrogate-id rebuild). **T8 ✅ 2026-10-27, the plumbing** (`stepctx` carries the step's row id through the one dispatch site;
`ToolContext.Records` is a function seam — nil fails closed with a sentence; `BuildTools`' signature
never moved). **Measured:** full suite **2590 PASS / 0 FAIL**. **T9 ✅ 2026-10-27, the management catalog** (`internal/records` pure data — 23 actions,
project/component/epic/goal/brief/attachment/loop families; identity stamped from the step through
step → loop → component → project; the registry is **the honest 33**, default grant untouched at
`read, list, write`). **Measured:** full suite **2601 PASS / 0 FAIL**. **T10 ✅ 2026-10-27, the task family + the brainstormer** (catalog 29 names; registry **the honest 39**;
plan speaks its own refusal sentences through `task_create`; the ladder is reached THROUGH
`SetTaskStatus` never around it; `brainstormer` the first body authored fresh — provenance via its
drop record's LINEAGE). **Measured:** full suite **2607 PASS / 0 FAIL**. **T11 ✅ 2026-10-27, the blueprint keys** (`output:`/`emits:`/`requires:`/`decides:` load at schemaVersion 1;
the grant check runs through `handler.ToolsFor`; P31's third state kept honest in both directions; all
eight shipped blueprints byte-identical against `git show HEAD:`). **Measured:** full suite **2616 PASS / 0
FAIL**. **T12 ✅ 2026-10-27, contract enforcement** (a model step with `emits:` does not complete ok until the
records exist — one renderer for the refusal, unreadable/unwired both answer the third state, resumed
steps pass the same door; the steer nudge recorded NOT feasible — rework is the contract path).
**Measured:** full suite **2624 PASS / 0 FAIL**. **T13 ✅ 2026-10-27, start on a goal or task** (`goalId`/`taskId` on loop.start — checked before any row
exists, requires refused with candidates in hand, the goal version pinned beside the pin, the task gains
its worker link after the row; additive protocol, no version bump). **Measured:** full suite **2633 PASS / 0 FAIL**. **T14 ✅ 2026-10-27, the verdicts reach the ladder** (the pair ships — standardNames 10; the capture replay files the goal AND both attachments, and files-only is rejected by name; `DecideFromGate` lands the person's gate answer through `DecideBrief`/`DecideGoal` only, durable-first, claimed-FROM idempotent; revise re-issues v2 and decides nothing; the records arms now pin a lane's existing workspace file into `loop_document` — the missing bridge that made `attach` walkable, refusing a hopeful name exactly as before; bodies and drop records re-measured; **2641 PASS / 0 FAIL**, ui 488/493 the same 5 foreign reds). **T15 ✅ 2026-10-27, one definition, two doors** (the catalog projects onto the socket — 29 Specs become methods, the projection skipping names that already stand: `project.*`/`component.*`/`loop.list` registered before the catalog existed, `task.list`/`goal.show` standing as the person's richer doors; the parity test walks both directions. The service's person half is an allow-list plus a named refusal: a person reads the whole org without a step, edits only manual rungs (T7's sentence answers engine rows verbatim), and step acts are refused by name with the door that answers a person. Decisions stamp the KERNEL's peer uid — a smuggled actor field is inert (forward-compat pins protocol decode; the catalog decode keeps refusing), a second decision names the first decider, `task.changed` fires only after commit (the wire test asserts the row moved first), and the storeless door answers the third state through `Data.What`. `DeriveExecutionState` is a pure ladder, the cost composed from rows only — a grep guard walks `../store` to keep it so. protogen's lesson: method names come from consts in `method.go` (beside the handlers they emitted nothing). The grep guards caught their own file first: the refusal sentences named the decide methods literally, and the guard fired on `records.go` — sentences reworded, guard kept verbatim). **Measured:** full suite **2660 PASS / 0 FAIL**, ui 488/493 the same 5 foreign reds; `check-protogen` green, contract.ts +116.)

**T16 ✅ 2026-10-27, the terminal's org doors** (`aeon goal list|show|create|submit|decide`, `aeon brief show|decide`, `aeon task list|show|create` — P34 clients of the socket, three files that import `protocol` and nothing else (the DoD grep is a test now). The T15 refusal table had closed exactly the doors this spec opens; it shrank to eight and the four arms grew person branches that take the stamped half as SAID arguments — same store calls, same refusals, no second rules. Usage is exit 2 (`--version` is not optional at a decide), assignee kind rides the colon (`user:carlos`), cost prints with its floor flag, and one `personLoopScope` serves `brief.show` and the brief decision method — with an additive optional `component` on `brief.decide` for the measured fact that two components can run the same slug. The walk found what the spec could not know: `loop create` does not register a component and no CLI verb does either — hand-made tasks correctly refuse unregistered roots by name. The grep guard caught its own file again (a comment spelled a decide method's dot-name); the CLI decide stamps the runner's OWN uid through a real socket — 3002 on this box, asserted in the ledger line). **Measured:** full suite **2666 PASS / 0 FAIL** (−p 2; two timing-under-load flakes recorded for T17: one steer, one narrow-degradation), ui 488/493 the same 5 foreign reds.)

**T17 ✅ 2026-10-27, the close-out** (zero-regression: shipped blueprints unchanged, the ungranted tool offer now pinned BY DIGEST in a test this task had to write — the spec had assumed one existed, steering fixtures diff-empty against main; the walk ran on a throwaway store with every output quoted — `S38-org-layers/DEMO.md`, findings F1–F8 — the replay lanes green including files-alone-rejected and gate-answers-deciding; the **deletion drill** (branch `t17-deletion-drill`, −3324/+129 over 38 files) stripped `internal/records`, both projections, the YAML keys, the brainstormer body AND its drops record and the S38 blueprints, and the stripped tree builds and runs **2599 PASS / 0 FAIL** with the person's doors answering — the drill's findings are the headline: startup-panic coupling through handler grants (O1 catching it at the best moment), a projection file load-bearing for the PERSON door, and a hand-written re-registration that drifted on first write — T9's warning reproduced on cue; ROADMAP rows for S39/S40 added: **unblocked, gates MET, not started**; `make ci` red ONLY in the foreign-dirty ui typecheck (`router.tsx "home"` vs `RouteId`) — named, not ours).

**S38 is BUILT: T0–T17 ✅, measured 2667 PASS / 0 FAIL (−p 1 official; three load-timing flakes recorded at F6), ui 488/493 with the 5 foreign reds, Rust 5/5 with cargo present.** The S39 gate (after T13) and the S40 gate (after T7+T15) stand MET and recorded — **neither is started; the slice stops here awaiting instruction**.)

**Gate record (2026-10-27, S38 T13):** the **S39 gate is open** — S39 (chat runs on
the org layers) has everything it waits for from S38 (T11's keys, T12's contract,
T13's coordinates). It is NOT started: S38's Phase B continues (T14–T17) and S39
awaits its own instruction. The S40 gate (event fires with goal/task ids,
plus the board read and the person's decisions) is MET — T7 ✅ + T15 ✅ (2026-10-27).
S40 is NOT started: gates recorded here wait for their own instruction.)

**S39 — STARTED 2026-10-27 (the user's instruction), sequential from here.** **T0 ✅ (investigation):**
all nine questions `settled` by running every command — `idle` needs **no DDL** (no CHECK on
`loop.status`, measured at the DDL), CR1's reader table is complete and the `done`+`conversing`
fallback is **not** needed, `loop_blueprint` is **not** re-keyed (seven `store/blueprint.go` statements
are the write set; recovery reads steps, not pins), the `chat` agent + `chat.yaml` names measured
**FREE** (design header fixed to the author's ruling the same commit), S38 T12 did **not** pre-add
`step_steer.source` — T5 owns both columns after the ALTERs ran **on a populated store** (and the live
`chat_message` key is `component_root` — the 0035 rename the immutable DDL cannot show), the `run_start`
ask row is one operator command with **zero policy-engine change**, a turn-as-step is yes/yes/yes, and
the migration ground truth is **L=36 → T1=0037, T5=0038** with exactly eight pinned-count sites named.
`investigation.md` carries every quoted command; `design.md` CR1–CR3 are FINAL. **T1 ✅ 2026-10-27** — `0037_loop_run` lands: `loop_run` (one immutable pinned copy per run, the end-CHECK
tying `ended_at` to state, run state `running|done|failed|cancelled` with waiting/blocked **derived** from
`loop.status` per DB6), `step.run_seq` by ALTER (per-run cost is a P32 group-by, never a stored total), and
**run 1 backfilled from the pins** — provenance through `COALESCE(l.trigger_id,'')` (T1's draft learned the
column is nullable the loud way), goal coords from S38's loop columns, `task_id` honestly NULL. The seed SQL
speaks `component_root` — the live name since 0035; the count pins moved 36→37 at all eight T0-named sites
including `loop/counters_test`'s last-ID/name lock. **Measured 2676 PASS / 0 FAIL (−p 1 official)** — +9
tests, zero regressions; `make fmt-check` and `go vet` clean, `make vet` red only in the foreign-dirty ui
typecheck (named at T17, not ours). **T2 ✅ 2026-10-27** — `runs.go` gives the store its run verbs: `BeginRun` claims an `idle` loop
(`UPDATE … WHERE status='idle'` — the claim IS the guard), inserts the next `loop_run`, and re-pins
the loop, all in ONE transaction (`TestBeginRunIsAtomic` sabotages the pin row and reads the rollback
from outside); `EndRun`'s `AND state='running'` makes a double end harmless; `CurrentRun`/`ListRuns`
read the reader pool (ascending, never nil); `SetStepRun` tags steps for the P32 floor query —
`RunCost`/`LoopCost` sum floors over `component_root` (the live name; the task's SQL said
`project_root`), and the CASE wears `IN ('partial')` so steps.go's one-writer grep stays assertable —
never a stored total. `CreatePinnedLoop` now writes run 1 beside the pin in the same transaction,
trigger provenance per S20's columns (the test pins both kinds). Exports: exactly the DoD list.
**Measured 2688 PASS / 0 FAIL (−p 1 official)** — +12 tests; store, loop and daemon green; `fmt-check`
and `guard` clean. **T3 ✅ 2026-10-27** — the blueprint vocabulary widens additively: `after: converse|finish` (closed set — a
value nobody compiled is a behavior nobody wrote), `converse: <agent>` — refused without `after: converse`
and deliberately never checked against RESOLVERS (a resolver is not a conversation), and `kickoff: <template>`
validated at BOTH ends, parse and execute against `KickoffData` — an author's typo fails at load, not at the
start of a run. Twelve tests (`chat_keys_test.go`), the `futureAgents` Unverifiable branch proven alive by
`third_state_t16_test.go`'s mutation pattern, the shipped set read from the SOURCE TREE (`StandardNames()`)
still byte-identical and key-free, `afterr:` still refused. README carries the three keys under the GAP table,
lineage to GAP-7 explicit. **Measured 2700 PASS / 0 FAIL (−p 1 official)**; blueprint and loop green. One
FLAKE recorded not fixed: `cmd/aeon`'s scheduled-fire test raced the engine's saved-block re-save
(`the loop is blocked` overwriting the outward deny sentence before the 250ms poll) on the first −p 1 round —
3/3 green standalone, pre-existing (green at T1/T2 counts), none of T3's files on the daemon path; named again
at T13 if it recurs. **T4 ✅ 2026-10-27** — the resting state is real: `ActIdle`/`endStatus`/`emitIfOver` give the engine ONE terminal writer
and ONE run-settling path (DoD greps: `loop.finished` exists once outside tests, `Status = "done"` not at all); a
converse walk ends `[loop.advanced, loop.run.finished]` with the run row closed and its cost floor on the event,
everything else ends `done` byte-identically — including fail-item, whose resting twin sets `run-failed` ONLY
when resting. `loop.close` arrives on the socket as a human door: store claims idle→done, everything else
refuses as DATA (`not-idle` + the daemon's sentence); recovery and startup sweep past idle unseen (D9 — and the
test calling `StartLoops` learned the hard way that S20's clock must be cancelled with the test; recorded for
the next slice). `contract.ts` regenerated (+7 lines, additive); steer fixtures sha-pinned untouched.
**Measured 2713 PASS / 0 FAIL (−p 1 official)** — +13 tests. **T5 ✅ 2026-10-28** — provenance is recorded:
migration `0038` lands BOTH columns by plain ALTERs on a populated store (re-measured inside the tests via
`seedPre0038`; no rebuild, D10 held). The asymmetry is the point: a chat row nobody typed has **NO** source
(NULL — `agent`/`system` stay NULL, inventing `'user'` would put words in a person's mouth), while a pre-S39
steer defaults honestly to `'user'` because before S39 it could only have been a person's; the four-word CHECK
set mirrors `ValidSource` at the door. `loop.message.posted` files under the loop's own root (a subscriber on
another project provably does not receive the conversation) and `SteerView` carries `source` only when it is
NOT user — the unlabelled frame is asserted to have every S38 key and no other (CR10 byte-identity). The
task's step-6 nudge condition never fired: `missingRecord` is a REJECT with a reason, not a queued steer
(one non-test `NewSteer` caller, the socket door) — recorded in T5's Notes, `QueueSteerFrom` not born.
**Measured 2723 PASS / 0 FAIL (−p 1 official)** — +10 tests. **T6 ✅ 2026-10-28** — the turn is ordinary:
`loop.message` opens on the socket and `runTurn` speaks through the very same `RunStepForLoop` a blueprint
step takes — streamed, steerably (the steerability test finds the turn's step id on its own
`loop.step.dispatched` and walks the ordinary `step.steer` door), recoverably (`RunningStepIDs` sweeps an
abandoned turn; the resting loop never moved — Q7 measured), un-capped (R3: `MaxIterations` stays zero, the
loop's bound is the turn's bound). The order of the guards IS the contract: the step check reads `ListSteps`
BEFORE `Note` writes, so a refused message leaves no ghost thread row, and the `turns` map is only the
race-window backstop. `chat` ships as one general body (31→32 at the four measured count sites, second
authored-fresh record naming the planner discipline it inverts) and `chat.yaml` rests on `after: converse`;
`brainstorm` was rested IN PLACE (unreleased) with `org_test` pinned honestly — HEAD to the pre-rest sha via
a named `preEdit` map, disk to the new. The T6/T10 `run_start` conflict resolved toward T10 by measurement
(`BuildToolset` refuses unknown grant names — declaring it would have broken every turn until T10 exists);
`TestChatBodyLoads` pins its absence with the reason. The walk is a kept test: brainstorm's capture run ends
idle, "hello" comes back as an agent row in the thread. Turn goroutines drain through the guard before
cleanup — T4's landmine fired in development with the same signature and the same fix. Known gap recorded: a
continued answered turn's final reply lives in the step transcript, not mirrored to `chat_message`. And a
pre-existing S20 race surfaced: `awaitSettled`'s 150 ms grace beat lost 3/6 under full-suite load (the
clock runner's final emit reads a closed store) — recorded NOT fixed, same class as S38's `cmd/aeon` flake;
the official run went green on attempt 2. **Measured 2745 PASS / 0 FAIL (−p 1 official)** — +22 tests.
**T7 ✅ 2026-10-28** — the run's first message has a renderer and a provenance that never reach each other:
`RenderKickoff` sits beside `ParseKickoff` (empty in empty out; an absent Params key is a blank —
missingkey=zero, the map is the author's to fill; an unknown FIELD errors at render, because StartRun can
hold a pinned template older than any gate), and 50 renders of a map-ranged template are identical — no map
order may leak into a first message. `RuntimeState.Kickoff` is `omitempty` with a test pinning WHY: a loop
started before S39 must save the bytes it always saved (D1). `goalText` is the single new read in
`modelPrompt` — a one-line diff, branching on emptiness alone, structurally unable to read any source; the
byte-identical test drives the renderer and the typing to the same string and asserts their SOURCES differ
(`template` vs `user`) — same to the model, distinct in the thread (CR6/D7). The golden prompt was captured
BEFORE the edit and re-passed AFTER. A pin test caught its own slice: org_test's `preEdit` HEAD-leg expired
the moment T6's commit landed (HEAD caught up to the rest) — now accepts HEAD equal to the pinned bytes OR
the pre-edit bytes, refusing any third shape. Second pre-existing flake recorded NOT fixed: S38's
steer-skip event race (`steers_daemon_test.go:227` "0 skip events") failed official attempt 1; proven
not-T7 by stashing T7's files and failing 3/14 at plain HEAD; attempt 2 green. Three recorded flakes now
(`cmd/aeon` decide, `awaitSettled`, steer-skip) — all the same family: event delivery waited on by polling
instead of a barrier the emitter owns. **Measured 2759 PASS / 0 FAIL (−p 1 official)** — +14 tests.
**T8 ✅ 2026-10-28** — a loop is not its pin: `StartRun` gives an idle loop a SECOND blueprint as run N+1.
Steps 1–4 are pure reads (`ReadPinnedLoop` → `pinSource` → `FromPinned`/`KickoffFor`), `BeginRun` is the
first word that writes — one transaction claims idle→running AND re-pins AND inserts the run row, so no
window exists where the loop runs a half-written blueprint; the kickoff `Note` lands only after the run
exists, and its failure leaves a WORKING run (the saved state carries the text) with the error said
plainly. The extraction itself was the discipline: `pinSource` came out of `Start` before `run.go` existed
and the loop suite went green on the extraction alone — a second pin meeting the first pin's validators is
D4 honoured, not promised. Run 1 is immutable history: its pinned `Content` byte-checked while run 2 pins
fresh and rests (`after: converse` both). Every step now knows its run — `MarkStepStarted` consults
`CurrentRun`, and `Step.RunSeq` (COALESCE 0 = honestly untagged) exists because CR2's tag needed a reader
to prove itself. Kickoff messages reach the thread through T5's doors with their source intact — typed→
user, template→template, bodies→template — `KickoffAuthor` says human/system, and the attended declaration
is checked across all three states after every start (CR7: `StartRun` never touches it — enforced by the
DoD grep AND by the tests). Both recorded flakes fired on attempt 1 (steer-skip "0 skip events" + the known
closed-store runner panic, same stack family as T6's attempt 1 — runner-registry wait still owed,
S40-adjacent); attempt 2 green. **Measured 2772 PASS / 0 FAIL (−p 1 official)** — +13 tests.
**T9 ✅ 2026-10-28** — the run has human doors: `loop.run` claims a SECOND blueprint for a resting loop and
`loop.run.options` answers the dialogue before it. No ask stands in front of either — a person typing IS the
confirmation (CR4); the agent's ask-gated twin arrives with T10 as a catalog action. The registration took
Outcome A on measurement: the catalog does project socket methods (`registerRecords`), but T10's own
prerequisite owns the `run_start` row, and the projection **skips, never shadows** name-twins — the
`loop_list`/`loop.list` precedent. Decisions are the daemon's and refusals are DATA: not-idle,
invalid-blueprint, unknown-link, requires-unmet all answer `refused{kind,detail,candidates}` with
CheckRequires' sentence verbatim (one formatter), and the CR5 order is an observable — an unmet requirement
leaves `ListRuns` and the pin provably untouched. The `loop.start` closure gave up `resolveLinks` and
`goalPromptText` (daemon suite green on the extraction alone, and one test re-walks its sharpest observable
so "neutral" that moved a word fails at T9); `d.candidatesFor` was already `requiresOffer` and was reused,
not renamed. `loop.show` now carries the loop's MEMORY: `runs[]` in order each with its P32 FLOOR (partial
steps keep their `USDAtLeast` — the field IS the floor, and the test says so after `USD` measured a silent
0.3 vanishing), `runSeq` the current, and `attended` as the honest triplet (P31: `true`/`false`/`null` all
reach the wire — T12's unattended notice cannot exist without it). Peer identity for `startedById` is the
kernel's uid, the same source every records door stamps (OL3). Two structural pins keep the slice honest:
`StartRun(` lives in exactly `loop/run.go` + `daemon/runs.go` (D5: no third door), and neither
`conversation.go` nor `loop/turn.go` contains either spelling of it (CR4: what a person writes is answered,
never executed). Fixture five + handshake list + params/result switches amended additively; `PROTOCOL_VERSION`
untouched; `make check-protogen` ok. **Measured 2787 PASS / 0 FAIL (−p 1 official), green on attempt 1** —
+15 tests.
**T10 ✅ 2026-10-28** — the agent's one loop action stands: a `run_start` catalog row whose two doors
share ONE code path — the socket's human `loop.run` and the tool's agent half both land on `d.startRun`,
wired in `initBlueprints` after the runner exists (D5: the T9 walk still finds `StartRun(` in exactly two
production files). Identity is structural, never validated: the loop is the step's, the actor is the
handler name, and the `additionalProperties:false` schema turns a smuggled `loopId` into a refusal the
model reads as "identity fields are stamped by the step". The refusal renderer learned to speak BOTH
shapes the boundary hands it — JSON maps from the socket, the store's `[]loop.Candidate` from the tool —
because a renderer that knows one delivers D6's dead end. The table's "approve starts the run" met S11's
measured finding (F-21: `approve` is the blocked call's tool_result delivered to the MODEL; the frozen
`ask` re-asks; running takes an `allow`) and the Do-NOT (never touch the gate) resolved it: the tests pin
both truths — the answer alone mints no run; under an allow row the same call starts run 2 provenanced
`agent/chat` with its announced beat. Nothing seeds: Notes carries the T0-Q5 operator command; attended
asks and unattended (declared-false AND never-declared) deny at once with the verdict sentence verbatim,
zero policy-engine change. Registry 39→40 with dated comments at four pins; `chat` and `brainstormer` gain
the grant — the over-grant guard made the bodies NAME the tool before the pins would pass, and the
brainstormer drop record moved 57→60 lines honestly. **Measured 2798 PASS / 0 FAIL (−p 1 official), green
on attempt 2** — +11 tests; the recorded steers flake was attempt 1's only failure.
**T11 ✅ 2026-10-28** — the screen knows the loop is resting: `session.loopStatus` gains `idle` from
`loop.run.finished` (back to `running` on `loop.run.started`) while the composer rule is exactly
`messageMode = idle && running.length === 0` — an idle loop keeps its composer and sends `loop.message`;
a dispatched turn flips it to steer mid-view; a `done` loop still loses the box (the S9 test passes
unmodified). The thread shows person-side messages with the provenance chip only when the daemon says the
words were not typed (an agent-authored `loop.message.posted` is never drawn — the stream already shows
it, twice is twice), and one card per run wears its cost as the P32 floor: `≥ $0.50`, `$0.50`, or `—` —
never a confident `$0.00`. The *Run blueprint…* dialogue lists every blueprint unhidden-with-verdict
(CR5, the S9 start-form rule), and choosing the daemon's offered candidate is what enables Run — `goalId`
for a goal, `taskId` for a task, with the optional first message; a refusal keeps the panel open wearing
the daemon's sentence. The bench's scripted mode grew honest twins for the extended wiring; `LoopDetail`
gained `runSeq`/`attended`/`runs`. +21 tests; the three DoD suites pass 112/112 UNCHANGED; the six
full-run ui failures were proved pre-existing (clean-HEAD worktree reproduces four; one fails on the
foreign-dirty `package.json` against `port.test.ts`'s committed ui-kit pin; one is a load-flake) —
recorded not fixed. Native desktop appearance not verified (jsdom only).
**T12 ✅ 2026-10-28** — the ask the agent earns is readable and the loop says who is home. The
permission card for `run_start` grows one branch above the daemon's body: with structured input it names
the blueprint and the record (`Start blueprint goal-to-brief on goal g1`), and — step 1 verified the wire
carries none (`StepEvent`: Tool/Summary/bash-only Command) — without it the card says the truth it has,
`Start a blueprint run`, and NEVER an invented blueprint; the event-parse adapter test sits skipped with
its re-enable condition rather than pretending. The card is `ApprovalCard` (the question parks on its own
tool item), so the approve/deny path stays S11's single door; the answered card still collapses to a row.
The loop's declared attendedness now stands in the chat header in three states with three
character-exact texts — attended, unattended (`run_start cannot be confirmed here.`), never-declared
(treated as unattended, its own words) — and renders nothing when an older daemon has no word; it is
never inferred from socket liveness and never blocks the human peer's *Run blueprint…* door. +9 tests;
the four covering files pass 141 with the one skip, the two pre-existing pairs 36+51 UNCHANGED; the six
full-run ui failures remain the proved-pre-existing six. Native desktop appearance not verified.
**Next: T13** (the close-out: zero-regression walk, the DEMO and its Findings — and then S39 rests).

**T13 ✅ 2026-10-28, the close-out** — the slice rests with its walk walked. Two invariant tests
landed (`internal/protocol/s38_invariants_test.go`: version asserted **2** — the spec's "1" is stale
since S38 Phase A `0da33b16`, the comment says so — and the four chat doors proven absent from
`narrow.Actions()`; external `protocol_test` package, because narrow imports protocol), and the
`-race` pass caught its own test-side race in the gatedModel helper (`m.calls` written by `Stream`,
read raw by T7's wait) — fixed with a `callCount()` accessor, production stacks absent from the trace.
The full operator walk ran live on a throwaway store (`/tmp/aeon-walk`, fixtures on
`fake-anthropic --no-price`): capture→`loop.run.finished done`→**idle**; converse turns minting and
versioning goals in-thread (`v2 … readable by the walk reader`); `loop.run.options` speaking the S38
sentence with `candidates:null` until `aeon goal decide … --version 2 --decision approved` flipped it;
the `ask` row suspending a `run_start` on a question naming agent/tool/rule — PL4's own parenthetical
(`an allow exists … asked anyway, PL4`) — and `answer approve` delivering to the model with **zero
runs started** (F-21, live), the `allow` row then firing `loop.run.started {goalVersion:2}`; a live
`step.steer` making the next tool say `not run: the person steered before this tool ran` (and a
re-issue reusing a `tool_use` id meeting the checkpoint's `appears twice … a tool result could match
either` — the invariant right, the fixture wrong); the **stepless gate** (`kind:"gate", answers
["approve","revise","abort"]`) reachable only by `loop.answer` — `aeon answer` addresses by step and
the gate has none; `--attended=false` denying `run_start` **at once with zero question rows**;
`loop.close` closing idle and refusing done (`only an idle loop can be closed; this one is done`);
and the restart reading the resting loop back `idle`, 19 run rows untouched, zero recovery lines.
The machine corrected the spec three times (approve-never-starts; goal-to-brief has no `after:` so its
`done` ends the loop `done`; the gated blueprint is goal-to-brief, not dev-loop) and the walk found
F-1's sibling gaps again — no CLI door registers a component, `policy remove` needs `--id`, slug
lookups resolve against the CWD's component (**fifteen-row findings table**,
[`S39-chat-runs/DEMO.md`](S39-chat-runs/DEMO.md)). **Measured:** official suite **2800 PASS / 0 FAIL
exit 0**; `fmt-check`/`guard`/`check-protogen`/`go vet` all exit 0; `make ci` exit 2 solely on the
foreign-dirty `PageLayout.tsx` typecheck; ui **519 passed / 6 failed** (all six proved pre-existing
on a clean worktree) +1 `it.skip`; `-race` loop ok, `-race` daemon exit 1 = the closed-store family
(b) recurring through `answer.go:114` — **recorded, not fixed** (ruling). Desktop and real-provider
behaviour remain **unverified rows, not claims**.

**S39 is BUILT: T0–T13 ✅, measured 2800 PASS / 0 FAIL, ui 519/6 with the six proved-pre-existing
reds.** S40 (automation on the org layers) stands gate-MET and **not started — the slice stops here
awaiting instruction.**

**S40 — STARTED 2026-10-28 (the user's instruction), sequential from here.** **T0 ✅ (investigation,
no code):** [`S40-automation/investigation.md`](S40-automation/investigation.md) — ten questions
closed by quoted commands, prerequisites (S38 T6/T7/T15, S39 T8) confirmed by the questions
themselves. The slice edits functions other slices own, and this is where the invented names die:
the migration is **`0039_automation`** (fresh store: "38 migration(s) applied"); the emit sites are
`DecideGoal`/`CreateManualTask`/`DecideTask` **plus `SetManualTaskStatus`, which writes without a
transaction — T2 wraps it before it may emit** (and the plan's `SetTaskStatus` site was wrong,
corrected with the reason); the design's manual-only `BEFORE INSERT` trigger was executed
**verbatim** on a populated copy — ghost-safe as written, and the measurement recorded why the
tidied variant is not (`COALESCE` inside the subquery lets a ghost row pass). **Two design rules
moved, dated:** AU7 — `loop_run.started_by_kind`'s CHECK is `('user','agent','trigger')` and
rejects `event` (measured error quoted), so T1 rebuilds the table (end-to-end measured, traps
noted) and T4 extends the two Go whitelists; AU9 — the extraction closure is `blueprint.go:315–394`,
not the document's 281–322. `EventTriggerSpend` ran on real rows (`0.7|1|1|3`; cost vocabulary is
`known|partial|unknown`). The machine caught the investigation once: a probe that briefly "proved"
the CHECK allowed `event` was **contaminated by the investigator's own abandoned rebuild** — killed,
re-measured on a fresh copy, verdict reversed. The slice's own drift note: the developer's
`~/.local/state/aeon/state.db` is a stale artefact (37 ledger rows, no `task.manual`) — inspected,
recorded, left alone; every measurement ran on copies. **T1 ✅ (the migration, 2026-10-28):**
`0039_automation` landed — `automation_event` + `automation_cursor`, `event_trigger` +
`event_trigger_fire` beside S20's, the manual-only trigger verbatim with its ghost arm pinned by a
test, `loop.automation_depth`, `chat_message.source_ref`, and the `loop_run` rebuild done on the
**real 0037 table** — 15 columns, `PRIMARY KEY (loop_id, run_seq)`, `idx_loop_run_state` dropped
before the swap and recreated after (AGENTS §3's 0019 scar; T0's probe clone had 16 columns and a
`UNIQUE` — the test now upgrades through the migration itself, so no hand-clone can drift again).
The `;`-in-trigger-body preflight passed by **shipped precedent** (0023's
`plugin_agent_no_exit_revoked` runs green today), not runner surgery. The count pins moved 38→39 at
five sites — two of them T0 got wrong and both are corrected with the reason on record:
`policy_test.go:306` is an **absolute** pin (Q3 called it delta-only) and `message_source_test`
froze `chat_message`'s tail at `source` where 0039's `source_ref` ALTER now lands last. **2807 PASS
/ 0 FAIL** (official, exit 0 — the 2800 baseline plus exactly the 7 in the new
`automation_schema_test.go`); fmt/vet/guard/protogen all 0. **T2 ✅ (the outbox writes, 2026-10-28):**
four emit sites inside their domain transactions through **one** helper (`automation.go`'s
`insertAutomationEvent` — the one `INSERT INTO automation_event` in the module, grepped with a
non-vacuity control), `SetManualTaskStatus` wrapped in a transaction first as T0-Q1 demanded. The
origin field landed on **`NewManualTask`, not `NewTask`** (the plan's name — the engine's input, from
which a manual row can never be born), and created-as-approved proved **unreachable** at S38's door
(birth is none|pending), so `task.approved` has exactly one door: `DecideTask`. The tests caught a
replay bug before it shipped: `ValidateTransition` rules `from==to` OK (machine.go:244, the engine's
idempotent replay), so a re-delivered completion would have **emitted twice** — the UPDATE gained its
claimed-FROM `AND status<>?`; the replayed call's answer is unchanged, and it no longer re-stamps
`updated_at` on a row that provably did not move (deviation recorded). **Flagged for the
coordinator:** the agent-side `OriginLoopID` wiring (`daemon/records.go:1111`'s stamped `task_create`
passing its step's loop) is named by **no task file** — until wired, agent-filed manual tasks emit
depth 0, today's honest zero; **T4 is the natural owner.** Depth rides the origin loop's row (event
→ cause fills; S20's trigger fire → depth without cause, AU5) and an unknown origin fails CLOSED
with the task itself rolled back. **2820 PASS / 0 FAIL** (official, exit 0 — the 2807 plus exactly
the 13 of `automation_test.go`); the five new exported readers named in `store.go`'s consumer table.
**T3 ✅ (the trigger store and the match grammar, 2026-10-28):** `trigger_match.go` is AU6 closed —
six keys, values are **literals** (a tested refusal of `b*` matching `bug`), `null` refused at the
type door (`json.Unmarshal`'s silent no-op caught before it speaks), and absent ≠ empty kept distinct
in both `Validate` and `Matches` (the first draft conflated them and every keyless match died — the
distinction is recorded, not bypassed). `triggers.go`: every write refuses a non-person **before**
`s.w.Begin` — the tests prove it by counting rows after the refusal — and the fire ledger is AU8 as
built: the `UNIQUE(trigger_id, outbox_id)` **is** the claim, `outcome NULL` is in flight (found by
`OpenEventFires`), deciding is claimed-FROM (`ErrEventFireDecided` names the first speaker), and
AU12's vocabularies carry a drift test that loops the real `EventFireReasons` map. Deviations
recorded with reasons: the plan's `UNIQUE constraint failed: trigger.name` names the wrong table (the
real message says `event_trigger.name`), S38's `Actor` was reused rather than redefined, and
`Admits` deliberately ignores `enabled` so the dispatcher can record `skipped(disabled)` instead of
swallowing it. 16 new tests; **2836 PASS / 0 FAIL** (official, exit 0 — the 2820 plus exactly 16);
the 13 new `*Store` methods named in `store.go`'s consumer table.
**T4 ✅ (StartFromEvent, 2026-10-28):** `internal/automation/starter.go` is the new between-package
(request/result/`Refusal`/`AutoSlug` — daemon → automation → store, the store never learns it
exists); `daemon/start_loop.go` is the **narrow extraction** — the `StartBlueprintLoop` closure's
body moved verbatim into `startLoopWith` with four provenance fields on the `NewLoop` literal, the
socket closure now an 11-line call, and the two socket-shape tests written **green against the
unextracted closure first** so a drift is a failing test, not a rumour. `daemon/event_fire.go` is
AU9's door: workspace → subject → blueprint → kickoff → one door, with **exactly one `Attended:`
assignment (`attended := false`) and zero `AllowOutward:`** grep-pinned (AU2), five typed refusal
classes and everything else plain for AU12's could-not-say. The kickoff rides `CreatePinnedLoop`'s
**one transaction** (`ChatMessage`/`NewChatMessage` gained `SourceRef`; run 1's `started_by_kind`
admits `event` in both Go halves): forced-failure proof — `RAISE(ABORT)` on the message insert
leaves `loop` rows 0, `loop_blueprint` rows 0 (the test lives in `internal/store`, stated deviation:
only this package holds the handle). Measured and corrected on the spot: **`loop` has no uniqueness
on (component_root, slug)** — AutoSlug is AU13's recovery handle, not the dedup (T5's fire row is),
and the twin-start's plain error comes from `task_loop`'s real unique index. S38's `GetGoal` absence
is a sentence, not `ErrNotFound`; `ErrRequiresUnmet` rides `WithWhat`'s copy so the recogniser is
the code and the sentence `Data.What` (both measured, both pinned). **T2's flagged line wired:**
`records.go`'s stamped `task_create` now passes the step's loop as the write's cause — the outbox
row copies depth 2 and `tr-orig` from the event-started loop instead of today's honest zero.
14 new tests; **2850 PASS / 0 FAIL** (official, exit 0 — the 2836 plus exactly 14; the four
flaked attempts were the recorded daemon `answer.go:114` closed-store panic family at load 2.9–4.1,
the daemon green in isolation each time).
**T5 ✅ (the dispatcher, 2026-10-28):** `internal/automation` learned to think. `Drain` reads the
outbox after the durable cursor and per event matches triggers, **claims before starting** (the
loser returns nil and says nothing — the fire row's UNIQUE is the dedup truth, not the loop slug),
and `execute` walks AU8's exact order — depth → overlap → limits → target → start — recording
exactly one of the five outcomes with a reason from AU12's closed list. The package is a pure
function of injected time and an injected Starter: `time.Sleep` nowhere, `time.Now` nowhere
(grep-pinned with the needles split so the grep cannot match itself); the suite runs 3.5 s, most of
it migration replay. The machine corrected the plan's premises four times: the types are
`store.Trigger`/`store.Fire`; **a pin rests idle until a runner takes it** (S39) — a fake starter
that pinned without starting unmade the overlap guard, so the fake starts, as `startLoopWith`
does; `Trigger.Admits` fails closed against an empty event component, making the plan's
"multi-component goal + trigger component → started" unreachable — the started arm now uses a
single-component goal and the **event's own component** fallback got its own arm; and
`ListEventFires` reads `id DESC`. The store's own disciplines fired and were honoured, not
silenced: the cost-column grep demands `IN ('partial')` spellings even in reads — the house
costQuery's own spelling — and the surface table gained its **S40 dispatch reads** row.
`S20Outcome` is total over the closed sets (T8's UI will never re-implement the translation), and
the honest zero survives: a person's approval emits depth 0 even on a deep loop's task — only the
agent's `task.created` carries depth. 24 new tests (21 dispatcher, 3 store); **2874 PASS / 0 FAIL** (official, exit 0 — the 2850 plus exactly 24, every new test line verified present in the log).

**T6 ✅ (Crash recovery, `Run`, the daemon wiring, 2026-10-28):** AU13 got its table in `recover.go`,
row by row, each arm tested by a real crash simulation — `ClaimEventFire` without `DecideEventFire`,
then a **new** `Dispatcher` over the same store. The rule that matters — never a second loop for one
`(trigger, outbox)` pair — is asserted on **loop-row counts**, not starter calls: row 1 looks the loop
up by the deterministic `AutoSlug` and completes the record **without ever calling the Starter**
(pinned by a test whose starter is configured to fail, so a wrong branch order cannot pass); row 5
refuses naming **both** vanished ids, which the FK-less fire ledger makes a real arm, not a fiction;
row 4 re-enters `execute` verbatim and the store's claimed-FROM decide makes "never decide twice"
**structural**. `Run` reconciles once before any tick and drains thereafter — errors `Infof`'d, never
fatal, serve's own bargain; `defaultClock` is the package's **one** wall-clock read, and T5's grep was
amended rather than relaxed: it counts occurrences and requires exactly one, pinned to the
single-expression function by text. `serve.go` carries exactly one goroutine — gated on
`recoveryDone` (Reconcile must not race loop recovery) and on `!noRecover` — and one shutdown join.
The E2E walks the whole seam through the real dispatcher and the real starter: an approved goal
becomes an unattended loop (`attended=false`, `allow_outward=false`, depth 1, `started_by=event`,
`trigger_id` set), its first thread row is the kickoff naming its trigger, the fire says `started`
with the loop's id. The machine corrected the plan twice more: `ReadComponent` does **not** map
no-rows to `ErrNotFound` (bare `sql.ErrNoRows` — one sentence for both spellings in `componentRoot`),
and `Trigger.Admits` closed the component-less goal event at the daemon boundary (T5's fact again) —
the E2E goal carries its component now. Measured, not promised: **1,000 open claims reconcile in
1.9 s** (logged by the test; the ceiling catches only catastrophes). The daemon's flake recurred once
in three isolated runs — the recorded `answer.go:114` closed-store family, trace confirmed not this
task's code. 14 new tests (10 recover, 3 daemon E2E, 1 wiring grep); **2888 PASS / 0 FAIL** (official,
exit 0).

**T7 ✅ 2026-10-28 — the desk's three reads are on the socket.** `automation.trigger.list /
show / fires` arrive through the `EventTriggerReader` seam (AU14: reads only — the write doors
are T8's, and none of these touches a row), with views that carry the canonical `match` object
and the kickoff **template**, never a rendered body. The honesty rules are handler-tested, not
reviewed: an empty catalogue is `[]` never null, the five outcomes are always in `counts` (zeros
shown), an invented `outcome` filter is refused by name, the limit clamps 50/500, and an unwired
daemon answers `ErrStoreUnavailable` rather than panicking. Two premises the plan carried into
the wall: `PROTOCOL_VERSION` is **2** (the repo's own pins say so; the plan said 1 — measured,
and no bump happened), and `handleTriggerList` **already exists** — S20's schedule surface owns
it, so `automation.trigger.*` handlers are `handleAutomationTrigger*`. The lesson protogen
taught, recorded: it is **fixture-driven**, so a fixture that happens to omit a null teaches the
TypeScript page a non-nullable lie — five fixtures serve three methods now, because
`trigger-show-paused.json` is what makes `maxCostPerDayUsd: null | number` and `lastFire: null |
{…}` true in `contract.ts`, and `trigger-fires-limited.json` is what makes the `outcome` filter
and `beforeId` cursor visible to callers. The fire vocabulary is pinned **against its owner** —
the daemon reader test checks the counts keys against `store.EventFireOutcomes()`, not a second
literal list (protocol must not import store; two lists without a pin are two truths). The
store gained the one line T7's fires read needs: `EventFireCounts("")` = all triggers, spelled
`(? = '' OR trigger_id = ?)`. `make -C ui typecheck` and `make check-protogen` are clean and the
contract diff carries only the three new method entries; the narrow HTTP surface needs nothing —
it is its own enumerated actions table, socket methods do not auto-project. 7 new tests (5
protocol handlers, 2 daemon readers over a real store); **2895 PASS / 0 FAIL** (official, exit 0).

**T8 ✅ 2026-10-28 — the four write doors are human-only where it counts: in the kernel.**
`automation.trigger.create / update / enable / disable` stand on the socket with the actor check as
the FIRST state-touching step of every handler — an agent is refused before anything is judged, so
it learns nothing about validity — and `ActorFor` is stamped from the connection's peer credential
exactly as S38 stamps its deciders (bare kernel uid, one person one string at every store door).
The store's `requireUser` remains the last door, and the writer dresses its refusal in the handler's
exact sentence — one crime, one sentence, whichever reports it. AU1 is executable now: three
grep-pinned tests (tool vocabulary, agent-facing packages, handler bodies) each carry a scan-size
guard, and both were proven to bite by deliberately planting a violation and watching the match
(then reverting). Two plan premises corrected on contact: the "pure extraction" was already done by
S39 T8 (`CheckBlueprint` just lends `pinSource` a name; `Start` unmoved, the whole `loop` suite
green without an edit), and the task file's `{{.Nope}}` kickoff does not refuse — S39's parser is
parses-only by design (`missingkey=zero`); malformed syntax is the honest refusal. And the fixture
lesson deepened: **a refused request still teaches protogen** — the `labels`-key error fixture leaked
a refused key into `contract.ts`, so it demonstrates the same class of violation via a value the
grammar refuses (measured sentence, quoted verbatim), while the unknown-key sentence keeps its
byte-for-byte test against `ParseMatch` directly. Scope ids are not fields of the update params (to
move a trigger, create another); authorship rows never move; `clearMaxCost` is its own intention and
absent keeps. AU14's ruling beside PL15's in the pinned method list: policy authoring stays CLI-only;
trigger writes are the socket's only writes whose person is the kernel's. 11 new tests (1 loop
check, 2 protocol handlers, 7 daemon over a real store, 1 tools pin); **2912 PASS / 0 FAIL**
(official, exit 0) — the count is T7's 2895 + my 11 + **6 foreign S41-notify tests** the user's
parallel notifications work left uncommitted in the tree mid-task (set-diff adds 17 names, not 11;
they pass, they are not T8's, and they are not in its commit — recorded so the arithmetic reads true).
**Next: T9** (the bench page — triggers, enable/disable, the fire log, on the generic bridge).

**T9 ✅ 2026-10-28 — the automation desk gets its bench, and the bench keeps its promises.**
`/bench/automation` renders the trigger table (event, blueprint, scope, enabled, last outcome), the
two human writes (enable one-click; disable gated on a non-empty reason typed for the *selected*
trigger — a stale reason from another row cannot ride over), the fire log with **five pills mapped
from the closed list rather than the counts' keys** — a daemon that omitted a key still shows the
pill with its zero, because AU12's "zero shown as zero" is a promise about the page, not about the
response — and `could-not-say` wears its own violet beside its gloss, *we cannot prove whether this
started*. The transient `claimed` (outcome `""`) is asserted to live outside the five and never
borrow one (P31's third state stays a third state). Reasons print verbatim in monospace; the JSON
create box parses locally — invalid JSON sends **nothing** — and every daemon refusal prints whole,
the fixture's grammar sentence quoted byte-identically. `-32601` answers with the honest *predates
S40* note, never a lying empty table; there is no delete/approve/run/fire/retry control (role-queried
away) and the page only ever calls the six trigger methods. 12 new vitest tests, all green; the
project turned out to have **no jest-dom** — the assertions speak the house `.textContent` idiom.
Three of the five edited files were foreign-dirty on arrival (the user's parallel routes/protocol
work), so the commit is **surgical by hunk**: pure `aeon/ui/` additions, self-contained against HEAD.
Full ui run **555 PASS / 6 FAIL (all foreign-baseline families)** / 1 skipped; `make -C ui typecheck`
and `make check-protogen` rc 0; **Tauri native appearance not verified** (web layer only, said
plainly per the DoD).
**T10 ✅ 2026-10-28, the close-out** (zero-regression, measured not asserted: the four diff-checks against
the S40 base `a45c9413` all print **0** — shipped blueprints, the tool pins, the `loop.start` conformance,
and S20's code and docs entire; per-package `--- PASS` counts run at base and HEAD in clean worktrees:
`blueprint 68→68` untouched, `store 362→402`, `daemon 238→267` (23 mine + 6 foreign-notify, named),
`protocol 243→250`, `tools 162→163`, `loop 144→145`, `cmd/aeon 188→189`, and `internal/automation` **0→33**
as a new package; `make ci` rc 2 red ONLY in the foreign ui vitest families — port/app-icon/product — named,
not ours, the same pattern S38 T17 recorded (the final re-run stopped earlier, in the `vet` chain's ui
typecheck, on the user's meanwhile-modified foreign `daemonFooter.tsx` — TS18048, named not ours; the Go
gate's green stands from this step-1 run, and this task's diff is docs-only). The **zero-trigger daemon** walked: outbox rows 2, fires 0,
loops 0, cursor at row 2, 274 bytes across 2 rows (≈137 B/event — R7's number). The **deletion drill**
(worktree, branch `t10-deletion-drill`, commit `d5a3620f`, NEVER merged) stripped all S40 files plus the six
wiring lines and the closure reverted to its inline body: **GO BUILD: OK, GO VET: OK incl. all test files**
— nothing in core imports `internal/automation`; the drill's two findings are the shared `startLoopWith`
(the plan's own revert step) and a foreign observation recorded honestly: HEAD alone does not compile
`daemon` — `notifyHintFor`'s definition lives only in the uncommitted `lifecycle.go`. **DEMO.md** walked all
seven walks with every output quoted and a **14-row Findings table** — `started_by=event` correcting the
task text, the unattended-outward denial unreachable in a route-less store (test-quoted instead), `cost-
unknown` needing a real unknown-cost step, the `task_type` grammar refusal arriving **untyped at −32603**
while `event` arrives −32602 (a follow-up, not a T10 fix), Reconcile completing a re-opened claim without a
second loop, `could-not-say` on the down-window edit, the depth refusal before the target gate, and the
agent-actor refusal run by name. T4's stale "Not started." Notes stub removed (the real Notes were there
underneath it); every task T1–T9 keeps measured numbers, none recalled. `AU` reserved in
ARCHITECTURE §11; S40 rows/records landed in ROADMAP, PLAN and TASKS; S41 gets its **one roadmap line
only**; the S38 OL13 link needed no fix — it already names `brief.md` at HEAD, said in the record.
**S40 is BUILT: T0–T10 ✅, measured 2912 PASS / 0 FAIL (official, T8's run; the Go tree unchanged since),
ui 555/6 foreign-baseline, automation package wall 8.38 s, Reconcile over 1,000 claims 1.9 s (T6's
measurement).** **Next: none — the slice stops here awaiting instruction; S41 is a roadmap line, not a
started slice.**

**[S24 · Queue](S24-loop-queue/)** goes further than its original `brief.md` now — `design.md`, `plan.md`
and all 9 task files exist (re-examined 2026-10-26, once S11–S13 landed, since the brief's own §7 questions
needed real code to answer rather than guesses). A per-project, self-sustaining backlog of loops, a
project-level gate on auto-advance, and sizing manual or by a skill via a native `report_size` tool.
**Two findings the original brief could not have had**: `loop.status` has no `CHECK` and the real
vocabulary (`running`/`blocked`/`waiting`/`done`) has no distinct "permanently failed" value — `blocked`
covers both a retriable stall and an exhausted one (`LoopCeiling`, `internal/loop/counters.go`) — so this
slice never tries to tell them apart from outside and instead halts unconditionally on any `blocked`,
regardless of the project's `queueAdvance` setting. **Needs S1–S10, S8, S9 (all built) and, for the
sizing-skill path only, S13's handler registry (also built)** — T1–T4 and T6–T9 need nothing from S13;
only T5 (sizing) does. 🔨 **building — instructed 2026-10-29; sequential T1–T9. T1 ✅ 2026-10-29** — migration **0041** (`0041_loop_queue`; the plan's `0026` was a document, the machine said 0040 — found, not hardcoded): `queue_advance`/`sizing_mode` plain-ALTER CHECKs on **`component`** (the slice's docs predate S38's OL1 rename — the settings live on the row that owns roots, keyed `component_root`; a group-tier column would let two repos share an advance mode) + `queue_item`: seven-status CHECK (CG1), size one-fact-or-none CHECK (D8 as SQL — bare number, basis-less, inverted and empty-basis all unwritable), `attended` 0/1/NULL (D5), `loop_slug` composite FK **no cascade** (D12 — the ledger of intent survives its product), position index. **Found by the tests:** `CreateLoop` seeds NO tier rows (loop's root column predates the tier, not FK-enforced) — `queue_item` is the first child that enforces it for real. Five count pins 40→41. Fresh-equals-migrated green. 3 gates. **Official: 3087 PASS / 0 FAIL, rc 0** (3084 + exactly the 3, attempt 1) — **T2 ✅ 2026-10-29**: `internal/queue` (zero SQL — machine and meaning; wraps `*store.Store` like S22's `retrieval.Index`) + store doors in `internal/store/queue.go`. The **11-edge** table over CG1's seven, and the cancellable four `{pending,sizing,ready,blocked}` reconcile a three-way doc disagreement (CG1 prose `{pending,ready}` vs the diagram's `sizing │` vs T2.md's four vs CG2's escape hatch) with the union recorded in code; `reorderable = {pending,sizing,ready}` — ready included by the recorded call, since `manual` lands everything `ready` and pending-only reorder would be a no-op in the default mode; self-transitions idempotent (machine.go's rule: re-Remove of a cancelled item is success); claimed-FROM everywhere (loser reads `Moved`); `NextReadyQueueItem` carries D2 inside the read (`NOT EXISTS running`); corrupt `with_json` refuses the read naming corrupt; `Remove` never deletes (row count identical). 6 gates. **Official: 3093 PASS / 0 FAIL, rc 0** (3087 + exactly the 6; attempt 6 — attempts 1–5 drew the pre-existing daemon closed-store/tick flakes: all solo-green and the HEAD-only rate audit (5/1 vs candidate 4/6) named them pre-existing, S40-family recorded not fixed)  — **T3 ✅ 2026-10-29**: the reactor joins the daemon's OWN emission point — every terminal emit rides `Deps.Event` → `publishLoop` (which already resolves the loop identity), one call line, two kinds, no pub/sub invented (the inline-at-emission pattern found). Goroutine dispatch — the emit path never waits (wall-time-asserted with the start door shut); `advanceNext` is the ONE materialization through `d.Table.StartBlueprintLoop` (grep-pinned, never `startLoopWith`); appending starts nothing (CG4) — only the explicit call begins a project's first item; blocked ALWAYS halts and asks regardless of `queue_advance`, no reason sub-classification; untracked loops finishing are quietly not the queue's business; a failed start reads **blocked** (no running→ready edge exists — recorded decision, the halt+question+hatch ride together). Store: `QueueItemByLoopSlug` · `SetQueueItemLoop` (root-scoped — the scope guard caught the unscoped first cut) · `ComponentQueueSettings(±Set)`. 9 gates. **Official: 3102 PASS / 0 FAIL, rc 0** (3093 + exactly the 9; attempt 4 — attempts 1–3 the pre-existing closed-store/steer flakes, all solo-green, plus a stale-patch guard miss caught and re-run)  — **T4 ✅ 2026-10-29**: the answer door grew the queue's arm (tried BEFORE the ladder's routing; discriminates on the ITEM — tracked + done/blocked, because a ladder gate ALSO rides a NULL step — a looser arm swallowed S38's brief test, measured and fixed): `advance`/`hold` closed set validated before the write; declined = durable + silent (CG4: nothing re-asks); replay refused by name with the first answer's timestamp (new `LatestLoopGateQuestion`; unreadable ledger errors, never falls through); affirmative calls the SAME `advanceNext` — one materialization, two callers, asserted. 7 gates. **Official: 3109 PASS / 0 FAIL, rc 0** (3102 + exactly the 7, attempt 1; restores the blueprint.go emission-hook hunk the T3 blob lost) — **T5 ✅ 2026-10-29**: sizing by skill. `report_size` is the one native tool — one shape `{sizeMin,sizeMax,sizeBasis}`, refused at the tool AND at the row (0041's CHECK is the last word), once-per-run by the `todo` closure (minted per step, so a retry after a reverted sizing gets a fresh refusal — asserted). The `sizing-estimator` body (the third authored-fresh; investigator-loop's measured-size vocabulary narrowed to one call) rides the ORDINARY step door — R3 grepped AND behavioural: the E2E proves the TOOL ran by finding its answer in the second model request (the first E2E passed on a policy-DENIED tool call's handoff prose, measured; tool rules must name the **policy-agent** — the borrowed-blame audit said so). `advanceNext`'s empty-backlog branch dispatches the sizing under `skill` (manual inert, nothing watches); size+`sizing→ready` land in ONE claimed UPDATE, failed/shapeless/lawless runs revert `pending` (CG5), and ask STOPS after a report (CG4 — sizing is not a terminal; the advance stays the person's explicit act). 10 gates. **Official: 3119 PASS / 0 FAIL, rc 0** (3109 + exactly the 10, attempt 5 — the S40 panic family struck the full run twice, solo-green, daemon-only rate audit 1/5 vs HEAD-only 1/5 pre-existing)  — **T7 ✅ 2026-10-29** (executed BEFORE T6, recorded: P34/CG9 make the CLI a socket client, so the verbs land first): seven socket methods — the six the task listed **plus `queue.advance`**, the design's named explicit-materialization door the list omitted (CG4 cold start; the daemon seam IS the same `advanceNext`, its explicit third caller). One file owns name+params+handler (`protocol/queue.go`); method.go/handshake_test.go are user-WIP → staged as surgical HEAD blobs. **Measured**: loop.start validates with `loadStartBlueprint`, NOT `blueprint.Resolve` (Resolve answers nil for a missing file) — `queue.append` calls the same function, unknown-name sentences identical by construction. `SizeAndReadyQueueItem` grew the store-whitelisted claim {sizing, pending} for the manual set-size; one new code −31700 `ErrQueueRefused` (reason in `what`, missing rows ride it) with 8 conformance fixtures; settings writes are all-pair-or-read refused before the seam. The **Q4 attendedness guard fired and was amended with its argument** — two named+typed doors now (start params + append params: D5's declaration made earlier, G14's law intact, never set after the pin). CLI-identical-rows gate moved to T6 (needs both transports). 8 gates. **Official: 3127 PASS / 0 FAIL, rc 0** (3119 + exactly the 8, attempt 1) |

**[S28 · Memory](S28-memory/)** went to full depth 2026-10-26 — `brief.md`, `design.md`, `plan.md` and all
8 task files exist. A classified, queryable learnings store — person, project and agent scope, the latter
two cross-project — plus a directed agent-to-agent question whose answer files into the same store rather
than a second one. **Two corrections found while designing it**: the natural package name, `internal/
memory`, is already S20's real, built, unrelated mechanism (`AppendMemory`/`ReadMemory`, per-loop notes) —
the actual code is `internal/learnings`; and the brief's "agent_id is a foreign key into S13's registry"
doesn't work, since S13's 25 built-in bodies have no database row — agent-scope is a Go-level check instead.
A board answer files as a **project**-scoped learning (not agent-scoped), following the brief's own words
more literally than the original framing had. **T1–T6 and T8's learnings half need only S4, S9 and S13 (all
built) — agent scope folded into this free tier now that S13 is built, no longer a separate wait. Only T7
(the message board) is blocked, on S14** (specced, not built). Not yet built.

**[S29 · Triage](S29-triage/)** went to full depth 2026-10-26, same day as S28 — `brief.md`, `design.md`,
`plan.md` and all 9 task files exist. Is a bug real, is it a duplicate, how urgent — validated by an
**automatically dispatched** ordinary S5 loop (no project gate, unlike S24's queue: G14 already bounds what
an unattended attempt may do), with a new pre-attempt `unvalidated` state kept distinct from
`could-not-tell`. **Q2 turned out to already be answered by D2** (validation and priority were always two
separate fields, never one crossed enum); priority's shape is copied verbatim from `S26-roadmap`'s own
columns. It schedules nothing itself: once validated and prioritized, the record hands off to **S26's
roadmap only** (never S24 directly — S26 already knows how to reach S24's queue when one exists), through
S26's ordinary `idea` status, needing zero changes to S26. **5 of 9 tasks (T1–T4, T7–T8) need only S4, S5
and S13 — all built.** T5 (the S28 learning-feed hook) needs `S28-memory`'s learnings core as real code; T6
(the S26 hand-off) needs `S26-roadmap`'s core, which itself needs S24/S25 built. Not yet built.

**[S25 · History](S25-history/)** was promoted from a sketch 2026-10-26, kept to its original scope exactly
— a pure read, no new storage. `brief.md`, `design.md`, `plan.md` and all 6 task files exist. The cross-loop,
time-ordered projection of every loop a project has run, grouped by loop/phase/task/agent, with the one real
gap the original sketch could not have had: a naive query over `loop` alone would silently stop showing a
project's older work the day its first loop is archived. The design merges live `loop` rows and
`loop_archive` rows into one sequence rather than two screens. **Needs only S4 and S9 (both built)** — the
archive tables themselves shipped with S4's migration `0004`, not S17 as the sketch's own phrasing implied
(corrected while writing `design.md`). **One caveat**: nothing in the CLI or daemon calls `ArchiveLoop`
today, so this slice's own test fixtures are likely the first real caller — a real end-to-end archived loop
needs a trigger this slice deliberately does not add (`brief.md` N6, probably S17's job). Not yet built.

**[S26 · Roadmap](S26-roadmap/)** was promoted from a sketch 2026-10-26, the third item in the same
sitting. `brief.md`, `design.md`, `plan.md` and all 7 task files exist. Ideation → brief, and the forward
Gantt — S25's own lane component, pointed at the future. **One correction found while specing it**: the
sketch assumed ideation was a `conversational-map`-shaped blueprint; `S8-blueprints/design.md`'s own GAP-7
had already found that shape's iterative phases are not a blueprint's shape at all — "a chat with tools."
Ideation is an ordinary loop ending at the engine's existing `produces:`/`missingArtifact` check — no new
blueprint shape, no new gate type. Priority is three-state (`known`/`assumed`/`unknown`), satisfying a
requirement `S29-triage/brief.md`'s own D2 already stated before this slice had a brief. Materializing a
`ready` item prefers S24's queue when present, falling back to a direct `loop.start` otherwise — one
materialization path either way. **Depends on S8, S9, S13 (all built) and, genuinely, on S24 and S25's
actual code** (T4 calls `internal/queue.Append`; T6 imports S25's Gantt component) — **unlike S24/S25/S30,
this one cannot be built until its two sibling slices are**, even though all four are equally "specced to
task files" on paper. Not yet built.

**[S27 · Continuous work](S27-continuous/)** has a real `brief.md` now too, deliberately kept to brief
depth — asked explicitly, since its own sketch called itself *"the largest change to the daemon's model in
the whole plan,"* a self-assessment that still holds regardless of what's built underneath it. At write
time it sat on three unbuilt layers (S16, S24, S26); **S16 is now built (2026-10-26 check)**, leaving two
(S24, S26). What it resolves against real code, not just briefs: S26's own `D4` "ready" definition, adopted
verbatim; S16's D8 on effect classes, **now cited from its actual shipped code**
(`internal/action/class.go`: *"G14: unattended resolves outward and irreversible to deny"*) rather than
only its brief, confirming an unattended/scheduler-started loop structurally cannot merge a PR, since S11's
own ask-denies-at-once rule already covers it. What it names rather than solves: **a real loop-level
stop/cancel does not exist yet** (re-checked 2026-10-26, still only `step.stop`) and **S26's `roadmap_item`
will need an `attended` column added** when this slice is actually built — not added now, since S26 has no
scheduler yet to need it. Needs S8, S11, S16 (all built) and S24/S26 (specced, neither built). Not yet
built.

**[S33 · Telemetry](S33-telemetry/)** — ✅ **BUILT 2026-10-30** (T1–T9 ✅, [demo walked](DEMO.md#s33--telemetry-the-counting-tiers-read-where-they-are-read)) —
the counting tiers this slice actually built are **four**: loop (all detail rows), **component**, **project**
and **workspace** (the brief's two-tier "project/app" sketch is corrected to S38's real naming — TL1).
One migration (`20261030084236_telemetry`, file-form), five detail tables keyed per step, three rollup tiers
bumped **in the same transaction** as the detail writes they summarise, a nil-safe `TelemetrySink` whose
capture failures can never fail the step, cost three-state (known/partial/unknown) floored by one
`floorCostState` at every tier, D2's two numbers (current vs cumulative) kept forever distinct, and D8 —
rollups survive a loop's archival unmoved (proved by a socket-walked test) and ride a component's move
between groups as a transfer with cost-state recomputed at both ends. The surface is **reads-only**
(`telemetry.loop · component · project · workspace`, T8) — no write method may ever appear — plus one
lifecycle door (`loop.archive`, the honest Delete) and the CLI as its second client; the bench page
(`TelemetryPage.tsx`, registry-registered) renders the three states as three renderings. `internal/runtime`
capture rides T3's `RunOpts` (Q2: all three call sites wired, AST-pinned) and coexists with S31 T4's
steer-drain (R1's check: 134 insertions, zero deletions, hunks disjoint). **Measured:** full
`go test ./... -count=1 -v` **3469 PASS**; the 15 failing names are all pre-existing classes recorded in
`S33-telemetry/tasks/T9.md` (triage/history want-list gaps, the leaked-checkout git tests, the 7 live-daemon
CLI tests, one brainstorm refusal test) — none reproducible from this slice's diff, all green solo or red at
base; write volume measured ≈605 µs/row across 160 rows (no-batching confirmed, no follow-up owed).
Known gap kept open, named not fixed: `edit` emits no write shape, so edits produce no file-activity rows
(T6's finding, S31's tools home — pinned by `TestEditDoesNotYetEmitWriteShape`).

**[S34 · Trust bench](S34-trust-bench/)** — ✅ **BUILT 2026-10-29** (T1–T8, no `DEMO.md` — this slice's
acceptance is unit/integration-test-only by design, confirmed by `grep -n 'DEMO' brief.md plan.md` at T8's
close returning no hits). It is a slice at full plan depth (real `brief.md`, `design.md`,
`plan.md`, eight tasks) — the first slice written after **P34** was adopted the same day (the daemon surface
is the primary interface; the CLI secondary), and designed so its build order *is* that doctrine: four new
daemon read methods (`agent.list`, `tools.grants`, `mcp.status`, `mcp.check` — all conformance-fixture-
proven, three of them pure exposure of already-built code) land before the two bench pages render a single
fact. The pages: an **agents readiness view** (identity/provenance/state; the S11 four buckets — declared ·
granted · registered-not-granted · unregistered — rendered separately, never merged; MCP declares; guard
verdicts from the existing `policy.*` reads) and an **MCP config-and-Check view** (config truth with refusal
reasons and the MC11 absent-config empty; an operator-clicked probe per server that shares the CLI's own
dial path, is single-flight, timeout-bounded, cached with its timestamp, and never issues `tools/call` — the
last asserted against a fake transport, not promised in prose). Every fact is three-state (value / `not
wired` / `error`) so an old daemon is harmless; every section owns exactly one menu row, one page file and
one dispatch branch, which is what makes a proven section a one-commit deletion. All dependencies (S4, S9,
S11, S12, S13) are built code; no live-runtime edits; the only new daemon behavior is the probe.

**T1–T4** landed the four daemon reads plus `mcp.check`'s probe (`internal/mcp.Probe`, single-flight-cached
in `cmd/aeon/mcp_services.go`); **T5** wired the two bench menu rows and dispatch branches; **T6** built the
shared `Fact`/`useFact`/`classify`/`FactCell`/`ProbeChip` infrastructure and the Agents readiness page;
**T7** built the MCP config-and-Check page on that same shared infrastructure. Several of the brief's and
design's own guesses turned out wrong against the real code — recorded in each task's own Notes: the real
state vocabulary (`running` / `disabled|enabled|revoked`, not the brief's four buckets), `GrantView`'s real
four states (`available|unavailable|refused|ask`), `mcp.status`'s real three states (absent / present+valid
/ present+broken via a whole-file `loadError`, not a per-server `refused` field), and the probe's chip text
collapsing to one generic `error` string rather than the design's imagined `unreachable`/`timed out` split.

**T8 (close-out)** proved both load-bearing claims in git rather than prose: a P34 grep traced every fact
on both pages to the four new methods or the pre-existing `policy.*` reads; a real deletion drill (file
surgery, not a scratch branch — S34 is still uncommitted on `main`, shared with other live sessions, so a
branch switch/stash risked their in-flight edits) removed `McpPage.tsx` and found real coupling beyond the
task's own prediction — `product.test.tsx`'s shared nav-label test, `bench-pages-order.test.ts`, and
`app.test.tsx`'s disconnected-state `it.each` all enumerate both trust-bench pages together — then restored
every file and reproduced the identical pre-drill pass/fail state. The clean-tree run this close-out forced
surfaced and fixed four pre-existing gaps the earlier tasks left behind: T1's `agent.list` needed the same
`agentsvc`-exclusion the `agent.plugins` precedent already uses but never got (test now asserts it),
and three S42 (a different, earlier slice) leftovers — two tests seeding a transcript starting with an
assistant message against the real, pre-existing `ValidateTranscript` rule, and `ReadLoopTranscriptPage`
missing from `store.go`'s own surface table. Measured clean-tree count: **3093 Go PASS / 2 FAIL** (both
unrelated to this slice and not reproducible in isolation — a port-503 flake under full-suite load in an
untouched `cmd/aeon` file, and a different concurrent session's in-progress `internal/retrieval` work), UI
**602 passed / 10 failed** (the same pre-existing icon-rework baseline, confirmed via `git status` on
`src/icons/*`). Full task-by-task evidence: [`S34-trust-bench/tasks/T8.md`](S34-trust-bench/tasks/T8.md).

**[S35 · Trust desk](S35-trust-desk/)** — ✅ **BUILT 2026-10-29** (T1–T9, no `DEMO.md` — this slice's
acceptance is test-only by design, confirmed by `grep -n 'DEMO' brief.md plan.md` returning no hits). It is
S34's sequel at full plan depth (`brief.md`, `design.md` with
rules TD1–TD12, `plan.md`, nine tasks, strictly sequential per the operator's 2026-10-26 no-parallel
directive) — the slice that resolves P34's one collision with a standing ruling, using the peace P34 itself
pre-named: **inert proposals on the socket, commit through a human-only door.** Agent
enable/disable/revoke become plain daemon writes — they call `TransitionPluginAgent` verbatim, so a
reasonless revoke stays un-writable *through the new door*, teeth intact. Everything that widens authority —
policy add/remove/seed, grants (grants are policy, S12), MCP server add/remove (there is no MCP add path
anywhere today; `mcp/config.go` has validators and zero writers) — becomes a row in one new `proposal`
table that no guard read path may ever read, validated at create by the *same* validator the eventual
commit re-runs. The only door that promotes a row to live effect is `aeon proposal commit` in a terminal —
**the app has no commit affordance in any form**, pinned by grep, and the amended
`TestNoPolicyAuthoringReachesTheNarrowSurface` keeps its original assertions byte-preserved while gaining a
positive whitelist: the only table a socket handler may write is `proposal`. The bench grows a **desk**
(open proposals, payload-vs-live, copyable commands, Discard as the only action), live and proposed never
render merged, and the menu earns its Trust group (policy · agents · mcp · proposals). The slice is one
test: create proposals of every kind over the socket → `policy.why` verdicts byte-identical → commit one
via the CLI → exactly that verdict flips. Depends on S4/S9/S11/S12/S13 (built) and on S34 — its bench tier
edits S34's pages, so it seeds after S34's daemon tier.

**Built 2026-10-29, T1–T9 ✅.** The table-driven slice test (`cmd/aeon/proposal_slice_test.go`,
`TestProposalSliceAcrossAllKinds`) runs over all **five** proposal kinds (the task named four and left
`mcp_server_remove` to be decided — it carries independent, MC12-inverse risk, so it got its own case): for
each, `proposal.create` over the real socket method table leaves an affected subject's and an unrelated
control subject's `policy.why`/`mcp.status` output byte-identical, then `aeon proposal commit` flips
exactly the affected one. PL15 (`TestNoPolicyAuthoringReachesTheNarrowSurface` plus T6's sibling
`TestOnlyProposalIsSocketWritable`), T2/T5's validator-parity tests, and T4's store-teeth tests were all
re-run standalone in this close-out task rather than inherited. The P34 grep over all four Trust pages
traces every rendered fact/effect to a method T2–T4 or an earlier slice registered. MC11's three-state
honesty, previously proven only for `McpPage`/`AgentsPage`, got real coverage added for `ProposalsPage`/
`PolicyPage`'s new `proposal.list` read (both pass). The deletion drill ran as file surgery (the shared
working tree has concurrent-session churn a branch switch would risk entangling) and found a real gap TD12
didn't settle on its own: `lib/proposals.ts`/`lib/ProposalRowView.tsx` are shared by all three other Trust
pages and do not delete along with `ProposalsPage.tsx` — only the page and its own test are a one-file
deletion. A separate throwaway `DROP TABLE proposal` test on `internal/store` confirmed the corpus/config
read paths never reference the table while the proposal surface itself fails loudly rather than silently
emptying. This slice's own packages (`cmd/aeon`, `internal/store`, `internal/mcp`, `internal/policy`) and
the UI suite (627 vitest passed) measured clean in isolation; two different full-repo runs under heavy
concurrent-session load each showed a different, confirmed-unrelated set of transient failures (brainstorm
lane tests, a stale `history.*` method pin, disk-quota build errors) — recorded as environmental, not
re-run to chase a single noisy number. No `DEMO.md`: `brief.md`/`plan.md` name none, and this slice's seven
acceptance criteria are tests and greps throughout, never a walked UI script. Full evidence:
[`S35-trust-desk/tasks/T9.md`](S35-trust-desk/tasks/T9.md#notes).

**[S36 · MCP dispatch](S36-mcp-dispatch/)** — **✅ Built 2026-10-30, T1–T6 ✅** — is the trust trio's load-bearing member — **S34 sees it · S35
authorizes it · S36 runs it** — at full plan depth (`brief.md`, `design.md` with rules MD1–MD12,
`plan.md`, six tasks, strictly sequential per the operator's 2026-10-26 no-parallel directive). The slice
where an MCP tool a human added, checked and granted finally *executes inside a loop step*, under the
**double gate** by ruling: the agent must see the server (S12's tier map — private nested, public
declared) **and** a `ToolAccess` allow row must name the exact qualified `server.tool`; **no row, never
runs**, and `ask` — by row or by S12's own L22 outward/irreversible rule — parks on the **existing**
`pendingPermission` path. The degradation law is the other wall: **a dead server degrades, never hangs,
never promises** (build failures withhold with `grants.go`'s verbatim MC10 sentences; dispatch failures
land as error-text `tool_result`s; the loop must never freeze because a child process died). The
architecture turned on a measured discovery — `absent_test.go`'s banned list *names `internal/daemon`* —
which killed the adapter-in-daemon sketch before it could dull MC11: the MCP caller instead enters as a
**function value in `daemon.Options` implemented in `cmd/aeon`** (the already-sanctioned importer), so a
function value carries no import, `internal/daemon` stays MCP-blind, and the guard test is proven
**zero-diff** at close-out. The offer *calls* `GrantView` (zero new verdict logic; MC10's sentence stays
single-defined across CLI, view and runtime), enters through the one choke point (`BuildTools`, all four
callers get it for free), and the slice deletes in one commit — three files and one wiring line. Six
tasks, strictly sequential; **zero socket methods, zero pages**; free to start (S5/S11/S12/S13 are built
code, no page dependencies, no S35 wait). It also caught and amended S34's design §5, which had the
daemon about to import the layer directly — one seam pattern, both slices, MC11's wall unmoved.
**Built 2026-10-30, T1–T6 ✅.** The one acceptance test runs a real loop step — `buildMCPBundle` →
`daemon.BuildTools` (the composite) → `runtime.RunStep` (the loop), only the dial faked, gate and loop
real — and the model is offered `server.tool` with the server's live schema, calls it, and the answer
lands as a `tool_result` in the transcript (validated); a dead server degrades to an error-text
`tool_result`, never a hang; `ask` parks on S11's existing `pendingPermission` path (attended) and
denies at once (unattended, no question row); the first-party and qualified name spaces do not
cross-canonicalize (SEC-059); and `internal/runtime/loop.go` gained no MCP-aware branch (confirmed by
diff, zero lines). MC11 stays zero-diff (`absent_test.go` unchanged; `go list -deps` 0/0/0/0; the layer
builds without `internal/mcp`), the `ask` edges and the canonicalizer edge are green, and the full suite
is **2149** green on a clean tree. The one gap named in `T6.md`: `aeon serve` does not yet wire
`Options.StepMCP`, so the DEMO walk is the real code chain the E2E drives, not a live app
click-through — the seam is inert in production (byte-identical to today by construction), so the
feature deletes cleanly (MD8/MD12). Walk: [`DEMO.md`](DEMO.md#s36); close-out and drills:
`S36-mcp-dispatch/tasks/T6.md`.

**[S37 · Agent admission](S37-agent-admission/)** is the trust trio's fourth member, closing the one gap
S35 named and deliberately left open — at full plan depth (`brief.md`, `design.md` with rules AD1–AD6,
`plan.md`, seven tasks T1–T7 + T0 investigation, sequential; **built** on branch `s37-agent-admission` and
merging to main). `aeon agent import` is the one widening action in S15 that S35's own
brief treats as already finished before its scope starts (*"the imported agent already passed blocklist +
consent at import"*); this slice gives it the same staged-proposal treatment S35 gave policy and MCP
authoring — one new entry (`agent_import`) in S35's existing `kind` enum, not a new mechanism.
The payload is deliberately the smallest of any kind (a bare `pkgPath`): the daemon re-reads the package at
both `proposal.create` (preview, refuses early with the real validator's reason) and `aeon proposal commit`
(the actual import, through `ImportChecked` unchanged — the identical function `aeon agent import` already
calls). The one genuinely new risk this kind has that S35's other four do not: the filesystem can move
between propose and commit, so a commit that now refuses sets `applied_error` and leaves the row `proposed`
rather than flipping on a stale preview — a different failure shape from S35's crash-retry story, not the
same test reused. One new export (`readPackage` → `ReadPackagePreview`, a rename, not new logic); zero new
socket methods; zero new bench pages — the desk and the Agents-page toggle pattern are already generic
over `kind`. Depends on S4/S9/S10/S11/S13 (built), S15 (`internal/plugin`, built, unedited) and S35 (seeds
after its T1–T5). **Built** — T1–T7 on branch `s37-agent-admission` (worktree), merging to main.

**[S30 · Code Graph](S30-code-graph/)** sits at the same depth as S24 now — `brief.md`,
`design.md`, `plan.md` and all 8 task files exist. A structural map of the codebase (`calls`/`imports`/
`inherits`/`mixes_in`/`uses`/`references`), parsed by `tree-sitter`, stored as plain relational rows (no
`sqlite-vec`), queried as raw data — deliberately separate from S22's content retrieval. New this session,
not a promotion of an existing sketch. Needs only S4 and S9 (both built); shares one open question with S22
(the write-signal hook) as infrastructure, not a build dependency. Not yet built.

**[S31 · Loop workspace](S31-loop-workspace/)** has a real `brief.md` (2026-10-24): the backend and Rust shell
for the chat-first loop screen — loop steps that stream live, steering (queued at the turn boundary, durable,
delivered exactly once), stop/continue on loop steps, provider/effort changes mid-run, read-only git status and
diffs, and a daemon-owned terminal. Needs only S2–S9 (all built); **buildable now**, ahead of S11.
**Building (2026-10-24):** the brief's §6 split into [`tasks/T1–T9.md`](S31-loop-workspace/tasks/) is recorded
here; [`CODEMAP.md`](S31-loop-workspace/CODEMAP.md) pins the tree as measured at the baseline commit.
**Baseline repairs landed first** (`065c22f`): databases that built `step.pause_reason` as `0019` (the number
S11 later took) died on every open with `duplicate column name: pause_reason` — `hasLegacyPauseReason` now
recognises them the way `hasLegacyProjectIcon` recognises the pre-merge `0016` icon (ledger row **plus** column
shape, never the row alone), and the user's own store was repaired through that guard from `0020|failed` to
`applied`; and `cmd/aeon`'s TestMain now sets the throwaway `XDG_STATE_HOME` package-wide, because an
in-process verb that forgot the convention had migrated the **real** store.  Counts re-measured at slice close — see the Progress table.
**Built (2026-10-24):** all nine task specs landed in one solo sequential pass, commits `70cc5a0` (baseline) → `065c22f` (repairs) → `46b332e` (docs split) → `01d9ffc` (protocol skeleton) → `ba9f1b9`/`928df08` (T8) → `3833bfa` (T3) → `7c2ea0d` (T1) → `b1c4f1f` (T2) → `e2d726b` (T4) → `fc64e6d` (T5) → `1d416a7` (T6) → `1f3375d` (T7) → T9. `make ci` exit 0 and `make test-race` clean; **1598** Go / **275** UI / **28** Rust (all measured, dated, above). The socket was walked live — [§S31 of DEMO.md](DEMO.md) quotes the git three states, the PTY shell (`echo $((6*7))` → `42` through `terminal.output`, `terminal.exit {exitCode:7}`, zero `event` notifications across the terminal's whole life — LW7 counted), the steered ladder (`step.tool.skipped` + `step.steer.delivered` with `skippedTools`), and the D8 counts (`"linesAdded":1,"linesRemoved":1` on the wire, `· +1 −2` on the CLI) — with seven findings, F-22 (`write_file` refused on the **call** side while the grant-side alias works — S8-era replays freeze; the honest block did its job) and F-26 (identical writes publish no count keys — honest absence, unmarked) named as **the two the next slice should own**. **N1 held through T1–T9**: no UI in this slice's own nine tasks. **It stopped holding the very next day** — see below — and **2026-10-26: T10 and T11 close the two loose ends**, giving the UI work a task entry in this slice (T10, retroactive) and a spec for F-22/F-26 (T11, not yet executed) instead of leaving either as prose with no owner.

**Loop workspace page — built (2026-10-25), given a home in [`S31-loop-workspace/tasks/T10.md`](S31-loop-workspace/tasks/T10.md) 2026-10-26.** The designed screen now exists at `/loops/:id` (the old page moved to
`/loops/:id/classic`, one click from the page's ⋮ menu). Plan: [`docs/superpowers/plans/2026-10-25-loop-workspace-page.md`](../superpowers/plans/2026-10-25-loop-workspace-page.md),
executed inline; **UI 425 tests in 35 files, `tsc` clean** (measured 2026-10-25). Collapsible workflow rail (kit
`WorkflowTrackerRail`) with Aeon's own agents panel and footprint in its `renderControls` slot; the agent chat with a
page bar (git state, terminal toggle) and a resizable `TerminalTabs` dock wired to `terminal.*`; a collapsible tabbed
inspector (our own Step tab with sub-steps, the kit `Tree` for Files, per-file tabs); breadcrumb, engine state and nav
in the app header through a new header portal (`app/HeaderSlot.tsx`) and a full-bleed layout mode. The side menu is
untouched. The bench's Agent chat tab has a **Full page** layout that draws the same view from the scripted scenarios.
**Not built, because the daemon does not provide the data (no invented figures):** tokens/sec and token counts in the
footprint (a `usage` field on `step.done` would enable them — **`S33-telemetry/brief.md` is the slice that would add it**, cross-referenced there), the "Workspace architecture" card (a project-facts reader),
and "Open in Editor" (a Tauri opener command — needs an AGENTS §4b exception decision). The Telemetry and Agents header
tabs are drawn disabled ("Coming next"); there is no Config tab (user decision). **The inspector is the kit's collapsible `SidePanel`** (0.2.11) — it folds to the same 56 px rail as the workflow rail; because
`SidePanel` overlays its container and has no width callback, a spacer column of the panel's width keeps the chat from being
covered, and resizing is left off. **Kit requests found:** an `onWidthChange` on `SidePanel` (so it can be resizable
without covering the chat), a trailing
slot on `Tree` items (so `+18` / `read` / `1.23` can sit on the row instead of in the card below); no `ScrollArea` mode
that clips rather than widens its content (the rail's controls needed a width-neutral wrapper). **Findings from the
visual check that the jsdom tests could not see (all fixed):** the agents panel and the inspector's content widened
their kit parents and were clipped mid-word; the kit terminal defaulted to its light scheme; a not-started step showed
"Cost: unknown — …". Auto-approve is intentionally **not** on the real page — it would let the app answer policy
"ask" prompts, which belongs in a daemon-recorded loop setting (G14), not a browser toggle.

**[S12 · MCP client](S12-mcp-client/)** — **Built (2026-10-24):** all ten specced tasks landed in one
solo sequential pass, commits `f5b58b8` (T1) → `d8df86a` (T2) → `67d9e44` (T3) → `8b5090b` (T4) →
`b45b991` (T5) → `d2674f0` (T6) → `c911f0e` (T7) → `0f71f1f` (T8) → `9a16ea2` (T9) → T10 (this record,
the walked demo, the QA amendment). The protocol client is hand-rolled — Q1 was decided by measuring
the SDK (8 new modules for the JSON-RPC half alone against a 393-module tree), not by taste. The
slice's four load-bearing refusals are each grep-pinned, not merely tested: **private is invisible**
(peer and stranger get byte-identical `unknown tool:` refusals — MC2's escalation dissolves with no
shared tools to compute over); **classes are configured, never guessed** (`classify.go` is the only
raise site; annotations may raise, never lower; no `default:` arms — MC6); **grants are `ToolAccess`
policies and nothing else** (no struct holds a `policy.Policy`; `TestExactlyOneWayToRevoke` scans
every non-test file with a positive control — MC4/MC10); **the layer is deletable** (`cmd/aeon` is the
only importer, no feature flag exists to grep for, whole module passes with no configuration — MC11).
Outward/irreversible tools **ask at use even under an allow** (L22), the 30s bound prints `timeout:`
as a fact and never an empty result, and the walk at [§S12 of DEMO.md](DEMO.md) — 8 steps against the
spec-literal fake, **12 findings (0 high / 4 medium / 8 low)** — is what earned the last three code
fixes of the slice (a class had been printed over a dead wire; a dead server killed the CLI with
exit-1 on stderr instead of reporting absence; the fake listed a tool its call dispatch refused).
**The runtime still does not import the layer, on purpose** — DEMO §6/§7's step-level halves are
F-1, S13's registry work, recorded rather than propped up. **QAN-10 stays open on purpose: no test in
this repository has met a real MCP server.** The tree carried someone else's in-flight uncommitted
work throughout (`step.policy.decided` in runtime/protocol plus `ui/src/components/agent-chat/`); it
was never staged, never reverted, and its author keeps those paths.

**[S13 · Agents](S13-agents/)** — **Built (2026-10-25):** all eleven task specs landed in one solo
sequential pass, commits `1da1d19` (T1) → `e4953b8` (T2) → `413442a` (T3) → `683758d` (T4) → `be0ada3` (T5) →
`47894c3` (T6) → `a2e4391` (T7) → `fb06ec7` (T8) → `d4a0f77` (T9 · git-loop: no handler, all 372 lines
dispositioned) → `4a11531` (T10 · resolver, validator flip, retry) → `2832b7e` (T10 · **the dispatch seam the
demo walk found**) → T11 (this record: the walked demo, the QA amendment, the honest refusals). **1800** Go
PASS / **431** UI (tree-wide, measured 2026-10-25); `go test -race ./...` clean this round; vet and the import
guard clean. What shipped: the loader that panics at init on any half-true body and `handlers.json` pinned
**both ways** (25 bodies ⇄ file, `git-loop` absent); 25 bodies carrying zero retired vocabulary — the guard is
trusted because **no original passes it** (`TestVerifyFailsOnAnOriginalBody`, all 26, each naming ≥3 forbidden
tools + the skill + the ritual); drop records with line-referenced arithmetic and the split half-line
(exit-137 decoding deleted, its *consequence* kept — QAC-05); the **closed** tech-tag registry (ten tags, one
table; `init()` panics on a duplicate naming both; `Resolvers()` returns a copy; **no fallback handler**,
asserted against `default` and `read-only`); the validator's D13 flip — `by-tech-tag` is a real check, shipped
blueprints now validate against the **real** registry in CI, and `dev-loop`'s unverifiable set shrank to the 9
S16 rows; the policy gate **finally speaking to the corpus** (body name → `policy-agent` bound at the single
choke point, PL5); per-body handoff schemas reaching the model for all 25 (the validator-side adoption recorded
as a FINDING on a call site this session did not own); and T10's retry: one eligibility function, six refusals
byte-unchanged, one transition, one dispatch, `loop.retried` once, the app rendering the **daemon's** verdict.
The walk itself produced the slice's best catch: the resolver was registered, validated and closed — and
**nothing called it at dispatch**; every test stayed green until the demo ran (§2 found it in one command; the
lesson, restated for the next engineer: walk the demo). Model-dependent claims are marked, not smoothed:
this box carried **no provider credential** on the walk day (the daemon's own banner reads `routes none`), so
the Go the specialist writes, the answered gate and the live eligible-retry carry the exact operator commands
in [S13-agents/UI-QA-DEMO.md](S13-agents/UI-QA-DEMO.md); there was **no S9-era loop on this box** to retry
(the pre-S13 state directories went with the `/tmp` sandboxes), and per the task's own rule the historical
retry is recorded **not walked** rather than fabricated. `conversational-map` still refuses at `triage` —
`task-coordinator` is one of **five bodies S13 did not ship** (brief-author, product-manager, project-manager,
proposal-author, task-coordinator — the follow-up; the refusal message, which still promised "arrives at S13",
was caught by this walk and made honest in the same commit). **(Closed 2026-10-26, S17 follow-up pass: all five bodies shipped — task-coordinator `1c8403a6`, proposal-author `c627f2aa`, brief-author `ad366615`, product-manager `c2d131af`, project-manager `db982ae2`; each a rewrite under the S13 guards with its drop record, each with a run-once, the named-five negative control standing on the originals (floors 6/5/4/3/3), the S9 retry flip landed, `futureAgents` now EMPTY with its third state pinned (QAC-34–40). The four blueprints that named them — development-map-direct, conversational-map, proposal-brief, development-map-pipeline — answer 0 invalid / 0 unverifiable through the daemon's own Analyze path, and `aeon loop start` on a throwaway `XDG_STATE_HOME` refused nothing for any of the four (measured 2026-10-26; completion beyond preflight remains OWED — scripted provider only, per the standing ruling).**)** The tree again carried someone else's in-flight
work throughout (S31: steers, agent-chat, the workspace page, `step.policy.decided` fixtures, a live dev daemon
and `tauri dev`); it was never staged, never reverted, and its author keeps those paths — the two external
blocks (`TestASteer...` flakes 1/3; their `PageLayout.tsx` unused import fails the UI typecheck inside
`make vet`) were recorded, not gated on. **F-1 is closed: the runtime now runs 25 specialists.**

**[S32 · Parallel agents](S32-parallel-agents/)** has a `SKETCH.md`: **P33** (new in ARCHITECTURE §8) lets a
loop run up to `project.maxParallelAgents` steps at once, default 1. A1/AS4/K9 unchanged. Open questions on
independence, shared working tree, recovery and budgets are listed for its brief. Not yet built.

**S25 history, S26 roadmap and S27 continuous work are no longer sketches** (stale as of 2026-10-26 — S25
and S26 both reached `design.md`/`plan.md`/task files that day, S27 a brief). The Stage 5 FLEET section
above this one is now the record; [`ROADMAP.md`](ROADMAP.md) §2 carries the detail.

**[S72 · Configuration shell](S72-configuration-shell/)** — **Built 2026-11-06 — T1–T4, UI-only** (no daemon
method, no migration, no Rust). One rail entry + self-registering page registry + `/config/:page?`; thirteen
Settings sections moved unchanged to one page each, `/settings` reduced to appearance/language/tones;
Agent management extracted from the 922-line bench page into `ui/src/features/agents/` (fourteen files, all
under SIZE1) with the bench page and `/config/agents` rendering one implementation. Measured: **vitest 159
files / 1555 passed / 1 skipped**; `tsc -b` = the nine pre-existing `features/settings` errors, none S72's;
`vite build` ✓ 6.26 s. The T4 browser walk (system Chrome over a stubbed shell + REAL daemon, throwaway
store) framed all fourteen pages, walked every primary action, and proved acceptance 6 with the daemon
really down — it found the two live defects **F-1** (the Daemon-settings toggle click never reaches
`settings.set` in a real browser) and **F-2** (`scheduler.status` answers `repos: null` against its contract's
array, so the Continuous status card shape-refuses on root-less stores); findings table in
[T4 Notes](S72-configuration-shell/tasks/T4.md).

**T5, same day — the shell stays live while a page works** (not UI-only: it changed the seed engine and the
Rust shell, after the operator reported "the page does say loading but the app is locked"). Three causes,
measured apart: `rpc_call` was a **synchronous** Tauri command, so a slow daemon call stopped the window
painting at all (`connect`/`disconnect`/`workspace_*` writes/`component_*` now async over a cloneable
`SessionHandle`, every blocking step in `spawn_blocking`); the seed engine re-read each set's **uncached**
source once per record — `seed.status` over the fifteen sets **6 164 ms → 140 ms**, `agents.firstParty`
3 960 → 77, `tools.core` 2 050 → 17 — now one read per operation via `seed/snapshot.go`; and the Policy page
asked for all fifteen sets to render one. The Configuration row now wears the kit's own loading state while
its page waits (query or lazy chunk), and in real Chrome the page renders in 316 ms, navigable away
mid-flight. Measured: `cargo check --all-targets` rc=0; `cargo test --all-targets` rc=0 (55 passed / 4
ignored — the shell's own suite, run here for the first time); vitest **161 files / 1567 passed / 1 skipped**;
`tsc -b` the same nine pre-existing errors; `vite build` ✓ 6.09 s; the Go seed family and `cmd/aeon -run
Seed` ok. **F-3 closed, F-6 opened.** A follow-up ask of the same day — "make sure everything we do in Rust
is async… is this possible?" — finished the job: the last four synchronous commands
(`endpoint_path`/`background_presence`/`workspace_list`/`workspace_active`) are async too, thirteen of thirteen,
with **zero web-layer change** because `lib/bridge.ts` already awaits them all; the rule is written in
`main.rs`'s header and held by `ui/src-tauri/tests/command_threading.rs`, which walks `src/` so a command in a
file nobody has written yet is covered and was canaried to prove it bites. Following the rule past the command
surface **found two freezes that were not commands** and fixed both: the window's close handler took the
session-slot mutex that `connect` holds across a whole connect (the X froze the app for the length of a
connect — now an atomic mirror, with `close_policy` untouched and still pure), and the tray poller held that
same mutex across a daemon call, so a wedged daemon parked every command behind it (now a `SessionHandle`
cloned out before the wait). A `tokio::net` transport was considered and declined with reasons. **F-7 and F-8 opened.** The desktop window is still unrun here (the
named gap), so the Rust half is proven by construction, compile, the Rust suite, and a guard that refuses a
synchronous command — not by a clicked
window — see [T5 Notes](S72-configuration-shell/tasks/T5.md).

**[S73 · Agent CRUD page](S73-agent-crud-page/)** — **Built 2026-11-06 — 8/8, closed.** The paging convention every
daemon read follows (`docs/aeon/PAGING.md` + `internal/paging`), `agent.rows`, the append-only `agent_change` ledger
under audited doors that replaced the un-audited writers, and `/config/agents`: server-paged kit `Table`, `SidePanel`
seeded from `agent.view` (the nine-field hazard walked surviving on the real store), typed delete, AM4 refusals
verbatim, ledger tab. Walked 44 steps in real Chrome against a real daemon, rc=0 both modes; findings **G-1…G-6**
in its own namespace. Evidence [T8 Notes](S73-agent-crud-page/tasks/T8.md), demo + STATUS in the slice folder.

---

## The documents, and what each is for

| | |
| --- | --- |
| **[STATUS.md](STATUS.md)** | this page — what is built and what is next |
| [ROADMAP.md](ROADMAP.md) | **the plan of record.** 23 slices, policy, triggers, agents, decisions, amendments, risks |
| [VISION.md](VISION.md) | the pitch: diagrams, stage-by-stage, mockups |
| [TASKS.md](TASKS.md) | all 232 tasks in execution order, with the framework-readiness audit |
| [PLAN.md](PLAN.md) | the phases, and what blocks what |
| [ARCHITECTURE.md](ARCHITECTURE.md) | invariants, and the decision-prefix registry |
| [SEEDING.md](SEEDING.md) | how a slice becomes a loop |
| [`../test_suites/`](../test_suites/) | the QA suites, amended in place by each slice |
| [`../../AGENTS.md`](../../AGENTS.md) | **start here to write code** — layout, the framework, the rules |

**Inside a slice folder:** `brief.md` is the contract · `design.md` the decisions · `plan.md` the
decomposition · `tasks/T*.md` the specs, each carrying its own **Notes** once built · `DEMO.md` what you
can run.

---

## What built slices left for later ones

Carried deliberately, so nothing is rediscovered:

| From | For | What |
| :--: | :--: | --- |
| S1 | S4 | The store path and the framework's database contracts are settled; the schema is S4's |
| S2 | S4 | **`seq` is in-memory and resets on restart.** S4 moves it onto the `event` table — written beside the counter itself |
| S2 | S4 | Retention is a bounded 1024-event ring, a guess. The store replaces it |
| S2 | S19 | `internal/protocol` imports nothing transport-related, and the debug method is marked so the narrow surface excludes it mechanically |
| S2 | S3–S17 | Every method, field and event kind is **additive**. The rule is beside `PROTOCOL_VERSION`; `conformance/forward-compat.json` is its standing test |
| S2 | anyone | **A client's cursor is `params.seq`, never the envelope's** — the transport's restarts at reconnection. QAP2-10 |
| S3 | S4 | The store persists `Message` as canonical JSON via `FormatVersion`, `ValidateTranscript` and `StoreTranscript`. **A suspended transcript is valid** — an unanswered `tool_use` in the final assistant turn is a waiting loop, not a lost result |
| S3 | S5 | Build a `ModelRequest`; never a wire body. `Cost.Add` is there for rolling a loop's cost up, and **an aggregate over uncertain values is itself uncertain** |
| S3 | S5 | `OwedToolErrors` on a result means the model's arguments would not parse and **a `tool_result` with `IsError` is owed.** This layer says so rather than inventing a tool outcome |
| S3 | S7 | Recovery replays a stored transcript, so `thinking` signature fidelity is what makes it possible — and the fixture it is tested against is **hand-written** |
| S3 | S18 | A third provider is one file in `adapters/` plus one `RegisterAdapter` line. `AEON_*_BASE_URL` already exists and is the mechanism a local model will use |
| S3 | S19 | `debug.subscriptions` and `debug roundtrip` are debug surfaces. The narrow table excludes anything marked `Debug` |
| S3 | S9 | **The bench is not a draft of the app.** S9 inherits the generic bridge, the correlating session, the three-state renderers and the credential rule — and decides whether the tabs stay, hide behind a developer flag, or go |
| S3 | every slice | **A slice extends the cumulative demo** (`AGENTS.md` §4b — `docs/aeon/DEMO.md`, appended per slice), and **a credential goes in and never comes out** (§4c) — two types, not one redaction |
| S3 | S9 | **Tauri v2 gates `listen` behind its ACL.** `src-tauri/capabilities/` must grant `core:event:default` or every call works and every stream is silent. Guarded by `ui/test/capability.test.ts` |
| S3 | S9 | **`Alert` discards its children.** The kit renders `title` and `description` only; five alerts were silently losing their explanation before this was found |
| shell (2026-10-18) | S9 T3 | **Liveness already exists in the shell — build on it, do not reinvent it.** `rpc::Timing` is the single place the connection's clock lives (`read_tick` is a wake-up, not a deadline; `probe_after`/`probe_timeout`/`probe_strikes` are the liveness policy), and `dead_after()` names the worst case. T3's **Q8 is answered for the shell half**: the mechanism is a `ping` probe, threshold `dead_after()` ≈ 40s, and it is a *probe* not polling — I4 still forbids an interval that re-reads state, and `send_ping` reads none. What T3 still owns above it: the staleness **clock and age** the window shows, `stale` vs `given up`, and reconnect-and-**re-read** (D7). What it must not do is treat silence as staleness (D6) — `tests/liveness.rs::silence_on_a_live_socket_stays_connected` is the assertion it must keep passing |

## Known gaps, named rather than discovered later

| | |
| --- | --- |
| **Windows cannot serve** | The framework's named-pipe transport is `ErrPlatformNotImplemented`, deferred upstream. A Windows build runs `version` and `--vectors` only. [`BUILD.md`](../../docs/BUILD.md) · QAP2-12 |
| **Six-target build: 1 of 6** | `cgo` needs a C toolchain per target. The decision is a native runner per OS |
| **QAP2-07 unwalked** | A third-party client written from `conformance/` alone, by somebody who has not read Aeon's Go client. Cannot be self-served |
| **No adapter has ever spoken to a live provider** | All ten fixtures are hand-written, every one labelled with what was not verified. `docs/test_suites/aeon-model.md` is the suite that converts belief into fact, and **not one of its 31 cases has been run.** QAM-11 is the one that matters most |
| **The signed-`thinking` fixture is a plausible guess** | Its real length, alphabet and delta count are unknown, and **S7's recovery rests on byte-identical replay of whatever really arrives.** QAM-04 |
| **Routes and keys are not persisted** | They live in the daemon's memory and are gone on restart. The UI says so where a person reads it. S4 |
| **`aeon/` is uncommitted** | S1 and S2 together. The first loop runs `git add -A`, so an uncommitted ground floor stops being a diffable baseline |
