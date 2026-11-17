/**
 * continuousRpc — the ONE file through which Settings → Continuous speaks
 * (S27 T10, CN13/P7; the UI half of S27).
 *
 * Every scheduler method literal the surface uses stands here spelled
 * exactly once, so the guard (`continuousRpc.guard.test.ts`) has one file
 * to scan besides the components that call these wrappers, and the
 * generated contract settles whether a name is registered at all — the
 * same discipline as `systemRpc.ts` (S54), `triageRpc.ts` (S51) and
 * `memoryRpc.ts` (S52).
 *
 * The eleven doors are the whole surface: the four reads (status, preview,
 * decisions, attention), the master pair (enable, disable), the pause pair
 * (pause, resume), the row pair (`scheduler.root.set`,
 * `scheduler.root.disable`), and `scheduler.stopAll`. Two doors are NOT
 * here on purpose: `scheduler.enableTree` is the CLI's bulk act (the panel
 * enables repositories one at a time, CN6's note matters enough to be a
 * typed `--yes` on a terminal), and no `settings.*` door appears — the
 * scheduler's settings are S44-declared values this panel READS inside
 * `scheduler.status` and does not edit (CN18: the panel steers the
 * scheduler, not its configuration).
 *
 * The actor is never among the params: these doors take the person from
 * the connection's peer (P26/S27 CN2), and the contract's optional
 * `actor` field on `scheduler.enable` is a direct-arm shape this surface
 * never fills. The pause and stop-all doors REQUIRE a reason (CN18) — the
 * modal collects the person's own words and sends them verbatim; nothing
 * here invents a default reason.
 */

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import { DECISIONS_LIMIT, type SchedulerStatus, type RootSetInput } from "./continuousModel";

// ── reads ──────────────────────────────────────────────────────────────────

/** The whole view: master, pause + its reason, settings with source words,
 *  slots, next tick, the repository rows with the daemon's state sentence,
 *  the attention count, and the statement `enable` confirms with. */
export function useSchedulerStatus(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<SchedulerStatus | undefined> {
  return useRpcQuery("scheduler.status", {}, controller, live);
}

/** The tick, run — CN15's `would start` list and every line that was not
 *  started with the daemon's reason (and instant, where the reason has
 *  one). Asking starts nothing; zero writes. */
export function useSchedulerPreview(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["scheduler.preview"]["result"]> {
  return useRpcQuery("scheduler.preview", {}, controller, live);
}

/** The decision ledger, newest first, over the panel's window. The door
 *  filters by repository; an empty `componentRoot` asks for all. */
export function useSchedulerDecisions(
  componentRoot: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["scheduler.decisions"]["result"]> {
  return useRpcQuery("scheduler.decisions", { componentRoot, limit: DECISIONS_LIMIT }, controller, live);
}

/** The open needs-you lines — kinds verbatim, notes (S55's raise
 *  instruction) verbatim. */
export function useSchedulerAttention(
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["scheduler.attention"]["result"]> {
  return useRpcQuery("scheduler.attention", {}, controller, live);
}

// ── writes (each preceded by its modal; a refusal rides verbatim) ──────────

/** `scheduler.enable` — the master switch. The statement the confirm
 *  quotes rode `scheduler.status`; the daemon answers `{enabled}`. */
export const enableMaster = () => rpcTyped("scheduler.enable", {});

/** `scheduler.disable` — the master off. Narrowing needs no statement:
 *  the click is the decision, and the daemon answers `{enabled}`. */
export const disableMaster = () => rpcTyped("scheduler.disable", {});

/** `scheduler.pause` — CN18: pause STOPS PICKING, running loops keep
 *  running; the answer's `runningLoops` sentence says exactly that and
 *  the panel prints it. The reason is the person's own words, required. */
export const pauseMaster = (reason: string) => rpcTyped("scheduler.pause", { reason });

/** `scheduler.resume` — the pause off; no reason needed (nothing is
 *  being granted that was not granted before). */
export const resumeMaster = () => rpcTyped("scheduler.resume", {});

/** `scheduler.stopAll` — pause AND stop the running loops; the confirm
 *  shows the daemon's own live numbers first (slots in use, the
 *  repositories it would stop) and the reason rides verbatim. Absent
 *  plane, absent panel: without the scheduler plane the status read
 *  already failed with the daemon's method-not-found, so this control is
 *  never rendered against a daemon that lacks it (T7 registers the twelve
 *  together). */
export const stopAllScheduler = (reason: string) => rpcTyped("scheduler.stopAll", { reason });

/** `scheduler.root.set` — the template form's submit (create-or-update).
 *  An invalid blueprint comes back as the daemon's sentence, verbatim,
 *  with the draft kept (behaviour 2). */
export const setSchedulerRoot = (input: RootSetInput) => rpcTyped("scheduler.root.set", input);

/** `scheduler.root.disable` — the row off. Narrowing again: the click is
 *  the decision. */
export const disableSchedulerRoot = (componentRoot: string) =>
  rpcTyped("scheduler.root.disable", { componentRoot });

export type { RpcMethods };
