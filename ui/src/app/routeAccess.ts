import type { LiveState } from "../lib/live";
import { checkProtocolCompat } from "../lib/protocolCompat";
import type { AppRoute } from "./routes";

export type RouteDecision = { allowed: true } | { allowed: false; reason: string };

/**
 * One policy boundary for route access. Authentication rules can be added here later.
 *
 * The mismatch check itself comes from `checkProtocolCompat` — the one place that
 * decides this — rather than a second, local `protocolVersion > …` comparison. This
 * gate used to carry its own copy, hardcoded to `1`, which is exactly how it went
 * stale behind S38's protocol bump while `lib/protocol.ts`'s own constant did too:
 * nothing forced the two to agree, and the test bench route quietly refused to open
 * against a perfectly compatible daemon.
 */
export function routeAccess(route: AppRoute, live: LiveState): RouteDecision {
  if (route.access === "compatible-daemon" && live.state === "current") {
    const compat = checkProtocolCompat(live);
    if (compat.mismatched) return { allowed: false, reason: compat.explanation ?? "The daemon speaks a newer protocol than this app understands." };
  }
  return { allowed: true };
}
