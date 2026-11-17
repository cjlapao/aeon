import type { AeonEvent } from "../../lib/protocol";
import type { LoopDetail } from "../loops/pages/LoopScreen";

// What a loop is still owed, read from the store (S76). The live event ring is
// bounded and a chat opened after the fact never saw `question.asked`,
// `loop.waiting` or `step.steer.queued`, so without this a loop waiting on a
// person shows a history with nothing to answer, and an instruction the person
// queued before leaving is simply gone from view.
//
// Each recovered fact becomes the SAME event the daemon would have sent, so the
// one mapper draws it and the one action path handles it. A fact the ring still
// holds is drawn once, from its own event.

/** One question of the parked step's block: answered ones keep their answer. */
export interface BlockQuestion {
  id: number;
  stepId?: number;
  kind: string;
  prompt: string;
  answers: string[];
  answer?: string;
  /** The tool call that asked it: where a replayed transcript draws the card. */
  toolUseId?: string;
}

export interface PendingGate {
  id: number;
  prompt: string;
  answers: string[];
  lane?: string;
}

export interface PendingSteer {
  steerId: number;
  stepId: number;
  text: string;
  createdAt: string;
}

export interface Recovered {
  questions: BlockQuestion[];
  gate: PendingGate | null;
  steers: PendingSteer[];
  /** Every question the loop ever asked, with its answer: the replay of the past. */
  past: BlockQuestion[];
}

export const NOTHING_RECOVERED: Recovered = { questions: [], gate: null, steers: [], past: [] };

const field = (e: AeonEvent): Record<string, unknown> => (e.payload ?? {}) as Record<string, unknown>;
const has = (events: readonly AeonEvent[], kind: string, key: string, id: number): boolean =>
  events.some((e) => e.kind === kind && field(e)[key] === id);

/** The agent that ran a step, from the loop's own ladder. */
function agentOfStep(loop: LoopDetail | null, stepId?: number): string | undefined {
  if (!loop || stepId === undefined) return undefined;
  for (const lane of loop.lanes) {
    for (const step of [...(lane.steps ?? []), ...(lane.items ?? []).flatMap((item) => item.steps)]) {
      if (step.stepId === stepId) return (step as { agent?: string }).agent;
    }
  }
  return undefined;
}

/**
 * The stream plus whatever the store says the loop is still owed. The block of
 * questions is added in the order asked, each followed by its answer when it has
 * one, so the mapper's own grouping draws one card ("1/3 answered") and the
 * remaining questions can be answered in place.
 */
export function withRecovered(events: AeonEvent[], r: Recovered, loop: LoopDetail | null): AeonEvent[] {
  if (!loop) return events;
  let seq = events[events.length - 1]?.seq ?? 0;
  const at = new Date().toISOString();
  const add: AeonEvent[] = [];
  const push = (kind: string, payload: Record<string, unknown>) => add.push({ seq: ++seq, kind, at, payload: { loopId: loop.id, ...payload } } as AeonEvent);

  for (const q of r.questions) {
    if (!has(events, "question.asked", "id", q.id)) {
      push("question.asked", { id: q.id, kind: q.kind, body: q.prompt, options: q.answers, stepId: q.stepId, agent: agentOfStep(loop, q.stepId) });
    }
    if (q.answer !== undefined && q.answer !== "" && !has(events, "question.answered", "questionId", q.id)) {
      push("question.answered", { stepId: q.stepId, questionId: q.id, answer: q.answer });
    }
  }
  if (r.gate && !has(events, "loop.waiting", "questionId", r.gate.id)) {
    push("loop.waiting", { questionId: r.gate.id, answers: r.gate.answers, prompt: r.gate.prompt, lane: r.gate.lane ?? loop.position.lane });
  }
  for (const s of r.steers) {
    if (!has(events, "step.steer.queued", "steerId", s.steerId) && !has(events, "step.steer.delivered", "steerId", s.steerId)) {
      push("step.steer.queued", { stepId: s.stepId, steerId: s.steerId, agent: agentOfStep(loop, s.stepId) });
    }
  }
  return add.length === 0 ? events : [...events, ...add];
}
