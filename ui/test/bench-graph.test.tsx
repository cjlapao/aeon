import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { GraphPage, buildGraphNetwork, type ExplainResult } from "../src/features/test-bench/pages/GraphPage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { getBenchPage } from "../src/features/test-bench/lib/benchRegistry";

// S30 T7's bench page, held to the things the slice is for:
//
//   * the FOUR states are four renderings (D8/CG7) — each state of
//     graph.explain gets its own DOM shape, asserted by the testids the
//     state itself chooses, so a future collapse of two states into one
//     branch fails HERE rather than in a screenshot review;
//   * ambiguous lists ALL and picks NONE (CG5) — every candidate is
//     present, and there is no affordance that resolves to one;
//   * there is no free-text ANSWER anywhere (CG8) — every text input on
//     the page is a PARAMETER the daemon asked for; a page that could
//     type an answer could display a fabricated fact;
//   * the root is REQUIRED on every door (T5 §5) — with it empty, no
//     graph method reaches the wire and the refusal names the thing
//     refused;
//   * the maintenance verbs are socket calls (P34) — build/reparse/delete
//     go through rpc() like every read; the page opens no second door.
//
// The payloads are the CONFORMANCE FIXTURES — the found answer is
// conformance/graph/explain.json's, the ambiguous pair is
// explain-ambiguous.json's byte-for-byte, and the build answer is
// S64's build.json handle (workerId/state/started), not a report — the
// counters belong to the worker row and to bench-async-workers.test.tsx.
// Testing the page against those pins means a Go-side re-wording that
// breaks the app shows up here and not in production.

const FOUND_ANSWER: ExplainResult = {
  state: "found",
  node: { id: 41, symbol: "order.ProcessOrder", kind: "function", filePath: "internal/order/process.go", startLine: 16, endLine: 24, language: "go" },
  outgoing: [
    { kind: "calls", resolution: "inferred", to: { id: 12, symbol: "order.validateOrder", kind: "function", filePath: "internal/order/validate.go", startLine: 18, endLine: 20, language: "go" } },
    { kind: "calls", resolution: "inferred", to: { id: 7, symbol: "billing.ChargePayment", kind: "function", filePath: "internal/billing/charge.go", startLine: 9, endLine: 14, language: "go" } },
  ],
  incoming: [
    { kind: "calls", resolution: "inferred", from: { id: 51, symbol: "api.HandleOrderRequest", kind: "function", filePath: "internal/api/handler.go", startLine: 38, endLine: 43, language: "go" } },
  ],
};

const AMBIGUOUS_ANSWER: ExplainResult = {
  state: "ambiguous",
  candidates: [
    { id: 12, symbol: "order.Validate", kind: "function", filePath: "internal/order/validate.go", startLine: 8, endLine: 12, language: "go" },
    { id: 26, symbol: "billing.Validate", kind: "function", filePath: "internal/billing/validate.go", startLine: 6, endLine: 9, language: "go" },
  ],
};

const NOT_FOUND_ANSWER: ExplainResult = { state: "not-found" };
const NOT_BUILT_ANSWER: ExplainResult = { state: "not-built" };

function mount(responses: Record<string, unknown>): FakeDaemon {
  const fake = installFakeDaemon({
    responses: {
      "graph.roots": ["/home/dev/shop"],
      // S64: the page reads the ledger for its root on mount (the
      // reopen-mid-build answer), so an empty ledger is part of the
      // page's floor like graph.roots is. Individual tests override it
      // to put a row on the page.
      "asyncworkers.list": { workers: [], types: ["graph.build"] },
      ...responses,
    },
  });
  render(<GraphPage />);
  return fake;
}

