import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { appSettings } from "../src/lib/appSettings";
import { ProductApp } from "../src/app/router";
import { parseTarget, pathFor } from "../src/app/NavigateListener";

// S58 T8 — the way back in (SV9): `aeon://navigate` opens a place, and
// only a place. The web parser and the Rust parser read the SAME fixture
// (test/fixtures/deeplinks.json); the table below runs every case through
// `parseTarget` so a grammar move on one side reddens here the day it
// happens. Mounting `ProductApp` (not the bare listener) keeps the proof
// in the place the links actually arrive: the app the shell talks to.
//
// The spec's two table rows "navigate handler never calls an rpc" and
// "deleted loop link says so" are resolved here in the open: the handler
// NAVIGATES (that half is rpc-free — proved with inbox/root/malformed
// events, zero bridge calls); the one thing it may additionally do is a
// single READ (`loop.show`) on a loop target, to tell a deleted loop from
// a slow one — a read cannot answer, retry or stop anything, and the
// refusal sentence rendered is the daemon's own, verbatim.

const BASE_RESPONSES = {
  "loop.list": { loops: [], all: true },
  "component.list": { components: [] },
  "blueprint.list": { names: [] },
  "routes.list": { routes: [], providers: [], persisted: true },
  subscribe: { subscriptionId: "s1", from: 0 },
};

interface FixtureCase {
  url: string;
  accept: { kind: string; id?: string } | null;
  reason: string | null;
}

const FIXTURE: { cases: FixtureCase[] } = JSON.parse(
  readFileSync(resolve(process.cwd(), "test/fixtures/deeplinks.json"), "utf8"),
);

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  window.location.hash = "#/";
});
afterEach(() => {
  cleanup();
  resetFakeDaemon();
  vi.unstubAllGlobals();
  act(() => appSettings.reset());
  window.location.hash = "#/";
});

describe("parseTarget — the same table the Rust side runs", () => {
  it("accepts only the three forms (shared fixture, all cases)", () => {
    expect(FIXTURE.cases.length).toBeGreaterThanOrEqual(20);
    for (const c of FIXTURE.cases) {
      const got = parseTarget(c.url);
      if (c.accept === null) {
        expect(got, `refuse ${JSON.stringify(c.url)}`).toBeNull();
      } else {
        expect(got, `accept ${JSON.stringify(c.url)}`).toEqual(
          c.accept.id === undefined ? { kind: c.accept.kind } : { kind: c.accept.kind, id: c.accept.id },
        );
      }
    }
  });
  it("maps places to routes — and the inbox still maps Home (SV18 TODO)", () => {
    expect(pathFor({ kind: "inbox" })).toBe("/");
    expect(pathFor({ kind: "loop", id: "x1" })).toBe("/loops/x1");
    expect(pathFor({ kind: "root", id: "x1" })).toBe("/components/x1");
  });
  it("never returns anything but a place (no answer can ride in)", () => {
    for (const c of FIXTURE.cases) {
      const got = parseTarget(c.url);
      if (!got) continue;
      const keys = Object.keys(got).sort();
      expect(keys, c.url).toEqual(got.id === undefined ? ["kind"] : ["id", "kind"]);
    }
  });
});

async function mountApp() {
  const daemon = installFakeDaemon({ responses: { ...BASE_RESPONSES } });
  render(<ProductApp />);
  await screen.findByTestId("product-home");
  await waitFor(() => expect(daemon.subscribes()).toContain("aeon://navigate"));
  await waitFor(() => expect(daemon.subscribes()).toContain("aeon://navigate-refused"));
  return daemon;
}

