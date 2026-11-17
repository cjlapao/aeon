import type { AgentRun, ChatItem, ChatSession, FileChange, ToolStatus } from "../../components/agent-chat";
import type { WorkflowData, WorkflowStatus, WorkflowSubStep } from "../../components/kit";
import { formatUSD, totalCost } from "../../lib/cost";
import { formatMs } from "../../lib/duration";
import type { AeonEvent } from "../../lib/protocol";
import { uncertainText, type UncertainValue } from "../../lib/uncertain";
import type { BlockedReason, LadderLane } from "../loops/components/Ladder";
import { laneAggregate, summaryLine, type StepSummary } from "./stepSummaries";
import { flattened, statusOf, workflowData } from "../loops/components/Workflow";
import type { LoopDetail } from "../loops/pages/LoopScreen";

/**
 * The loop page's pure derivations. Nothing here touches the daemon or React:
 * events and a `loop.show` snapshot in, display facts out, so every figure the
 * page shows has a test that pins it.
 */

/** Every daemon step id the loop's lanes, items and steps name. */
export function stepIdsOf(loop: LoopDetail | null): number[] {
  if (!loop) return [];
  const ids: number[] = [];
  for (const lane of loop.lanes) {
    for (const step of [...(lane.steps ?? []), ...(lane.items ?? []).flatMap((item) => item.steps)]) {
      if (typeof step.stepId === "number") ids.push(step.stepId);
    }
  }
  return ids;
}

/**
 * The events that belong to one loop: its own `loop.*` events (by `loopId`)
 * and every event of a step it ran. A step is the loop's if a
 * `loop.step.dispatched` event in hand says so, or if the `loop.show` snapshot
 * names it — the second covers a loop opened after its dispatch events aged
 * out of the daemon's bounded ring.
 */
export function scopeToLoop(events: readonly AeonEvent[], loopId: string, knownStepIds: readonly number[] = []): AeonEvent[] {
  const steps = new Set<number>(knownStepIds);
  for (const event of events) {
    const p = (event.payload ?? {}) as Record<string, unknown>;
    if (event.kind === "loop.step.dispatched" && p.loopId === loopId && typeof p.stepId === "number") steps.add(p.stepId);
  }
  return events.filter((event) => {
    const p = (event.payload ?? {}) as Record<string, unknown>;
    return p.loopId === loopId || (typeof p.stepId === "number" && steps.has(p.stepId));
  });
}

// --- the rail, the inspector and the footprint ------------------------------------------------

type ToolItem = Extract<ChatItem, { kind: "tool" }>;

const NOT_SPENT = new Set(["not-started", "skipped", "not-needed"]);
const toolItems = (items: readonly ChatItem[]): ToolItem[] => items.filter((i): i is ToolItem => i.kind === "tool");

/** The model steps of a lane that have actually run — a step that has not started has spent nothing. */
function spentSteps(lane: LadderLane) {
  return flattened(lane).filter((step) => step.kind !== "gate" && !NOT_SPENT.has(step.status));
}

/** The agent runs that belong to one lane (every run, not just each agent's latest). */
function runsOf(session: ChatSession, laneId: string): AgentRun[] {
  return [...session.byStep.values()].filter((run) => run.lane === laneId);
}

const summaryOf = (summaries: readonly StepSummary[], stepId: number): string | undefined => {
  const sum = summaries.find((x) => x.stepId === stepId);
  return sum && (sum.turns > 0 || sum.toolCalls > 0) ? summaryLine(sum) : undefined;
};

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const usd = (value: number) => `$${formatUSD(value)}`;

/** The agent's latest thing, in a line: a thought's first line, a call, or a sentence it said. */
export function agentActivity(items: readonly ChatItem[], agent: string): string | undefined {
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const item = items[i]!;
    if (!("agent" in item) || item.agent !== agent) continue;
    if (item.kind === "tool") return [item.tool, item.summary].filter(Boolean).join(" ");
    if (item.kind === "thinking" || item.kind === "text") {
      const line = item.text.split("\n").find((l) => l.trim());
      if (line) return line.trim();
    }
  }
  return undefined;
}

export interface ToolCallSummary {
  tool: string;
  summary?: string;
  durationMs?: number;
  status: ToolStatus;
  /** Parked on, or decided by, a person's approval. */
  gated: boolean;
  path?: string;
}

