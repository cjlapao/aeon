import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { ProductApp } from "../src/app/router";
import { forgetOrigin, originLabel } from "../src/lib/origin";

// Opening a loop folds the component tree away (the conversation gets the width), and the loop page's
// Back goes to the page it was opened from, not always to Home.

const loop = { id: "l1", slug: "l1", title: "Survey packages", componentRoot: "/work/a", status: "waiting", phase: "approve", updatedAt: "2026-10-27T08:00:00Z", startedAt: "2026-10-27T07:00:00Z", awaiting: true, mode: "standard" };

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  forgetOrigin();
  window.location.hash = "#/";
});
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

function go(hash: string) {
  act(() => { window.location.hash = hash; window.dispatchEvent(new HashChangeEvent("hashchange")); });
}

function daemon() {
  installFakeDaemon({
    negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
    responses: {
      "loop.list": { loops: [loop], all: true },
      "component.list": { components: [{ id: "c1", name: "a", slug: "a", root: "/work/a", parentId: "", shortDescription: "" }] },
      "blueprint.list": { names: [] }, "routes.list": { routes: [], providers: [], persisted: true },
      "subscribe": { subscriptionId: "s1", from: 0 },
      "question.pending": { pending: true, question: { id: 1, kind: "approve", prompt: "Ship it?", answers: ["approve"], askedAt: "2026-10-27T08:00:00Z" } },
      "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Ship it", root: "/work/a", status: "waiting", position: { lane: "approve" }, lanes: [{ id: "approve", status: "waiting", steps: [] }] },
    },
  });
}

describe("loop page: space and the way back", () => {
  it("names the way back from the page it was opened from, and folds the component menu", async () => {
    daemon();
    window.location.hash = "#/work/c1/loops";
    render(<ProductApp />);
    expect(await screen.findByPlaceholderText("Find a component")).toBeTruthy(); // the tree is there on a Work page
    go("#/loops/l1");
    await screen.findByTestId("loop-workspace");
    const back = await screen.findByTestId("loop-back");
    expect(back.getAttribute("aria-label")).toBe("Back to the project");
    expect(back.getAttribute("href")).toBe("#/work/c1/loops");
    // folded away while a loop is open: the kit keeps it mounted but inert and hidden
    expect(screen.getByPlaceholderText("Find a component").closest('[aria-hidden="true"]')).not.toBeNull();
    fireEvent.click(back);
    await vi.waitFor(() => expect(screen.getByPlaceholderText("Find a component").closest('[aria-hidden="true"]')).toBeNull()); // and back when you leave
  });

  it("from Home, Back goes Home", async () => {
    daemon();
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    go("#/loops/l1");
    await screen.findByTestId("loop-workspace");
    const back = await screen.findByTestId("loop-back");
    expect(within(back).getByText("Back to Home")).toBeTruthy();
    expect(back.getAttribute("href")).toBe("#/");
  });

  it("labels the way back honestly", () => {
    expect(originLabel("/")).toBe("Home");
    expect(originLabel(null)).toBe("Work");
    expect(originLabel("/work/ui/loops")).toBe("the project");
    expect(originLabel("/work/all/loops")).toBe("All projects");
    expect(originLabel("/config/policy")).toBe("the previous page");
  });
});
