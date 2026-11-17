import type { ChatItem } from "./types";
import { formatDuration, Mono, Spinner, useNow } from "./parts";

/**
 * The line at the foot of the stream that says the loop is alive between events. A model turn can take a long
 * while before its first word, and nothing in the stream moves meanwhile; this says what is being waited for and
 * for how long, until the agent's next item arrives. It stays out of the way whenever something on screen already
 * shows the work (a streaming reply, a running call) or the loop is waiting on the person.
 */
export function workingLabel(items: readonly ChatItem[]): { agent?: string; since?: string; text: string } | undefined {
  const last = items[items.length - 1];
  if (!last) return { text: "Starting…" };
  if (last.kind === "question" || last.kind === "gate" || last.kind === "steer") return undefined;
  if (last.kind === "tool") {
    if (last.status === "awaiting" || last.status === "running") return undefined;
    return { agent: last.agent, since: last.at, text: "reading the result and thinking" };
  }
  if ((last.kind === "text" || last.kind === "thinking") && last.streaming) return undefined;
  const agent = "agent" in last ? last.agent : undefined;
  const since = "at" in last ? last.at : undefined;
  return { agent, since, text: last.kind === "thinking" ? "still thinking" : "waiting for the model" };
}

export function WorkingIndicator({ items }: { items: readonly ChatItem[] }) {
  const label = workingLabel(items);
  const now = useNow(Boolean(label));
  if (!label) return null;
  const since = label.since ? Date.parse(label.since) : NaN;
  const waited = Number.isFinite(since) ? now - since : undefined;
  return <div role="status" data-testid="agent-chat-working" className="flex items-center gap-2 py-3 text-sm text-slate-500 dark:text-slate-400!">
    <Spinner />
    <span>{label.agent ? `${label.agent} is working — ` : "Working — "}{label.text}…</span>
    {waited !== undefined && waited > 3000 && <Mono className="text-xs">{formatDuration(waited)}</Mono>}
  </div>;
}
