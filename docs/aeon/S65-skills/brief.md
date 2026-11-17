# S65 — Agent Skills

## The problem

The operator already keeps Agent Skills (the SKILL.md open standard from
agentskills.io) at `~/.agents/skills` — seven real ones today
(brainstorming, caveman, find-skills, frontend-design,
microsoft-foundry, skill-creator, vercel-react-best-practices), the same
folder Codex reads at USER scope. Aeon had no way to SEE them, let alone
install one: "skill" meant only the A2A card's `skills` array
(`internal/agentsvc`) and the queue's sizing skills
(`internal/queue_sizing`). The user asked for skills — "compatible with
most skills out there" — read from the user folder plus an app folder, a
CLI that installs from GitHub or a local folder into the app, and a view
in the test bench that shows the capabilities validated. Not chat
injection: the capability and its health, first.

## The rulings

| | |
| --- | --- |
| **SK1** | Two roots, user-first: `~/.agents/skills` (READ-ONLY by construction — Aeon never writes or deletes there) and the managed folder (`$XDG_DATA_HOME/aeon/skills`, overridable by `AEON_SKILLS_DIR`; the user root by `AEON_USER_SKILLS_DIR`). A name in both roots: the user copy wins, the managed copy stays LISTED with `shadowed` + `shadowedBy`. |
| **SK2** | Disk is truth. No table, no migration: the catalog is a cached scan, `skills.validate` forces a re-read. (This also keeps the parallel-worktree migration ledger untouched.) |
| **SK3** | Every discovered folder is listed, never silently dropped. A broken SKILL.md is `invalid` WITH its issues — broken is visible. |
| **SK4** | Five socket methods — `skills.list · get · validate · install · remove` — registered CONDITIONALLY (V13: nil seam = absent). None is Debug. None is on the narrow surface: the whole family, reads included, stays off the allow-list (`cmd/aeon/skills_guard_test.go`). |
| **SK5** | Install runs in the DAEMON: GitHub locator or local folder; shallow clone with a 60 s budget (git CLI is already a project dependency); staging outside the destination; validation against the FINAL folder name BEFORE placement; overwrite refuses without `force`; a refused install leaves nothing behind. Nothing in a skill is ever executed — scripts are listed, never run. |
| **SK6** | No chat injection, no tools, no prompts: `tools.Registry()` stays the pinned ten. Discovery + validation + management only. |
| **SK7** | One Go package, `internal/skills` — its docstring pins the three-meanings distinction (this one ≠ agentsvc's A2A card skills ≠ queue_sizing's sizing skills). |

## The shape

```
~/.agents/skills/<name>/SKILL.md          user root — discovered, never written
<$XDG_DATA_HOME|~/.local/share>/aeon/skills/<name>/SKILL.md   managed — installs land here
        │
        ▼  Discover(roots, caps, clock)          internal/skills
        ├─ splitFrontmatter / ParseFrontmatter   lenient YAML: unknown keys → Unknown, never a rejection
        ├─ ValidateSkill                         spec rules as Issues (errors fold to invalid, warnings to warn)
        └─ manifest                              scripts/references/assets/other counts + bytes, capped walks
              │
              ▼  Service — ONE per daemon (P34), cached scan, install/remove serialize under one mutex
   table.Skills ──▶ skills.list · get · validate · install · remove   internal/protocol/skills.go
        │                       │
        │                       └─ conformance/skills/ — seven fixtures
        ▼
cmd/aeon skills …            ui SkillsPage + lib/skills.ts     (both thin clients of the same doors)
```

## What "compatible with most skills out there" pins

* Required `name` (1–64, `[a-z0-9-]`, no lead/trail/double hyphen, **must
  equal the folder**) and `description` (1–1024); optional `license`,
  `compatibility` (≤500), `metadata` (string map), `allowed-tools` (kept,
  NOT enforced — warned).
* Unknown keys survive as `Unknown` and display: a Claude-Code
  `allowed-tools`/`hooks`-style field set or a `metadata.author/version`
  never invalidates a skill.
* Only TOP-LEVEL folders are skills; a nested `SKILL.md` (the
  microsoft-foundry shape) is a resource of the parent, counted in
  `references`.
* Dot/underscore entries are skipped; a folder-level symlink is followed
  (a symlinked folder IS a skill); missing roots are an empty answer, not
  an error.
* The whole corpus at `/home/cjlapao/.agents/skills` is the validation
  fixture (internal/skills' `TestDiscoverRealShapes`).
