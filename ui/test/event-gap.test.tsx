import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { gapTracker, registerResync, reportEventGap } from "../src/lib/eventGap";
import { LiveController, type LiveTransport } from "../src/lib/live";
import { useLiveSubscription } from "../src/lib/useLiveSubscription";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

afterEach(() => { cleanup(); resetFakeDaemon(); });

describe("noticing dropped events", () => {
  it("reports a jump in a subscription's delivery counter, and nothing else", () => {
    const gaps: number[] = [];
    const track = gapTracker((n) => gaps.push(n));
    for (const d of [undefined, 1, 2, 3, 6, 7, 1, 2]) track(d);   // 4 and 5 were lost; the counter restarting is a resubscribe, not a loss
    expect(gaps).toEqual([2]);
  });

  it("answers a gap with one re-read of everything: the readers run, then the generation moves", async () => {
    const transport: LiveTransport = { connect: async () => ({ daemonVersion: "1", protocolVersion: 1, endpoint: "e" }), endpointPath: async () => "e", onClosed: async () => () => undefined };
    let t = 10_000;
    const live = new LiveController(transport, () => t);
    await live.start();
    const reader = vi.fn(async () => undefined);
    live.registerReader(reader);
    const before = (live.getState() as { generation: number }).generation;
    reportEventGap();
    await waitFor(() => expect((live.getState() as { generation: number }).generation).toBe(before + 1));
    expect(reader).toHaveBeenCalledTimes(1);
    reportEventGap();                                          // a burst inside a second is one re-read
    await Promise.resolve();
    expect(reader).toHaveBeenCalledTimes(1);
    t += 1500;
    reportEventGap();
    await waitFor(() => expect(reader).toHaveBeenCalledTimes(2));
    live.stop();
    reportEventGap();                                          // a stopped controller does nothing
    await Promise.resolve();
    expect(reader).toHaveBeenCalledTimes(2);
  });

  it("a subscription's hook takes only its own events, and reports a gap in them", async () => {
    const resync = vi.fn();
    const off = registerResync(resync);
    const daemon = installFakeDaemon({ responses: { subscribe: { subscriptionId: "s1", from: 1 } } });
    const handler = vi.fn();
    renderHook(() => useLiveSubscription({ state: "current", endpoint: "e", daemonVersion: "1", protocolVersion: 1, generation: 1 }, ["loop.*"], handler));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    await new Promise((r) => setTimeout(r, 20));
    const ev = (over: Record<string, unknown>) => ({ subscriptionId: "s1", seq: 1, dseq: 1, kind: "loop.waiting", at: "t", ...over });
    act(() => daemon.emit(ev({ dseq: 1 })));
    act(() => daemon.emit(ev({ subscriptionId: "other", dseq: 9, seq: 2 })));   // another subscription's delivery: not this handler's
    expect(handler).toHaveBeenCalledTimes(1);
    expect(resync).not.toHaveBeenCalled();
    act(() => daemon.emit(ev({ dseq: 4, seq: 7 })));                            // 2 and 3 never arrived
    expect(handler).toHaveBeenCalledTimes(2);
    expect(resync).toHaveBeenCalledTimes(1);
    off();
  });
});
