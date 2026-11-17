# §S73 · The agent CRUD page — walked end to end, 2026-11-06

The slice's own demo file, because `DEMO.md` is on the deferred-docs-debt list (4,530 lines) and the debt rule sends
new content to per-slice files. The pointer stands in `DEMO.md` itself.

**The setup, in one paragraph.** A real daemon (`aeon serve`) on a throwaway `XDG_STATE_HOME`, seed sets applied —
**34 agents** in the store, no fixtures. The built UI (`ui/dist`) served by the S72 harness bridge, whose `rpc_call`
forwards to the daemon's real socket; the Tauri shell stubbed at the same seam. System Chrome, headless, driven by
`aeon/scripts/qa-s73-walk/walk.mjs`; the CLI in the walk's own steps runs the same `aeon` binary against the same
store. 44 steps, `rc=0`; every line below is a quoted observation from `.s73-scratch/walk-run/walk.log`, not a
restatement. Findings it raised: **G-1…G-6** in [S73-walk-findings.md](../S73-walk-findings.md).

1. **First paint answers in the daemon's numbers.** `/config/agents`: `Showing 1 to 20 of 34 results`, `Page 1 of 2`,
   the first slugs `["brainstormer","brief-author","chat"]`. Seven columns, table 1,070 px inside a 1,280 px window,
   `document.scrollWidth 1280` — the table scrolls its own frame, the page never slides sideways. Every row on the
   first screen is stamped first-party or yours.
2. **Sorting, paging, searching are all one question to the daemon.** First press of `Updated` → `aria-sort=ascending`
   (G-5 names the expectation question); page 2 renders `Page 2 of 2`; a term nothing matches shows the daemon's own
   sentence, not a locally sliced zero; and `%` — the LIKE metacharacter — comes back **0 rows, literal**, which is
   PG3 seen at the only place PG3 can be seen.
3. **The create names its own child.** *New agent* → the panel answers `Created walk-probe — "Walk Probe"`, the slug
   the daemon coined. The copy is where the first-party rule shows its shape: the first-party row offers no Edit and
   does offer **Duplicate**, and the copy answers `Created walk-copy — "Walk Copy", a copy of the row above`.
4. **The nine-field hazard, walked on the real store — this is the step the slice was for.** `walk-copy`'s prompt is
   **3,569 characters**, rendered in a textarea. The walk edits the **description alone**, saves, reloads the whole
   window, reopens the row: **3569/3569 characters of prompt, metadata unchanged**, description changed. AR5 is not
   a unit test's claim anymore; it is a thing that was done, to a real row, and survived.
5. **Read-only means read-only, with the daemon's reason.** The same row's other fifteen facts render as facts, above
   the sentence naming why: *`agent.edit` writes nine content fields and re-reads these from the row…* — the words
   come from `agentmgr`, not from a UI-catalog copy.
6. **Export wrote what it says; then the page's two panels disagreed — G-3.** Export answered with the path; the file
   is 11 keys, prompt 3,569. Handing that same file to the import panel, with the fixed harness (G-2) to show the
   envelope, refused: `refused: the card envelope has no card body`. The export format is not the import format —
   named for the agent plane, walked on a file the daemon itself wrote.
7. **The seatbelt, both ways — AM3's three words, all three heard.** A fresh-slug envelope with the setting closed:
   `refused: walk-imported is unsigned, and unsigned imports are refused by default — set agents.allowUnsignedImport
   to admit it marked unsigned`. With it open, the same file answers `word: created`, the same file again answers
   `word: no-op (same version already stored)`, and the card at version 99 answers `word: updated`. Then the setting
   went back closed, walked either way.
8. **Delete makes a person type the name.** The `alertdialog` names the slug and the consequence; typing `walk-victi`
   leaves the confirm button dead and nothing reaches the daemon; typing it right answers `Deleted — the daemon
   removed walk-victim, and the table above has been asked again`, and the re-read leaves **0 cells**.
9. **AM4, verbatim, with the blocker named and removable.** `aeon policy add --target agent:walk-copy` laid a real
   reference; the delete then refused exactly as the store words it — `refused: delete of walk-copy — 1 policy row(s)
   still reference it (s73-walk-ref); remove or re-point them first (AM4)` — printed whole, row still on screen; the
   walk removed the policy and moved on. This is acceptance item 9 with the reference list, on a reference the walk
   itself made.
10. **The ledger is readable, scoped and unfiltered, and it names people.** History on `walk-copy`:
    `["edit walk-copy (9 content fields)","duplicate brainstormer as walk-copy"]`, both stamped `user:3002`. The
    unfiltered toggle shows twenty newest-first across every row the walk touched. The `proposal <id>` chip rendered
    **0** times on production rows — G-6, because no road in this tree mints a proposal-bearing summary.
11. **Migrate is honest about doing nothing — and still signs its name.** The confirm says the run will be recorded
    as one entry naming no single agent; the run answers `Derived agent rows: 0 — … nothing was overwritten`; the
    unfiltered ledger carries `migrate derived 0 rows from plugin_agent` with a **NULL slug** (AR1) stamped
    `user:3002` (AR6: the person who pressed it, not `system`). The `aeon agent import` CLI on the walk's card files
    refused for the honest reason — they are cards, not packages (no `agent.md`, no signed `card.json`).
12. **The old bench is still the bench.** `/bench/agent-management` still renders the raw verb panels inside the shell;
    its three tests ride the 55-test folder run.
13. **The daemon-down mode is its own walk.** Kill the daemon, reload: the shell's `tab-needs-connection` state, and
    the action bar **absent entirely** — `agents-create`/`agents-import`/`agents-migrate` are not disabled controls,
    they do not exist. Stronger than the brief asked, and it is what the second pass (`rc=0`) showed.

**What this walk does NOT show.** The Tauri window itself (the S49/S58/S59 named gap — no webkit here; the shell's
Rust half was walked separately at S72 T5). The `agent_import` **proposal** road through the UI: deviation 15
measured that it writes the plugin plane and mints no agent ledger row, so there is nothing for History to show —
the walk proved that truth instead of the planned step's premise (its Notes say so). The consent text a CLI package
import prints — it does not cross this socket, and the panel says it does not. And the page-size control the History
tab does not have (G-4): recorded, not hidden.

**Reproduce:** `bash aeon/scripts/qa-s73-walk/run.sh` (needs `ui/dist` built and the `aeon` binary; prints `STEP …
:: observed` lines, exits 0 only with zero FAIL). Logs: `aeon/.s73-scratch/walk-run/`.