function setRoot(value: string) {
  fireEvent.change(screen.getByTestId("graph-root"), { target: { value } });
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("registration (the bench registry, not a shared array)", () => {
  it("registers itself under /bench/graph and needs the daemon", async () => {
    await import("../src/features/test-bench/pages/registerAll");
    const def = getBenchPage("graph");
    expect(def).toBeDefined();
    expect(def?.path).toBe("/bench/graph");
    expect(def?.needsConnection).toBe(true);
  });
});

describe("the four states are four renderings", () => {
  it("found renders the node with edges in BOTH directions", async () => {
    mount({ "graph.explain": FOUND_ANSWER });
    setRoot("/home/dev/shop");
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "order.ProcessOrder" } });
    fireEvent.click(screen.getByTestId("graph-explain"));
    await waitFor(() => expect(screen.queryByTestId("graph-state-found")).not.toBeNull());
    expect(screen.getByTestId("graph-node").textContent).toContain("order.ProcessOrder");
    expect(screen.getAllByTestId("graph-edge-out").length).toBe(2);
    expect(screen.getAllByTestId("graph-edge-in").length).toBe(1);
    // Resolution tags travel (D4): the guessed cross-file links say so.
    expect(screen.getAllByText("inferred").length).toBeGreaterThanOrEqual(3);
    // And the states stay exclusive: no ambiguous/not-found/not-built body leaked in.
    expect(screen.queryByTestId("graph-state-ambiguous")).toBeNull();
    expect(screen.queryByTestId("graph-state-not-found")).toBeNull();
    expect(screen.queryByTestId("graph-state-not-built")).toBeNull();
  });

  it("ambiguous lists EVERY candidate and picks none", async () => {
    mount({ "graph.explain": AMBIGUOUS_ANSWER });
    setRoot("/home/dev/shop");
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "Validate" } });
    fireEvent.click(screen.getByTestId("graph-explain"));
    await waitFor(() => expect(screen.queryByTestId("graph-state-ambiguous")).not.toBeNull());
    const rows = screen.getAllByTestId("graph-candidate");
    expect(rows.length).toBe(2);
    expect(rows[0]?.textContent).toContain("order.Validate");
    expect(rows[1]?.textContent).toContain("billing.Validate");
    // The instruction is the disambiguation (re-ask qualified) — never a
    // button that resolves to one candidate silently.
    expect(screen.getByTestId("graph-state-ambiguous").textContent).toContain("qualified");
    expect(screen.queryByTestId("graph-candidate-pick")).toBeNull();
  });

  it("not-found and not-built are DIFFERENT answers with DIFFERENT instructions", async () => {
    mount({ "graph.explain": NOT_FOUND_ANSWER });
    setRoot("/home/dev/shop");
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "NoSuch" } });
    fireEvent.click(screen.getByTestId("graph-explain"));
    await waitFor(() => expect(screen.queryByTestId("graph-state-not-found")).not.toBeNull());
    expect(screen.queryByTestId("graph-state-not-built")).toBeNull();
    expect(screen.getByTestId("graph-state-not-found").textContent).toContain("indexed");
  });

  it("not-built points at the Build door rather than at giving up", async () => {
    mount({ "graph.explain": NOT_BUILT_ANSWER });
    setRoot("/home/dev/never");
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "Anything" } });
    fireEvent.click(screen.getByTestId("graph-explain"));
    await waitFor(() => expect(screen.queryByTestId("graph-state-not-built")).not.toBeNull());
    expect(screen.getByTestId("graph-state-not-built").textContent).toContain("Build");
    expect((screen.getByTestId("graph-build") as HTMLButtonElement).disabled).toBe(false);
  });
});

describe("status speaks the same two ways", () => {
  it("built true and built false render apart", async () => {
    const fake = mount({
      "graph.status": (params: unknown) =>
        (params as { componentRoot: string }).componentRoot === "/home/dev/shop"
          ? { root: "/home/dev/shop", built: true, files: 214, nodes: 1893, edges: 5310 }
          : { root: "/x", built: false, files: 0, nodes: 0, edges: 0 },
    });
    setRoot("/home/dev/shop");
    fireEvent.click(screen.getByTestId("graph-status"));
    await waitFor(() => expect(screen.getByTestId("graph-status-row").textContent).toContain("built"));
    expect(screen.getByTestId("graph-status-row").textContent).toContain("1893");
    setRoot("/x");
    fireEvent.click(screen.getByTestId("graph-status"));
    await waitFor(() => expect(screen.getByTestId("graph-status-row").textContent).toContain("not built"));
    expect(fake.methods()).toContain("graph.status");
  });
});

