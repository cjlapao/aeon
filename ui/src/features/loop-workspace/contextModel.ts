import type { StepSummary } from "./stepSummaries";

/** How full a step's context is. `pct` is absent when the route's window is not known. */
export interface ContextUse {
  stepId: number;
  agent: string;
  status: string;
  tokens: number;
  window: number;
  pct?: number;
  tone: "ok" | "warm" | "hot" | "unknown";
  route?: string;
}

const WARM = 0.7;
const HOT = 0.9;

export function contextUse(s: StepSummary): ContextUse {
  const tokens = s.contextTokens ?? 0;
  const window = s.contextWindow ?? 0;
  const pct = window > 0 ? tokens / window : undefined;
  return { stepId: s.stepId, agent: s.agent || "agent", status: s.status || "", tokens, window, pct, route: s.route || undefined,
    tone: pct === undefined ? "unknown" : pct >= HOT ? "hot" : pct >= WARM ? "warm" : "ok" };
}

/** Every step that has called a model, newest first; the running one (or else the latest) is the one to look at. */
export function contextSteps(summaries: readonly StepSummary[]): { all: ContextUse[]; current?: ContextUse } {
  const all = summaries.filter((s) => (s.contextTokens ?? 0) > 0).map(contextUse).sort((a, b) => b.stepId - a.stepId);
  const running = all.find((u) => u.status === "running");
  return { all, current: running ?? all[0] };
}

/** "25.2k" for large counts, the plain number below 1000. */
export const compactTokens = (n: number): string => (n >= 1000 ? `${(n / 1000).toFixed(n >= 100000 ? 0 : 1)}k` : String(n));

/** The first `limit` items of a list kept in arrival order, newest last: the newest `limit`, unless everything is asked for. */
export function newest<T>(items: readonly T[], limit: number, showAll: boolean): { shown: readonly T[]; hidden: number } {
  if (showAll || items.length <= limit) return { shown: items, hidden: 0 };
  return { shown: items.slice(items.length - limit), hidden: items.length - limit };
}
