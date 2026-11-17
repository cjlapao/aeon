import { onEvent, rpc, rpcTyped, type Unlisten } from "./bridge";
import { gapTracker, reportEventGap } from "./eventGap";
import type { AeonEvent } from "./protocol";

/**
 * One event subscription per window, with the fan-out done here.
 *
 * Every screen that wanted live events used to open its own subscription. The daemon delivers an event once per matching
 * subscription, so five screens watching `loop.*` meant five deliveries of every loop event through one bounded queue,
 * and each screen then had to filter the rest. The hub opens ONE subscription for the union of what the mounted screens
 * ask for, hands each event to the handlers whose patterns match it, and re-opens the subscription (new one first, then
 * the old one closed, so nothing falls in between) when the union changes or the connection starts a new generation.
 *
 * An event in the overlap is delivered twice by two subscriptions; the log's `seq` is unique per event, so a recent-seq set
 * drops the repeat. A jump in the live subscription's delivery counter (`dseq`) means the daemon dropped something: that is
 * reported, and the window re-reads everything.
 */
type Handler = { patterns: string[]; fn: (event: AeonEvent) => void };

/** The pattern for "any event that carries a notify hint": the daemon decides which those are, so it is not a kind. */
export const NOTIFY_PATTERN = "@notify";

export const kindMatchesAny = (patterns: readonly string[], kind: string, hinted = false): boolean =>
  patterns.some((p) => (p === NOTIFY_PATTERN ? hinted : p === "*" || p === kind || (p.endsWith("*") && kind.startsWith(p.slice(0, -1)))));

/** What the one subscription must carry: every kind asked for (everything when one asks for `*`), and the hinted events when asked. */
export function unionFilter(handlers: Iterable<Handler>): { kinds: string[]; notifyOnly?: true } {
  const all = new Set<string>();
  let notify = false;
  for (const h of handlers) for (const p of h.patterns) { if (p === NOTIFY_PATTERN) notify = true; else all.add(p); }
  if (all.has("*")) return { kinds: [] };
  const kinds = [...all].sort();
  return notify ? { kinds, notifyOnly: true } : { kinds };
}

const RECENT = 4096;

class EventHub {
  private readonly handlers = new Set<Handler>();
  private generation = -1;
  private liveKey: string | undefined;
  private liveId = "";
  private track: ((dseq: number | undefined) => void) | undefined;
  private stopListening: Unlisten | undefined;
  private listening: Promise<void> | undefined;
  private chain: Promise<void> = Promise.resolve();
  /** Every subscription this hub has opened: events of any other subscription are not ours to hand to a screen. */
  private ours = new Set<string>();
  private recent = new Set<number>();
  private order: number[] = [];

  subscribe(generation: number, patterns: string[], fn: (event: AeonEvent) => void): () => void {
    const handler: Handler = { patterns, fn };
    this.handlers.add(handler);
    if (generation !== this.generation) { this.generation = generation; this.liveKey = undefined; } // a new connection: start over
    this.sync();
    return () => {
      this.handlers.delete(handler);
      // Nothing is watching: the overlap bookkeeping has no one to protect, and a restarted window starts clean.
      if (this.handlers.size === 0) { this.recent.clear(); this.order = []; }
      this.sync();
    };
  }

  private sync(): void {
    this.chain = this.chain.then(() => this.apply()).catch(() => undefined);
  }

  private async apply(): Promise<void> {
    if (this.handlers.size === 0) {
      if (this.liveId) void rpc("unsubscribe", { subscriptionId: this.liveId }).catch(() => undefined);
      this.liveId = ""; this.liveKey = undefined; this.track = undefined; this.ours.clear();
      this.stopListening?.(); this.stopListening = undefined; this.listening = undefined;
      return;
    }
    const filter = unionFilter(this.handlers);
    const key = `${filter.kinds.join("|") || "*"}${filter.notifyOnly ? "+notify" : ""}`;
    if (key === this.liveKey) return;
    this.listening ??= onEvent((raw) => this.dispatch(raw as AeonEvent)).then((stop) => { this.stopListening = stop; });
    await this.listening;
    const previous = this.liveId;
    const sub = await rpcTyped("subscribe", { filter });
    // The new subscription is live: switch to it, and only then close the old one.
    this.liveId = sub.subscriptionId;
    this.ours.add(sub.subscriptionId);
    this.liveKey = key;
    this.track = gapTracker(() => reportEventGap());
    if (previous && previous !== sub.subscriptionId) void rpc("unsubscribe", { subscriptionId: previous }).catch(() => undefined);
  }

  private dispatch(event: AeonEvent): void {
    if (event.subscriptionId && this.liveId !== "" && !this.ours.has(event.subscriptionId)) return;
    // Only the live subscription's counter is followed; the old one is draining during a switch.
    if (this.liveId !== "" && event.subscriptionId === this.liveId) this.track?.(event.dseq);
    if (typeof event.seq === "number") {
      if (this.recent.has(event.seq)) return;
      this.recent.add(event.seq);
      this.order.push(event.seq);
      if (this.order.length > RECENT) this.recent.delete(this.order.shift()!);
    }
    for (const h of [...this.handlers]) if (kindMatchesAny(h.patterns, event.kind, event.notify != null)) h.fn(event);
  }
}

export const eventHub = new EventHub();
