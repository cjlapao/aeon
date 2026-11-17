import type { AeonEvent } from "../../lib/protocol";

/**
 * The loop's journal, as the page uses it (loop-replay).
 *
 * Every event the daemon publishes is stored; `loop.events` reads one loop's. The page feeds those events through
 * the SAME reducer it uses for live ones, so a reopened loop shows what a live one did. This file holds the pure
 * parts: merging windows, deciding which steps the journal fully covers, and reading the loop's true status from
 * the events when the saved status lags behind them.
 */

/** One row of `loop.events`, which is a live event without the subscription tag. */
export interface JournalRow { seq: number; kind: string; at: string; componentRoot?: string; stepId?: number; payload?: unknown }
export interface JournalWindow { events: JournalRow[]; hasOlder: boolean; hasNewer: boolean; head: number }

export const JOURNAL_WINDOW = 400;

const asEvent = (row: JournalRow): AeonEvent => ({ subscriptionId: "journal", seq: row.seq, kind: row.kind, at: row.at, componentRoot: row.componentRoot, payload: row.payload } as AeonEvent);

/** Windows joined by seq: sorted, with an event seen twice (a window and the live tail overlap) kept once. */
export function mergeEvents(current: readonly AeonEvent[], incoming: readonly (AeonEvent | JournalRow)[]): AeonEvent[] {
  const bySeq = new Map<number, AeonEvent>();
  for (const e of current) bySeq.set(e.seq, e);
  for (const e of incoming) if (!bySeq.has(e.seq)) bySeq.set(e.seq, "subscriptionId" in e ? e : asEvent(e));
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq);
}

const stepOf = (event: AeonEvent): number | undefined => {
  const v = ((event.payload ?? {}) as Record<string, unknown>).stepId;
  return typeof v === "number" ? v : undefined;
};

/** Steps whose model turns are in the events: a finished-turn event or streamed text. Only these can be drawn from events alone. */
export function stepsWithTurns(events: readonly AeonEvent[]): Set<number> {
  const out = new Set<number>();
  for (const e of events) {
    if (e.kind !== "step.turn.completed" && e.kind !== "model.delta") continue;
    const id = stepOf(e);
    if (id !== undefined) out.add(id);
  }
  return out;
}

const DETAIL = new Set(["step.tool.start", "step.tool.end", "step.tool.skipped", "step.policy.decided", "model.delta"]);

/**
 * A loop that ran before the journal recorded finished turns has step events without the text around them. For
 * such a step the transcript supplies both the words and the calls, so the journal's tool rows for it are left out
 * rather than drawn twice. A step with turns keeps everything.
 */
export function withoutLegacyDetail(events: readonly AeonEvent[], covered: ReadonlySet<number>): AeonEvent[] {
  return events.filter((e) => {
    if (!DETAIL.has(e.kind)) return true;
    const id = stepOf(e);
    return id === undefined || covered.has(id);
  });
}

/**
 * The loop's status as the events say it is. The saved status stays `waiting` until a resumed step ends, so a long
 * step shows "answer requested" for its whole run. Waiting is only true while a question is owed: if every question
 * asked has been answered and the agent has acted since the last ask, the loop is running.
 */
export function statusFromEvents(status: string, events: readonly AeonEvent[]): string {
  if (status !== "waiting") return status;
  const asked = new Set<number>();
  let lastWait = 0;
  let activity = 0;
  for (const e of events) {
    const p = (e.payload ?? {}) as Record<string, unknown>;
    if (e.kind === "question.asked" && typeof p.id === "number") { asked.add(p.id); lastWait = e.seq; }
    else if (e.kind === "question.answered" && typeof p.questionId === "number") asked.delete(p.questionId);
    else if (e.kind === "loop.waiting") { lastWait = e.seq; if (typeof p.questionId === "number") asked.add(p.questionId); }
    else if (e.kind === "loop.advanced" || e.kind === "loop.blocked" || e.kind === "loop.finished" || e.kind === "loop.resumed") { asked.clear(); activity = Math.max(activity, e.seq); }
    else if (e.kind === "step.tool.start" || e.kind === "step.tool.end" || e.kind === "step.turn.completed" || e.kind === "model.delta") activity = Math.max(activity, e.seq);
  }
  return asked.size === 0 && activity > lastWait && lastWait > 0 ? "running" : status;
}

const payloadOf = (e: AeonEvent): Record<string, unknown> => (e.payload ?? {}) as Record<string, unknown>;

/** Two streamed chunks of the same step, turn and kind: one growing run of text. */
const sameRun = (a: AeonEvent, b: AeonEvent): boolean => {
  if (a.kind !== "model.delta" || b.kind !== "model.delta") return false;
  const x = payloadOf(a);
  const y = payloadOf(b);
  return x.stepId === y.stepId && x.iteration === y.iteration && x.tool === y.tool;
};

/**
 * Live events folded into the list the page keeps. Consecutive streamed chunks of one run become ONE event (text
 * joined, the first chunk's seq kept so the row's identity never changes): a turn that streams a thousand chunks is
 * one event, not a thousand, so rebuilding the chat from the list stays cheap and the bounded window is not eaten by
 * chunks. Events already held (by seq) are ignored; the result stays in seq order and at most `keep` long.
 */
export function appendLive(current: readonly AeonEvent[], incoming: readonly AeonEvent[], keep: number): AeonEvent[] {
  const out = current.slice();
  const seen = new Set(out.map((e) => e.seq));
  let ordered = true;
  for (const event of incoming) {
    if (seen.has(event.seq)) continue;
    seen.add(event.seq);
    const last = out[out.length - 1];
    if (last && sameRun(last, event) && event.seq > last.seq) {
      out[out.length - 1] = { ...last, payload: { ...payloadOf(last), text: `${String(payloadOf(last).text ?? "")}${String(payloadOf(event).text ?? "")}` } } as AeonEvent;
      continue;
    }
    if (last && event.seq < last.seq) ordered = false;
    out.push(event);
  }
  if (!ordered) out.sort((a, b) => a.seq - b.seq);
  return out.length > keep ? out.slice(-keep) : out;
}
