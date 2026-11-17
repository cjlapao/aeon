import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ToastApi } from "@cjlapao/ui-kit";
import { notifications, registerNavigateHandler, registerOsNotifier, registerToastApi } from "../src/lib/notifications";

// S58 T8 — dedup by `attentionKey` (SV4/SV7/SV-D10) and the toast action
// (SV9). Direct on the module: `push` is callable from anywhere by design,
// and the dedup rule lives entirely inside it — the mounted-app proof that
// a daemon hint reaches push lives in notifications-golden (T1) and
// navigate-listener (T8). The window is the DAEMON's 10 minutes
// (internal/notify/dispatcher.go's `dedupWindow`); the boundary tests below
// are what keeps the two constants honest to each other.

interface ToastCall {
  tone: string;
  title?: unknown;
  body?: unknown;
  opts?: { meta?: string; actions?: { label: unknown; onClick?: () => void }[] };
}

function fakeToastApi() {
  const calls: ToastCall[] = [];
  const rec = (tone: string) => (title?: unknown, body?: unknown, opts?: ToastCall["opts"]) => {
    calls.push({ tone, title, body, opts });
    return calls.length;
  };
  const api = {
    show: () => 0,
    info: rec("info"),
    success: rec("success"),
    warning: rec("warning"),
    danger: rec("danger"),
    neutral: rec("neutral"),
    update: () => {},
    close: () => {},
    closeGroup: () => {},
    clear: () => {},
    onClose: () => () => {},
    onLifeEnd: () => () => {},
  } as unknown as ToastApi;
  return { api, calls };
}

let toast: ReturnType<typeof fakeToastApi>;
let osCalls: unknown[];

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-11-03T12:00:00Z"));
  localStorage.clear();
  notifications.clear();
  toast = fakeToastApi();
  osCalls = [];
  registerToastApi(toast.api);
  registerOsNotifier((input) => osCalls.push(input));
});
afterEach(() => {
  registerToastApi(null);
  registerOsNotifier(null);
  registerNavigateHandler(null);
  vi.useRealTimers();
});

describe("dedup by attentionKey (SV-D10, app half)", () => {
  it("dedups the same attentionKey inside the window", () => {
    notifications.push({ title: "A question waits", tone: "warning", source: "daemon", attentionKey: "question:x1" });
    vi.advanceTimersByTime(5 * 60 * 1000);
    const second = notifications.push({ title: "A question waits", tone: "warning", source: "daemon", attentionKey: "question:x1" });
    // One toast, one OS call, one history entry — and the second push
    // ANSWERS with the entry it deduped into, not a new one:
    expect(toast.calls).toHaveLength(1);
    expect(osCalls).toHaveLength(1);
    expect(notifications.getSnapshot()).toHaveLength(1);
    expect(second.id).toBe(notifications.getSnapshot()[0]?.id);
    // Two different subjects, two notifications — dedup is by subject,
    // not by silence:
    notifications.push({ title: "A gate waits", tone: "warning", source: "daemon", attentionKey: "gate:x2" });
    expect(toast.calls).toHaveLength(2);
    expect(notifications.getSnapshot()).toHaveLength(2);
  });

  it("the window is the daemon's ten minutes — boundary, not vibes", () => {
    notifications.push({ title: "Q", source: "daemon", attentionKey: "question:x1" });
    vi.advanceTimersByTime(9 * 60 * 1000 + 59 * 1000); // 9:59
    notifications.push({ title: "Q", source: "daemon", attentionKey: "question:x1" });
    expect(toast.calls).toHaveLength(1); // still deduped
    vi.advanceTimersByTime(2 * 60 * 1000); // now 11:59 from the first
    notifications.push({ title: "Q", source: "daemon", attentionKey: "question:x1" });
    expect(toast.calls).toHaveLength(2); // window passed: a real re-tell
    expect(notifications.getSnapshot()).toHaveLength(2);
  });

  it("a hint with NO key behaves exactly as today (the T1 golden rule)", () => {
    notifications.push({ title: "A", source: "daemon" });
    notifications.push({ title: "A", source: "daemon" });
    expect(toast.calls).toHaveLength(2);
    expect(osCalls).toHaveLength(2);
    expect(notifications.getSnapshot()).toHaveLength(2);
  });

  it("history entries carry the key additively (the storage gate never moved)", () => {
    notifications.push({ title: "Q", source: "daemon", attentionKey: "question:x1", deepLink: "aeon://loop/x1" });
    const entry = notifications.getSnapshot()[0];
    expect(entry?.attentionKey).toBe("question:x1");
    expect(entry?.deepLink).toBe("aeon://loop/x1");
    // An entry pushed WITHOUT the new fields has exactly the pre-S58 shape —
    // which is the same shape the unchanged `isAppNotification` gate has
    // always accepted, so every stored entry written before S58 still loads
    // (`createPersistedStore` filters the parse through it unchanged).
    notifications.push({ title: "Old", source: "daemon" });
    const old = notifications.getSnapshot().find((item) => item.title === "Old");
    expect("attentionKey" in (old ?? {})).toBe(false);
    expect("deepLink" in (old ?? {})).toBe(false);
    // Pushed entries round-trip through the same JSON the store persists:
    const roundTripped = JSON.parse(JSON.stringify(old));
    expect(roundTripped).toMatchObject({ title: "Old", source: "daemon" });
  });
});

describe("the toast carries the way back (SV9)", () => {
  it("a deep-linked toast gets an Open action routed to the navigate handler", () => {
    const navigate = vi.fn();
    registerNavigateHandler(navigate);
    notifications.push({ title: "Q", source: "daemon", deepLink: "aeon://loop/x1" });
    const action = toast.calls[0]?.opts?.actions?.[0];
    expect(action?.label).toBe("Open");
    action?.onClick?.();
    expect(navigate).toHaveBeenCalledWith("aeon://loop/x1");
    expect(notifications.getSnapshot()[0]?.deepLink).toBe("aeon://loop/x1");
  });

  it("no deepLink — no action (the shape before S58, unchanged)", () => {
    notifications.push({ title: "Q", source: "daemon" });
    expect(toast.calls[0]?.opts?.actions).toBeUndefined();
  });

  it("the OS notifier receives the link (osNotify puts it in `extra`)", () => {
    notifications.push({ title: "Q", source: "daemon", deepLink: "aeon://loop/x1" });
    expect(osCalls[0]).toEqual({ title: "Q", source: "daemon", deepLink: "aeon://loop/x1" });
    void 0;
  });
});
