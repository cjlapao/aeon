# Design — S43 Agent management

Written 2026-10-30, by a session handing off before credits run out. Every ruling below came from a
real brainstorming conversation with the operator (not assumed) — see the conversation this slice was
born in for the exact exchange if a ruling needs re-litigating. `brief.md`/`plan.md`/`tasks/*` build on
these; do not re-open a ruling without a reason recorded in that task's own Notes.

## AM1 — Every agent is a database row, including first-party ones

Today: an agent's real content (name, description, prompt, skills) lives in a file or a Go handler body
(`internal/handler`'s 25 hand-rewritten bodies, S13). The database (`plugin_agent`, `internal/store/schema.go`
migration 0024-ish — `VERIFY` the exact migration number, it moves) holds only a thin governance row: `name,
source, state, imported_at`. This slice makes the **content** a database row too, for every agent, system
ones included. `plugin_agent`'s existing shape, its state machine (`disabled→enabled→revoked`, revoked
terminal, enforced by trigger) and every test pinning it are **untouched** — this slice does not touch
lifecycle, only content. The new content lives in a new table (name TBD by T1, e.g. `agent`), joined to
`plugin_agent` by the same key both now share: the slug (AM2).

**`internal` flag, not a separate class.** A system/first-party agent is a normal row with `internal = true`.
That flag refuses direct edit and delete (the write boundary, same shape as PL1's authorship check) but never
refuses duplication — duplicating an internal agent produces an ordinary, fully-editable row with
`internal = false` and its own fresh slug (AM2). This is the operator's own words: "duplicate the system
ones" has to work on exactly the rows AM1 otherwise locks.

## AM2 — The slug is the universal reference key, from here on, everywhere

`id` (GUID/int, store's usual PK shape — `VERIFY` which this project's other tables use, store.go's
convention) is internal identity only, never shown as *the* name of anything. `slug` is generated from the
agent's `name` at creation time (and at duplication time, from the copy's name — never copied from the
original), is **immutable once set**, and is **globally unique** — locked 2026-10-30: a second agent whose
name slugifies to one already taken is **refused**, naming the conflict, never silently suffixed. The
operator's own words: "spaces do not play well in text, so
slug is the identifier... internally everything is per slug." The app's own UI still shows **name** to a
person; slug is for code, chat references, and cross-references between rows.

