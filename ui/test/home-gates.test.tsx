import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { ProductApp } from "../src/app/router";

// The Home "needs your input" section reads real daemon state: every loop the
// daemon itself flags `awaiting` (never a guess this screen makes), across
// every project and component, each with its own `question.pending` call —
// not just the one most recently touched.

function loop(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    id: "l1", slug: "l1", title: "Survey packages", componentRoot: "/work/a", status: "waiting",
    phase: "approve", updatedAt: "2026-10-27T08:00:00Z", startedAt: "2026-10-27T07:00:00Z",
    awaiting: true, mode: "standard", ...overrides,
  };
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  window.location.hash = "#/";
});
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("the home screen's gates", () => {
  it("shows one row per loop awaiting input, across different components, not just the first", async () => {
    const loopA = loop({ id: "l1", slug: "l1", title: "Survey packages", componentRoot: "/work/auth-service" });
    const loopB = loop({ id: "l2", slug: "l2", title: "Bump deps", componentRoot: "/work/aeon-core-engine" });
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [loopA, loopB], all: true },
        "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 },
        "question.pending": (raw: unknown) => {
          const { slug } = raw as { slug: string };
          if (slug === "l1") return { pending: true, question: { id: 1, kind: "approve", prompt: "Ship the auth patch?", answers: ["approve"], askedAt: "2026-10-27T08:00:00Z" } };
          return { pending: true, question: { id: 2, kind: "approve", prompt: "Ship the dependency bump?", answers: ["approve"], askedAt: "2026-10-27T08:00:00Z" } };
        },
        "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Ship it", root: "/work/auth-service", status: "waiting",
          position: { lane: "approve" }, lanes: [{ id: "approve", status: "waiting", steps: [] }] },
      },
    });
    render(<ProductApp />);

    const gates = await screen.findAllByTestId("home-gate-banner");
    expect(gates).toHaveLength(2);
    expect(within(screen.getByTestId("home-gates")).getByText("Needs you")).toBeTruthy();
    expect(within(screen.getByTestId("home-gates")).getByText("2")).toBeTruthy();

    const authRow = gates.find((row) => within(row).queryByText(/auth-service/));
    const engineRow = gates.find((row) => within(row).queryByText(/aeon-core-engine/));
    expect(authRow && within(authRow).getByText("Ship the auth patch?")).toBeTruthy();
    expect(engineRow && within(engineRow).getByText("Ship the dependency bump?")).toBeTruthy();

    fireEvent.click(within(authRow!).getByRole("button", { name: /Answer/ }));
    expect(await screen.findByTestId("loop-workspace")).toBeTruthy();
  });

  it("shows nothing when no loop is actually awaiting, even with loops running", async () => {
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [loop({ awaiting: false, status: "running" })], all: true },
        "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    expect(await screen.findByText("Nothing is waiting on you.")).toBeTruthy();
    expect(screen.queryByTestId("home-gate-banner")).toBeNull();
  });

  it("drops a loop from the list once its question is answered elsewhere, without stranding the other", async () => {
    const loopA = loop({ id: "l1", slug: "l1", componentRoot: "/work/a" });
    const loopB = loop({ id: "l2", slug: "l2", componentRoot: "/work/b" });
    const daemon = installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [loopA, loopB], all: true },
        "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 },
        "question.pending": { pending: true, question: { id: 1, kind: "approve", prompt: "Decide?", answers: ["approve"], askedAt: "2026-10-27T08:00:00Z" } },
      },
    });
    render(<ProductApp />);
    await waitFor(() => expect(screen.getAllByTestId("home-gate-banner")).toHaveLength(2));

    act(() => daemon.emit({ kind: "loop.advanced", seq: 2, at: "2026-10-27T09:00:00Z",
      payload: { loopId: "l1", row: { ...loopA, awaiting: false, status: "running", updatedAt: "2026-10-27T09:00:00Z" } } }));
    await waitFor(() => expect(screen.getAllByTestId("home-gate-banner")).toHaveLength(1));
  });
});
