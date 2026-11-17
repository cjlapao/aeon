import type { MinionMood } from "../minion-avatar";
import type { ChatSession } from "./session";
import type { ChatItem } from "./types";

// An agent's mood, read off what the chat already knows (S76). One pure
// function, so the rail, the stream header and a bench demo cannot disagree:
//
//   asked a question, or waiting on an approval / a gate  → stuck
//   running, and the last thing it did was thinking       → thinking
//   running                                               → working
//   the agent the loop is parked on, while it waits        → stuck (earlier agents rest)
//   stopped, or between runs                              → idle
//   its loop finished                                     → done
//
// A blocked loop reads as stuck: somebody has to do something before it moves.

const owes = (item: ChatItem, agent: string): boolean => {
  if (item.kind === "question") return item.agent === agent && item.questions.some((q) => q.answer === undefined);
  if (item.kind === "tool") return item.agent === agent && Boolean(item.approval && item.approval.answer === undefined && item.status === "awaiting");
  return false;
};

export function agentMood(agent: string, items: readonly ChatItem[], session: Pick<ChatSession, "runs" | "loopStatus">): MinionMood {
  const run = [...session.runs].reverse().find((r) => r.agent === agent);
  if (!run) return "idle";
  if (items.some((i) => owes(i, agent))) return "stuck";
  if (run.state === "running") {
    for (let i = items.length - 1; i >= 0; i--) {
      const it = items[i];
      if (it && "agent" in it && it.agent === agent) return it.kind === "thinking" && it.streaming ? "thinking" : "working";
    }
    return "working";
  }
  // A step that ended while the loop waits on a person or is blocked: the person owes THAT agent something, so only
  // the agent whose step the loop is parked on shows it — the one that stopped, or the latest to have run. Agents
  // that finished earlier are at rest.
  const gate = items.some((i) => i.kind === "gate" && i.state === "awaiting");
  const latest = session.runs.reduce((a, r) => (r.stepId > a ? r.stepId : a), -1);
  const parkedHere = run.state === "stopped" || run.stepId === latest;
  if ((gate || session.loopStatus === "blocked" || session.loopStatus === "waiting") && parkedHere) return "stuck";
  if (session.loopStatus === "finished") return "done";
  return "idle";
}