describe("the CRUD doors are socket calls", () => {
  it("build, reparse and delete reach the daemon with the root, and the answers arrive", async () => {
    // S64's contract change, pinned: build answers with a HANDLE in
    // milliseconds and the report belongs to the worker row — the page
    // must not pretend it still holds the ten-minute call.
    const fake = mount({
      "graph.build": { workerId: "aw_3c9f0a1b2c3d4e5f60718293a4b5c6d7", state: "queued", started: true },
      "graph.reparse": { root: "/home/dev/shop", files: 1, filesSkipped: 0, nodes: 9, edges: 14, unresolvedEdges: 2 },
      "graph.delete": { root: "/home/dev/shop", nodesDeleted: 1893 },
      "graph.status": { root: "/home/dev/shop", built: true, files: 214, nodes: 1893, edges: 5310 },
    });
    setRoot("/home/dev/shop");
    fireEvent.click(screen.getByTestId("graph-build"));
    await waitFor(() => expect(screen.getByTestId("graph-report").textContent).toContain("build started"));
    expect(screen.getByTestId("graph-report").textContent).toContain("aw_3c9f0a1b2c3d4e5f60718293a4b5c6d7");
    // And no counters leaked onto the handle answer — the page may not
    // render a build report it never received.
    expect(screen.getByTestId("graph-report").textContent).not.toContain("unresolved");

    fireEvent.change(screen.getByTestId("graph-reparse-path"), { target: { value: "internal/order/process.go" } });
    fireEvent.click(screen.getByTestId("graph-reparse"));
    await waitFor(() => expect(screen.getByTestId("graph-report").textContent).toContain("reparse"));
    expect(screen.getByTestId("graph-report").textContent).toContain("unresolved");

    fireEvent.click(screen.getByTestId("graph-delete"));
    await waitFor(() => expect(screen.getByTestId("graph-report").textContent).toContain("1893 nodes deleted"));

    expect(fake.methods().filter((m) => m.startsWith("graph."))).toEqual(
      expect.arrayContaining(["graph.roots", "graph.build", "graph.status", "graph.reparse", "graph.delete"]),
    );
    // Every method the page sends is from the graph family or the ledger
    // read the S64 contract added — no other side doors.
    for (const m of fake.methods()) {
      expect(m.startsWith("graph.") || m.startsWith("asyncworkers.")).toBe(true);
    }
    // The start door is the graph method; the ledger door carries no
    // start verb, and this page never asks for one.
    expect(fake.methods().some((m) => m === "asyncworkers.start")).toBe(false);
  });

  it("S64: a build already flying for the root shows as a banner from the ledger — the reopen-mid-build case", async () => {
    const row = {
      workerId: "aw_running01",
      workerType: "graph.build",
      componentRoot: "/home/dev/shop",
      state: "running",
      progress: 0.62,
      progressNote: "parsing internal/handler/resolve.go",
      queuedAt: "2026-10-30T17:41:02.000Z",
      startedAt: "2026-10-30T17:41:02.000Z",
    };
    const fake = mount({ "asyncworkers.list": { workers: [row], types: ["graph.build"] } });
    setRoot("/home/dev/shop");
    await waitFor(() => expect(screen.queryByTestId("graph-build-banner")).not.toBeNull());
    expect(screen.getByTestId("graph-build-banner").textContent).toContain("building");
    expect(screen.getByTestId("graph-build-banner").textContent).toContain("62%");
    expect(screen.getByTestId("graph-build-banner").textContent).toContain("resolve.go");

    fireEvent.click(screen.getByTestId("graph-build-cancel"));
    await waitFor(() => expect(fake.methods()).toContain("asyncworkers.cancel"));
  });

  it("the maintenance label says the truth: writes go through the daemon", () => {
    mount({});
    expect(screen.getByTestId("graph-maintenance-panel").textContent).toContain("writes go through the daemon");
  });
});

