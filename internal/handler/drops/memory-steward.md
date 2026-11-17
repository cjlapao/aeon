# memory-steward — drop record

original: opencode/agents/memory-steward-loop.md  (93 lines)
rewrite:  aeon/internal/handler/bodies/memory-steward.md  (63 lines)
delta:    30 lines removed, 0 added

**THE CHANGE OF SHAPE, STATED PLAINLY (spec requirement):** this handler's
entire job was `memory_append`. Aeon's registry has no memory tool — the
harvest is now an ordinary file written through `write` (with `edit` for the
strengthen case) at the path the blueprint declares with `produces:`. S4's
memory table and S22's index are not tools a handler calls in this slice.
The judgement — triage, phrasing, de-duplication, when NOT to record — is
what survived, below; the mechanism rows say what replaced it.

## RITUAL (deleted — the engine does this)
- L35-L37  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L39-L41  "Load the dev-loop-protocol skill first"         → nothing loads skills here; the instructions are the body or nowhere (D2)
- L45      step 0: `pwd` + `loopSlug` + "the one long-lived file you write is Learnings.md" → the address goes with produces:; "one long-lived file, read before you add" survives (Working method 3)
- L46      step 1: loop_get/memory.md/qa-report/unresolved-pr-comments reads → the raw material arrives in the step's request and the reports it names (the WHY-context reads survive there); memory.md itself is the DEFERRED surface below
- L56      step 7: scratch harvest-audit note via `loop_writeFile` → the handoff counts (added/strengthened/rejected) carry the audit headline; the full triage trail was loop plumbing
- L57      step 8: `event_append` return checkpoint         → the daemon emits step events; a body cannot write its own timeline row
- L58      step 9: FINAL REPORT line (half)                 → the evidence triple survives verbatim as the lessons_recorded handoff field (REWORDED)
- L78      "append lessons via `memory_append`... never hand-write memory.md (the MCP renders it)" → the tool it addressed does not exist; see the change-of-shape note

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, glob, grep, todo]`: task DROPPED (A1, never renamed), todowrite→todo (A19), codesearch/lsp/patch retired (W1), webfetch not re-granted (nothing in the body uses it); the harvest file is written through `write`, strengthened rows through `edit`, near-duplicates found with `grep` — the toolset replaces the append mechanism
- L22-L32  permission.bash deny table (git/rm rules)        → the corpus carries these byte-identically; bash itself is absent from the allow-list — a harvester needs no shell
- L81      "git-loop commits Learnings.md at git-feature"   → durable commits are the blueprint's; the body keeps "never run git writes"

## DOMAIN (kept)
- L47      read the existing file IN FULL first — you must know what is already recorded before adding anything → Working method 3
- L48-L52  the IRON TRIAGE verbatim, all three tests (Durable 3-months / Non-obvious / Actionable) with its examples, and "most lessons FAIL this bar — correct and expected. A loop that produces zero lessons is a normal outcome; report it honestly" → Working method 4
- L53      the extraction format verbatim: Category list, root cause 1 sentence, GENERAL RULE 1 imperative sentence "Do X"/"Never Y", Ref (loop slug + PR/issue #); "if you cannot state the rule in one imperative sentence, it is not ready — drop it" → Working method 5
- L54      de-duplication verbatim: grep 2–3 distinctive keywords; exact → skip-and-note; partial → STRENGTHEN the existing row (tighten wording, update date/Ref); genuinely new → append → Working method 6
- L55      write rules verbatim in substance: one row per rule never two in a row; Top-Level Patterns promotion only for project-wide rules with the ≤10-bullets-and-retire-one law; match existing heading format exactly; never rewrite existing entries except strengthen → Working method 7
- L60-L74  the Learnings.md template verbatim (title, Agent Instruction line, both section headings, the table columns) — first-creation-only → the template section
- L58      cannot-write → blocked; "never paste lessons into chat — a deliverable in chat is lost" → Working method 8 + Common mistakes
- L79      rules-not-history: one sentence imperative, "a paragraph is a triage failure" → Boundaries
- L80      never delete existing lessons — strengthen or leave alone → Boundaries
- L82      one harvest per dispatch → Boundaries
- L84-L93  the Common mistakes table verbatim in substance: history-not-rule, raw-material-vs-rule, paragraph-per-lesson, near-duplicate-row, done-only-in-chat → blocked, padding-to-look-useful with "zero lessons is a valid, honest result"

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L46/L78  the loop-memory MCP surface: `memory_append` rows, the rendered memory.md ("never hand-write memory.md — the MCP renders it"), `loop_get`'s lessons feed → re-read against S12 per ROADMAP §7: MCP tooling is the deferred capability — an operator who CONFIGURES such a server gets its rendering; the body instructs none of it. S4's memory table and S22's planned index are NOT handler-callable tools in this slice (spec ruling): the harvest is a file, and `produces:` declares where it lands — the shape-change note above is the plain statement the spec demanded, not a rewrite pretending the handler is unchanged

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3/L37   the description/guard framing compressed to the blueprint
- L45      "Learnings.md" as an addressed file → the Learnings file at the path the step's request names; the NAME/shape (template below) stays, the address goes with produces:
- L53      Ref "(loop slug + PR/issue #)" kept — loop-level provenance is still the citation form a reader follows
- L58      `evidence="Learnings.md: {N} added, {M} strengthened, {K} rejected by triage"` restated as the lessons_recorded handoff field, counts and all
- L89      "Copying memory.md entries verbatim" restated as raw lesson entries (the source no longer has the MCP's rendered filename)
