/**
 * Lost events, and the one answer to them.
 *
 * The daemon hands events to the app through a bounded queue and drops what does not fit, so a screen that only reacts to
 * events can silently keep an old picture. Each subscription's events carry their own counter (`dseq`); a jump in it means
 * something was dropped. Events are hints to look again, never the data, so the answer to a gap is to look again: every
 * displayed reader runs and every query refetches, exactly as after a reconnect. The window's live controller registers
 * how; the subscription hook reports the gap.
 */
let resync: (() => void) | undefined;

/** The controller says how to re-read everything. Returns how to stop. */
export function registerResync(fn: () => void): () => void {
  resync = fn;
  return () => { if (resync === fn) resync = undefined; };
}

/** A subscription saw a jump in its delivery counter. */
export function reportEventGap(): void {
  resync?.();
}

/**
 * Tracks one subscription's counter. `seen` says whether this event is in order (or the first), and reports a gap when
 * one or more deliveries are missing. A counter that goes backwards (a resubscribe restarts it) is not a gap.
 */
export function gapTracker(onGap: (missing: number) => void): (dseq: number | undefined) => void {
  let last = 0;
  return (dseq) => {
    if (dseq === undefined || dseq <= 0) return;
    if (last > 0 && dseq > last + 1) onGap(dseq - last - 1);
    last = dseq;
  };
}
