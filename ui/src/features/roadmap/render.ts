import type { RoadmapItemView, RoadmapStatus } from "../../lib/protocol";

/**
 * The roadmap's ONE renderer of its honest words (S26, T5/T6).
 *
 * The rule the whole slice shares: `unknown` priority renders as the WORD
 * (never a blank, a dash, or the silently-implied-low), and an estimate
 * renders as `min–max (basis)` or as its explicit absence — a blank cell
 * reads as zero, which is the confident wrong answer P31 forbids. The CLI
 * prints these same words from Go (`cmd/aeon/roadmap.go`); this is the web
 * half of that one law, defined ONCE and imported by both the bench tab
 * (`test-bench/pages/RoadmapPage.tsx`) and the app view
 * (`features/roadmap/RoadmapPage.tsx`) — a second copy is the second truth
 * the slice forbids.
 */

/** The five pre-loop rungs cancel may act on (running is the loop's own record; done/cancelled are terminal). */
export const CANCELLABLE: readonly string[] = ["idea", "ideating", "brief-agreed", "planned", "ready"];

/** Eight distinct renderings — none of these strings repeats another (T6's gate asserts it). */
export const STATUS_LABELS: Record<RoadmapStatus, string> = {
  idea: "idea — the item's only entrance",
  ideating: "ideating — the brief is being written (the loop owns the work)",
  "brief-agreed": "brief-agreed — the brief landed with the loop's own `done`",
  planned: "planned — the plan landed with task_count and plan.md on disk",
  ready: "ready — brief, plan and a stated priority, re-checked at the attempt",
  running: "running — it is a loop now (D6: the item's own field, beside the loop)",
  done: "done — its loop finished; the loop's rows are the truth (D11)",
  cancelled: "cancelled — its own outcome (D7), never a deletion",
};

/** D2 rendered as words: `unknown` is the honest absence, distinct from both stated states. */
export function priorityWords(item: RoadmapItemView): string {
  if (item.priorityState === "unknown") return "priority unknown — not low, NOT STATED (D2)";
  if (item.priorityState === "assumed") return `priority assumed: ${item.priorityLabel ?? "—"}`;
  return `priority known: ${item.priorityLabel ?? "—"}`;
}

/** D3 rendered whole or as its honest absence — never a bare number, never a blank (a blank reads as zero). */
export function estimateWords(item: RoadmapItemView): string {
  if (item.estimateMin !== null && item.estimateMax !== null && item.estimateBasis !== null) {
    return `${item.estimateMin}–${item.estimateMax} (${item.estimateBasis})`;
  }
  return "not estimated — the trio is all-present or all-absent (D3)";
}
