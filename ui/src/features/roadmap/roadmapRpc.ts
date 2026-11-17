/**
 * roadmapRpc — the ONE file through which the roadmap management surface
 * speaks (S50 T7).
 *
 * Every method literal the surface uses stands here spelled exactly once, so
 * the guard (`roadmapRpc.guard.test.ts`) has one surface to scan besides the
 * components that call these wrappers, and the contract itself settles
 * whether a name is registered at all. Same discipline as `automationRpc.ts`
 * (S49), `gatesRpc.ts` (S57) and `environmentsRpc.ts` (S59).
 *
 * Params are assembled here from data the caller already holds. The actor is
 * never among them: the plane takes the person from the connection's peer,
 * and an identity key in params is refused by the daemon before it looks at
 * anything else (the same reflection guard the Go tests carry).
 *
 * Several param types are widened by a documented assertion, and only here:
 * the generated contract is fixture-derived — the board fixture answered
 * empty arrays (params `Record<string, never>`), the history fixture always
 * sent a limit, the update fixture only carried title/status, the ideate and
 * materialize fixtures always sent blueprint/tech, and the proposals fixture
 * never carried the status filter. Each widening is `omitempty` on the wire
 * (measured against the Go structs `RoadmapBoardParams`,
 * `RoadmapHistoryParams`, `RoadmapUpdateParams`, `RoadmapIdeateParams`,
 * `RoadmapMaterializeParams`, `proposalListParams`) — never an invented
 * field. The BLUEPRINT PICKER is S49's component and asks `blueprint.list`
 * through S49's file; this folder therefore spells no blueprint literal and
 * the guard's allow-list equals exactly what lives here.
 */

import { rpcTyped } from "../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../lib/live";
import type { RpcMethods } from "../../proto/contract";
import {
  boardParams,
  boardShape,
  healthShape,
  historyShape,
  type BoardResult,
  type FilterDraft,
  type HealthResult,
  type HistoryRow,
  type ShowResult,
  type UpdateParams,
  type UpdateResult,
  type Vocabulary,
} from "./roadmapModel";

type BoardParams = RpcMethods["roadmap.board"]["params"];
type HistoryParams = RpcMethods["roadmap.history"]["params"];
type UpdateWireParams = RpcMethods["roadmap.update"]["params"];
type IdeateParams = RpcMethods["roadmap.ideate"]["params"];
type MaterializeParams = RpcMethods["roadmap.materialize"]["params"];
type ProposalListParams = RpcMethods["proposal.list"]["params"];

// ── reads ───────────────────────────────────────────────────────────────────

/** The board — the daemon's filters as params, never a client sweep (RM6).
 *  The answer is read through `boardShape` (the documented widening: the
 *  fixture was empty, so the generated row type is `Array<unknown>`). */
