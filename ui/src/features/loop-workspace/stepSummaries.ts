// The daemon's per-step summaries (`loop.steps`) and what the page makes of them. Pure: no clock, no daemon.

/** One step's summary, as `loop.steps` returns it: what the step has done so far, kept by the daemon. */
export interface StepSummary {
  stepId: number; loopId: string; lane?: string; agent?: string; status?: string;
  turns: number; toolCalls: number; toolErrors: number;
  inputTokens: number; outputTokens: number; reasoningTokens: number; wallMs: number;
  costState: string; costUsd?: number; firstEvent: number; lastEvent: number;
  /** How much of its context window the step holds now (its latest call's input), the route, and that route's window (0: unknown). */
  contextTokens?: number; route?: string; contextWindow?: number;
}
export interface LoopTotals {
  steps: number; turns: number; toolCalls: number; toolErrors: number;
  inputTokens: number; outputTokens: number; reasoningTokens: number; wallMs: number; costUsd?: number; costState: string;
}
/** One lane's summaries added up: what the rail's step and the inspector's numbers say. */
export function laneAggregate(summaries: readonly StepSummary[], lane: string) {
  const mine = summaries.filter((s) => s.lane === lane);
  if (mine.length === 0) return undefined;
  const sum = (f: (s: StepSummary) => number) => mine.reduce((a, s) => a + f(s), 0);
  const spent = mine.filter((s) => s.turns > 0);
  const known = spent.filter((s) => s.costState === "known").length;
  return {
    steps: mine.length, turns: sum((s) => s.turns), toolCalls: sum((s) => s.toolCalls), toolErrors: sum((s) => s.toolErrors),
    inputTokens: sum((s) => s.inputTokens), outputTokens: sum((s) => s.outputTokens), reasoningTokens: sum((s) => s.reasoningTokens),
    wallMs: sum((s) => s.wallMs), costUsd: spent.some((s) => s.costUsd !== undefined) ? sum((s) => s.costUsd ?? 0) : undefined,
    costState: spent.length === 0 ? "unknown" : known === spent.length ? "known" : known === 0 && !spent.some((s) => s.costUsd !== undefined) ? "unknown" : "partial",
  };
}
export type LaneAggregate = NonNullable<ReturnType<typeof laneAggregate>>;

/** "3 turns · 7 calls (1 failed)" — the line a step shows when no live activity does. */
export function summaryLine(a: { turns: number; toolCalls: number; toolErrors: number }): string {
  const calls = `${a.toolCalls} call${a.toolCalls === 1 ? "" : "s"}${a.toolErrors > 0 ? ` (${a.toolErrors} failed)` : ""}`;
  return `${a.turns} turn${a.turns === 1 ? "" : "s"} · ${calls}`;
}