export interface StepTelemetry {
  /** The lane's status word as the loop reports it ("running", "waiting", …); "unknown" for a lane we cannot find. */
  status: string;
  /** Three-state, never a bare number: a step that spent nothing yet is not "$0". */
  cost?: UncertainValue<number>;
  elapsedMs?: number;
  activeAgents: number;
  produces?: string;
  tools: ToolCallSummary[];
  filesChanged: FileChange[];
  nextGate?: { lane: string; prompt: string };
}

export function stepTelemetry(loop: LoopDetail, session: ChatSession, items: readonly ChatItem[], laneId: string, now?: number): StepTelemetry {
  const lane = loop.lanes.find((l) => l.id === laneId);
  if (!lane) return { status: "unknown", activeAgents: 0, tools: [], filesChanged: [] };
  const current = lane.id === loop.position.lane;
  const status = current && ["blocked", "waiting", "running"].includes(loop.status) ? loop.status : lane.status;
  const runs = runsOf(session, laneId);
  const stepIds = new Set([...runs.map((r) => r.stepId), ...flattened(lane).flatMap((s) => (typeof s.stepId === "number" ? [s.stepId] : []))]);
  const calls = toolItems(items).filter((t) => t.stepId !== undefined && stepIds.has(t.stepId));

  const spent = spentSteps(lane);
  const wall = spent.map((s) => s.wallMs).filter((v): v is number => typeof v === "number" && Number.isFinite(v));
  let elapsedMs = wall.length ? wall.reduce((a, b) => a + b, 0) : undefined;
  // Only with a clock the caller passes in: this file reads no clock, so a render is repeatable.
  if (elapsedMs === undefined && status === "running" && now !== undefined) {
    const first = calls.map((t) => Date.parse(t.at ?? "")).filter(Number.isFinite).sort((a, b) => a - b)[0];
    if (first !== undefined) elapsedMs = Math.max(0, now - first);
  }

  const files = new Map<string, FileChange>();
  for (const call of calls) if (call.file) files.set(call.file.path, { path: call.file.path, linesAdded: call.file.linesAdded, linesRemoved: call.file.linesRemoved });

  const index = loop.lanes.indexOf(lane);
  const next = loop.lanes[index + 1];
  const produces = lane.produces?.length ? lane.produces.join(", ") : undefined;
  let nextGate: StepTelemetry["nextGate"];
  if (next && flattened(next).some((s) => s.kind === "gate") && statusOf(next.status) !== "done") {
    const after = loop.lanes[index + 2]?.id;
    nextGate = { lane: next.id, prompt: `You'll be asked to review and approve ${produces ?? "the result"}${after ? ` before step ${after} is initiated` : ""}.` };
  }

  return {
    status,
    cost: spent.length ? totalCost(spent.map((s) => s.cost)) : undefined,
    elapsedMs,
    activeAgents: runs.filter((r) => r.state !== "done").length,
    produces,
    tools: calls.map((t) => ({ tool: t.tool, summary: t.summary, durationMs: t.durationMs, status: t.status,
      gated: t.status === "awaiting" || t.approval !== undefined, path: t.file?.path })),
    filesChanged: [...files.values()],
    nextGate,
  };
}

const TOOL_SUBSTEP: Record<ToolStatus, WorkflowStatus> = {
  running: "running", done: "done", error: "failed", denied: "failed", skipped: "skipped", awaiting: "attention",
};
const RUN_SUBSTEP: Record<AgentRun["state"], WorkflowStatus> = { running: "running", stopped: "attention", done: "done" };

/**
 * The tracker's data for the rail: the existing blueprint adapter's steps, with
 * each lane that has agent runs re-described from the live stream — how many
 * agents, the cost and time so far, and sub-steps that are the agents followed
 * by the calls they made.
 */