export function useRoadmapBoard(
  filters: FilterDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<BoardResult> {
  const query = useRpcQuery("roadmap.board", boardParams(filters) as unknown as BoardParams, controller, live);
  return { data: query.data === undefined ? undefined : boardShape(query.data), loading: query.loading, error: query.error, refetch: query.refetch };
}

/** One item in full plus the daemon's describe sentence — the same two
 *  bytes the board row quoted, from the same formatter (one definition). */
export function useRoadmapShow(
  id: number,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<ShowResult> {
  return useRpcQuery("roadmap.show", { id }, controller, live);
}

/** The item's audit trail, newest first as the daemon answers it. The
 *  contract demands a limit (the one fixture sent one); the wire omits it
 *  and the daemon applies its own default. */
export function useRoadmapHistory(
  id: number,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<HistoryRow[]> {
  const query = useRpcQuery("roadmap.history", { id } as unknown as HistoryParams, controller, live);
  return { data: query.data === undefined ? undefined : historyShape(query.data), loading: query.loading, error: query.error, refetch: query.refetch };
}

/** The vocabulary: statuses and their edges, the three priority words,
 *  the provenances, the estimate and path rules, the byte limits, the
 *  path-editability window and the authority statement. Loaded once per
 *  page; every dial on this surface reads from it. */
export function useRoadmapVocabulary(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<Vocabulary> {
  return useRpcQuery("roadmap.vocabulary", {}, controller, live);
}

/** `roadmap.health` — the two blueprint settings named, each ok, missing,
 *  invalid or unverifiable. A read, never a gate (RM6): the panel shows it
 *  and dispatch re-checks at its own moment. */
export function useRoadmapHealth(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<HealthResult> {
  const query = useRpcQuery("roadmap.health", {}, controller, live);
  return { data: query.data === undefined ? undefined : healthShape(query.data), loading: query.loading, error: query.error, refetch: query.refetch };
}

/** One declared setting's view (S44's door): the effective value, where it
 *  came from, and the invalid states the View carries. */
export function useSettingView(
  key: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["settings.get"]["result"]> {
  return useRpcQuery("settings.get", { key }, controller, live);
}

/** The shared desk's listing, narrowed by its OWN filter (status) — the
 *  door has no kind filter, so the kind read is client-side narrowing of
 *  the daemon's answer and `roadmapModel.roadmapIdeaProposals` says so. */
export function useProposedIdeas(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<unknown> {
  return useRpcQuery("proposal.list", { status: "proposed" } as unknown as ProposalListParams, controller, live);
}

// ── writes ──────────────────────────────────────────────────────────────────

/** `roadmap.update` — only touched fields travel; the daemon answers the
 *  item, the changed flag, or a refusal sentence this surface renders
 *  verbatim (RM7). Path fields the vocabulary excludes are never even
 *  editable — and the daemon still holds the line. */
export const updateRoadmapItem = (params: UpdateParams): Promise<UpdateResult> =>
  // Widening, documented: the contract's result carries the full item
  // struct; `UpdateResult` is the same answer read by this folder — changed
  // plus the item — with the optional fields the fixture never filled.
  rpcTyped("roadmap.update", params as unknown as UpdateWireParams).then(
    (answer) => answer as unknown as UpdateResult,
  );

/** A dispatch that answers a refusal names why (RM6, T3): the caller
 *  renders `toRpcError(…).message` verbatim. Declining a confirm below
 *  sends NOTHING — none of these runs on its own. */
export const ideateRoadmapItem = (id: number, workspaceRoot: string) =>
  rpcTyped("roadmap.ideate", { id, workspaceRoot } as unknown as IdeateParams);

export const planRoadmapItem = (id: number, workspaceRoot: string) =>
  rpcTyped("roadmap.plan", { id, workspaceRoot });

/** Materialize is the item's own `loop.start`: the blueprint the person
 *  picks and the goal they name travel; `tech` is omitempty and this form
 *  does not carry one. */
export const materializeRoadmapItem = (id: number, blueprint: string, goal: string, workspaceRoot: string) =>
  rpcTyped("roadmap.materialize", { id, blueprint, goal, workspaceRoot } as unknown as MaterializeParams);

/** Save one blueprint setting through the ONLY settings door (S44): the
 *  daemon answers the new View or a refusal, both verbatim material. */
export const saveRoadmapSetting = (key: string, value: string) =>
  rpcTyped("settings.set", { key, value });

/** Proposals read and decide through S45's generic doors. The digest
 *  comes from `proposal.get` — the digest the person READ is the digest
 *  the decision CARRIES (S45's PM7 discipline; a mismatch is the daemon's
 *  "changed since you read it", shown verbatim). Reject REQUIRES a reason.
 *
 *  This folder is the PL15 ban's second NAMED caller (2026-11-04, S50 T7):
 *  `test/policy.test.tsx`'s proposalDoor predicate exempts
 *  `features/roadmap/` by name for the two decision doors — the strip here
 *  commits and rejects only `roadmap_idea` rows, and its own guard
 *  (`roadmapRpc.guard.test.ts`) pins the panel to that story. A named
 *  exemption, not a textual dodge: the plain literals below are what the
 *  folder guard's census counts, byte for byte. */
export const readProposal = (id: number) => rpcTyped("proposal.get", { id });

export const commitRoadmapIdea = (id: number, digest: string) =>
  rpcTyped("proposal.commit", { id, digest });

export const rejectRoadmapIdea = (id: number, reason: string) =>
  rpcTyped("proposal.reject", { id, reason });
