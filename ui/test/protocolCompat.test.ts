import { describe, expect, it } from "vitest";
import { checkProtocolCompat } from "../src/lib/protocolCompat";
import { MIN_COMPATIBLE_APP_PROTOCOL_VERSION } from "../src/lib/protocol";
import type { LiveState } from "../src/lib/live";

describe("checkProtocolCompat — the one place App.tsx and daemonFooter.tsx both read", () => {
  it("allows writes at the exact ceiling this app understands", () => {
    const live: LiveState = { state: "current", endpoint: "e", daemonVersion: "1.0", protocolVersion: MIN_COMPATIBLE_APP_PROTOCOL_VERSION, generation: 1 };
    const result = checkProtocolCompat(live);
    expect(result).toEqual({ protocolVersion: MIN_COMPATIBLE_APP_PROTOCOL_VERSION, canWrite: true, mismatched: false, explanation: undefined });
  });

  it("allows writes below the ceiling", () => {
    const live: LiveState = { state: "current", endpoint: "e", daemonVersion: "1.0", protocolVersion: MIN_COMPATIBLE_APP_PROTOCOL_VERSION - 1, generation: 1 };
    expect(checkProtocolCompat(live).canWrite).toBe(true);
  });

  it("flags a mismatch, with an explanation, and disables writes above the ceiling", () => {
    const over = MIN_COMPATIBLE_APP_PROTOCOL_VERSION + 1;
    const live: LiveState = { state: "current", endpoint: "e", daemonVersion: "9.9", protocolVersion: over, generation: 1 };
    const result = checkProtocolCompat(live);
    expect(result.canWrite).toBe(false);
    expect(result.mismatched).toBe(true);
    expect(result.explanation).toContain(`protocol ${MIN_COMPATIBLE_APP_PROTOCOL_VERSION}`);
    expect(result.explanation).toContain(`negotiated ${over}`);
  });

  it("still reports a mismatch while stale (last known version), but never allows writes", () => {
    const over = MIN_COMPATIBLE_APP_PROTOCOL_VERSION + 1;
    const live: LiveState = { state: "stale", endpoint: "e", since: 0, cause: "lost", retry: 0, daemonVersion: "9.9", protocolVersion: over };
    const result = checkProtocolCompat(live);
    expect(result.mismatched).toBe(true);
    expect(result.canWrite).toBe(false);
  });

  it("reports neither canWrite nor mismatched before a protocol version is known at all", () => {
    for (const live of [
      { state: "connecting", endpoint: "e" },
      { state: "disconnected", endpoint: "e", reason: "down" },
      { state: "stale", endpoint: "e", since: 0, cause: "lost", retry: 0 },
    ] satisfies LiveState[]) {
      expect(checkProtocolCompat(live)).toEqual({ canWrite: false, mismatched: false });
    }
  });
});
