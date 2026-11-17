# S55 × S45 collision check — the forward S45→main merge

Recorded 2026-11-01. The S55→main merge itself is **clean** (main
`725748d0` = S55 `279116a8` merged `--no-ff`; S45 was not in main, so
`go build ./...` is green on merged main). The user's worry was the
*forward* direction — S45 (also in play, `f643b8c3`) eventually merging
into main, which now carries S55. This is the answer to that.

## Verdict

Only **one** real collision, and it is **trivial and additive**:
`aeon/internal/store/store.go`, the surface-table comment block.
`docs/aeon/SLICES.md` auto-merges clean (the S45 and S55 hunks are on
different lines — S45 @57/836/848 vs S55 @67/985).

Everything else auto-merges: the S55 migrations are new files (no
collision), `loop_control.go` / `budget.go` / `budget_spend.go` are S55's
own files, and S45's `policy*.go` are its own.

## How it was verified (non-destructive)

1. `git merge-tree --write-tree --name-only main s45-policy-management`
   reported the conflict in `store.go` only (`SLICES.md` auto-merged),
   exit 1.
2. A throwaway worktree was cut at `725748d0` (detached) and
   `git merge --no-ff --no-commit s45-policy-management` was run there.
   Exactly one conflict: `store.go`.
3. The conflict was resolved and the result **built and tested** with the
   cgo shim (`CGO_CFLAGS=-I…/.tmp/cgo-shim`, `XDG_STATE_HOME` a
   throwaway dir, `-tags sqlite_fts5`):
   - `go build -tags sqlite_fts5 ./internal/store/...` — **OK**.
   - `TestEveryExportedMethodIsNamedInTheSurfaceTable` — **PASS**
     (the guard that the surface-table text feeds; it reads the comment
     to confirm every exported store method is named).
   - `TestNoSQLLivesOutsideTheStore` — **FAIL**, but the failure is an
     **S45 defect, not a collision** (see below).
4. The throwaway worktree was removed; `main` and `s45-policy-management`
   are untouched. S45 is another agent's branch and was **not** merged.

## The collision, exactly

Both S45 and S55 insert their surface-table entries at the **same
anchor** — immediately after the `S44 config store` block, immediately
before the `// # No SQL outside this package (I2)` line. S45 inserts at
the line-561 position; S55 at line-565. Same anchor ⇒ git cannot
auto-merge ⇒ a content conflict in the comment block only.

The resolved block, in natural slice order (S44 < S45 < S55):

```
S44 config store      SettingGet · …        (pre-existing, unchanged)
S45 policy audit      PolicyChangeInsert · PolicyChangeInsertTx ·
                      PolicyChangeList · PolicyChangeCount   (S45's block)
S45 policy seed       PolicyAddTx · PolicyDeleteTx · PolicySeedRow · … (S45's)
S55 loop control      ParkLoopStopped · ResumeLoop · CancelLoop · … (S55's)
S55 budget           CreateBudget · … · SpendFor · SpendSeries  (S55's)
// # No SQL outside this package (I2)
```

Resolution = keep both blocks; S45 first, then S55. No line from either
side is dropped; the conflict is 100% additive.

## The one real merged-tree failure (NOT a collision)

`TestNoSQLLivesOutsideTheStore` fails in the merged tree on:

```
internal/desk/decide.go:33:
  // flip is `UPDATE … WHERE status='proposed'`: the loser's flip
  changes zero rows and refuses naming the winner. …
```

That comment is **only on S45** (`f643b8c3`); it is **absent on main**
(`725748d0`), where the same test **PASSES**. The guard greps
`internal/` and `cmd/` for SQL-like text and flags this comment. It is
an S45 authoring slip (a `UPDATE … WHERE` in a prose comment), not
anything S55 touches, and not a merge collision. It is the S45 agent's
to fix (reword the comment, or it is a false-positive the guard should
not match — either is S45's call). It does **not** block the S55
merge, which is already done and green.

## Consequence

The forward S45→main merge will produce exactly this one trivial
`store.go` conflict (plus, until S45 fixes it, the pre-existing
`TestNoSQLLivesOutsideTheStore` failure that has nothing to do with
S55). Resolving the `store.go` conflict is keep-both / additive; there
is no S55-side risk. S55 does not need to change anything to keep the
future S45 merge clean.
