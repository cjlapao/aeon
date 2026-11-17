import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { kindMatchesAny, unionFilter } from "../src/lib/eventHub";
import { useLiveSubscription } from "../src/lib/useLiveSubscription";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

afterEach(() => { cleanup(); resetFakeDaemon(); });
const live = { state: "current" as const, endpoint: "e", daemonVersion: "1", protocolVersion: 1, generation: 1 };

describe("one subscription for the whole window", () => {
  it("matches patterns the way every screen asks, and widens to everything for a star", () => {
    expect(kindMatchesAny(["loop.*", "question.asked"], "loop.waiting")).toBe(true);
    expect(kindMatchesAny(["loop.*", "question.asked"], "question.asked")).toBe(true);
    expect(kindMatchesAny(["loop.*"], "model.delta")).toBe(false);
    expect(kindMatchesAny(["*"], "anything")).toBe(true);
    expect(unionFilter([{ patterns: ["loop.*", "todo.*"], fn: vi.fn() }, { patterns: ["loop.*"], fn: vi.fn() }])).toEqual({ kinds: ["loop.*", "todo.*"] });
    expect(unionFilter([{ patterns: ["loop.*"], fn: vi.fn() }, { patterns: ["*"], fn: vi.fn() }])).toEqual({ kinds: [] });
    expect(unionFilter([{ patterns: ["@notify"], fn: vi.fn() }])).toEqual({ kinds: [], notifyOnly: true });
    expect(unionFilter([{ patterns: ["@notify"], fn: vi.fn() }, { patterns: ["loop.*"], fn: vi.fn() }])).toEqual({ kinds: ["loop.*"], notifyOnly: true });
    expect(kindMatchesAny(["@notify"], "x", true)).toBe(true);
    expect(kindMatchesAny(["@notify"], "x", false)).toBe(false);
  });

  it("serves several screens from one subscription, hands each only what it asked for, and never twice", async () => {
    let n = 0;
    const daemon = installFakeDaemon({ responses: { subscribe: () => ({ subscriptionId: `s${++n}`, from: 1 }), unsubscribe: {} } });
    const loops = vi.fn(); const tasks = vi.fn(); const loopsAgain = vi.fn();
    renderHook(() => useLiveSubscription(live, ["loop.*"], loops));
    renderHook(() => useLiveSubscription(live, ["todo.*"], tasks));
    renderHook(() => useLiveSubscription(live, ["loop.*"], loopsAgain));
    await waitFor(() => expect(daemon.paramsOf("subscribe")).toEqual({ filter: { kinds: ["loop.*", "todo.*"] } }));
    await new Promise((r) => setTimeout(r, 30));
    const live1 = `s${n}`;
    act(() => daemon.emit({ subscriptionId: live1, seq: 10, dseq: 1, kind: "loop.waiting", at: "t" }));
    act(() => daemon.emit({ subscriptionId: live1, seq: 11, dseq: 2, kind: "todo.updated", at: "t" }));
    act(() => daemon.emit({ subscriptionId: live1, seq: 11, dseq: 3, kind: "todo.updated", at: "t" }));   // the same event again (a switch overlap): once
    await waitFor(() => expect(tasks).toHaveBeenCalledTimes(1));
    await new Promise((r) => setTimeout(r, 30));
    expect(loops).toHaveBeenCalledTimes(1);
    expect(loopsAgain).toHaveBeenCalledTimes(1);
    expect(tasks).toHaveBeenCalledTimes(1);     // the repeat of seq 11 was dropped
    expect(daemon.calls.filter((c) => c.method === "subscribe").length).toBeLessThanOrEqual(3);   // growing the union opens a new one, never one per screen at once
  });

  it("closes the old subscription only after the new one is open, and everything when the last screen leaves", async () => {
    let n = 0;
    const daemon = installFakeDaemon({ responses: { subscribe: () => ({ subscriptionId: `s${++n}`, from: 1 }), unsubscribe: {} } });
    const a = renderHook(() => useLiveSubscription(live, ["loop.*"], vi.fn()));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const b = renderHook(() => useLiveSubscription(live, ["todo.*"], vi.fn()));
    await waitFor(() => expect(daemon.methods().filter((m) => m === "subscribe")).toHaveLength(2));
    await waitFor(() => expect(daemon.paramsOf("unsubscribe")).toEqual({ subscriptionId: "s1" }));
    const order = daemon.methods().filter((m) => m === "subscribe" || m === "unsubscribe");
    expect(order).toEqual(["subscribe", "subscribe", "unsubscribe"]);
    a.unmount(); b.unmount();
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "unsubscribe").length).toBeGreaterThanOrEqual(2));
  });
});