export function workflowFromLoop(loop: LoopDetail, session: ChatSession, items: readonly ChatItem[], reason?: string | BlockedReason, now?: number, summaries: readonly StepSummary[] = []): WorkflowData {
  const base = workflowData(loop, reason);
  const calls = toolItems(items);
  return { ...base, steps: base.steps.map((step) => {
    const lane = loop.lanes.find((l) => l.id === step.id);
    if (!lane) return step;
    const gate = flattened(lane).some((s) => s.kind === "gate");
    const runs = runsOf(session, lane.id);
    const withBadge = gate && step.badge !== "Blocked" ? { ...step, badge: "Gate" } : step;
    // Nothing has run here yet: no agents to count and no cost to report, so no meta line at all.
    if (runs.length === 0) return statusOf(lane.status) === "not_started" && !gate ? { ...withBadge, meta: undefined, elapsed: undefined } : withBadge;

    const telemetry = stepTelemetry(loop, session, items, lane.id, now);
    const agg = laneAggregate(summaries, lane.id);
    const meta = [
      plural(runs.length, "agent"),
      telemetry.elapsedMs === undefined ? undefined : formatMs(telemetry.elapsedMs),
      // A step that has not reported cost yet is the normal case early on; the rail says so briefly.
      telemetry.cost ? (telemetry.cost.state === "unknown" ? "cost not reported" : uncertainText(telemetry.cost, usd)) : undefined,
      // The daemon's own count, so it holds for a step whose calls the page has not loaded.
      agg && agg.toolCalls > 0 ? plural(agg.toolCalls, "call") : undefined,
    ].filter(Boolean).join(" · ");
    const stepIds = new Set(runs.map((r) => r.stepId));
    const subSteps: WorkflowSubStep[] = [
      ...runs.map((run) => ({ id: `agent:${run.stepId}`, label: run.agent, status: RUN_SUBSTEP[run.state],
        note: agentActivity(items, run.agent) ?? (summaryOf(summaries, run.stepId)), badge: run.state })),
      ...calls.filter((t) => t.stepId !== undefined && stepIds.has(t.stepId)).map((t) => ({
        id: `tool:${t.toolUseId ?? t.id}`, label: [t.tool, t.summary].filter(Boolean).join(": "), status: TOOL_SUBSTEP[t.status],
        duration: t.durationMs === undefined ? undefined : formatMs(t.durationMs) })),
    ];
    const running = statusOf(telemetry.status) === "running";
    return { ...withBadge, meta, badge: running && !gate ? "RUNNING" : withBadge.badge, elapsed: undefined, subSteps };
  }) };
}

/** Every file the loop touched, and how: read, created (a first write that removed nothing), or modified. */
export function filesInScope(items: readonly ChatItem[]): Map<string, "read" | "created" | "modified"> {
  const scope = new Map<string, "read" | "created" | "modified">();
  for (const call of toolItems(items)) {
    if (call.tool === "read" && call.summary) {
      if (!scope.has(call.summary)) scope.set(call.summary, "read");
    } else if (call.file && (call.tool === "write_file" || call.tool === "edit")) {
      const prior = scope.get(call.file.path);
      scope.set(call.file.path, prior === undefined && !(call.file.linesRemoved ?? 0) ? "created" : "modified");
    }
  }
  return scope;
}

export interface Footprint {
  perStep: { lane: string; costUsd?: number; partial: boolean }[];
  totalUsd?: number;
  /** The total is a floor: some step that ran has incomplete accounting. */
  partial: boolean;
}

const asNumber = (value: UncertainValue<number>): { usd?: number; partial: boolean } =>
  value.state === "known" ? { usd: value.value, partial: false } : value.state === "partial" ? { usd: value.atLeast, partial: true } : { partial: true };

/** Cost only — the stream carries no token counts, and a figure we were not given is not shown. */
export function footprint(loop: LoopDetail): Footprint {
  const perStep = loop.lanes.map((lane) => {
    const spent = spentSteps(lane);
    if (!spent.length) return { lane: lane.id, costUsd: undefined, partial: false };
    const { usd: costUsd, partial } = asNumber(totalCost(spent.map((s) => s.cost)));
    return { lane: lane.id, costUsd, partial };
  });
  const all = loop.lanes.flatMap(spentSteps);
  if (!all.length) return { perStep, totalUsd: undefined, partial: false };
  const { usd: totalUsd, partial } = asNumber(totalCost(all.map((s) => s.cost)));
  return { perStep, totalUsd, partial };
}

/** Every file the loop wrote, with its added and removed lines summed across writes. */
export function fileChanges(items: readonly ChatItem[]): FileChange[] {
  const byPath = new Map<string, { linesAdded: number; linesRemoved: number }>();
  for (const call of toolItems(items)) {
    if (!call.file) continue;
    const prior = byPath.get(call.file.path) ?? { linesAdded: 0, linesRemoved: 0 };
    byPath.set(call.file.path, { linesAdded: prior.linesAdded + (call.file.linesAdded ?? 0), linesRemoved: prior.linesRemoved + (call.file.linesRemoved ?? 0) });
  }
  return [...byPath].map(([path, lines]) => ({ path, ...lines }));
}
