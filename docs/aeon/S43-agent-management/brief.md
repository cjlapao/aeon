# Brief — S43 Agent management

**Stage:** 5 · Fleet (the first slice whose UI surface is the real app's Settings, not a bench tab).
**Depends on:** S4 (store), S9 (app shell + Settings route), S10/S11 (policy — `AgentKind` targets, the
thing AM2 migrates), S13 (handler registry — first-party agents, the thing AM1 moves into the database),
S15 (`internal/plugin` — import, `plugin_agent`, `CardInfo`, the thing AM3 expands), S8/S17 (blueprints —
the lane `Agent` field AM2 migrates).

## 0. Why now

Most of the Fleet stage is built (S26, S34–S40 all Built per `SLICES.md`). The operator's framing: the repo
is "a big messy set of functions that are not a product in the app" — the next job is piecing it into
something a person configures, starting with the two smallest, most load-bearing pieces: **agents and
policies**. This slice is agents. A sibling slice (not this one) will eventually do the same for policies'
own settings surface, once agents — the thing policy targets — have a real identity (the slug) to target.

## 1. What this is

Every agent (first-party and imported) becomes a full database row: identity (`id`, `slug`), display
(`name`, `description`, `functionality`, `metadata`, avatar fields), lifecycle (unchanged — `plugin_agent`'s
existing state machine), and a `version`. The **agent card** — today's thin `CardInfo` — becomes the
exportable, importable, cryptographically-signed, versioned form of that row: a slice-producing community
builds a card, signs it against a certificate from an external marketplace site, shares it; an aeon user
imports it, aeon verifies it was not tampered with, and a new (or updated, if the slug already exists at a
lower version) agent row appears. A Settings page lists every agent, lets you create one from scratch,
duplicate any agent (including a locked first-party one, which produces an editable copy), edit every new
field (refused on a first-party row, named), delete one (refused if a blueprint still references its slug),
and view an agent's full content — prompt, skills, card — read-only.

**Everything that currently references an agent by name switches to slug.** This is the single biggest
piece of work in the slice and the reason it is sized this way rather than as a small UI addition: policy's
`AgentKind` targets (PL1's closed vocabulary, S10/S11), blueprint lanes' `Agent` field, task assignment,
agent-chain edges, and the handler dispatch registry all currently hold a bare name string. Existing rows of
each need a migration path from name to slug, named and verified, not assumed to always find a match.

## 2. What exists today, measured — the reuse surface

| Piece | Where | What it is today |
| --- | --- | --- |
| Agent content | `internal/handler/*.go` (25 first-party bodies), imported agents' card/prompt files | Code or files, not rows |
| Agent governance | `internal/store/schema.go`'s `plugin_agent` table | `name, source, state, imported_at` — lifecycle only, untouched by this slice |
| The import object | `internal/plugin.Agent` (`import.go:47`) | `Name, Description, State, Requests, Prompt, Card, Source, ImportedAt, FirstParty, Carried, ClaimedFirstParty` |
| The card | `internal/plugin.CardInfo` (`import.go:406`) | `Name, Description, Version, Skills []CardSkill` — AM3 expands this |
| Policy agent targets | `internal/policy/types.go:164`, `AgentKind` | "names one agent by name" — AM2 migrates this |
| Blueprint agent references | `internal/blueprint/types.go:85`, lane `Agent string` | A bare name; AM2 migrates this |
| Avatar system | `ui/src/components/minion-avatar/` | `MinionAvatar.tsx`, `characters.ts` (`MinionMood`, `MinionColor`, hair/body/eyes), `catCharacters.ts` — already built, this slice only wires an agent row to a subset of its props |
| The new migration/bench-page registries | `internal/store/migrations/README.md`, `ui/src/features/test-bench/lib/benchRegistry.ts` | Built 2026-10-30 this session — use them: a new migration is a timestamped file, not a `schema.go` edit; **this slice's UI is Settings, not a bench page, so the bench registry does not apply here** — a new daemon-surface convention question for T-whichever builds the UI (`VERIFY` how Settings pages are currently added; there is no registry for that yet, possibly worth building the same way if more than one Settings section is coming) |

## 3. Locked decisions (`AM`)

See `design.md` AM1–AM5 for the full reasoning. Summary:

- **AM1** — every agent is a DB row; `internal = true` blocks edit/delete, never duplication.
- **AM2** — `slug` (generated from name, immutable, unique) is the universal reference key, replacing every
  current by-name reference, including policy's `AgentKind` targets.
- **AM3** — the card is expanded (functionality/metadata/avatar/version/signature), verified for integrity
  on import (Ed25519 over an X.509 certificate, stdlib only) against its own embedded certificate, with no
  local trust-root management; import is idempotent by `(slug, version)`. An unsigned card refuses by
  default, behind a config override (`agents.allowUnsignedImport`) that imports it marked `unsigned`.
- **AM4** — deletion is refused when a blueprint **or a policy row** still references the agent's slug.
- **AM5** — the Settings surface is daemon-method-backed (P34), never file-only.
- **Slug collisions refuse**, naming the conflict — never silently suffixed.
- **Blueprint YAML takes a raw slug directly** — no name→slug resolution step.

## 4. Out of scope

- **A trust-root/signer registry, or revocation.** This slice verifies tamper-evidence only; it does not
  judge or refuse by signer identity. Named as a real gap for a later slice.
- **The marketplace/signing website itself.** External, not aeon's to build.
- **Live mood reflecting a running loop's real state.** The avatar mood this slice stores is a static,
  per-agent flavour default shown in Settings; wiring an avatar to a live loop's state (if ever wanted) is a
  different slice.
- **Policy's own settings/authoring UI.** This slice only migrates policy's *target* semantics (name→slug);
  it does not add a UI for authoring policy rows — PL15 (policy authoring is CLI-only) is untouched here.
- **Removing `plugin_agent`'s existing lifecycle machinery.** Untouched; the new content table joins it by
  slug, nothing about `disabled/enabled/revoked` changes.

## 5. Open questions — all resolved 2026-10-30

All six are now locked (see `design.md`'s "Resolved 2026-10-30" section and AM1–AM4 above); kept here as a
record of what was asked and decided, not as open work:

1. ~~Exact new table name/shape~~ — mechanical, left to T0/T1 (not a product question).
2. Policy rows also block deletion, not just blueprints.
3. Slug collision: refuse and ask for a different name, never silent-suffix.
4. Signature/certificate format: Ed25519 signature over an X.509 certificate, Go stdlib only.
5. Blueprint agent reference: a raw slug, typed directly in YAML — no name resolution.
6. Unsigned card: refuses by default; a config setting (`agents.allowUnsignedImport`) overrides this to
   import it marked `unsigned`.

Three small mechanical items remain for T0 (table shape details, the daemon-method seam location, where the
new config setting is scoped) — see `design.md`'s own "Open items this design still leaves to T0."

## 6. Acceptance criteria

1. Every first-party and imported agent is a database row; `aeon agent list` (or its socket equivalent)
   shows all of them, including ones that were previously only code.
2. A first-party agent's row refuses a direct edit/delete with a named reason; duplicating it succeeds and
   produces an editable, non-internal row with a freshly generated, unique slug.
3. Importing the same card `(slug, version)` twice is a no-op; a higher version updates in place; a lower
   version is refused, named.
4. A tampered card (signature does not match its embedded certificate over the card's bytes) is refused on
   import, named as tampered, never silently accepted.
5. Every existing policy row with an `AgentKind` target and every existing blueprint's lane `Agent` field
   resolve correctly after the slug migration — a dedicated test walks the real, current corpus/blueprint
   set and confirms zero orphaned references (a reference that finds no matching agent is a refusal at
   migration time, never a silently dropped row).
6. Deleting an agent still referenced by a blueprint **or a policy row** is refused, naming the
   blueprint(s)/policy row(s).
7. The Settings UI lists, creates, duplicates, edits (with the internal-row refusal visible), deletes
   (with the blueprint/policy-reference refusal visible), and shows an agent's full content read-only — all
   through daemon methods, confirmed by the same P34 grep every Trust-trio slice ran (`grep -n
   'rpc\(Typed\)\?('` across the new Settings page, confirm every literal is a registered method).
8. With `agents.allowUnsignedImport` at its default (`false`), importing an unsigned card is refused, named;
   flipping the setting imports it, visibly marked `unsigned` in the UI.

## 7. Handoff status (2026-10-30, written by the planning session — not the builder)

**Done:** this brief (all 6 open questions resolved 2026-10-30), `design.md` (AM1–AM4 + 3 remaining
mechanical items for T0), `plan.md`, and `tasks/T0.md` (investigation).
**Not started:** T1 onward (schema, card format, import/signing, policy/blueprint migration, the daemon
methods, the Settings UI, close-out) — `plan.md`'s task sequence names all of them with enough detail to
start from, but only T0 has a full task file. The next session should run T0 first (it is deliberately
investigation-only, no code, and settles several of §5's open questions by reading the real current
call-sites before T1 commits to a schema) and either answer §5's remaining operator-facing questions itself
by asking, or make a documented call the way this brief and `design.md` did throughout, recorded in T0's own
Notes before T1 begins.
