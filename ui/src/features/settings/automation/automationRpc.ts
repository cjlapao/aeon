// automationRpc.ts — S49 T9: the one file through which the Automation
// panel speaks.
//
// Every method literal the panel uses stands here spelled exactly once, so
// the guard (`test/automationRpc.guard.test.ts`) has one surface to scan
// besides the components that call these wrappers, and the contract itself
// settles whether a name is registered at all. Same discipline as
// `environmentsRpc.ts` (S59) and `gatesRpc.ts` (S57).
//
// Params are assembled here from data the caller already holds. The actor is
// never among them: the plane takes the person from the connection's peer,
// and an identity key in params is refused by the daemon before it looks at
// anything else. A webhook secret has no param to arrive under either —
// `secretEnv` is the NAME of the daemon's own environment variable (C17);
// the wire type carries no field a value could travel in, and this file
// never composes one.
//
// Several param types are widened by a documented assertion, and only there:
// the generated contract is fixture-derived, and no S49 fixture exercised
// the unified list's filters or the schedule write's window/secret fields, so
// the generated type omits fields the wire accepts (`omitempty` on each in
// `AutomationListParams` / `TriggerAddParams` / `TriggerUpdateParams`).

import { useCallback, useEffect, useRef, useState } from "react";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import { listParams, type AutomationList, type FilterDraft, type FirePage, type Vocabulary } from "./automationModel";

type ListParams = RpcMethods["automation.list"]["params"];
type TriggerAddParams = RpcMethods["trigger.add"]["params"];
type TriggerUpdateParams = RpcMethods["trigger.update"]["params"];
type TriggerEventCreateParams = RpcMethods["automation.trigger.create"]["params"];
type TriggerEventUpdateParams = RpcMethods["automation.trigger.update"]["params"];

