import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { appSettings } from "../src/lib/appSettings";
import { notifications } from "../src/lib/notifications";
import { ProductApp } from "../src/app/router";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  window.location.hash = "#/";
  notifications.clear();
});
afterEach(() => {
  cleanup();
  resetFakeDaemon();
  vi.unstubAllGlobals();
  act(() => appSettings.reset());
  notifications.clear();
});

describe("the notification system", () => {
  it("turns a notify-carrying daemon event into both a toast and a bell entry, and leaves a plain event silent", async () => {
    const daemon = installFakeDaemon({
      responses: { "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 } },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    // The global notification listener's own subscribe — a broad filter, not
    // the narrower one HomeScreen's own listener uses — must have landed
    // before anything is emitted, or the emit races its setup.
    await waitFor(() => {
      const globalSub = daemon.calls.some((call) => {
        const params = call.params as { filter?: { kinds?: unknown[] } } | undefined;
        return call.method === "subscribe" && Array.isArray(params?.filter?.kinds) && params.filter.kinds.length === 0;
      });
      expect(globalSub).toBe(true);
    });

    // A plain event (no notify hint) must not produce a toast or a bell entry.
    act(() => daemon.emit({ kind: "loop.advanced", seq: 1, at: "2026-10-28T00:00:00Z", payload: { loopId: "l1" } }));
    expect(screen.queryByText("Loop finished")).toBeNull();

    // A notify-carrying event produces both.
    act(() => daemon.emit({
      kind: "loop.finished", seq: 2, at: "2026-10-28T00:00:01Z", payload: { loopId: "l1" },
      notify: { title: "Loop finished", body: "l1 is done", tone: "success" },
    }));

    await waitFor(() => expect(screen.getByText("Loop finished")).toBeTruthy());

    fireEvent.click(screen.getByTestId("notification-bell-trigger"));
    const panel = await screen.findByTestId("notification-bell-panel");
    expect(panel.textContent).toContain("Loop finished");
    expect(panel.textContent).toContain("l1 is done");
  });

  it("marks an entry read on click, and clear empties the bell", async () => {
    installFakeDaemon({
      responses: { "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 } },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");

    act(() => notifications.push({ title: "Saved", body: "Settings updated", tone: "success", source: "local" }));

    fireEvent.click(screen.getByTestId("notification-bell-trigger"));
    const panel = await screen.findByTestId("notification-bell-panel");
    expect(panel.textContent).toContain("Saved");
    expect(notifications.getSnapshot().every((item) => !item.read)).toBe(true);

    fireEvent.click(within(panel).getByText("Saved"));
    expect(notifications.getSnapshot().find((item) => item.title === "Saved")?.read).toBe(true);

    fireEvent.click(within(panel).getByText("Clear"));
    expect(notifications.getSnapshot()).toHaveLength(0);
  });

  it("persists across a reload (localStorage, never the daemon)", () => {
    notifications.push({ title: "Workspace added", tone: "success", source: "local" });
    const raw = window.localStorage.getItem("aeon-notifications-v1");
    expect(raw).toBeTruthy();
    expect(JSON.parse(raw ?? "[]")[0].title).toBe("Workspace added");
  });
});
