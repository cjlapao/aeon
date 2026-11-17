// S31 T8 (D10): the typed terminal listener on the aeon://terminal channel.
//
// Mirrors `bridge-errors.test.tsx`'s subscriber seam — nothing here imports
// Tauri; the fake `Subscriber` records the channel it was asked for and hands
// back the handler, so these tests fire the shell's payloads and assert what
// the listener sees.
import { afterEach, describe, expect, it } from "vitest";
import {
  CODE_LISTENER_UNAVAILABLE,
  EVENT_CHANNEL,
  TERMINAL_CHANNEL,
  onEvent,
  onTerminal,
  resetBridge,
  setSubscriber,
  type EventListener,
  type TerminalNotification,
} from "../src/lib/bridge";

afterEach(() => {
  resetBridge();
});

/** Capture the (channel, handler) pairs a subscriber call installs. */
function captureSubscribers(): {
  subs: { channel: string; handler: EventListener }[];
  fire: (channel: string, payload: unknown) => void;
} {
  const subs: { channel: string; handler: EventListener }[] = [];
  setSubscriber(async (channel, handler) => {
    subs.push({ channel, handler });
    return () => {};
  });
  const fire = (channel: string, payload: unknown) => {
    for (const s of subs) if (s.channel === channel) s.handler(payload);
  };
  return { subs, fire };
}

describe("onTerminal", () => {
  it("subscribes to aeon://terminal and routes matching notifications verbatim", async () => {
    const { subs, fire } = captureSubscribers();
    const received: TerminalNotification[] = [];

    await onTerminal((n) => received.push(n));

    expect(subs.map((s) => s.channel)).toEqual([TERMINAL_CHANNEL]);
    // The channel is the wire contract with session.rs's TERMINAL_CHANNEL.
    expect(TERMINAL_CHANNEL).toBe("aeon://terminal");

    // The shapes brief §5 publishes; identity (toBe), not just equality —
    // the bridge must not decode, re-key or rebuild the payload on the way.
    const output = { terminalId: "t-1", seq: 7, data: "ZWNobyBoaXQK" };
    const exit = { terminalId: "t-1", exitCode: 3 };
    fire(TERMINAL_CHANNEL, output);
    fire(TERMINAL_CHANNEL, exit);

    expect(received).toHaveLength(2);
    expect(received[0]).toBe(output);
    expect(received[1]).toBe(exit);
  });

  it("ignores non-terminal payloads, including event envelopes", async () => {
    const { fire } = captureSubscribers();
    const received: unknown[] = [];
    await onTerminal((n) => received.push(n));

    // An event envelope on the terminal channel is a mis-routed frame, not a
    // terminal notification: it has no terminalId and must not reach the
    // screen, where it would render as nonsense.
    fire(TERMINAL_CHANNEL, { kind: "step.tool.end", seq: 3, payload: {} });
    // Neither is junk of every other shape — the gate is allow-list, not
    // deny-list: anything unrecognised is dropped.
    fire(TERMINAL_CHANNEL, null);
    fire(TERMINAL_CHANNEL, "ZWNobyBoaXQK");
    fire(TERMINAL_CHANNEL, { terminalId: 5, seq: 1, data: "x" });
    fire(TERMINAL_CHANNEL, {});
    // A terminalId with no output half and no exit half is not the published
    // shape of either method (brief §5), so it is dropped rather than guessed.
    fire(TERMINAL_CHANNEL, { terminalId: "t-1" });
    // A signal-only exit is a real exit and a codeless string is not.
    fire(TERMINAL_CHANNEL, { terminalId: "t-2", signal: "SIGTERM" });

    expect(received).toEqual([{ terminalId: "t-2", signal: "SIGTERM" }]);
  });

  it("keeps the terminal and event channels apart", async () => {
    const { subs, fire } = captureSubscribers();
    const events: unknown[] = [];
    const terminals: unknown[] = [];

    await onEvent((e) => events.push(e));
    await onTerminal((n) => terminals.push(n));

    expect(subs.map((s) => s.channel).sort()).toEqual([EVENT_CHANNEL, TERMINAL_CHANNEL].sort());

    // Event traffic on the terminal channel is dropped…
    fire(EVENT_CHANNEL, { kind: "loop.advanced", seq: 9 });
    expect(events).toHaveLength(1);
    expect(terminals).toHaveLength(0);

    // …and terminal traffic on the event channel is dropped by onEvent's
    // isKnownEventKind gate — terminal notifications are NOT event kinds,
    // so the event gate must (and does) refuse them.
    fire(TERMINAL_CHANNEL, { terminalId: "t-1", seq: 1, data: "bQ==" });
    expect(events).toHaveLength(1);
    expect(terminals).toEqual([{ terminalId: "t-1", seq: 1, data: "bQ==" }]);
  });

  it("reports a failed subscription as the three-part listener-unavailable error", async () => {
    setSubscriber(async () => {
      throw new Error("event.listen not allowed. Permission core:event:allow-listen is missing");
    });

    await expect(onTerminal(() => {})).rejects.toMatchObject({
      name: "RpcError",
      code: CODE_LISTENER_UNAVAILABLE,
      message: "the app could not install its event listener",
      data: { fatal: false },
    });

    // The three parts, each carrying its weight: what names the cause, means
    // and todo are actionable. Same wording onEvent's callers produce, so the
    // remedy reads the same wherever the failure surfaces.
    const err = await onTerminal(() => {}).catch((e) => e);
    expect(err.data.what).toContain("event.listen not allowed");
    expect(err.data.means.length).toBeGreaterThan(0);
    expect(err.data.todo).toContain("core:event");
  });
});
