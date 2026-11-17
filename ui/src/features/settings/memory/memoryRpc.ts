/**
 * memoryRpc — the ONE file through which the Memory management surface
 * speaks (S52 T6, LM10/P34).
 *
 * Every method literal the surface uses stands here spelled exactly once,
 * so the guard (`memoryRpc.guard.test.ts`) has one surface to scan besides
 * the components that call these wrappers, and the contract itself settles
 * whether a name is registered at all. Same discipline as `triageRpc.ts`
 * (S51), `roadmapRpc.ts` (S50) and `automationRpc.ts` (S49).
 *
 * The actor is never among the params: the plane takes the person from the
 * connection's peer (P26), and an identity key in params is refused by the
 * daemon before it looks at anything else. The agent whose memory
 * `forAgent` answers for is NOT an actor claim — it is the subject of a
 * read, named in the open, exactly as `learnings.forAgent` asks for it.
 *
 * The retraction verbs carry NO confirmation field: the confirm is the
 * click (the drawer quotes `retractLaw` and the row first, LM9 — the
 * CLI's twin asks for `--yes`), and the daemon stamps the peer. The
 * proposals for `learning_retract` are decided through S45's desk, whose
 * doors (`proposal.*`) live in the desk's own file — this folder spells no
 * proposal literal, and the reused `AgentPicker` (S43's component) spells
 * only `agents.list`, which the guard reads from its own file.
 */

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import {
  memoryBoardParams,
  type ForAgentHit,
  type MemoryRow,
  type MemoryFilterDraft,
} from "./memoryModel";

// ── reads ───────────────────────────────────────────────────────────────────

/** The board — the daemon's filters as params, never a client sweep (LM6).
 *  The board's generated params type is fixture-narrower than the wire
 *  (the T2 fixture sent one filter); the composed params are asserted at
 *  this boundary — the ONLY such point in the folder — and the wire
 *  decodes all seven. */
export function useLearningsBoard(
  draft: MemoryFilterDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<MemoryRow[]> {
  const params = memoryBoardParams(draft) as RpcMethods["learnings.board"]["params"];
  const query = useRpcQuery("learnings.board", params, controller, live);
  return {
    data: query.data === undefined ? undefined : rowsOf(query.data),
    loading: query.loading,
    error: query.error,
    refetch: query.refetch,
  };
}

/** `learnings.board` answers `{ learnings: [...] }`; the hook hands the
 *  rows up. A response without the list is an empty board, never a guess —
 *  and the emptiness is a SHARED constant, not a fresh literal. The board
 *  rows ride a `useEffect` dependency (the mirror up to the OPEN drawer),
 *  and a NEW `[]` identity on every render re-fires that effect forever:
 *  measured at `test/product-shell.test.tsx`, where the fake daemon's
 *  generic `{}` fallback for an unregistered method livelocked the whole
 *  worker — a CPU-pinned suite that never even reaches its test timeouts,
 *  because timers cannot fire while microtasks never settle. A stable
 *  empty makes the malformed answer one quiet render, which is the honest
 *  shape anyway. */
const NO_ROWS: MemoryRow[] = [];
const NO_HITS: ForAgentHit[] = [];

function rowsOf(data: unknown): MemoryRow[] {
  if (typeof data === "object" && data !== null && Array.isArray((data as { learnings?: unknown }).learnings)) {
    return (data as { learnings: MemoryRow[] }).learnings;
  }
  return NO_ROWS;
}

/** One learning in full — the S28 shape plus the retraction stamp when the
 *  row carries one (the widened read in `memoryModel.ts` says where). */
export function useLearningsShow(
  id: number,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["learnings.show"]["result"]> {
  return useRpcQuery("learnings.show", { id }, controller, live);
}

/** The memory's audit career, as the daemon ordered it. An unknown id is
 *  the daemon's refusal, shown verbatim — never a silence here. */
export function useLearningsHistory(
  id: number,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["learnings.history"]["result"]> {
  return useRpcQuery("learnings.history", { id } as RpcMethods["learnings.history"]["params"], controller, live);
}

/** The knowledge map — the same filters as the board, run by the same daemon function, so the map and
 *  the board are two views of one set. Positions come from the stored embedding vectors (a deterministic
 *  2-D projection) and the links are nearest neighbours by cosine similarity in the full space; a learning
 *  with no vector is counted by the answer, never positioned by a guess. The generated params type is
 *  fixture-narrower than the wire (the fixture sent one filter); the composed params are asserted here. */
export function useLearningsMap(
  draft: MemoryFilterDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["learnings.map"]["result"]> {
  const params = memoryBoardParams(draft) as RpcMethods["learnings.map"]["params"];
  return useRpcQuery("learnings.map", params, controller, live);
}

/** The vocabulary: the three closed sets, the limits, the M1 note, and the
 *  ONE retract law the confirms quote. Loaded once per section. */
export function useLearningsVocabulary(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["learnings.vocabulary"]["result"]> {
  return useRpcQuery("learnings.vocabulary", {}, controller, live);
}

/** "What would this agent see?" — the readable set with the WHY column,
 *  byte-equal to what the agent itself would be shown (LM5's fidelity is
 *  the daemon's differential test; this panel only asks). A slug that is
 *  no agent answers its own refusal, verbatim. */
export function useLearningsForAgent(
  agent: string,
  query: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<ForAgentHit[]> {
  // The generated params type is fixture-narrower than the wire (the T2
  // fixture sent no query); the composed params are asserted at this
  // boundary — the wire decodes the query and the limit the Go handler
  // declares.
  const params = (query === "" ? { agent } : { agent, query }) as RpcMethods["learnings.forAgent"]["params"];
  const state = useRpcQuery("learnings.forAgent", params, controller, live);
  return {
    data: state.data === undefined ? undefined : hitsOf(state.data),
    loading: state.loading,
    error: state.error,
    refetch: state.refetch,
  };
}

function hitsOf(data: unknown): ForAgentHit[] {
  if (typeof data === "object" && data !== null && Array.isArray((data as { hits?: unknown }).hits)) {
    return (data as { hits: ForAgentHit[] }).hits;
  }
  return NO_HITS;
}

/** The loop view route lives by id, while a learning remembers only its
 *  source loop's SLUG; this read resolves the one to the other for the
 *  row's source-loop link. It is the panel's single foreign read — the
 *  guard names it in the allow-list — and it indexes loops, it never
 *  filters memory (an unresolved slug renders as a plain word, never a
 *  guessed route). */
export function useLoopSlugIndex(
  controller: LiveController,
  live: LiveState,
): Record<string, string> {
  const query = useRpcQuery("loop.list", { all: true } as RpcMethods["loop.list"]["params"], controller, live);
  const index: Record<string, string> = {};
  const data = query.data as { loops?: Array<{ id: string; slug: string }> } | undefined;
  for (const loop of data?.loops ?? []) index[loop.slug] = loop.id;
  return index;
}

// ── writes ──────────────────────────────────────────────────────────────────

/** `learnings.retract` — the stamp, never a delete (LM2). The reason rides
 *  the wire exactly as typed; its length rule (1..256 bytes) is the
 *  plane's, enforced underneath, and a refusal comes back verbatim. The
 *  answer carries the row and the honest `changed` flag: a replayed
 *  retraction answers `changed:false` and writes nothing, audit included
 *  — the panel says so plainly rather than re-printing a stamp. */
export const retractLearning = (id: number, reason: string) =>
  rpcTyped("learnings.retract", { id, reason });

/** `learnings.restore` — the way back, the same human door. */
export const restoreLearning = (id: number) => rpcTyped("learnings.restore", { id });
