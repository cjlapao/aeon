import type { AgentRun, ChatSession } from "../../components/agent-chat";
import { flattened } from "../loops/components/Workflow";
import type { LoopDetail } from "../loops/pages/LoopScreen";
import type { StepSummary } from "./useLoopSteps";

/**
 * The agents that worked on the loop, from the loop's own record. The event window only knows the agents that
 * spoke inside it, so a reopened loop lost the rest: the rail showed one agent and the inspector one step. The
 * stored ladder knows every step that ever ran and who ran it; events still win for the state of a step they
 * describe, because they are newer than the last read.
 */
export function withLoopRuns(session: ChatSession, loop: LoopDetail | null, summaries: readonly StepSummary[] = []): ChatSession {
  if (!loop) return session;
  const byStep = new Map(session.byStep);
  for (const lane of loop.lanes ?? []) {
    for (const step of flattened(lane)) {
      const agent = (step as { agent?: string }).agent;
      if (typeof step.stepId !== "number" || !agent || byStep.has(step.stepId)) continue;
      const state: AgentRun["state"] = step.status === "running" ? "running" : ["waiting", "blocked", "stopped"].includes(step.status) ? "stopped" : "done";
      byStep.set(step.stepId, { agent, stepId: step.stepId, loopId: loop.id, lane: lane.id, step: step.id, state });
    }
  }
  // The summaries name every step the daemon kept, including any the ladder view does not list.
  for (const sum of summaries) {
    if (!sum.agent || byStep.has(sum.stepId)) continue;
    const state: AgentRun["state"] = sum.status === "running" ? "running" : ["waiting", "blocked", "stopped", "suspended", "orphaned"].includes(sum.status ?? "") ? "stopped" : "done";
    byStep.set(sum.stepId, { agent: sum.agent, stepId: sum.stepId, loopId: sum.loopId, lane: sum.lane || undefined, state });
  }
  if (byStep.size === session.byStep.size) return session;
  const latest = new Map<string, number>();
  for (const id of [...byStep.keys()].sort((a, b) => a - b)) latest.set(byStep.get(id)!.agent, id);
  // First-seen order of agents, as deriveSession keeps it: by the step each agent FIRST ran.
  const first = new Map<string, number>();
  for (const id of [...byStep.keys()].sort((a, b) => a - b)) if (!first.has(byStep.get(id)!.agent)) first.set(byStep.get(id)!.agent, id);
  const runs = [...latest.entries()].sort((a, b) => (first.get(a[0]) ?? 0) - (first.get(b[0]) ?? 0)).map(([, id]) => byStep.get(id)!);
  return { ...session, runs, byStep };
}
