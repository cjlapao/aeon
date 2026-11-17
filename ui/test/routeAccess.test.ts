import { describe, expect, it } from "vitest";
import { routeAccess } from "../src/app/routeAccess";
import { MIN_COMPATIBLE_APP_PROTOCOL_VERSION } from "../src/lib/protocol";
import type { AppRoute } from "../src/app/routes";
import type { LiveState } from "../src/lib/live";

const bench: AppRoute = { id: "bench", path: "/bench/:page?", title: "Test bench", access: "compatible-daemon" };
const open: AppRoute = { id: "home", path: "/", title: "Home", access: "open" };

// This gate used to carry its own hardcoded `protocolVersion > 1` comparison,
// a second copy of the rule `checkProtocolCompat` now owns — and it drifted
// behind S38's protocol bump exactly like the constant did, quietly refusing
// the test bench route against a daemon this app was fully compatible with.
describe("routeAccess — the compatible-daemon gate reads the one compatibility verdict", () => {
  it("allows the bench route at the exact protocol ceiling this app understands", () => {
    const live: LiveState = { state: "current", endpoint: "e", daemonVersion: "1.0", protocolVersion: MIN_COMPATIBLE_APP_PROTOCOL_VERSION, generation: 1 };
    expect(routeAccess(bench, live)).toEqual({ allowed: true });
  });

  it("refuses the bench route, naming both versions, once the daemon is ahead of this app", () => {
    const over = MIN_COMPATIBLE_APP_PROTOCOL_VERSION + 1;
    const live: LiveState = { state: "current", endpoint: "e", daemonVersion: "9.9", protocolVersion: over, generation: 1 };
    const decision = routeAccess(bench, live);
    expect(decision.allowed).toBe(false);
    if (!decision.allowed) {
      expect(decision.reason).toContain(`protocol ${MIN_COMPATIBLE_APP_PROTOCOL_VERSION}`);
      expect(decision.reason).toContain(`negotiated ${over}`);
    }
  });

  it("never gates an 'open' route on protocol compatibility", () => {
    const over = MIN_COMPATIBLE_APP_PROTOCOL_VERSION + 1;
    const live: LiveState = { state: "current", endpoint: "e", daemonVersion: "9.9", protocolVersion: over, generation: 1 };
    expect(routeAccess(open, live)).toEqual({ allowed: true });
  });

  it("never gates a connection that isn't current (nothing negotiated yet to compare)", () => {
    const live: LiveState = { state: "connecting", endpoint: "e" };
    expect(routeAccess(bench, live)).toEqual({ allowed: true });
  });
});
