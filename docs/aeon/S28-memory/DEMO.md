# Demo — S28 Memory

**Predictive.** Written before the slice is built, per `AGENTS.md` §4a. T8 walks this for real — the
learnings-core steps (1–6) fully; the board-question steps (7–8) once T7 has landed — **no longer gated on
S14, which is built** (2026-10-26 check) — otherwise left predictive and clearly labeled as such.

## Fixture

A project with one real S13 body name available (e.g. `golang-engineer-loop`), one `plugin_agent` row in
`enabled` state, and one in `revoked` state (to exercise §3's agent-scope validation both ways). `aeon
serve` on a throwaway `XDG_STATE_HOME`.

## Steps

**1. `aeon learnings add --scope project --scope-key <root> --body "this team wraps errors with a custom
type" --provenance told`.** **Predicted**: succeeds, appears in `aeon learnings list`.

**2. `aeon learnings add --scope agent --scope-key golang-engineer-loop --body "forgets to check nil maps"
--provenance observed --observed-count 3`.** **Predicted**: succeeds — `handler.Lookup` finds the name.

**3. `aeon learnings add --scope agent --scope-key <the revoked plugin_agent's name> --body "x" --provenance
assumed`.** **Predicted**: refused, naming the reason — revoked, not merely unregistered.

**4. `aeon learnings add --provenance relayed ...`.** **Predicted**: refused at both CLI and socket layers
— write-protected to T7's board-answer hook alone (`design.md` M1).

**5. `aeon learnings list --tag auth` on a store with no embedding route configured.** **Predicted**: tag
filtering works, returns correctly filtered results — D5 demonstrated directly, not just asserted in a test.

**6. Configure an embedding route and repeat step 5's query with similarity ranking requested.**
**Predicted**: the same tag-filtered set, now ordered by similarity — never a result the tag filter alone
would have excluded (D6).

**7. [If T7 is built] Ask a board question**: one agent's step invokes the other's `answer` skill through
S14's mechanism. **Predicted**: the invocation is refused if it names any skill other than `answer`; on
completion, exactly one new learning appears, `scope: project`, `provenance: relayed` (M6).

**8. [If T7 is built] Attempt a board question naming no target agent.** **Predicted**: refused before any
model call, named reason (D8).

## What this demo does not show

**Steps 7–8 are not walkable until T7 is built**, and, once it is, require T6's card-issuance to have run
first (a board question against an agent with no signed card refuses immediately, a different, earlier
failure than anything these two steps intend to exercise). If this walk happens before T7 lands, say so plainly in
the §Findings table rather than silently omitting the steps. **The UI half** is untested if this checkout's
Tauri libraries are unavailable, the standing headless-blocked note. **Embedding quality** — step 6
demonstrates the *mechanism* (ranking within the filtered set), not whether the ranking is actually good;
that is a provider/model question outside this slice's own claims.
