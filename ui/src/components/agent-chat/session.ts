import type { ChatEventLike } from "./fromEvents";

/**
 * What a host needs to WIRE the chat, derived from the same events it renders:
 * which step each agent is running (steer/stop are keyed by `stepId`, never by
 * agent — D12), what route/effort it is on (the composer only sends a change),
 * and which step a question belongs to (`answer` needs both ids).
 */
export interface AgentRun {
  agent: string;
  stepId: number;
  loopId?: string;
  /** The blueprint lane the step was dispatched for. */
  lane?: string;
  step?: string;
  route?: string;
  reasoningEffort?: string;
  /** `running` until its `step.done`; `stopped` between a stop and a continue. */
  state: "running" | "stopped" | "done";
}

export interface ChatSession {
  /** Latest run per agent, in first-seen order. */
  runs: AgentRun[];
  byStep: Map<number, AgentRun>;
  questionStep: Map<number, number>;
  /** The loop's gate question id → lane, for answering with `loop.answer`. */
  gates: Map<number, { loopId?: string; lane?: string }>;
  loopIds: Set<string>;
  /**
   * The loop's state as its own events last said — `unknown` before any.
   * A loop parked at a gate has no running agent and is NOT finished, so
   * "read-only" must come from here, never from "nobody is running".
   */
  /** `idle` is S39's rest: the loop stands, no run in flight (loop.run.finished). */
  loopStatus: "unknown" | "running" | "waiting" | "finished" | "blocked" | "idle";
}

const rec = (v: unknown): Record<string, unknown> => (v && typeof v === "object" ? v as Record<string, unknown> : {});
const s = (v: unknown) => (typeof v === "string" && v !== "" ? v : undefined);
const n = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);

export function deriveSession(events: readonly ChatEventLike[]): ChatSession {
  const byStep = new Map<number, AgentRun>();
  const latestByAgent = new Map<string, number>();
  const questionStep = new Map<number, number>();
  const gates = new Map<number, { loopId?: string; lane?: string }>();
  const loopIds = new Set<string>();
  let loopStatus: ChatSession["loopStatus"] = "unknown";

  const ensure = (stepId: number, agent?: string): AgentRun => {
    let run = byStep.get(stepId);
    if (!run) {
      run = { agent: agent ?? "agent", stepId, state: "running" };
      byStep.set(stepId, run);
    }
    if (agent && run.agent === "agent") run.agent = agent;
    latestByAgent.set(run.agent, stepId);
    return run;
  };

  for (const ev of [...events].sort((a, b) => a.seq - b.seq)) {
    const p = rec(ev.payload);
    const stepId = n(p.stepId);
    const loopId = s(p.loopId);
    if (loopId) loopIds.add(loopId);
    // A conversation turn is one step spoken in a resting loop, not the loop walking:
    // it must not turn the loop "running", or the composer never offers a message again.
    const turn = ev.kind === "loop.step.dispatched" && (s(p.step) === "turn" || s(p.step) === "title") && !s(p.lane);
    if (ev.kind === "loop.started" || ev.kind === "loop.advanced" || (ev.kind === "loop.step.dispatched" && !turn)) loopStatus = "running";
    else if (ev.kind === "loop.waiting") loopStatus = "waiting";
    else if (ev.kind === "loop.finished") loopStatus = "finished";
    else if (ev.kind === "loop.blocked") loopStatus = "blocked";
    else if (ev.kind === "loop.run.finished") loopStatus = "idle";
    else if (ev.kind === "loop.run.started") loopStatus = "running";
    switch (ev.kind) {
      case "loop.step.dispatched":
        if (stepId !== undefined) Object.assign(ensure(stepId, s(p.agent)), { loopId, lane: s(p.lane), step: s(p.step), route: s(p.route), reasoningEffort: s(p.reasoningEffort), state: "running" });
        break;
      case "step.route.changed":
        if (stepId !== undefined) {
          const run = ensure(stepId, s(p.agent));
          if (s(p.route)) run.route = s(p.route);
          if (s(p.reasoningEffort)) run.reasoningEffort = s(p.reasoningEffort);
        }
        break;
      case "step.stopped":
        if (stepId !== undefined) ensure(stepId, s(p.agent)).state = "stopped";
        break;
      case "step.continued":
        if (stepId !== undefined) ensure(stepId, s(p.agent)).state = "running";
        break;
      case "step.done":
        if (stepId !== undefined) ensure(stepId, s(p.agent)).state = "done";
        break;
      case "question.asked": {
        const qid = n(p.id);
        if (qid !== undefined && stepId !== undefined) questionStep.set(qid, stepId);
        if (stepId !== undefined) ensure(stepId, s(p.agent));
        break;
      }
      case "loop.waiting": {
        const qid = n(p.questionId);
        if (qid !== undefined && Array.isArray(p.answers)) gates.set(qid, { loopId, lane: s(p.lane) });
        if (s(p.reason) === "stopped" && stepId !== undefined) ensure(stepId).state = "stopped";
        break;
      }
      default:
        if (stepId !== undefined && (ev.kind.startsWith("step.") || ev.kind === "model.delta")) ensure(stepId, s(p.agent));
    }
  }
  const runs = [...latestByAgent.values()].map((id) => byStep.get(id)!).filter(Boolean);
  return { runs, byStep, questionStep, gates, loopIds, loopStatus };
}