**This reaches every existing by-name reference, not just new code.** Confirmed by grep this session (file
list, not yet verified line-by-line — that is T0's job):

```
internal/policy/evaluate.go       internal/policy/inforce.go        internal/policy/corpus.go
internal/blueprint/validate.go    internal/blueprint/types.go       (Agent string field, types.go:85)
internal/store/agent_tasks.go     internal/store/transcript.go      internal/store/events.go
internal/store/plugin_state.go    internal/store/loops.go           internal/store/steps.go
internal/handler/handler.go       internal/handler/registry.go      (dispatch itself)
internal/plugin/state.go          internal/plugin/state_store.go    internal/plugin/revoke.go
internal/plugin/consent.go        internal/plugin/import.go
```

The single largest, highest-blast-radius item: `internal/policy/types.go:164`'s `AgentKind` is documented
**"names one agent by name"** — PL1's whole closed, audited, heavily-tested vocabulary (S10/S11) targets
agents by name today. This slice changes that to slug. Every existing `policy` row with an agent-kind target
needs a migration (backfill: look up the row's `target.id` against the agent table's `name` column, write
back the matching `slug`) — this is the single riskiest migration in the slice, because a lookup that finds
no match (a policy row naming an agent that no longer exists, or a name that never matched exactly) must
refuse loudly, not drop the row silently (DB19 — a refusal names the row, never guesses).

Blueprints are the second-largest: `internal/blueprint/types.go:85`'s lane `Agent string` becomes a slug
reference. Locked 2026-10-30: a blueprint author types the **raw slug** directly (`agent: my-reviewer-bot`
IS the slug) — no name→slug resolution step, matching how blueprints already use other closed vocabularies
by their literal string value.

## AM3 — The agent card: expanded, versioned, signed

Today's `CardInfo` (`internal/plugin/import.go:406`): `Name, Description, Version, Skills []CardSkill`. This
slice expands it to carry everything AM1's new row holds: `Slug` (NOT regenerated on import — a card's own
slug travels with it, since it is the whole point of re-importing the SAME agent at a new version rather than
creating a new one), `Functionality` (free text or a small closed set — `VERIFY` with the operator which),
`Metadata` (a JSON blob, operator's own word, open-ended by design), and the avatar fields from
`ui/src/components/minion-avatar/characters.ts`: `AvatarType` (`minion`/`cat`), `AvatarColor` (`MinionColor`
or `CatCoat` depending on type), `AvatarMood` (`MinionMood`: `idle/hi/thinking/working/stuck/done` — the
default/flavour mood this agent's avatar shows at rest in the settings list, not a live runtime state;
nothing in this slice wires mood to a running loop's real state, and the brief should say so as a named
non-goal unless the operator asks for it later), plus whatever subset of `colorVariant`/`overallsColor`/etc.
is worth exposing (`VERIFY` with a UI pass — start with type/color/mood, the three the operator named, and
treat the rest as optional polish).

**Signature — locked, 2026-10-30.** Ed25519 signature over an X.509 certificate, verified with Go's
standard library only (`crypto/ed25519`, `crypto/x509`) — no third-party dependency. This app holds **no
trust-root or signer registry of its own** — the operator's own words: the marketplace ("another website
people can use to share their agents") issues certificates and does the trust management; "the app only
contains the public version of it." So import verification in this slice proves exactly one thing: **the
card was not altered after it was signed** (the Ed25519 signature matches the embedded certificate's public
key over the card's own bytes) — it does **not** judge whether the signer is who they claim, and does
**not** refuse an unfamiliar signer. The UI shows the verification result (verified / tampered / unsigned)
and the signer's claimed identity from the certificate; a human decides whether that is good enough. A
future slice can add a trust-root list or revocation — explicitly out of scope here.

**Unsigned cards — locked, 2026-10-30.** A card with no signature at all refuses import **by default**,
behind a config setting that flips this to "import, marked `unsigned`" when the operator wants
hand-written/local agents importable without going through the marketplace. The setting lives wherever this
project's other per-component settings live (`VERIFY` the real config surface in T0 — likely the same shape
`component`'s existing settings columns use, e.g. `queue_advance`/`sizing_mode` in migration `0041`, or a
daemon-level config value if this is meant to apply app-wide rather than per component) — name it something
like `agents.allowUnsignedImport`, default `false`.

**Versioning is import-time idempotency, keyed by `(slug, version)`.** Importing the same `(slug, version)`
twice is a no-op (not an error — re-running an import script must be safe). Importing a higher version for
an existing slug updates that row in place (never a second row — a card is one evolving thing, unlike S35's
proposals, which are deliberately staged; there is no staging here because importing an agent card is not
widening a live guard the way policy/MCP/agent-transition writes are — P34 doesn't apply the same way, since
nothing about IMPORTING a card changes what a running loop is allowed to do until something actually uses
that agent). Importing a **lower** version than what is already stored is refused, named, never silently
accepted or silently ignored.

## AM4 — Deletion is refused when something still points at the slug

An agent cannot be deleted while any blueprint **or any policy row** references its slug — locked
2026-10-30, both block deletion, not blueprints alone. This is an application-layer check, not a real
foreign key for the blueprint half, because blueprints are not rows with a formal FK into an agent table
today (they are files/stored text) — `VERIFY` exactly how blueprints are persisted and searched
(`internal/blueprint`, `internal/store`'s blueprint-related tables if any) before assuming a simple `LIKE`
scan is sufficient; a lane's `Agent` field inside a structured document may need a real parse, not a text
match, to avoid a false negative on a slug that happens to be a substring of another. The policy half likely
**can** be a real check against `internal/policy`'s own store (`PolicyListAll` filtered by target), since
that is already a real table, not text. The same check extends to anything else AM2's migration made
slug-addressable — any stored `agent_chain` edge, any `task` row with an `assignee` naming the agent.

## AM5 — The settings surface, over the socket, never file-only (P34)

List, create, duplicate, edit (every new field; `internal = true` rows refuse this write, named), delete
(guarded by AM4), and **view an agent's content** (its prompt/skills/card, read-only) all go through new
daemon methods, following every other slice's pattern (T2-era validator/registry seam, or a plain CRUD
service — `VERIFY` against the current method-table conventions in `internal/protocol` before choosing).
This is the daemon's primary interface, per P34 — a capability reachable only from a CLI verb is not
accepted as finished, same rule S34 onward has followed throughout. The UI lives in Settings (the operator's
own words), not the bench — this is the first slice whose UI surface is the **real app**, not a bench tab;
say so explicitly in `brief.md`'s scope, since every bench-page convention this project built (S34-S41) does
not apply here.

## Resolved 2026-10-30, after the design review

All six of the brief's original open questions are now locked, folded into AM1–AM4 above: policy rows block
deletion alongside blueprints (AM4); slug collisions refuse rather than suffix (AM2); blueprint YAML takes
a raw slug, no name resolution (AM2); signatures are Ed25519 over an X.509 certificate, stdlib-only (AM3);
an unsigned card refuses by default, with a config override to import it marked `unsigned` (AM3).

## Open items this design still leaves to T0 — mechanical, not product calls

1. Exact new table shape and name (`agent`, columns, FK/join to `plugin_agent`) — T1's job, grounded in
   T0's investigation of this project's own existing PK/table conventions.
2. Where exactly the daemon-method seam lives (new package vs. extending an existing one) — T0/T7's call,
   grounded in the current `internal/protocol` method-table convention.
3. Where the `agents.allowUnsignedImport` config setting lives (per-component, like `queue_advance`, or
   daemon-level) — T0/T5's call, grounded in how this project's other settings are scoped.
