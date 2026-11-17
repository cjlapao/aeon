# Drop records — what was removed from each body, and where the cover went

One record per handler, beside the rewrite. The record exists because 2,902
lines of prose were deleted from 26 originals and a reader is entitled to
know **what each removed line did** and **what covers that job now** — a
deletion nobody can attribute is a capability nobody reviewed out.

This is A14's executable form: the review is the record, and the guard
(`aeon handlers verify` + `internal/handler`'s tests) checks the **format**,
not the prose's taste.

## The format

```markdown
# golang-engineer — drop record

original: opencode/agents/golang-engineer-loop.md  (84 lines)
rewrite:  internal/handler/bodies/golang-engineer.md  (31 lines)
delta:    53 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L8-L12   caller guard checking who dispatched   → the blueprint dispatches
...

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L57      "exit 137 is an OOM kill, not a lint finding" → bash reports oomKilled (W5)
...

## DOMAIN (kept)
- L24-L40  Go idiom, error wrapping, table-test shape
...

## DEFERRED (kept OUT — the capability is not here yet)
- L30      context7 doc resolution                → deferred; an MCP client is what would carry this

## REWORDED (A17's ledger — meaning preserved, words changed)
- L24 shortened; meaning unchanged
```

**All five section headers are mandatory, even when empty** — an empty
section is a claim ("this handler deferred nothing"), and the absence of a
section is how someone skips a category entirely. `BECAME MECHANISM` is the
one people get wrong: *"exit 137 is an OOM kill"* reads exactly like domain
knowledge and is now the bash tool's `oomKilled` reporting (W5); recording
it as mechanism is what keeps the next person from re-adding the prose.
`DEFERRED` is mandatory because **24 of 26 originals reference MCP** — that
content is not ritual, not mechanism, and not keepable domain, because the
tool does not exist; the section names what a future client is for (A18).

**Every entry carries line references** (`L<n>` or `L<n>-L<m>`) into the
original. "Removed the ritual preamble" is unreviewable; a record that
cannot be diffed against the original did not satisfy A4 (O2).

**The arithmetic must close:** `original + added == rewrite + removed`,
counted from the files by the checker, not typed from memory. An unexplained
gap is an omission nobody reviewed (D6).

**`REWORDED` exists so A17 stays followable:** without it, every clarity
improvement looks like invented content and the zero-added-lines rule gets
ignored rather than obeyed (O3). REWORDED entries still line-reference.

**MCP obligation:** if the original contains the word `mcp` (case-insensitive;
measured: 24 of 26), the `DEFERRED` section must be **non-empty**.

## The special case: `git-loop.md`

`git-loop` gets **no handler at all** (A3) — its record is the whole
disposition of a 372-line original, and its sections are written for that
job: `BECOMES A CODE ACTION` (naming S16's actions), `BECAME MECHANISM`,
`ABSORBED BY ANOTHER HANDLER`, `RITUAL`, and an explicit `LOST` section —
present even when empty, because "nothing was lost" is exactly the claim a
record must be forced to make. (T9's file carries the variant; the checker
knows it by name.)
