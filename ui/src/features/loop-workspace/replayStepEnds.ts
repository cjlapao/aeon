import type { ChatItem } from "../../components/agent-chat";
import type { LoopDetail } from "../loops/pages/LoopScreen";

// A step's end is not in the saved transcript, but the step row is: how it
// ended, when, and why. A reopened chat puts the same "finished" divider the
// live stream drew (`step.done`) where the step ended, so the history reads as
// it did when it happened.

interface EndedStep { stepId: number; agent?: string; outcome: string; reason?: string; endedAt: string }

function endedSteps(loop: LoopDetail): EndedStep[] {
  const out: EndedStep[] = [];
  for (const lane of loop.lanes) {
    for (const step of [...(lane.steps ?? []), ...(lane.items ?? []).flatMap((item) => item.steps)]) {
      if (typeof step.stepId !== "number" || !step.endedAt) continue;
      out.push({ stepId: step.stepId, agent: (step as { agent?: string }).agent, outcome: step.outcome || step.status || "done", reason: step.reason, endedAt: step.endedAt });
    }
  }
  return out;
}

const tone = (outcome: string): "success" | "danger" | "neutral" =>
  outcome === "ok" ? "success" : outcome === "failed" || outcome === "blocked" ? "danger" : "neutral";

/**
 * The history with each ended step's closing divider placed after the last item
 * that happened before it ended. `skip` names the steps the live stream already
 * draws (their own `step.done` is there), so none is drawn twice.
 */
export function withStepEnds(history: ChatItem[], loop: LoopDetail | null, skip: ReadonlySet<number>): ChatItem[] {
  if (!loop || history.length === 0) return history;
  const first = history.find((i) => i.at !== undefined)?.at;
  if (first === undefined) return history;
  const dispatchedAgent = new Map<number, string>();
  const present = new Set<number>();
  for (const i of history) {
    if (i.kind === "divider" && i.id.startsWith("dispatch:") && i.highlights?.[0]) {
      const id = Number(i.id.slice("dispatch:".length));
      dispatchedAgent.set(id, i.highlights[0]);
      present.add(id);
    }
    if (i.kind === "tool" && i.stepId !== undefined) present.add(i.stepId);
  }
  const out = [...history];
  for (const s of endedSteps(loop).sort((a, b) => Date.parse(a.endedAt) - Date.parse(b.endedAt))) {
    // Only steps whose own turns were loaded: an end with no beginning is noise.
    if (skip.has(s.stepId) || !present.has(s.stepId)) continue;
    const ended = Date.parse(s.endedAt);
    let at = -1;
    out.forEach((i, n) => { if (i.at !== undefined && Date.parse(i.at) <= ended) at = n; });
    const agent = s.agent ?? dispatchedAgent.get(s.stepId) ?? "agent";
    out.splice(at + 1, 0, { id: `stepend:${s.stepId}`, at: s.endedAt, kind: "divider", tone: tone(s.outcome),
      text: `${agent} finished · ${s.outcome}${s.reason ? ` · ${s.reason}` : ""}` });
  }
  return out;
}