/** The list read — the daemon's own filters as params, never a client sweep. */
export function useAutomations(
  filters: FilterDraft,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<AutomationList> {
  const params = listParams(filters) as unknown as ListParams;
  return useRpcQuery("automation.list", params, controller, live);
}

/** One automation in full: description sentence, canonical shape, health,
 *  recent fires, changes — every sentence here was written by the daemon. */
export function useAutomationShow(
  source: string,
  id: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["automation.show"]["result"]> {
  return useRpcQuery("automation.show", { source, id }, controller, live);
}

/** The plane-wide health answer, joined to the list by source:id. */
export function useAutomationHealth(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["automation.health"]["result"]> {
  return useRpcQuery("automation.health", {}, controller, live);
}

/** The vocabulary: zones, events and their applicable keys, limits, the
 *  authority statement and the five outcome words. Loaded once per section. */
export function useAutomationVocabulary(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<Vocabulary> {
  return useRpcQuery("automation.vocabulary", {}, controller, live);
}

/** The fire log — newest first, the daemon's five outcomes, a cursor page. */
export function useAutomationFires(
  filters: { source: string; outcome: string; limit: number; page: FirePage },
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["automation.fires"]["result"]> {
  const params = {
    limit: filters.limit,
    ...(filters.source !== "" ? { source: filters.source } : {}),
    ...(filters.outcome !== "" ? { outcome: filters.outcome } : {}),
    ...(filters.page.beforeId !== "" ? { beforeId: filters.page.beforeId } : {}),
  } as Record<string, unknown> as RpcMethods["automation.fires"]["params"];
  return useRpcQuery("automation.fires", params, controller, live);
}

/** The loops one automation started (drawer, linking onward to the loop). */
export function useAutomationRuns(
  source: string,
  id: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["automation.runs"]["result"]> {
  return useRpcQuery("automation.runs", { source, id }, controller, live);
}

/** The audit for one automation (the Audit tab's per-trigger listing). */
export function useAutomationChanges(
  source: string,
  id: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["automation.changes"]["result"]> {
  return useRpcQuery("automation.changes", { source, id }, controller, live);
}

/** The S38 components list — the event form's scope picker (S49 rides the
 *  read; S38 T10 shipped the door). */
export function useComponentOptions(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["component.list"]["result"]> {
  return useRpcQuery("component.list", {}, controller, live);
}

/** The S48 list — the forms' blueprint picker consumes the list METHOD, not
 *  S48's components (the picker shows each option's health when known). */
export function useBlueprintOptions(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["blueprint.list"]["result"]> {
  return useRpcQuery("blueprint.list", {}, controller, live);
}

/** The automation kinds of the proposal queue (decision doors live inside
 *  the reused ProposalCard, S45's one implementation). */
export function useAutomationProposals(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["proposal.list"]["result"]> {
  return useRpcQuery("proposal.list", {}, controller, live);
}

// ── live preview (debounced `automation.preview`) ─────────────────────────
//
// The form NEVER parses cron. It sends the draft's spec to the daemon and
// renders its answer verbatim — instants with their DST/window notes, or the
// daemon's refusal text under the field as the person types. Debounce is a
// courtesy to the socket, not a correctness device.

const PREVIEW_MS = 400;

export interface PreviewState {
  data: RpcMethods["automation.preview"]["result"] | undefined;
  /** The daemon's refusal, verbatim — shown under the field, never reworded. */
  error: string | undefined;
  pending: boolean;
}

export function useCronPreview(expr: string, zone: string): PreviewState {
  const [data, setData] = useState<RpcMethods["automation.preview"]["result"] | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [pending, setPending] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const seq = useRef(0);

  useEffect(() => {
    if (timer.current !== undefined) clearTimeout(timer.current);
    if (expr.trim() === "" || zone === "") {
      setData(undefined);
      setError(undefined);
      setPending(false);
      return;
    }
    setPending(true);
    const mine = (seq.current += 1);
    timer.current = setTimeout(() => {
      rpcTyped("automation.preview", { n: 3, spec: { expr, zone } } as unknown as RpcMethods["automation.preview"]["params"])
        .then((res) => {
          if (mine !== seq.current) return;
          setData(res);
          setError(undefined);
          setPending(false);
        })
        .catch((err: unknown) => {
          if (mine !== seq.current) return;
          setData(undefined);
          setError(err instanceof Error ? err.message : String(err));
          setPending(false);
        });
    }, PREVIEW_MS);
    return () => {
      if (timer.current !== undefined) clearTimeout(timer.current);
    };
  }, [expr, zone]);

  return { data, error, pending };
}

/** `automation.test` — the sample tester. One shot per click: the answer is
 *  the daemon's per-key ✓/✗ lines, not a client-side match. */
export function runMatchTest(
  match: Record<string, unknown>,
  sample: Record<string, unknown>,
): Promise<RpcMethods["automation.test"]["result"]> {
  return rpcTyped("automation.test", { match, sample } as unknown as RpcMethods["automation.test"]["params"]);
}

/** `automation.dryrun` — "would have matched in the last N events". The
 *  scanned/from window is the daemon's; the UI shows its numbers. */
export function runDryMatch(id: string): Promise<RpcMethods["automation.dryrun"]["result"]> {
  return rpcTyped("automation.dryrun", { id });
}

// ── the write doors ────────────────────────────────────────────────────────

/** schedule/webhook create — kind is the family the button belongs to. */
export function addScheduleTrigger(params: {
  kind: "schedule" | "webhook";
  id: string;
  blueprint: string;
  expr: string;
  zone: string;
  workspace: string;
  goal: string;
  windowStart: string;
  windowEnd: string;
  windowDays: string;
  secretEnv: string;
  eventFilter?: string;
}): Promise<RpcMethods["trigger.add"]["result"]> {
  const wire: Record<string, unknown> = {
    kind: params.kind,
    id: params.id,
    blueprint: params.blueprint,
    expr: params.expr,
    zone: params.zone,
    workspace: params.workspace,
    goal: params.goal,
  };
  if (params.windowStart !== "") wire.windowStart = params.windowStart;
  if (params.windowEnd !== "") wire.windowEnd = params.windowEnd;
  if (params.windowDays !== "") wire.windowDays = params.windowDays;
  if (params.secretEnv !== "") wire.secretEnv = params.secretEnv;
  if (params.eventFilter) wire.eventFilter = params.eventFilter;
  return rpcTyped("trigger.add", wire as unknown as TriggerAddParams);
}

/** schedule/webhook edit — only fields the person touched travel. */
export function updateScheduleTrigger(params: {
  id: string;
  blueprint?: string;
  cron?: string;
  timezone?: string;
  workspace?: string;
  goal?: string;
  windowStart?: string;
  windowEnd?: string;
  windowDays?: string;
  secretEnv?: string;
  /** Sent even when empty, so a filter can be cleared. */
  eventFilter?: string;
  clearWindow?: boolean;
}): Promise<RpcMethods["trigger.update"]["result"]> {
  const wire: Record<string, unknown> = { id: params.id };
  for (const key of ["blueprint", "cron", "timezone", "workspace", "goal", "windowStart", "windowEnd", "windowDays", "secretEnv"] as const) {
    const value = params[key];
    if (value !== undefined && value !== "") wire[key] = value;
  }
  if (params.eventFilter !== undefined) wire.eventFilter = params.eventFilter;
  if (params.clearWindow === true) wire.clearWindow = true;
  return rpcTyped("trigger.update", wire as unknown as TriggerUpdateParams);
}

export const pauseSchedule = (id: string) => rpcTyped("trigger.pause", { id });
export const resumeSchedule = (id: string) => rpcTyped("trigger.resume", { id });
export const fireNowSchedule = (id: string) => rpcTyped("trigger.fireNow", { id });

/** event create — the match object the MatchBuilder narrowed. */
export function createEventTrigger(params: {
  id: string;
  name: string;
  blueprint: string;
  match: Record<string, unknown>;
  kickoff: string;
  componentId: string;
  maxRunsPerHour: number | undefined;
}): Promise<RpcMethods["automation.trigger.create"]["result"]> {
  const wire: Record<string, unknown> = {
    id: params.id,
    name: params.name,
    blueprint: params.blueprint,
    match: params.match,
    maxRunsPerHour: params.maxRunsPerHour ?? 6,
  };
  if (params.kickoff !== "") wire.kickoff = params.kickoff;
  if (params.componentId !== "") wire.componentId = params.componentId;
  return rpcTyped("automation.trigger.create", wire as unknown as TriggerEventCreateParams);
}

/** event edit — AN9: the door itself only narrows; the form only offers
 *  narrower choices, and the daemon re-checks both claims. */
export function updateEventTrigger(params: {
  id: string;
  name: string;
  blueprint: string;
  match: Record<string, unknown>;
  kickoff: string;
  maxRunsPerHour: number | undefined;
}): Promise<RpcMethods["automation.trigger.update"]["result"]> {
  const wire: Record<string, unknown> = {
    id: params.id,
    name: params.name,
    blueprint: params.blueprint,
    match: params.match,
    maxRunsPerHour: params.maxRunsPerHour ?? 6,
  };
  if (params.kickoff !== "") wire.kickoff = params.kickoff;
  return rpcTyped("automation.trigger.update", wire as unknown as TriggerEventUpdateParams);
}

export const enableEvent = (id: string) => rpcTyped("automation.trigger.enable", { id });
export const disableEvent = (id: string, reason: string) => rpcTyped("automation.trigger.disable", { id, reason });

/** archive is NEVER delete (AN6): the row stays, the engine stops seeing it,
 *  and unarchive leaves it disarmed — the person re-arms deliberately. */
export const archiveAutomation = (source: string, id: string, reason: string) =>
  rpcTyped("automation.archive", { source, id, reason });
export const unarchiveAutomation = (source: string, id: string) =>
  rpcTyped("automation.unarchive", { source, id });

/** A stable callback over `useRpcAction`-style busy state is the caller's
 *  business (`useRpcAction` owns it); these are plain promises. */
export function useAsyncAction() {
  const [busy, setBusy] = useState(false);
  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
    setBusy(true);
    try {
      return await fn();
    } finally {
      setBusy(false);
    }
  }, []);
  return { busy, run };
}
