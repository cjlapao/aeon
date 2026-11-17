/**
 * triageRpc — the ONE file through which the triage management surface
 * speaks (S51 T7, TM10/P34).
 *
 * Every method literal the surface uses stands here spelled exactly once, so
 * the guard (`triageRpc.guard.test.ts`) has one surface to scan besides the
 * components that call these wrappers, and the contract itself settles
 * whether a name is registered at all. Same discipline as `roadmapRpc.ts`
 * (S50), `automationRpc.ts` (S49) and `environmentsRpc.ts` (S59).
 *
 * Params are assembled here from data the caller already holds. The actor is
 * never among them: the plane takes the person from the connection's peer
 * (P26), and an identity key in params is refused by the daemon before it
 * looks at anything else. The ONE exception the wire itself demands is
 * `confirm-duplicate`'s `confirmedBy` — not an actor claim but the human
 * name D5's gate checks against the people the daemon knows; a name that is
 * not a person comes back as the daemon's refusal, verbatim.
 *
 * The blueprint PICKER is S49's component and asks `blueprint.list` through
 * S49's file; this folder therefore spells no blueprint literal, and the
 * guard's allow-list equals exactly what lives here.
 */

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import {
  boardParams,
  healthShape,
  type BoardRow,
  type FilterDraft,
  type HealthResult,
} from "./triageModel";

type BoardParams = RpcMethods["triage.board"]["params"];

// ── reads ───────────────────────────────────────────────────────────────────

/** The board — the daemon's filters as params, never a client sweep (TM10).
 *  The rows the fixture carried are fully typed, so no shape widening: the
 *  board is its own truth, candidates and scored distances included. */
export function useTriageBoard(
  filters: FilterDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<BoardRow[]> {
  const query = useRpcQuery("triage.board", boardParams(filters) as BoardParams, controller, live);
  return {
    data: query.data === undefined ? undefined : Array.isArray(query.data) ? query.data : [],
    loading: query.loading,
    error: query.error,
    refetch: query.refetch,
  };
}

/** One bug in full — the S29 shape every door answers with. */
export function useTriageShow(
  id: number,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["triage.show"]["result"]> {
  return useRpcQuery("triage.show", { id }, controller, live);
}

/** The bug's audit trail, newest first as the daemon answers it. The
 *  contract demands a limit (the fixture sent one); the wire omits it and
 *  the daemon applies its own default. */
export function useTriageHistory(
  id: number,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["triage.history"]["result"]> {
  return useRpcQuery("triage.history", { id } as unknown as RpcMethods["triage.history"]["params"], controller, live);
}

/** The vocabulary: both closed sets, the legal edges, and the two
 *  authority statements the confirms quote. Loaded once per section. */
export function useTriageVocabulary(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["triage.vocabulary"]["result"]> {
  return useRpcQuery("triage.vocabulary", {}, controller, live);
}

/** `triage.health` — the blueprint setting named, ok, missing, invalid or
 *  unverifiable. A read, never a gate (the daemon's own line): the card
 *  shows it and validation re-checks at its own moment. */
export function useTriageHealth(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<HealthResult> {
  const query = useRpcQuery("triage.health", {}, controller, live);
  return { data: query.data === undefined ? undefined : healthShape(query.data), loading: query.loading, error: query.error, refetch: query.refetch };
}

/** One declared setting's view (S44's door): the effective value and where
 *  it came from. */
export function useSettingView(
  key: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["settings.get"]["result"]> {
  return useRpcQuery("settings.get", { key }, controller, live);
}

// ── writes ──────────────────────────────────────────────────────────────────

/** `triage.update` — TM1's law made visible on the wire: only title and
 *  body can be sent, because only title and body exist in these params.
 *  The daemon answers the bug plus the honest `changed` flag, or a refusal
 *  this surface renders verbatim with the draft kept. */
export const updateTriageBug = (id: number, title: string, body: string) =>
  rpcTyped("triage.update", { id, title, body });

/** Validation and hand-off answer a refusal by naming why (the caller
 *  renders `toRpcError(…).message` verbatim). Declining a confirm below
 *  sends NOTHING — neither verb ever runs on its own (TM10/RM9's law). */
export const validateTriageBug = (id: number) => rpcTyped("triage.validate", { id });

export const handoffTriageBug = (id: number) => rpcTyped("triage.handoff", { id });

/** D5's human gate: the confirmation carries the NAME a person typed and
 *  the daemon checks it names an actual person — a refusal on that check
 *  is shown as written. */
export const confirmTriageDuplicate = (componentRoot: string, id: number, of: number, confirmedBy: string) =>
  rpcTyped("triage.confirm-duplicate", { componentRoot, id, of, confirmedBy });

/** Save the blueprint setting through the ONLY settings door (S44): the
 *  daemon answers the new View or a refusal, both verbatim material. */
export const saveTriageSetting = (key: string, value: string) =>
  rpcTyped("settings.set", { key, value });
