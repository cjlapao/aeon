# S65 design — Agent Skills

## Why a new package, and why this name

"Skill" already means two things here: the A2A card's `skills` array
(`internal/agentsvc`) and the queue's sizing skills
(`internal/queue_sizing`). This slice adds the third — the Agent Skills
open standard (agentskills.io: a folder whose `SKILL.md` carries YAML
frontmatter + markdown instructions, plus optional `scripts/`,
`references/`, `assets/`). `internal/skills`' package doc names all
three so the next reader does not have to find out the hard way. The
package imports nothing from those two; nothing imports the word loosely.

## The parser is lenient BY DESIGN

Codex reads the same user folder and tolerates extra fields; Claude Code
ships skills whose frontmatter carries extension fields
(`allowed-tools`, `metadata`, …). A strict parser would invalidate real
skills, which is the opposite of "compatible with most skills out of
there". So:

* unknown keys land in `Unknown` (displayed, never fatal);
* numeric `metadata` values are stringified rather than refused;
* a list-form `allowed-tools` is joined rather than rejected;
* BOM and CRLF are tolerated; a missing/unclosed frontmatter block is
  ONE issue sentence, and the folder still lists as `invalid`.

The validator then judges only what the spec binds: the required
`name` (1–64, `[a-z0-9-]`, no lead/trail/double hyphen — and equal to
the FOLDER, which is the identity the invocation follows), the required
`description` (≤1024), `compatibility` (≤500). Everything else is a
warning at most: thin description, empty body, a body past 500 lines,
`allowed-tools` present-but-unenforced (SK6: this project has no tool
filter to enforce it with), metadata shape notes. The fold is the
policy pattern: any error ⇒ `invalid`, else any warning ⇒ `warn`.

## Roots, shadows, and the read-only promise

`Discover` walks root-ordered entries; the first root holding a name
wins and the loser stays in the list with `Shadowed`/`ShadowedBy`.
Sorting puts winners first, then name — the listing reads as "what the
agents will have", with the duplicates beneath, visible.

The user root is READ-ONLY by construction, and the construction is the
enforcement: every write path (`Install`, `Remove`) reaches only
`managedRoot`, the service exposes no user-root write at all, and the
tests assert a user-root file survives even when a same-named managed
removal is refused (the refusal answers, the user file is untouched —
never collateral).

Disk is truth (SK2): no table, no migration, no `schema.go` touch. The
service caches one scan; `validate` forces a fresh one; installs and
removals invalidate. Three-state honesty at the root level: a missing
root is an empty section with the path named, not an error and not
"nothing exists anywhere".

## Install: stage, validate, name, THEN place

The installer answers three questions in order, and refuses with
sentences before it touches the destination:

1. **What is the source?** A local folder if the path exists (an
   absolute path wins), else a GitHub locator — `owner/repo`,
   `owner/repo/sub@ref`, a full `https://github.com/…` (with `/tree/ref/…`),
   or `git@github.com:owner/repo.git`. Two ref markers refuse
   ("names a ref twice"); an empty marker (`…/tree/`) refuses; a
   `--ref` beside a locator-ref refuses. The grammar table is pinned in
   `TestGitHubGrammar` (11 shapes).
2. **Which folder is the skill?** `--subpath`, else the root `SKILL.md`,
   else a `skills/` dir's single entry, else a top-level single entry.
   Ambiguity refuses and LISTES the candidates rather than guessing.
3. **What is its name?** The frontmatter `name` — validated against the
   FOLDER it will land in, before placement (a checkout named
   `my-pdf-thing` holding `name: pdf` installs as `pdf`; a `name` that
   breaks a spec rule never lands). Then stage inside the managed root
   (`.staging-*`, symlink/non-regular files refused, caps enforced,
   modes preserved including the +x on scripts), and `rename` it into
   place. Overwrite refuses without `force`; a forced replace renames
   the old folder to `.trash-<name>-<ns>` rather than deleting.

The daemon-side default clone is `git clone --depth 1 --quiet
[--branch ref]` under a 60 s budget — git CLI is already a project
dependency; the budget is `CloneBudget`, named. Tests inject `Clone`;
one test (`TestRealGitCloneAgainstLocalOrigin`) stands up a real bare
origin and clones it for real, and another proves a shipped
`scripts/mark.sh` is NEVER EXECUTED by the installer — its exec bit
rides along, nothing runs it (SK5).

## The five doors

`internal/protocol/skills.go` marshals and refuses; the service decides.
`list` filters by status/source/query AFTER the whole scan (the summary
is the catalog's truth, the page is the filter's answer, and `limit`
applies after filtering so the page never depends on disk order); `get`
takes a name or a path (a shadowed copy's only real address is its
path); `validate` IS the freshness door (forced scan — no separate
`refresh`, two doors for one act would be two truths); `install`/`remove`
are the two human writes.

DB19 on the wire: an *unavailable* catalog is `ErrSkillsUnavailable`
(-31821) — not `ErrStoreUnavailable`, whose sentence talks about opening
a store; the substitution was caught on the live walk, when a git-clone
fault printed store-open advice beside the clone's own error (T7
findings). The installer's own refusal is `ErrSkillsRefused` (-31820) and
carries the service's sentence verbatim in `what` — the operator fixes
the FILE, so they read the file's fault. A miss (`ErrNotFound`) is
`ErrInvalidScope` naming the thing asked for and pointing at
`skills.list`. V13: nil seam ⇒ `RegisterSkills` registers nothing.

The narrow surface: none of the five is in `Actions()`, none appears in
`internal/narrow` or `internal/agentsvc` text — pinned by
`cmd/aeon/skills_guard_test.go` with its can-fail meta-test. Install on
the agent surface would let an agent plant the instructions the next
agent reads; even the reads stay off, because the allow-list is the
single decision about what is projected (S44 CS9's precedent).

## The app half

`ui/src/lib/skills.ts` mirrors the wire types verbatim; `SkillsPage`
(list at order 4500, `needsConnection: true`, registry-registered) is
the test bench's view: three statuses as three renderings with their
issues beneath, both roots named with their counts, install form
(locator/ref/subpath/force) that sends exactly what was typed, and a
detail pane that shows the body — truncation saying it was cut AND
that the disk holds the whole file. The user root renders without a
remove affordance, provably (button-scanned in
`test/bench-skills.test.tsx`). No chat, no injection, no secrets
(§4c — nothing here is credential-shaped).
