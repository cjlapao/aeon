# S65 DEMO — Agent Skills

Walked 2026-10-31 on a live socket; the step-by-step record with real
output lives in [tasks/T7.md](tasks/T7.md). This is the iterable shape.

## The walk

1. Start the daemon with `XDG_STATE_HOME`/`XDG_DATA_HOME` at a throwaway
   directory (never the real store) and `AEON_SKILLS_DIR` pointing into
   it. The startup log line answers before you ask:
   `skills: 7 valid, 0 with warnings, 0 invalid, 0 shadowed across
   /home/<you>/.agents/skills and <managed>` — the operator's real
   `~/.agents/skills` corpus, discovered at boot, boot never blocked.
2. `aeon skills list` — every folder under both roots, one row each:
   status marker, name, `user`/`mgmt`, first line of the description;
   warnings and errors printed beneath their skill. Broken is VISIBLE,
   never dropped.
3. `aeon skills get caveman` — frontmatter fields that exist, the
   manifest counts (`scripts= references= assets= other=`), the whole
   SKILL.md body; a cut body says it was cut and that the disk holds
   the rest.
4. `aeon skills install <local folder>` — a good folder lands under its
   frontmatter name; a broken one refuses in three parts quoting the
   validator's own sentences and lands NOTHING. Re-running without
   `--force` refuses naming the existing path; `--force` replaces (the
   old copy goes to a `.trash-*` sibling, never straight to unlink).
5. `aeon skills install anthropics/skills` refuses AMBIGUITY and lists
   the 19 candidates; `…/skills/pdf` installs; the full
   `…/tree/main/skills/frontend-design` URL installs — and the next
   `list` shows the managed `frontend-design` row saying `shadowed by
   /home/<you>/.agents/skills/frontend-design (the other root's copy of
   this name wins)`. The user root always wins and its files are
   NEVER touched: `remove caveman` refuses naming the user path
   (file `ls`-verified present afterwards).
6. `aeon skills get --path <managed>/frontend-design` reads the SHADOWED
   copy by its only address — a genuinely different skill sharing the
   name.
7. `aeon skills remove <managed name>` deletes only under the managed
   root. `aeon skills validate` re-reads the disk and says when.
8. A bad install SOURCE (missing GitHub repo) answers `-31821 the
   skills catalog could not be read or fetched` carrying git's own
   sentence — a FAULT, distinctly not the `-31820` refusal; the
   difference is what tells the operator whether to fix the file or the
   network.
9. The app: Test bench → **Agent Skills**. Same five doors through the
   generic bridge; filters (status/root/search), "validate (re-read
   disk)", install form, expand-to-body, remove only on managed rows.

## What it does NOT do

* Nothing in a skill is executed — scripts are counted and listed, and
  a test proves a shipped `marker.sh` never runs during install. No
  chat injection and no tool registry change: skills are SEEN here, not
  handed to any agent (SK6; that is a future slice's decision).
* No DB, no history: disk truth, one cached scan per daemon.
* Not verified this session: the Tauri window itself (jsdom-only UI
  checks), Windows named-pipe transport, and any real FORGE beyond the
  one GitHub walk. The box-level ssh_config quirk that forces
  `GIT_SSH_COMMAND` is named in T7 findings — an operator hitting it
  sees the -31821 naming ssh's exact complaint.
