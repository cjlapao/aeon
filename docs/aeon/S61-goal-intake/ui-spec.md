# UI spec — the new-project wizard (specification only)

Written 2026-10-30 for the app work that follows the daemon slices. **No UI code is part of S61.** Everything here is
read from the daemon's descriptors; nothing below types a vocabulary, a sentence or an authority statement in the client
(MANAGEMENT-PATTERNS P7). A guard test (`*Rpc.guard.test.ts`) asserts every method literal is registered in the
generated `contract.ts`.

## Entry points

- Home / Projects: a **"New project from a goal"** action. Disabled with the daemon's sentence when `intake.vocabulary` reports `routeAvailable` false — the same fact
  `intake.start`'s gate enforces, so the front door cannot open a wizard the back door would refuse (GI16; T11).
  `routes.list` answers for the settings screen that configures routes.
- Settings → Charter: a list of root components with their current charter revision and "not applied" proposals.

## Steps

1. **Goal** — a free-text field and the stack tags (suggestions from `intake.vocabulary`). `intake.start {name}` opens the
   interview; the conversation view is the existing chat/loop view over the scratch loop (no new chat component).
2. **Interview** — the intake agent asks questions through the normal question mechanism; the person answers as today.
3. **Plan preview** — when `intake_plan` exists: a read-only rendering of `intake.plan {intakeId}`: charter, components
   as a **tree** (folder = project), the template per component with its **exact file list** (path, mode, size), the
   git commands, the first epics/goals/items, and the digest. Editing happens by talking to the agent again (a new
   proposal), never by editing the proposal in place.
4. **Target** — a directory picker (native dialog via the existing Tauri bridge — no new permission is assumed; the
   chosen **absolute path** is sent, nothing is read by the web layer) and the publish choice from the descriptor:
  `publish[]` choices, each `{value, label}` — the LABEL is an i18n KEY (`wizard.publish.*.label`), never English
  typed in the client. `private`/`public` are disabled with `remoteCreateReason` (the daemon's own sentence for the
  closed gate) unless `intake.vocabulary` reports `remoteCreateEnabled`; a chosen `private`/`public` shows that
  choice's `statement` — the daemon's confirmation text, rendered verbatim — and `public` adds a required checkbox
  (sent as `confirmPublic`).
5. **Commit** — `useConfirm` with the statement of the chosen publish choice from `intake.vocabulary`, previewed by `intake.plan`
  (T11: `intake.plan` IS the preview read — there is no separate `preview` method; the digest it prints is what
  `intake.commit` must carry); declining sends nothing. On success the
   steps list streams from `intake.status` (each ledger step with its fixed-template detail); a failure shows the
   daemon's sentence verbatim and a **Resume** button that calls `intake.commit` again with the same digest.
6. **Done** — links to the new root component, its charter, and its roadmap; the goals are shown as **"awaiting your
   approval"** (they are `in_review`; approval is the existing goal flow).

## Rules

- Refusals shown verbatim with the draft kept; rows updated from responses, never optimistically.
- Offline daemon ⇒ every write disabled, reads show last data.
- en/pt parity; no new dependency; no `localStorage` for domain data; no `fetch`.
- The charter's `proposed.*` block renders as **"not applied"** with a link to the owning screen (budget → S55,
  autonomy → S56, verify → S57, environment → S59); there is no "apply" button in this wizard.
- A source grep test forbids hardcoded template names, publish values, `check` kinds and stack suggestions.
