import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { AppIcon } from "../../../components/icons";
import { CustomIcon, type WorkflowStatus, type WorkflowStep } from "../../../components/kit";
import { formatUSD } from "../../../lib/cost";
import { formatMs } from "../../../lib/duration";
import { useState } from "react";
import { newest } from "../contextModel";
import type { LaneAggregate, StepSummary } from "../stepSummaries";
import { ContextPanel } from "./ContextPanel";
import type { LoopTotals } from "../useLoopSteps";
import type { StepTelemetry, ToolCallSummary } from "../workspaceModel";

const DOT: Record<WorkflowStatus, string> = {
  done: "bg-emerald-500", running: "bg-blue-500 animate-pulse", in_progress: "bg-blue-500", attention: "bg-amber-500",
  blocked: "bg-red-500", skipped: "bg-slate-300", not_started: "bg-slate-300",
  // `failed` joins the map (the closed WorkflowStatus vocabulary requires
  // every state to have a dot; a missing key means an unstyled dot, which
  // is the bug the compiler caught):
  failed: "bg-red-500",
};

/** Most entries a list shows at first; the rest are older and hidden behind the toggle. */
const LIST_LIMIT = 10;

function MoreToggle({ hidden, all, onToggle, testId }: { hidden: number; all: boolean; onToggle: () => void; testId: string }) {
  if (hidden === 0 && !all) return null;
  return <button type="button" onClick={onToggle} data-testid={testId} className="mt-2 text-xs text-blue-700 hover:underline dark:text-blue-300!">
    {all ? "Show only the latest 10" : `See ${hidden} earlier`}</button>;
}

const tokens = (n: number) => (n >= 10000 ? `${Math.round(n / 1000)}k` : String(n));
const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"}`;
const dirOf = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "root dir");

/**
 * An agent sub-step's id is `agent:${stepId}` (workspaceModel.ts's `workflowFromLoop`) — the
 * one durable handle this card has back to that run's full, persisted transcript. The live
 * chat above only ever shows the daemon's shared, bounded event ring (`DefaultRetention`,
 * `internal/protocol/events.go`) — an old run's turns fall out of it long before the run
 * itself stops mattering, which is exactly when this link is the only way back to them.
 */
function agentStepId(subStepId: string): number | undefined {
  if (!subStepId.startsWith("agent:")) return undefined;
  const id = Number(subStepId.slice("agent:".length));
  return Number.isFinite(id) ? id : undefined;
}

function Section({ title, aside, children, testId }: { title: string; aside?: ReactNode; children: ReactNode; testId?: string }) {
  return <section data-testid={testId} className="px-4 py-3">
    <h3 className="mb-2 flex items-baseline justify-between text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400!">
      <span>{title}</span>{aside && <span className="font-mono normal-case tracking-normal text-blue-700 dark:text-blue-300!">{aside}</span>}
    </h3>
    {children}
  </section>;
}

function Fact({ label, children, testId }: { label: string; children: ReactNode; testId?: string }) {
  return <div className="min-w-0"><dt className="text-xs text-slate-500 dark:text-slate-400!">{label}</dt>
    <dd data-testid={testId} className="truncate font-mono text-sm text-slate-900 dark:text-slate-100!">{children}</dd></div>;
}

function ToolIcon({ tool }: { tool: string }) {
  const name = tool === "bash" ? "bash" : tool === "read" ? "read" : tool === "write_file" || tool === "edit" ? "edit" : undefined;
  return name ? <AppIcon name={name} /> : <CustomIcon icon="Cog" size="xs" />;
}

function ToolRow({ call }: { call: ToolCallSummary }) {
  return <li className="flex items-center gap-2 border-b border-slate-100 py-1.5 last:border-b-0 dark:border-white/5!">
    <span className="text-slate-500"><ToolIcon tool={call.tool} /></span>
    <span className="min-w-0 flex-1 truncate font-mono text-sm text-slate-800 dark:text-slate-200!">{[call.tool, call.summary].filter(Boolean).join(": ")}</span>
    {call.gated ? <span className="font-mono text-xs text-amber-600">gate</span>
      : call.status === "error" ? <span className="font-mono text-xs text-red-600">failed</span>
      : call.durationMs !== undefined ? <span className="font-mono text-xs text-slate-400">{formatMs(call.durationMs)}</span> : null}
  </li>;
}

export interface StepTabProps {
  telemetry: StepTelemetry;
  /** The tracker's step for this lane — its `subSteps` are what is listed. */
  step: WorkflowStep;
  position: { index: number; total: number };
  /** The daemon's totals for this lane's steps, and for the whole loop. */
  laneSummary?: LaneAggregate;
  loopTotals?: LoopTotals;
  /** Every step's summary, for the context graph; omit and the section is left out. */
  summaries?: readonly StepSummary[];
  onOpenFile(path: string): void;
}

/**
 * The selected step, in full: the numbers, the gate that follows it, the
 * agents and calls it is made of, and the files it touched. A section the
 * telemetry has nothing for is left out, and a figure the daemon did not give
 * is never drawn as zero.
 */