describe("required-arg honesty (T5 §5)", () => {
  it("an empty root sends NOTHING to the wire and the refusal names componentRoot", async () => {
    const fake = mount({ "graph.explain": FOUND_ANSWER });
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "X" } });
    // The button itself is gated, which IS the refusal-by-construction
    // door; and the message the page would show names the thing refused.
    expect((screen.getByTestId("graph-explain") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("graph-status")); // gated too
    expect(fake.methods().filter((m) => m.startsWith("graph.")).filter((m) => m !== "graph.roots")).toEqual([]);
  });
});

describe("CG8: coordinates only, no answer box", () => {
  it("every text input on the page is a PARAMETER the daemon defines", () => {
    mount({});
    const inputs = screen.getAllByRole("textbox");
    const ids = inputs.map((i) => i.getAttribute("data-testid"));
    const PARAMETERS = ["graph-root", "graph-symbol", "graph-path-from", "graph-path-to", "graph-query-prefix", "graph-reparse-path"];
    for (const id of ids) expect(PARAMETERS).toContain(id);
    // No free-text answer anywhere: no textarea, no content pane.
    expect(document.querySelector("textarea")).toBeNull();
    expect(screen.queryByTestId("graph-answer")).toBeNull();
  });

  it("found nodes render as file:lines coordinates, never content", async () => {
    mount({ "graph.explain": FOUND_ANSWER });
    setRoot("/home/dev/shop");
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "order.ProcessOrder" } });
    fireEvent.click(screen.getByTestId("graph-explain"));
    await waitFor(() => expect(screen.queryByTestId("graph-node")).not.toBeNull());
    expect(screen.getByTestId("graph-node").textContent).toContain("internal/order/process.go:16–24");
    // Nothing that looks like source bytes: no <pre>, and the fixture
    // itself carries no content field — the DOM cannot show what never
    // arrived.
    expect(document.querySelector("pre")).toBeNull();
  });
});

describe("an empty root list: the daemon answers null, not []", () => {
  // The never-built first-run shape. Go marshals a nil slice as JSON null
  // (not []), so a graphless daemon sends `null` for `graph.roots` even
  // though the contract types the result Array<string>. Before the fix the
  // page stored that null and threw on `roots.map` / `roots.length`; the
  // state is now coerced to an array, and the no-roots message speaks the
  // honest answer — nothing indexed, not "listing failed".
  it("renders the no-roots state instead of crashing on a null list", async () => {
    installFakeDaemon({ responses: { "graph.roots": null } });
    render(<GraphPage />);
    // The root input renders regardless of the list, so the page drew at all.
    await waitFor(() => expect(screen.getByTestId("graph-root")).not.toBeNull());
    // The empty datalist (roots.map) and the zero-length guard (roots.length)
    // both survived the null answer, and the honest no-roots message shows.
    await waitFor(() => expect(screen.getByTestId("graph-no-roots")).not.toBeNull());
    expect(screen.queryByTestId("graph-root-panel")).not.toBeNull();
  });
});

