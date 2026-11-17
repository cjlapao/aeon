# S77 — launch attachments + the context-aware home composer

Status: **built 2026-11-10** (brief-only record; no task files). Daemon + app halves
green; the loop-workspace composer wiring is the named follow-up.

## What it is

Two halves of one operator request: **a loop may START with files attached**, and
the Home composer becomes the special one — the agent-chat `MessageComposer` with
CONTEXT-AWARE panels (`@` targets the selected component's files, not the ether;
`//` the loops there; skills unchanged at `/`; `$` agents).

## Daemon half — launches carry receipts (bytes once, refs forever, at the FIRST turn)

- `internal/model/metadata.go`: `RequiredCapability(mediaType)` (image/*→`image`,
  `application/pdf`→`pdf`, text/*→`file`) and `Route.DeclaresCapability` — one
  vocabulary shared by both sides.
- `loop.start` gains `attachments?: [{document, rev}]` (`LoopStartParams`, with the
  S77 doc comment). **Absent = the pre-S77 request byte for byte**; `[]` refused
  (S69 AT2 law); named refs REQUIRE an explicit `slug` — a minted slug owns no
  earlier puts, so the refusal names the remedy.
- `internal/daemon/launch_attachments.go` (192 lines): the start door resolves refs
  against the put scope and REFUSES naming every offender the route cannot carry
  (route-unroutable also refuses — attaching forces checkability); per-loop receipts
  cache beside S68's chosen-skills map, **restart forgets** (same ruling), the
  restart door forgets both.
- Receipts ride the kickoff: `start_loop.go` puts the receipts ON the kickoff
  `chat_message` row (the durable receipt, same pattern as S68's chosen-skills
  kickoff), and `store.CreatePinnedLoop` now stamps `kickoff_message_id` on run 1 in
  the same transaction — so a restart re-reads bytes from the LINKED row, never a
  thread-order guess. `blueprint.go`'s step dispatch re-attaches the cached receipts
  to the run's model steps via `launchAttachmentParams`, which is ALSO the belt:
  it filters against the STEP's route, so a lane route moved after the launch
  never ships bytes it cannot carry. **The step compile itself stays S69 byte for
  byte** — the first cut of this slice filtered every attachment at the expansion
  and broke `TestAMessageWithAttachments…` (S69's mid-loop turn attachments: the
  recorded S69 ruling puts attachment FORMAT honesty at the ADAPTER, and routes
  that declare no capabilities legitimately carry turn files); the belt therefore
  sits only on the launch receipts' one road to the wire, where they are
  identifiable as such.
- Conformance fixture `conformance/blueprint/loop-start-attachments.json`;
  `contract.ts` regenerated (`protogen --check` rc=0).

## App half — the special composer

- `lib/attachments.ts`: `declaredType` broadened (source/text extensions declare
  `text/plain`; `application/octet-stream` stays the client-side refuse marker),
  `requiredCapability` + `attachmentLaunchGate(files, route)` — the gate speaks the
  daemon's own sentences BEFORE a byte is put, and **absent capabilities ≠ yes (P31)**.
- `lib/contextPickers.ts` (319 lines): `useContextPickers` — `@` gated until a
  component is chosen (the panel NAMES the missing context; after, it lists
  `workspace.git.status` changed files + debounced 250 ms stale-guarded
  `search.query` code-corpus hits, cap 50, failure renders its sentence — P31);
  `//` loops scoped to the component, **picking navigates**; `$` agents (lazy
  `agent.list`) and insert the name; `commands` honestly empty. Pure
  `applyTokenInsert` + `mergeSpecialKeyHandlers`/`mergeAddActionHandlers` compose
  the composer's ONE delegate with `useSkillPicker`'s.
- `features/home/pages/HomeLaunchComposer.tsx` (376 lines): MessageComposer with
  component/flow Pickers (testids kept), provider/mode in the footer's Advanced,
  skill chips, quick starters; submit mints a slug from the goal ONLY when files
  are staged, `putAllAttachments` under `{slug, componentRoot}`, then `loop.start`
  carries `slug` + receipts (absent when nothing staged); a staged file the chosen
  route cannot carry DISABLES Launch with the per-file sentence; attachments are
  always removable (composer chips); a failed submit keeps the draft.
- `HomeScreen.tsx` 652 → 381 lines; routes.list rows now carry `capabilities` down
  to the gate. README line updated ("HomeScreen migration… remains" → migrated).

## Validation (2026-11-10)

- `go test -tags sqlite_fts5 ./internal/model/ ./internal/protocol/ ./internal/store/` — ok
  (protocol 13.3s after the step.go revert).
- s77 daemon tests 7/7 pass (`TestALaunchWithAttachmentsFilesReceiptsAndRidesTheFirstStep`,
  `TestALaunchRefusesAttachmentsItsRouteCannotCarry`, `TestAPartiallyCapableRouteRefusesNamingTheOffender`,
  `TestStartWithAttachmentsAndNoSlugRefusesAtTheDoor`, `TestStartAttachmentsResolveAgainstThePutScope`,
  `TestLaunchReceiptsRerideFromTheLinkedKickoffAfterRestart`, `TestATurnAttachmentIsNeverMistakenForTheLaunch`),
  beside the S69 attachment tests (`TestAMessageWithAttachments…` green again after the revert).
- Full `./internal/daemon/` suite 2026-11-10: 14 reds, NONE attachment-related. One WAS mine —
  the first cut's per-step expansion filter broke `TestAMessageWithAttachments…` (S69 turn
  attachments on a no-capabilities route); fixed by moving the belt to the launch injection
  (above), the suite proves the fix. The other 13 are the shared dirty worktree's concurrent
  planes: their failure texts name them (e.g. brainstorm tests: "there is no tool called
  `write` in this step. The tools you have are: read, doc_write, doc_read, … goal_create,
  attach, run_start" — a tool-grant rework; the seed set's `templates.scaffold has no
  applier` from `seed.go`/`agentseed`; mergequeue vocabulary, surface classification, policy
  verbatim). The set SHIFTS between runs and several (`TestE2EBlockedHoldsTheRepositoryForever`,
  `TestResolveReverifiesTheNewHead`) PASS when run alone — load-flaky neighbours, named not
  chased here.
- `go build ./...`, `go vet`, `make guard` ok; `protogen --check` rc=0.
- vitest: `home-composer` 3/3 UNCHANGED (the `loop.start` payload assert proves
  absent-field byte-identity), `home-context-pickers` 7/7 new (gate before/after
  component pick, `//` navigation-is-the-pick, `$` insert, disabled-launch + gate
  sentence + remove re-enables + put-once-then-start ordering + receipt params,
  pure-helper units), plus `i18n` / `home-gates` / `product-start` / `product-shell`
  / `navigate-listener` / `notifications` / `notifications-golden` — 52 passed.
- `tsc -b --noEmit`: zero errors in any touched file (baseline errors elsewhere are
  concurrent dirty work).
- Full `npx vitest run` 2026-11-10: **180 files, 1719 passed / 1 skipped / 0 failed** —
  the whole app green with S77 in it.

## Phase D — the loop-workspace composer (same day)

`LoopWorkspaceView.tsx` owns its composer draft now (the pickers INSERT into it) and
merges BOTH picker sets into the chat composer's one delegate: `/` skills (as before)
plus `@` the loop's component files, `//` the loops (`LoopWorkspaceScreen` fetches
`loop.list` once per connection and owns the navigation), `$` agents. The bench page
passes no `contextLoops` and the panel honestly says nothing to open. Deviation,
stated: the plan said workspace `@` would browse `loop.files {id,path}` — what landed
is the same component-scoped pair (git-status + code search) the home uses, ONE lib
serving both hosts; `loop.files` browsing stays a follow-up that only widens `@`.

Suites (2026-11-10): the eleven chat/workspace files — **195 passed** (controlled
draft included), tsc zero owned errors.

## Chat surfaces on the ui-kit Panels, on the app theme (same day, operator request)

The chat's own boxes moved off hand-rolled CSS onto the kit: `ChatPanels.tsx` mounts
its three panel kinds (task list, persistent panels, temporary picker) as
`Panel variant="simple" padding="none"` — the local kit wrapper feeds the app's
`surfaceTone`, so every panel wears the theme; the kit's default corner is
`rounded-md`, and `chatPanels.css` was stripped to content (surface, border, radius
and shadow belong to the Panel now). `MessageComposer` (BOTH chat components — the
chat composer renders through it) took the same Panel for its surface with
`rounded-md` corners; textarea, submit button and chips switched from hard-coded
blue to the theme (`aeon-accent-wash/-panel/-text`, `aeon-accent-button`,
`--aeon-accent-*` focus colors), and `ChatComposer`'s pills, Stop button and the
`AgentChat` header divider followed (radii `rounded-md`, accent icon, accent divider).
The drop overlay and attachment chips in `composerAttachments.css` now speak the
theme variables too. Stream SEMANTIC colors (running pulse, orchestrator badge,
amber approval) deliberately stayed — they name states, not the brand.
Validation: full `npx vitest run` again **180 files / 1719 passed / 0 failed**;
tsc zero owned.

## Open / follow-ups

- Workspace `@` may widen to `loop.files {id, path}` browsing (dirs).
- `commands` trigger returns an honest empty until a daemon door offers commands.
- Live-daemon click-through, Tauri, and real-provider images unrun (fixtures/tests stand in).
- The UI gate mirrors the daemon sentences in ENGLISH lib copy (the daemon's refusals
  are English on the wire); panel titles/gate copy carry en+pt keys.
