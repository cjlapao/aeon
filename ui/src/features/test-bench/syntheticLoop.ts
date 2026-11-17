import type { ChatEventLike } from "../../components/agent-chat";
import type { LoopDetail } from "../loops/pages/LoopScreen";

/** One lane of a scripted loop. */
export interface ScenarioLane { id: string; kind: "model-step" | "gate"; produces?: string[] }

/**
 * A `loop.show` snapshot, built from the events a scenario has played so far.
 *
 * The bench has no daemon to ask, so the full-page mode derives the loop's
 * state the way the real one reports it: a dispatched lane is running; a
 * lane the loop moved past is done; a gate the loop is parked at is waiting
 * with its answers; a tool call parked on a permission question makes the
 * loop wait at the lane that call belongs to, until it is answered.
 */
export function syntheticLoop(lanes: readonly ScenarioLane[], events: readonly ChatEventLike[], root = ""): LoopDetail {
  const status: Record<string, string> = Object.fromEntries(lanes.map((l) => [l.id, "not-started"]));
  const stepId: Record<string, number | undefined> = {};
  const laneOfStep = new Map<number, string>();
  const parked = new Map<number, number>(); // permission question id → the step it parks
  let current = lanes[0]?.id ?? "";
  let gateAnswers: string[] | undefined;
  let gatePrompt: string | undefined;
  let outcome: "done" | "blocked" | undefined;

  const str = (v: unknown) => (typeof v === "string" ? v : undefined);
  for (const e of [...events].sort((a, b) => a.seq - b.seq)) {
    const p = (e.payload ?? {}) as Record<string, unknown>;
    switch (e.kind) {
      case "loop.step.dispatched": {
        const lane = str(p.lane) ?? str(p.step);
        if (!lane || !(lane in status)) break;
        status[lane] = "running"; current = lane;
        if (typeof p.stepId === "number") { stepId[lane] ??= p.stepId; laneOfStep.set(p.stepId, lane); }
        break;
      }
      case "loop.advanced": {
        const lane = str(p.lane);
        if (lane && lane in status) {
          status[lane] = "ok";
          const next = lanes[lanes.findIndex((l) => l.id === lane) + 1];
          if (next) current = next.id;
          if (str(p.answer)) { gateAnswers = undefined; gatePrompt = undefined; }
        }
        break;
      }
      case "loop.waiting": {
        const lane = str(p.lane);
        if (Array.isArray(p.answers) && lane && lane in status) {
          status[lane] = "waiting"; current = lane;
          gateAnswers = p.answers.filter((a): a is string => typeof a === "string"); gatePrompt = str(p.prompt);
        }
        break;
      }
      case "question.asked":
        if (p.kind === "permission" && typeof p.id === "number" && typeof p.stepId === "number") parked.set(p.id, p.stepId);
        break;
      case "question.answered":
        if (typeof p.questionId === "number") parked.delete(p.questionId);
        break;
      case "loop.finished": outcome = "done"; break;
      case "loop.blocked": outcome = "blocked"; break;
    }
  }

  const parkedLane = [...parked.values()].map((s) => laneOfStep.get(s)).find(Boolean);
  const position = parkedLane ?? current;
  const loopStatus = outcome ?? (status[current] === "waiting" || parkedLane ? "waiting" : "running");
  return {
    id: "loop-demo", slug: "loop-demo", blueprint: "two-step", goal: "A scripted run", root, status: loopStatus,
    position: { lane: position },
    ...(gateAnswers ? { gateAnswers, gatePrompt } : {}),
    lanes: lanes.map((lane) => ({
      id: lane.id, status: status[lane.id]!, structure: "direct" as const, produces: lane.produces,
      steps: [{ id: lane.id, kind: lane.kind, status: status[lane.id]!, stepId: stepId[lane.id] }],
    })),
  };
}
