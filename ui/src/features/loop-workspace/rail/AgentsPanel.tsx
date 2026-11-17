import { AgentAvatar, type AgentRun, type ChatAgent, type ChatItem } from "../../../components/agent-chat";
import { CustomIcon } from "../../../components/kit";
import { formatUSD } from "../../../lib/cost";
import { agentActivity, type Footprint } from "../workspaceModel";

/**
 * The agents panel — the rail's "controls" slot (the kit's WorkflowTrackerRail
 * has no agents list, so the page supplies its own). Expanded it is the list
 * and the loop's footprint; collapsed it is a column of avatars. The same
 * component renders both so its state survives the rail collapsing.
 */
export interface AgentsPanelProps {
  collapsed: boolean;
  runs: readonly AgentRun[];
  items: readonly ChatItem[];
  /** The agent the chat is zoomed to; "" is the loop view. */
  selected: string;
  onSelect(agent: string): void;
  footprint: Footprint;
  /** The loop is running — the footprint says so. */
  live: boolean;
  agents?: Record<string, ChatAgent>;
}

const LOOP_VIEW = "Loop view – all activity";
const STATE_DOT: Record<AgentRun["state"], string> = { running: "bg-blue-500", stopped: "bg-amber-500", done: "bg-emerald-500" };
const usd = (value: number) => `$${formatUSD(value)}`;

function Eyebrow({ children, right }: { children: string; right?: string }) {
  return <div className="mb-2 flex items-baseline justify-between text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400!">
    <span>{children}</span>{right && <span className="font-mono normal-case tracking-normal text-blue-700 dark:text-blue-300!">{right}</span>}
  </div>;
}

function CollapsedColumn({ runs, selected, onSelect, agents }: Pick<AgentsPanelProps, "runs" | "selected" | "onSelect" | "agents">) {
  const ring = (on: boolean) => `relative rounded-full p-0.5 transition ${on ? "ring-2 ring-blue-500" : "hover:bg-slate-100 dark:hover:bg-white/10!"}`;
  return <div className="flex flex-col items-center gap-2 py-2" data-testid="agents-collapsed">
    <span className="text-[9px] font-semibold uppercase tracking-wider text-slate-400">Agents</span>
    <button type="button" aria-label="Loop view" aria-pressed={selected === ""} onClick={() => onSelect("")} className={ring(selected === "")}>
      <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-700 text-white"><CustomIcon icon="Orchestrator" size="sm" /></span>
    </button>
    {runs.map((run) => <button key={run.agent} type="button" aria-label={run.agent} aria-pressed={selected === run.agent}
      title={`${run.agent} · ${run.state}`} onClick={() => onSelect(run.agent)} className={ring(selected === run.agent)}>
      <AgentAvatar id={run.agent} agents={agents} size={32} />
      <span className={`absolute bottom-0 right-0 h-2.5 w-2.5 rounded-full border-2 border-white dark:border-slate-900! ${STATE_DOT[run.state]}`} />
    </button>)}
  </div>;
}

function FootprintBars({ footprint, live }: { footprint: Footprint; live: boolean }) {
  const bars = footprint.perStep.filter((s): s is typeof s & { costUsd: number } => s.costUsd !== undefined);
  const max = Math.max(...bars.map((b) => b.costUsd), Number.MIN_VALUE);
  return <div className="min-w-0 rounded-xl border border-slate-200 bg-white/70 p-3 dark:border-white/10! dark:bg-white/5!" data-testid="footprint">
    <Eyebrow right={live ? "Live" : undefined}>LOOP FOOTPRINT</Eyebrow>
    {bars.length === 0 ? <p className="text-xs text-slate-500">No cost reported yet</p> : <>
      <div className="flex h-14 items-end gap-1.5" aria-hidden>
        {bars.map((b) => <div key={b.lane} data-testid="footprint-bar" title={`${b.lane}: ${usd(b.costUsd)}${b.partial ? " or more" : ""}`}
          className="flex-1 rounded-t bg-blue-500/80 dark:bg-blue-400/80!" style={{ height: `${Math.max(8, (b.costUsd / max) * 100)}%` }} />)}
      </div>
      <div className="mt-2 flex justify-between font-mono text-xs text-slate-600 dark:text-slate-300!">
        <span>{footprint.perStep.length} steps</span>
        <span>{footprint.totalUsd === undefined ? "—" : `${footprint.partial ? "at least " : ""}${usd(footprint.totalUsd)}`}</span>
      </div></>}
  </div>;
}

export function AgentsPanel({ collapsed, runs, items, selected, onSelect, footprint, live, agents }: AgentsPanelProps) {
  if (collapsed) return <CollapsedColumn runs={runs} selected={selected} onSelect={onSelect} agents={agents} />;
  const row = (on: boolean) => `flex w-full min-w-0 items-center gap-3 rounded-xl px-3 py-2 text-left transition ${on
    ? "bg-blue-700 text-white shadow-sm"
    : "text-slate-800 hover:bg-slate-100 dark:text-slate-100! dark:hover:bg-white/10!"}`;
  // width:0 + min-width:100% — the panel takes its parent's width but adds nothing to it. The kit's rail
  // sizes its scroll content to its widest child, so a long agent activity line would otherwise widen
  // the whole rail's content and be clipped at the card's edge instead of truncating.
  return <div className="space-y-4 overflow-hidden p-3" style={{ width: 0, minWidth: "100%" }} data-testid="agents-expanded">
    <div>
      <Eyebrow right={`${runs.length} Total`}>ORCHESTRATED AGENTS</Eyebrow>
      <div className="space-y-1">
        <button type="button" aria-label={LOOP_VIEW} aria-pressed={selected === ""} onClick={() => onSelect("")} className={row(selected === "")}>
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-white/20"><CustomIcon icon="Orchestrator" size="sm" /></span>
          <span className="min-w-0"><span className="block truncate text-sm font-medium">{LOOP_VIEW}</span>
            <span className={`block truncate font-mono text-xs ${selected === "" ? "text-blue-100" : "text-slate-500"}`}>unified stream</span></span>
        </button>
        {runs.map((run) => {
          const on = selected === run.agent;
          const activity = agentActivity(items, run.agent);
          return <button key={run.agent} type="button" aria-label={run.agent} aria-pressed={on} onClick={() => onSelect(run.agent)} className={row(on)}>
            <AgentAvatar id={run.agent} agents={agents} size={28} />
            <span className="min-w-0 flex-1">
              <span className="flex items-baseline justify-between gap-2"><span className="truncate text-sm font-medium">{agents?.[run.agent]?.name ?? run.agent}</span>
                <span className={`shrink-0 font-mono text-xs ${on ? "text-blue-100" : run.state === "running" ? "text-blue-700 dark:text-blue-300!" : "text-slate-400"}`}>{run.state}</span></span>
              {activity && <span className={`block truncate font-mono text-xs ${on ? "text-blue-100" : "text-slate-500 dark:text-slate-400!"}`}>{activity}</span>}
            </span>
          </button>;
        })}
      </div>
    </div>
    <FootprintBars footprint={footprint} live={live} />
  </div>;
}
