import { afterEach, describe, expect, it, vi } from "vitest";
import { LiveController, type LiveTransport } from "../src/lib/live";

afterEach(() => vi.useRealTimers());

function harness() {
  let available = true;
  let version = 1;
  let close: ((reason: string) => void) | undefined;
  const calls: string[] = [];
  const transport: LiveTransport = {
    endpointPath: async () => "/tmp/aeon/rpc.sock",
    onClosed: async (handler) => { close = handler; return () => { close = undefined; }; },
    connect: async () => {
      calls.push("connect");
      if (!available) throw new Error("daemon unavailable");
      return { daemonVersion: "1.0", protocolVersion: version, endpoint: "/tmp/aeon/rpc.sock" };
    },
  };
  return { transport, calls, close: (reason: string) => close?.(reason),
    setAvailable: (value: boolean) => { available = value; },
    setVersion: (value: number) => { version = value; } };
}

describe("live connection", () => {
  it("turns a drop into stale with an increasing age and socket path", async () => {
    vi.useFakeTimers();
    const h = harness();
    const live = new LiveController(h.transport, () => Date.now());
    await live.start();
    h.setAvailable(false);
    h.close("socket closed");
    expect(live.getState()).toMatchObject({ state: "stale", endpoint: "/tmp/aeon/rpc.sock", cause: "socket closed",
      daemonVersion: "1.0", protocolVersion: 1 });
    await vi.advanceTimersByTimeAsync(2500);
    expect(live.ageSeconds()).toBe(2);
    expect(live.getState().state).toBe("stale");
    expect(live.getState()).toMatchObject({ daemonVersion: "1.0", protocolVersion: 1 });
    live.stop();
  });

  it("keeps a silent live socket current", async () => {
    vi.useFakeTimers();
    const h = harness();
    const live = new LiveController(h.transport);
    await live.start();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    expect(live.getState().state).toBe("current");
    expect(h.calls).toEqual(["connect"]);
    live.stop();
  });

  it("re-reads displayed state before marking a restored connection current", async () => {
    vi.useFakeTimers();
    const h = harness();
    const live = new LiveController(h.transport);
    let serverValue = "before";
    let displayed = "";
    const read = vi.fn(async () => { displayed = serverValue; });
    live.registerReader(read);
    await live.start();
    expect(displayed).toBe("before");
    h.setAvailable(false);
    h.close("dropped");
    serverValue = "changed while disconnected";
    await vi.advanceTimersByTimeAsync(1000);
    expect(live.getState().state).toBe("stale");
    h.setAvailable(true);
    await vi.advanceTimersByTimeAsync(2000);
    expect(displayed).toBe("changed while disconnected");
    expect(read).toHaveBeenCalledTimes(2);
    expect(live.getState().state).toBe("current");
    live.stop();
  });

  it("distinguishes retrying from given up and allows manual reconnection", async () => {
    vi.useFakeTimers();
    const h = harness();
    const live = new LiveController(h.transport);
    await live.start();
    h.setAvailable(false);
    h.close("dropped");
    await vi.advanceTimersByTimeAsync(31_000);
    expect(live.getState()).toMatchObject({ state: "disconnected", endpoint: "/tmp/aeon/rpc.sock" });
    h.setAvailable(true);
    h.setVersion(99);
    await live.reconnect();
    expect(live.getState()).toMatchObject({ state: "current", protocolVersion: 99 });
    live.stop();
  });
});
