import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { rpc } from "../../lib/bridge";
export type { LoopTotals, StepSummary } from "./stepSummaries";

import type { LoopTotals, StepSummary } from "./stepSummaries";

interface StepsResult { steps: StepSummary[]; hasOlder: boolean; totals?: LoopTotals }

/** Steps in a page: the newest this many come on open, and older ones as the person scrolls back. */
export const STEP_WINDOW = 50;
const REFRESH_MS = 800;

export interface LoopSteps {
  /** Oldest first. */
  summaries: StepSummary[];
  totals?: LoopTotals;
  hasOlder: boolean;
  loadingOlder: boolean;
  loadOlder(): void;
}

/**
 * The loop's step summaries: the newest page on open, older pages on demand, the newest page re-read (debounced) when
 * `refreshKey` moves. A refresh replaces the summaries it returns and keeps the older pages already loaded, so
 * scrolling back is never undone by new activity. Summaries are small, so what was loaded stays in memory.
 */
export function useLoopSteps(loopId: string, enabled: boolean, refreshKey: unknown): LoopSteps {
  const [byStep, setByStep] = useState<Map<number, StepSummary>>(new Map());
  const [totals, setTotals] = useState<LoopTotals | undefined>();
  const [hasOlder, setHasOlder] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const olderKnown = useRef(false);
  const oldest = useRef<number | undefined>(undefined);

  useEffect(() => { setByStep(new Map()); setTotals(undefined); setHasOlder(false); olderKnown.current = false; oldest.current = undefined; }, [loopId]);

  const readFirst = useCallback(async () => {
    try {
      const res = await rpc<StepsResult>("loop.steps", { id: loopId, limit: STEP_WINDOW });
      if (!Array.isArray(res?.steps)) return;
      setByStep((cur) => { const next = new Map(cur); for (const s of res.steps) next.set(s.stepId, s); return next; });
      if (res.totals) setTotals(res.totals);
      if (!olderKnown.current) { olderKnown.current = true; setHasOlder(res.hasOlder); }
      const lowest = res.steps.length ? Math.min(...res.steps.map((s) => s.stepId)) : undefined;
      if (lowest !== undefined && (oldest.current === undefined || lowest < oldest.current)) oldest.current = lowest;
    } catch { /* an older daemon has no summaries: the page keeps what its events say */ }
  }, [loopId]);

  useEffect(() => {
    if (!enabled || !loopId) return;
    const timer = setTimeout(() => void readFirst(), REFRESH_MS);
    return () => clearTimeout(timer);
  }, [enabled, loopId, refreshKey, readFirst]);

  const loadOlder = useCallback(() => {
    if (!hasOlder || loadingOlder || oldest.current === undefined) return;
    setLoadingOlder(true);
    void (async () => {
      try {
        const res = await rpc<StepsResult>("loop.steps", { id: loopId, before: oldest.current, limit: STEP_WINDOW });
        if (!Array.isArray(res?.steps)) { setHasOlder(false); return; }
        setByStep((cur) => { const next = new Map(cur); for (const s of res.steps) next.set(s.stepId, s); return next; });
        setHasOlder(res.hasOlder);
        if (res.steps.length) oldest.current = Math.min(...res.steps.map((s) => s.stepId));
      } catch { setHasOlder(false); } finally { setLoadingOlder(false); }
    })();
  }, [hasOlder, loadingOlder, loopId]);

  const summaries = useMemo(() => [...byStep.values()].sort((a, b) => a.stepId - b.stepId), [byStep]);
  return { summaries, totals, hasOlder, loadingOlder, loadOlder };
}