describe("the navigate handler", () => {
  it("routes an accepted loop, root, and inbox link", async () => {
    const daemon = await mountApp();
    act(() => daemon.emitNavigate("aeon://loop/x1"));
    await waitFor(() => expect(window.location.hash).toBe("#/loops/x1"));
    // The deleted-vs-slow read fired; with a plain `{}` answer nothing toasts.
    await waitFor(() => expect(daemon.methods()).toContain("loop.show"));
    window.location.hash = "#/";
    await new Promise((r) => setTimeout(r, 0));
    act(() => daemon.emitNavigate("aeon://root/c9"));
    await waitFor(() => expect(window.location.hash).toBe("#/work/c9"));
    act(() => daemon.emitNavigate("aeon://inbox"));
    await waitFor(() => expect(window.location.hash).toBe("#/"));
  });

  it("never calls an rpc for a link that is not a loop (inbox/root/malformed)", async () => {
    const daemon = await mountApp();
    const before = daemon.methods().length;
    act(() => daemon.emitNavigate("aeon://inbox"));
    // Malformed ON THE ACCEPT CHANNEL (the shell should never send one; the
    // web parser still refuses it, fixed word, no navigation):
    act(() => daemon.emitNavigate("aeon://loop/x?token=t"));
    await new Promise((r) => setTimeout(r, 0));
    expect(daemon.methods().slice(before)).toEqual([]);
    await screen.findByText("Link not understood"); // the malformed one said so
    expect(window.location.hash).toBe("#/"); // and went nowhere
  });

  it("a link can never produce an acting verb (SV9: navigate is the whole vocabulary)", async () => {
    const daemon = await mountApp();
    const baseline = daemon.methods().length;
    act(() => daemon.emitNavigate("aeon://loop/x1"));
    await waitFor(() => expect(daemon.methods()).toContain("loop.show"));
    await new Promise((r) => setTimeout(r, 20));
    // Whatever the link caused after this moment — the existence read and
    // the destination page's own reads — NOTHING from it may be a verb
    // that acts on anything. The blacklist is the point: a link answering,
    // retrying, or stopping is a method name, and no method name in a
    // link-shaped flow may come from this code.
    const ACTING = ["question.answer", "question.expire", "loop.stop", "loop.resume", "loop.cancel",
      "loop.raise", "loop.retry", "step.answer", "step.retry", "gate.answer", "loop.start",
      "loop.spawn", "settings.set", "policy.add", "proposal.commit", "proposal.reject"];
    const after = daemon.methods().slice(baseline);
    for (const verb of ACTING) expect(after, `a link provoked ${verb}`).not.toContain(verb);
  });
});

describe("refusals", () => {
  it("shows a fixed reason, never the raw URL", async () => {
    const daemon = await mountApp();
    act(() => daemon.emitNavigateRefused("unknown-door"));
    await screen.findByText("Link not understood");
    expect(screen.getByText("unknown-door")).toBeTruthy();
    // Reflected text is the failure mode: the raw scheme is nowhere in the
    // rendered text, and the payload that WAS echoed is from the closed
    // five words only.
    expect(document.body.textContent).not.toContain("aeon://");
    act(() => daemon.emitNavigateRefused("http://evil.example/aeon://loop/x"));
    await waitFor(() => expect(screen.getAllByText("Link not understood").length).toBeGreaterThanOrEqual(2));
    expect(document.body.textContent).not.toContain("evil.example");
    expect(document.body.textContent).not.toContain("http://evil");
  });
});

describe("a deleted loop link", () => {
  it("says so, in the daemon's own verbatim words", async () => {
    const daemon = installFakeDaemon({
      responses: {
        ...BASE_RESPONSES,
        "loop.show": () => {
          throw "loop no longer exists: x9";
        },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    await waitFor(() => expect(daemon.subscribes()).toContain("aeon://navigate"));
    act(() => daemon.emitNavigate("aeon://loop/x9"));
    await screen.findByText("That loop no longer exists");
    // Verbatim, and it appears where a person cannot miss it: the toast,
    // and the destination page's own honest error state (which already
    // rendered refusals before S58 — the "blank screen" column of the
    // failure table was half-closed by that, and this closes the other).
    expect(screen.getAllByText("loop no longer exists: x9").length).toBeGreaterThanOrEqual(1);
  });
});

describe("the toast action path", () => {
  it("an Open action on a deep-linked toast navigates through the same handler", async () => {
    const daemon = installFakeDaemon({
      responses: {
        ...BASE_RESPONSES,
        "loop.show": { loop: { id: "x7", status: "running" } },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    await waitFor(() => expect(daemon.subscribes()).toContain("aeon://navigate"));
    // The event that starts as a notification, not a link:
    await waitFor(() => expect(daemon.subscribes()).toContain("aeon://event"));
    act(() =>
      daemon.emit({
        subscriptionId: "s1",
        seq: 1,
        kind: "question.asked",
        at: new Date().toISOString(),
        notify: { title: "A question", body: "in the open loop", tone: "warning", attentionKey: "question:x7", deepLink: "aeon://loop/x7" },
      }),
    );
    const open = await screen.findByText("Open");
    fireEvent.click(open);
    await waitFor(() => expect(window.location.hash).toBe("#/loops/x7"));
  });
});
