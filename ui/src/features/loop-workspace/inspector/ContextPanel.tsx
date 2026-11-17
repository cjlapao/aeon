import { useState } from "react";
import { Chart } from "../../../components/kit";
import { compactTokens, contextSteps, type ContextUse } from "../contextModel";
import type { StepSummary } from "../stepSummaries";

const TONE_COLOR: Record<ContextUse["tone"], string> = { ok: "#3b82f6", warm: "#f59e0b", hot: "#ef4444", unknown: "#94a3b8" };
const TONE_BAR: Record<ContextUse["tone"], string> = { ok: "bg-blue-500", warm: "bg-amber-500", hot: "bg-red-500", unknown: "bg-slate-400" };
const FREE = "#cbd5e1";
const ROWS = 6;

function Doughnut({ use }: { use: ContextUse }) {
  if (use.window <= 0) {
    return <p className="text-sm text-slate-500" data-testid="context-unknown-window">
      {use.agent} holds {compactTokens(use.tokens)} tokens; the route{use.route ? ` ${use.route}` : ""} has no context window set, so there is no share to draw.
    </p>;
  }
  const data = [{ name: "Used", value: Math.min(use.tokens, use.window) }, { name: "Free", value: Math.max(use.window - use.tokens, 0) }];
  return <div className="mx-auto w-44" data-testid="context-doughnut">
    <Chart.Svg height={176} animation={false} ariaLabel={`${use.agent} context use`}>
      <Chart.Pie data={data} valueField="value" categoryField="name" innerRadius={0.72} colors={[TONE_COLOR[use.tone], FREE]} />
      <Chart.PieCenter title={use.agent} value={`${Math.round((use.pct ?? 0) * 100)}%`} subtitle={`${compactTokens(use.tokens)} / ${compactTokens(use.window)}`} />
    </Chart.Svg>
  </div>;
}

/**
 * How full each agent's context is. Every step starts with a fresh context, so each is its own: the doughnut is the
 * one running now (or the latest), and the rows below are the others, any of which can be looked at by clicking it.
 */
export function ContextPanel({ summaries }: { summaries: readonly StepSummary[] }) {
  const { all, current } = contextSteps(summaries);
  const [picked, setPicked] = useState<number | undefined>();
  const [more, setMore] = useState(false);
  if (!current) return <p className="text-sm text-slate-500" data-testid="context-none">No agent has called a model yet.</p>;
  const focus = all.find((u) => u.stepId === picked) ?? current;
  const rows = more ? all : all.slice(0, ROWS);
  return <div data-testid="context-panel" className="space-y-3">
    <Doughnut use={focus} />
    {focus.tone === "hot" && <p role="status" className="text-center text-xs text-red-600 dark:text-red-400!">This context is nearly full; the agent is close to losing room to work.</p>}
    <ul className="space-y-1">
      {rows.map((u) => <li key={u.stepId}>
        <button type="button" aria-pressed={u.stepId === focus.stepId} onClick={() => setPicked(u.stepId)}
          className={`w-full rounded-md px-2 py-1.5 text-left hover:bg-slate-50 dark:hover:bg-white/5! ${u.stepId === focus.stepId ? "bg-slate-50 dark:bg-white/5!" : ""}`}>
          <span className="flex items-baseline gap-2 text-sm">
            <span className="min-w-0 flex-1 truncate font-medium text-slate-800 dark:text-slate-100!">{u.agent}{u.status === "running" ? " · running" : ""}</span>
            <span className="shrink-0 font-mono text-xs text-slate-500">{compactTokens(u.tokens)}{u.window > 0 ? ` / ${compactTokens(u.window)}` : ""}</span>
          </span>
          <span className="mt-1 block h-1.5 overflow-hidden rounded bg-slate-200 dark:bg-white/10!">
            <span className={`block h-full ${TONE_BAR[u.tone]}`} style={{ width: `${Math.min(100, Math.round((u.pct ?? 0) * 100))}%` }} />
          </span>
        </button>
      </li>)}
    </ul>
    {all.length > ROWS && <button type="button" onClick={() => setMore((v) => !v)} className="text-xs text-blue-700 hover:underline dark:text-blue-300!" data-testid="context-more">
      {more ? "Show fewer" : `Show ${all.length - ROWS} more`}</button>}
  </div>;
}
