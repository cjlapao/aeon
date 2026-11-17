import { useEffect, useRef } from "react";
import { eventHub } from "./eventHub";
import type { AeonEvent } from "./protocol";
import type { LiveState } from "./live";

/**
 * Subscribe to the daemon's event stream for as long as the connection is
 * current, re-subscribing after a reconnect and cleaning up on unmount —
 * the lifecycle every screen that wants live updates used to hand-roll
 * (`HomeScreen.tsx`'s original "Live updates" effect is where this was
 * lifted from).
 *
 * `kinds` is the same filter language the daemon speaks: an exact kind
 * (`"loop.advanced"`), a trailing-`*` prefix (`"loop.*"`), or `"*"` for
 * everything — the shape the global notification listener uses. Pass a
 * fresh array literal each render; the hook keys its own effect on a stable
 * join of the kinds, so that alone never causes a resubscribe.
 */
export function useLiveSubscription(live: LiveState, kinds: string[], handler: (event: AeonEvent) => void): void {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  const kindsKey = kinds.join("|");

  useEffect(() => {
    if (live.state !== "current") return;
    // The window keeps ONE subscription for everything its screens watch (eventHub.ts); this hook only adds its patterns.
    // live.generation is the restart trigger: a reconnect starts over rather than resuming the old subscription, matching how
    // every screen's reader already re-reads full state on reconnect.
    return eventHub.subscribe(live.generation, kindsKey.split("|"), (event) => handlerRef.current(event));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live.state === "current" ? live.generation : -1, kindsKey]);
}