export function StepTab({ telemetry, step, position, laneSummary, loopTotals, summaries, onOpenFile }: StepTabProps) {
  const { cost } = telemetry;
  const costText = cost === undefined ? undefined
    : cost.state === "known" ? `$${formatUSD(cost.value)}`
    : cost.state === "partial" ? `at least $${formatUSD(cost.atLeast)}` : "not reported";
  const subSteps = step.subSteps ?? [];
  // Long lists show their newest LIMIT entries; the older ones are one click away.
  const [allSub, setAllSub] = useState(false);
  const [allTools, setAllTools] = useState(false);
  const subView = newest(subSteps, LIST_LIMIT, allSub);
  const toolView = newest(telemetry.tools, LIST_LIMIT, allTools);
  return <div className="divide-y divide-slate-200 dark:divide-white/10!">
    <Section title="STEP TELEMETRY" aside={<span className="rounded-md bg-blue-50 px-1.5 py-0.5 dark:bg-blue-400/10!">Step {position.index} of {position.total}</span>}>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
        <Fact label="Status" testId="status">{telemetry.status}</Fact>
        {costText !== undefined && <Fact label="Cost" testId="cost">{costText}</Fact>}
        {telemetry.elapsedMs !== undefined && <Fact label="Step Time" testId="elapsed">{formatMs(telemetry.elapsedMs)}</Fact>}
        {telemetry.activeAgents > 0 && <Fact label="Active Agents">{telemetry.activeAgents} running</Fact>}
        {telemetry.produces && <Fact label="Produces" testId="produces">{telemetry.produces}</Fact>}
      </dl>
    </Section>
    {summaries && <Section title="CONTEXT" aside="per agent" testId="context-section"><ContextPanel summaries={summaries} /></Section>}
    {(laneSummary || loopTotals) && <Section title="WORK DONE" testId="work-done">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-3">
        {laneSummary && <><Fact label="Model turns" testId="lane-turns">{laneSummary.turns}</Fact>
          <Fact label="Tool calls" testId="lane-calls">{laneSummary.toolCalls}{laneSummary.toolErrors > 0 ? ` (${laneSummary.toolErrors} failed)` : ""}</Fact>
          <Fact label="Tokens in / out" testId="lane-tokens">{tokens(laneSummary.inputTokens)} / {tokens(laneSummary.outputTokens)}</Fact>
          <Fact label="Model time" testId="lane-time">{formatMs(laneSummary.wallMs)}</Fact></>}
        {loopTotals && <><Fact label="Whole loop" testId="loop-totals">{loopTotals.turns} turns · {loopTotals.toolCalls} calls</Fact>
          <Fact label="Loop cost" testId="loop-cost">{loopTotals.costUsd === undefined ? "not reported" : `${loopTotals.costState === "partial" ? "at least " : ""}$${formatUSD(loopTotals.costUsd)}`}</Fact></>}
      </dl>
    </Section>}
    {telemetry.nextGate && <div className="px-4 py-3">
      <div className="agent-chat-attention rounded-xl border border-amber-300 bg-amber-50/80 p-3 dark:border-amber-400/40! dark:bg-amber-400/10!">
        <h3 className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-amber-700 dark:text-amber-300!">NEXT: APPROVAL GATE</h3>
        <p className="text-sm text-slate-700 dark:text-slate-200!">{telemetry.nextGate.prompt}</p>
      </div>
    </div>}
    {subSteps.length > 0 && <Section title="SUB-STEPS" aside={plural(subSteps.length, "item")} testId="sub-steps">
      <ol className="space-y-1">
        {subView.shown.map((sub) => {
          const stepId = agentStepId(sub.id);
          return <li key={sub.id} className="flex items-start gap-2 py-1">
            <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${DOT[sub.status]}`} aria-hidden />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-mono text-sm text-slate-900 dark:text-slate-100!">{sub.label}</span>
              {sub.note && <span className="block truncate text-xs text-slate-500 dark:text-slate-400!">{sub.note}</span>}
            </span>
            <span className="shrink-0 font-mono text-xs text-slate-400">{sub.duration ?? sub.badge}</span>
            {stepId !== undefined && (
              <Link to={`/steps/${stepId}`} className="shrink-0 aeon-accent-text text-xs underline" data-testid="open-step-conversation">
                Open conversation →
              </Link>
            )}
          </li>;
        })}
      </ol>
      <MoreToggle hidden={subView.hidden} all={allSub} onToggle={() => setAllSub((v) => !v)} testId="sub-steps-more" />
    </Section>}
    {telemetry.tools.length > 0 && <Section title="TOOL INVOCATIONS" aside={plural(telemetry.tools.length, "call")}>
      <ul>{toolView.shown.map((call, i) => <ToolRow key={i} call={call} />)}</ul>
      <MoreToggle hidden={toolView.hidden} all={allTools} onToggle={() => setAllTools((v) => !v)} testId="tools-more" />
    </Section>}
    {telemetry.filesChanged.length > 0 && <Section title="FILES CHANGED" aside={plural(telemetry.filesChanged.length, "file")}>
      <ul className="space-y-1.5">
        {telemetry.filesChanged.map((f) => <li key={f.path}>
          <button type="button" onClick={() => onOpenFile(f.path)}
            className="flex w-full items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-left hover:border-blue-300 dark:border-white/10! dark:bg-white/5! dark:hover:border-blue-400/40!">
            <span className="min-w-0 flex-1"><span className="block truncate text-sm text-slate-900 dark:text-slate-100!">{f.path.split("/").pop()}</span>
              <span className="block truncate font-mono text-xs text-slate-500">{dirOf(f.path)}</span></span>
            {(f.linesAdded ?? 0) > 0 && <span className="rounded bg-emerald-50 px-1.5 font-mono text-xs text-emerald-700 dark:bg-emerald-400/10! dark:text-emerald-300!">+{f.linesAdded}</span>}
            {(f.linesRemoved ?? 0) > 0 && <span className="rounded bg-red-50 px-1.5 font-mono text-xs text-red-700 dark:bg-red-400/10! dark:text-red-300!">−{f.linesRemoved}</span>}
          </button>
        </li>)}
      </ul>
    </Section>}
  </div>;
}
