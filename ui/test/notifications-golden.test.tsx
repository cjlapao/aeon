import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { appSettings } from "../src/lib/appSettings";
import { notifications } from "../src/lib/notifications";
import { ProductApp } from "../src/app/router";

// S58 T1 — characterization goldens of the notification consumer, in the
// mounted app (notifications.test.tsx owns the storage/bell mechanics;
// this file owns the RULE as one pinned shape, because T4/T8 change what
// reaches it). Header, per tasks/T1.md: after S58 the hint carries
// attentionKey/deepLink and more kinds carry hints — these three
// behaviors must not move: (1) a hint-carrying event becomes a toast, a
// bell entry, and an OS notification when the window is unfocused;
// (2) a hint-less event is silent everywhere — the wire, not the UI,
// decides, and the class resolver must not change that for kinds the
// class table leaves off; (3) focus is the OS gate, not a mute — the
// toast and bell still land.
//
// The permission cache is module state (osNotify.ts); the Tauri plugin
// is mocked the same way osNotify.test.tsx mocks it, and reset between
// cases so each case proves the plugin call itself.

const isPermissionGranted = vi.fn();
const requestPermission = vi.fn();
const sendNotification = vi.fn();

vi.mock("@tauri-apps/plugin-notification", () => ({
  isPermissionGranted: () => isPermissionGranted(),
  requestPermission: () => requestPermission(),
  sendNotification: (options: unknown) => sendNotification(options),
}));

const { resetOsNotifyPermissionCache } = await import("../src/lib/osNotify");

const BASE_RESPONSES = {
  "loop.list": { loops: [], all: true },
  "component.list": { components: [] },
  "blueprint.list": { names: [] },
  "routes.list": { routes: [], providers: [], persisted: true },
  subscribe: { subscriptionId: "s1", from: 0 },
};

async function mountWithGlobalListener() {
  const daemon = installFakeDaemon({ responses: { ...BASE_RESPONSES } });
  render(<ProductApp />);
  await screen.findByTestId("product-home");
  // The NotificationListener's own broad subscribe must have landed
  // before anything is emitted, or the emit races its setup — the same
  // wait notifications.test.tsx learned the hard way.
  await waitFor(() => {
    const globalSub = daemon.calls.some((call) => {
      const params = call.params as { filter?: { kinds?: unknown[] } } | undefined;
      return call.method === "subscribe" && Array.isArray(params?.filter?.kinds) && params.filter.kinds.length === 0;
    });
    expect(globalSub).toBe(true);
  });
  return daemon;
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  window.location.hash = "#/";
  notifications.clear();
  resetOsNotifyPermissionCache();
  isPermissionGranted.mockReset().mockResolvedValue(true);
  requestPermission.mockReset();
  sendNotification.mockReset();
});
afterEach(() => {
  cleanup();
  resetFakeDaemon();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  act(() => appSettings.reset());
  notifications.clear();
});

describe("S58 T1 notification-consumer goldens", () => {
  it("a hint-carrying event is toast + bell entry + OS call, unfocused", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const daemon = await mountWithGlobalListener();

    act(() => daemon.emit({
      kind: "loop.finished", seq: 1, at: "2026-11-03T00:00:00Z", payload: { loopId: "l1" },
      notify: { title: "Loop finished", body: "l1 is done", tone: "success" },
    }));

    await waitFor(() => expect(screen.getByText("Loop finished")).toBeTruthy());
    fireEvent.click(screen.getByTestId("notification-bell-trigger"));
    const panel = await screen.findByTestId("notification-bell-panel");
    expect(within(panel).getByText("Loop finished")).toBeTruthy();
    expect(panel.textContent).toContain("l1 is done");
    await waitFor(() => expect(sendNotification).toHaveBeenCalledWith({ title: "Loop finished", body: "l1 is done" }));
  });

  it("a hint-less event is silent: no toast, no bell entry, no OS call", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const daemon = await mountWithGlobalListener();

    act(() => daemon.emit({ kind: "loop.blocked", seq: 1, at: "2026-11-03T00:00:00Z", payload: { loopId: "l1" } }));

    // Give any async toast/OS path a chance to (not) fire, then pin the
    // silence. The kind matters: loop.blocked is exactly the kind whose
    // daemon-side hint T0 found is absent on the live emit path, and
    // the consumer must stay as silent as the wire is empty.
    await waitFor(() => expect(screen.getByTestId("product-home")).toBeTruthy());
    expect(screen.queryByText("Loop blocked")).toBeNull();
    expect(notifications.getSnapshot()).toHaveLength(0);
    expect(sendNotification).not.toHaveBeenCalled();
  });

  it("focus gates the OS call only — toast and bell still land", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    const daemon = await mountWithGlobalListener();

    act(() => daemon.emit({
      kind: "budget.hit", seq: 1, at: "2026-11-03T00:00:00Z", payload: { loopId: "l1" },
      notify: { title: "A budget cap is exhausted", tone: "warning" },
    }));

    await waitFor(() => expect(screen.getByText("A budget cap is exhausted")).toBeTruthy());
    fireEvent.click(screen.getByTestId("notification-bell-trigger"));
    const panel = await screen.findByTestId("notification-bell-panel");
    expect(within(panel).getByText("A budget cap is exhausted")).toBeTruthy();
    expect(sendNotification).not.toHaveBeenCalled();
  });
});