// The network is a second view of the SAME explain result, not a second
// query (one source of truth): found draws the ego-network (the central
// symbol + its callers and callees, coloured by kind), and the other three
// states stay honest — a graph is invented from nothing that the text panel
// already refused. (D8/CG7)
describe("the network view (the found symbol's ego-network)", () => {
  const explain = (answer: unknown) => {
    mount({ "graph.explain": answer });
    setRoot("/home/dev/shop");
    fireEvent.change(screen.getByTestId("graph-symbol"), { target: { value: "order.ProcessOrder" } });
    fireEvent.click(screen.getByTestId("graph-explain"));
  };

  it("no explain yet: a hint, not a graph", () => {
    mount({});
    expect(screen.getByTestId("graph-network-hint")).not.toBeNull();
    expect(screen.queryByTestId("graph-network-found")).toBeNull();
  });

  it("found: the ego-network draws and no honest state leaks through", async () => {
    explain(FOUND_ANSWER);
    await waitFor(() => expect(screen.getByTestId("graph-network-found")).not.toBeNull());
    expect(screen.queryByTestId("graph-network-ambiguous")).toBeNull();
    expect(screen.queryByTestId("graph-network-not-found")).toBeNull();
    expect(screen.queryByTestId("graph-network-not-built")).toBeNull();
  });

  it("ambiguous: honest empty, no graph invented", async () => {
    explain(AMBIGUOUS_ANSWER);
    await waitFor(() => expect(screen.getByTestId("graph-network-ambiguous")).not.toBeNull());
    expect(screen.queryByTestId("graph-network-found")).toBeNull();
  });

  it("not-found: honest empty", async () => {
    explain(NOT_FOUND_ANSWER);
    await waitFor(() => expect(screen.getByTestId("graph-network-not-found")).not.toBeNull());
    expect(screen.queryByTestId("graph-network-found")).toBeNull();
  });

  it("not-built: honest empty that points at Build", async () => {
    explain(NOT_BUILT_ANSWER);
    await waitFor(() => expect(screen.getByTestId("graph-network-not-built")).not.toBeNull());
    expect(screen.queryByTestId("graph-network-found")).toBeNull();
  });
});

describe("buildGraphNetwork (the ego-network mapping)", () => {
  it("central + both edge directions, deduped, coloured by kind", () => {
    const net = buildGraphNetwork(FOUND_ANSWER)!;
    // center (41) + 2 callees (12, 7) + 1 caller (51) = 4 distinct nodes
    expect(new Set(net.nodes.map((n) => n.id)).size).toBe(net.nodes.length);
    expect(net.nodes.map((n) => n.id)).toEqual(expect.arrayContaining(["41", "12", "7", "51"]));
    // 2 outgoing + 1 incoming = 3 edges
    expect(net.edges.length).toBe(3);
    expect(net.edges.some((e) => e.source === "41" && e.target === "12")).toBe(true);
    expect(net.edges.some((e) => e.source === "51" && e.target === "41")).toBe(true);
    // one community per distinct kind (all "function" here), each with a tone
    expect(net.communities.length).toBe(new Set(net.nodes.map((n) => n.community)).size);
    expect(net.communities.every((c) => Boolean(c.tone))).toBe(true);
    // a click maps back to the originating node, so file:lines can be shown
    expect(net.byId.get("41")?.symbol).toBe("order.ProcessOrder");
    expect(net.byId.get("41")?.filePath).toBe("internal/order/process.go");
  });

  it("only a found result with a node draws; the others return null", () => {
    expect(buildGraphNetwork(NOT_FOUND_ANSWER)).toBeNull();
    expect(buildGraphNetwork(NOT_BUILT_ANSWER)).toBeNull();
    expect(buildGraphNetwork(AMBIGUOUS_ANSWER)).toBeNull();
    expect(buildGraphNetwork(null)).toBeNull();
    // found, but no node — the daemon's contract says this cannot happen;
    // refuse it here rather than throw at the caller.
    expect(buildGraphNetwork({ state: "found" })).toBeNull();
  });

  it("dedups a node that is both a callee and a caller, but keeps both edges", () => {
    const res: ExplainResult = {
      state: "found",
      node: { id: 1, symbol: "A", kind: "function", filePath: "a.go", startLine: 1, endLine: 2, language: "go" },
      outgoing: [
        { kind: "calls", resolution: "inferred", to: { id: 2, symbol: "B", kind: "function", filePath: "b.go", startLine: 1, endLine: 2, language: "go" } },
      ],
      incoming: [
        { kind: "calls", resolution: "inferred", from: { id: 2, symbol: "B", kind: "function", filePath: "b.go", startLine: 1, endLine: 2, language: "go" } },
      ],
    };
    const net = buildGraphNetwork(res)!;
    // B is one node even though it is both a callee and a caller
    expect(net.nodes.filter((n) => n.id === "2").length).toBe(1);
    // but the two distinct edges (A→B and B→A) both stand
    expect(net.edges.length).toBe(2);
  });
});
