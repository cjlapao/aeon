# Design — S52 Memory management

Rulings **LM1–LM11**; patterns from [`MANAGEMENT-PATTERNS.md`](../MANAGEMENT-PATTERNS.md). T0 re-verifies.

## LM1 — Agent scope is a slug
Migration (P2): every `learning` with `scope='agent'` has `scope_key` resolved `agent.name → agent.slug`; the **whole** migration refuses if any value resolves to nothing (naming all), skips values already equal to a slug, never guesses; `person`/`project` untouched. `validateScope` (the one Go door) checks the **agent row exists** (any `origin`, not archived/revoked per S43's own state fields — T0 records them) instead of `handler.Lookup`/`plugin_agent`. The agent-delete refusal (S43 AM4) gains "N learnings are scoped to this agent" via one accessor (S52's own adaptation, listed; S43's tests unedited).

## LM2 — Retraction, not deletion
`ALTER TABLE learning ADD COLUMN retracted_at TEXT, ADD retracted_reason TEXT NOT NULL DEFAULT '', ADD retracted_by TEXT NOT NULL DEFAULT ''`. `learnings.retract {id, reason}` (human door; reason 1–256; idempotent `changed:false`), `learnings.restore {id}`. Default readers (`List`, `Query`, `Show` for lists, the board feed, prompt retrieval) **exclude retracted**; `Show` by id still returns it flagged; `includeRetracted` is explicit. The retrieval index: T0 records how learnings are indexed (S22 corpus or `learning_vec`); retraction removes/marks the chunk so a retracted learning can never be *retrieved by similarity* either — tested by querying for its exact body.

## LM3 — `learning_change` audit (P3)
`learning_change(id, learning_id, op ∈ ('add','retract','restore'), summary, actor_kind ∈ ('user','cli','agent','system'), actor_id, occurred_at)`; append-only; CHECK `actor_kind='agent' ⇒ op='add'` (agents add, never retract/restore); `system` for the board-answer hook and triage feed (`relayed` adds). Fixed-template summaries (`"added observed learning %d (scope agent)"` — **never the body**).

## LM4 — The write door gets an actor, M3 keeps its shape
`learnings.add` takes `table.ActorFor`: a **person/cli** actor may add `told|observed|assumed`; an **agent** actor only `observed|assumed` (an agent cannot claim "a person said it"); `relayed` is refused on every public door (M1, unchanged) and written only by the two internal hooks (actor `system`). Params carry no actor/provenance-of-author fields beyond the existing `provenance`. No review gate (M3), no new rate limit on the socket (agent *tools* have one, LM7).

## LM5 — `forAgent`: the question memory management exists to answer
`learnings.forAgent {agent, query?, limit?}` (read): resolves the agent's scopes (person; the projects it has run in; its own agent scope) and returns hits through **the same `Query` function** the prompt-retrieval path calls, each with `provenance`, `observedCount`, the scope it came from, and a **`why`** (rank, the M1 ordering note, the match basis). A differential test pins equality with the real retrieval call. Read-only; no embedding cost beyond what `query` already incurs (T0 notes the cost behaviour; if `query` embeds on every call, `forAgent` without `query` uses the non-embedding path).

## LM6 — Reads
`learnings.board {scope?, provenance?, tag?, agent?, project?, loop?, includeRetracted?}` (rows with `Describe`, tags, source loop link, retraction state), `learnings.history {id}`, `learnings.vocabulary {}` (scopes, **four provenances in honesty order** from `Provenances()`, tag grammar, limits). Existing `list/show/query` unchanged (compat). `Describe` renders provenance as its own word and `observed (×N)` strength; `relayed` always labelled as a channel fact, never "verified".

## LM7 — Agent tools (P6)
Specs (family `memory`): `learning_query`, `learning_show` (safe; scoped to what the agent may read: person + its projects + its own scope — never another agent's agent-scoped memory), **`learning_add`** (Mutates; the M3 self-log: provenance ∈ `observed|assumed`, schema `{scope, scopeKey?, body, tags[], provenance}` with the agent's own scope key **stamped**, a per-agent rate limit named constant, ≤ 2 KiB body), **`learning_propose_retract`** (Mutates; proposal kind `learning_retract` `{id, reason}`; `Validate` = the learning exists, is not retracted, and is **in the agent's readable scope**; `Apply` = `learnings.retract` as the committing person). Counted bump **+4**, not granted by default. No tool retracts, restores, edits or writes `told`/`relayed`.

## LM8 — Methods and doors
Human-door writes (P4): `learnings.retract`, `learnings.restore`. `learnings.add` stays open to persons **and** agents (M3) — guarded by LM4's per-actor provenance rule, not by the human door. Reads LM5/LM6. All classified in S47's table; the agent-scoped twins (`learnings.mine.*`) refuse person sessions with a pointer sentence.

## LM9 — CLI (P5)
`aeon learnings add|list|show|query` unchanged (goldens) plus `retract ID --reason`, `restore ID`, `board`, `history ID`, `for-agent AGENT [--query]`, `vocabulary`. `add --provenance told` as a person only. `retract` needs `--reason` and `--yes`.

## LM10 — UI (P7)
Settings → Memory: board by scope/provenance/tag/agent with the four provenance pills distinct; detail drawer (body, tags, source loop link, retraction state, history); **"What would this agent see?"** (`AgentPicker` + optional query ⇒ `forAgent` with the why column); retract with reason (confirm shows the body excerpt and who it affects, from the daemon); restore; proposals via S45's desk for `learning_retract`. The bench page stays.

## LM11 — What does not move
M1–M6, the board-answer skill, the triage feed, S22's pipeline and costs, learning bodies (append-only), and every sibling file beyond the listed adaptations (agent-delete refusal; reading `retracted_at` in the retrieval filter).
