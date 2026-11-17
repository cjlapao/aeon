import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { notifications, registerUnreadSink } from "../src/lib/notifications";

// The tray-icon mirror, module half: every mutation of the UNREAD count the
// bell wears must reach the registered sink (the mounted listener wires the
// sink to the shell's `tray_unread` command; the shell decides the icon's
// dot from it). The rule lives here because the mutations do: push adds one,
// markRead/markAllRead/clear drain them, and a DEDUPED push — which lands no
// entry — must not announce a number that never changed.

let sinkCalls: number[];

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-11-06T12:00:00Z"));
  localStorage.clear();
  notifications.clear();
  sinkCalls = [];
  registerUnreadSink((unread) => sinkCalls.push(unread));
});
afterEach(() => {
  registerUnreadSink(null);
  vi.useRealTimers();
});

describe("the unread sink mirrors the bell", () => {
  it("push raises the count, markRead and markAllRead drain it", () => {
    const a = notifications.push({ title: "A", source: "daemon" });
    notifications.push({ title: "B", source: "daemon" });
    expect(sinkCalls).toEqual([1, 2]);
    notifications.markRead(a.id);
    expect(sinkCalls.at(-1)).toBe(1);
    notifications.markAllRead();
    expect(sinkCalls.at(-1)).toBe(0);
    // A markAllRead over an all-read history changes nothing and announces
    // nothing newsworthy wrong: the sink just restates zero.
    notifications.markAllRead();
    expect(sinkCalls.at(-1)).toBe(0);
  });

  it("clear zeroes the count", () => {
    notifications.push({ title: "A", source: "daemon" });
    notifications.clear();
    expect(sinkCalls).toEqual([1, 0]);
  });

  it("a deduped push announces nothing (no entry landed, no count moved)", () => {
    notifications.push({ title: "Q", source: "daemon", attentionKey: "question:x1" });
    vi.advanceTimersByTime(60 * 1000);
    notifications.push({ title: "Q", source: "daemon", attentionKey: "question:x1" });
    expect(sinkCalls).toEqual([1]);
  });

  it("no sink registered — mutations behave exactly as before (browser build)", () => {
    registerUnreadSink(null);
    const before = notifications.getSnapshot().length;
    notifications.push({ title: "A", source: "daemon" });
    notifications.markAllRead();
    notifications.clear();
    expect(notifications.getSnapshot()).toHaveLength(before);
  });
});
