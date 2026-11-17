// seedModel — the pure, testable half of the System records panel (S43-extra T8).
//
// No React, no RPC, no English chrome: every function here is unit-testable
// without a DOM, and the panel (SystemRecordsPanel.tsx) is descriptor-driven — it
// renders whatever `seed.status` / `seed.plan` return and names no set id, record
// key, state sentence or version comparison, so a newly declared set (or a state
// a later slice adds) appears with zero UI change (CS10, applied to seed). The
// panel localises the tokens these functions return; this file only decides
// *which* state is which and *what* is safe to do, never the wording.
//
// The one safety gate the whole panel is built around is `canApplyWithoutConfirm`:
// a guard set is never one-clicked. Everything else here is a read of the daemon's
// own vocabulary.

import type { RpcMethods } from "../../../proto/contract";
import type { TrueColor } from "@cjlapao/ui-kit";

/** One seed set, as `seed.status`'s element. */
export type SeedSetStatus = RpcMethods["seed.status"]["result"]["sets"][number];
/** One record of a set, as `seed.status`'s nested element. */
export type SeedRecordStatus = SeedSetStatus["records"][number];
/** `inert` | `guard` (SD5), as a string the daemon sends. */
export type SeedClass = SeedSetStatus["class"];

export type SeedPlanResult = RpcMethods["seed.plan"]["result"];
export type SeedPlan = SeedPlanResult["plan"];
export type SeedDecision = SeedPlan["decisions"][number];
export type SeedApplyResult = RpcMethods["seed.apply"]["result"];
export type SeedRestoreResult = RpcMethods["seed.restore"]["result"];

/**
 * The only place the inert/guard decision lives.
 *
 * True ONLY for an `inert` set — the one that may be applied on a single click
 * after the plan is shown. A `guard` set is never one-clicked: it always goes
 * through the confirmation. Because every apply path in the panel funnels
 * through this one gate, a guard set cannot be silently applied from anywhere.
 */
export function canApplyWithoutConfirm(set: SeedSetStatus): boolean {
  return set.class === "inert";
}

/**
 * One state, one distinct colour and one distinct label token.
 *
 * A record's `state` is the daemon's own word (absent / applied / removed /
 * diverged / retired / failed, or any a later slice adds). Each known state maps
 * to a DISTINCT colour and a DISTINCT `settings.seed.state.*` token, so two
 * states can never read as one (the "5 states ⇒ 5 labels" guard). A state the
 * panel has no token for falls back to a neutral badge that shows the raw state —
 * it is never blank and never conflated with a known one, so a new state still
 * reads distinctly instead of silently merging into another.
 */
export interface StateBadge {
  tone: TrueColor;
  /** The `settings.seed.state.*` suffix, or null when the state is unknown. */
  labelToken: string | null;
}

export function badgeFor(state: string): StateBadge {
  switch (state) {
    case "applied":
      return { tone: "emerald", labelToken: "applied" };
    case "behind":
      return { tone: "amber", labelToken: "behind" };
    case "absent":
      return { tone: "yellow", labelToken: "absent" };
    case "removed":
      return { tone: "rose", labelToken: "removed" };
    case "diverged":
      return { tone: "orange", labelToken: "diverged" };
    case "retired":
      return { tone: "slate", labelToken: "retired" };
    case "failed":
      return { tone: "red", labelToken: "failed" };
    default:
      return { tone: "neutral", labelToken: null };
  }
}

/**
 * The states `seed.restore` may re-apply — the ones a record can come back from.
 *
 * Named from the daemon's vocabulary, not from a UI rule: a record left the code
 * (retired), was changed (diverged) or was removed (removed). `applied` records
 * have nothing to restore, and `absent`/`failed` are not re-applied here. A state
 * a later slice adds simply is not restorable until this list says so — the safe
 * default is to offer nothing, never to guess a restore.
 */
export function isRestorable(state: string): boolean {
  return state === "removed" || state === "diverged" || state === "retired";
}

/**
 * A set's status, as structured facts the panel localises.
 *
 * `counts` tallies every record by its state (only the non-zero ones). `headline`
 * is the status line the panel shows: `upToDate` when every record is applied,
 * otherwise the most severe non-applied state the set actually carries. Nothing
 * here compares versions — the daemon already sent the states, so "behind" vs
 * "applied" is a read of what it said, never a client-side `available > applied`.
 */
export interface SeedSummary {
  total: number;
  /** Per-state counts, only the non-zero ones, in first-seen order. */
  counts: Record<string, number>;
  /** The status line's token: `upToDate`, or a state the set actually has. */
  headline: string;
  headlineCount: number;
}

// Most severe first: a failure outranks a divergence, which outranks a removal,
// … a retired row outranks an absent one. Only used to pick the one headline.
const PRECEDENCE = ["failed", "diverged", "removed", "retired", "behind", "absent"];

export function summarize(set: SeedSetStatus): SeedSummary {
  const counts: Record<string, number> = {};
  for (const record of set.records) {
    counts[record.state] = (counts[record.state] ?? 0) + 1;
  }
  const total = set.records.length;

  // "Up to date" only when the set is non-empty and every record is applied —
  // never collapsed from an empty set, which would read as a confident "fine".
  if (total > 0 && counts.applied === total) {
    return { total, counts, headline: "upToDate", headlineCount: total };
  }
  // Otherwise the most severe non-applied state the set actually carries.
  for (const state of PRECEDENCE) {
    if (counts[state]) {
      return { total, counts, headline: state, headlineCount: counts[state]! };
    }
  }
  // An empty set is honest about being empty, not "up to date".
  if (total === 0) {
    return { total, counts, headline: "upToDate", headlineCount: 0 };
  }
  // Only states the panel has no token for: name the first one, never blank.
  const first = set.records[0]?.state ?? "applied";
  return { total, counts, headline: first, headlineCount: total };
}

/**
 * A plan, as structured facts the panel and the confirmation use.
 *
 * The panel renders `decisions` read-only; the guard confirmation names `count`
 * and lists the records and ops the daemon returned. `hasRefusal` is true only
 * when the daemon refused a record — an empty `refusal` (its required shape) is
 * not a refusal, so a plan that refused nothing reads as clean.
 */
export interface PlanSummary {
  /** How many records the plan changes — what the guard confirmation names. */
  count: number;
  decisions: SeedDecision[];
  hasRefusal: boolean;
  refusal?: { key: string; reason: string };
}

export function describePlan(plan: SeedPlan): PlanSummary {
  const refusal = plan.refusal;
  const hasRefusal = refusal !== undefined && (refusal.key !== "" || refusal.reason !== "");
  return {
    count: plan.decisions.length,
    decisions: plan.decisions,
    hasRefusal,
    refusal: hasRefusal ? refusal : undefined,
  };
}
