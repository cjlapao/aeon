import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RetrievalPage } from "../src/features/test-bench/pages/RetrievalPage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { getBenchPage } from "../src/features/test-bench/lib/benchRegistry";

// The retrieval bench page (the spec's Part C, held to the things the slice
// is for — the same discipline bench-graph.test.tsx keeps for GraphPage):
//
//   * the FOUR availability states are four renderings (§4b three-state
//     honesty) — index-empty / no-provider / model-mismatch /
//     dimension-mismatch each get their own label, asserted by the text the
//     page itself chose, so a future collapse of two reasons into one branch
//     fails HERE rather than in a screenshot review;
//   * index-empty never collapses into "found nothing" (the query ran) — the
//     not-indexed state names the empty index, not an empty answer;
//   * there is no free-text ANSWER anywhere — every text input on the page
//     is a PARAMETER the daemon asked for (a search term is a question, not
//     a reply); a query surface that could display a hand-typed answer could
//     display a hand-typed fact;
//   * the roots come from graph.roots (B2 — the one open detail, resolved as
//     "reuse the graph's roots; they are the same set"), not a second list;
//   * the index / status / query doors are socket calls (P34) — they ride the
//     generic rpc() bridge and open no second door;
//   * the learnings door is SEPARATE — search and graph are never fused (§3),
//     and the page keeps them apart, calling each on its own.

const ROOT = "/home/dev/shop";

// The four reasons, the way the daemon returns them, each as its own corpus.
// A real daemon answers two corpora (code + history); the test supplies more,
// because the assertion is about the PAGE rendering four distinct states, not
// about the daemon's corpus count — the page maps over whatever it is given.
const STATUS_FOUR_REASONS = {
  root: ROOT,
  corpora: [
    { corpus: "code", files: 0, staleFiles: 0, pendingEmbed: 0, available: false, keywordOnly: false, reasons: ["index-empty"] },
    { corpus: "history", files: 0, staleFiles: 0, pendingEmbed: 0, available: true, keywordOnly: true, reasons: ["no-provider"] },
    { corpus: "model", files: 214, staleFiles: 0, pendingEmbed: 0, available: false, keywordOnly: false, reasons: ["model-mismatch"] },
    { corpus: "dim", files: 214, staleFiles: 0, pendingEmbed: 0, available: false, keywordOnly: false, reasons: ["dimension-mismatch"] },
  ],
  embedRoute: "",
  embedModel: "",
  embedDim: 0,
};

function mount(responses: Record<string, unknown>): FakeDaemon {
  const fake = installFakeDaemon({
    responses: {
      "graph.roots": [ROOT],
      ...responses,
    },
  });
  render(<RetrievalPage />);
  return fake;
}

function setRoot(value: string) {
  fireEvent.change(screen.getByTestId("retrieval-root"), { target: { value } });
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("registration (the bench registry, not a shared array)", () => {
  it("registers itself under /bench/retrieval and needs the daemon", async () => {
    await import("../src/features/test-bench/pages/registerAll");
    const def = getBenchPage("retrieval");
    expect(def).toBeDefined();
    expect(def?.path).toBe("/bench/retrieval");
    expect(def?.needsConnection).toBe(true);
  });
});

describe("the four availability states are four renderings", () => {
  it("index-empty / no-provider / model-mismatch / dimension-mismatch each render their own label", async () => {
    mount({ "retrieval.status": STATUS_FOUR_REASONS });
    setRoot(ROOT);
    fireEvent.click(screen.getByTestId("retrieval-status-btn"));
    await waitFor(() => expect(screen.queryAllByTestId("retrieval-availability").length).toBeGreaterThan(0));

    // Four states in the DOM — one per corpus — and the four reason labels
    // all present, so no two collapsed into one branch.
    const pills = screen.getAllByTestId("retrieval-availability");
    expect(pills.length).toBe(4);

    expect(screen.getAllByText(/not indexed — the corpus holds nothing/).length).toBe(1);
    expect(screen.getAllByText(/keyword-only — a route answers by FTS5/).length).toBe(1);
    expect(screen.getAllByText(/built by another model/).length).toBe(1);
    expect(screen.getAllByText(/a different dimension/).length).toBe(1);
  });

  it("index-empty is never the query's 'found nothing' — the two stay apart", async () => {
    // index-empty (no chunks) must not read like an empty query answer. The
    // label names the empty index; the query's own empty answer is a
    // different sentence the query panel owns.
    mount({ "retrieval.status": STATUS_FOUR_REASONS });
    setRoot(ROOT);
    fireEvent.click(screen.getByTestId("retrieval-status-btn"));
    await waitFor(() => expect(screen.queryAllByTestId("retrieval-availability").length).toBeGreaterThan(0));
    // The index-empty corpus says "holds nothing yet" — not "matched nothing".
    expect(screen.getByText(/holds nothing yet/).textContent).not.toContain("matched");
  });
});

describe("the status door answers indexed-or-not, by root (B1)", () => {
  it("reaches the daemon with the componentRoot and renders the corpora", async () => {
    const fake = mount({
      "retrieval.status": (params: unknown) =>
        (params as { componentRoot: string }).componentRoot === ROOT
          ? STATUS_FOUR_REASONS
          : { root: "/x", corpora: [], embedRoute: "", embedModel: "", embedDim: 0 },
    });
    setRoot(ROOT);
    fireEvent.click(screen.getByTestId("retrieval-status-btn"));
    await waitFor(() => expect(screen.queryAllByTestId("retrieval-availability").length).toBeGreaterThan(0));
    expect(fake.methods()).toContain("retrieval.status");
    expect((fake.paramsOf("retrieval.status") as { componentRoot: string }).componentRoot).toBe(ROOT);
  });
});

describe("the index / query doors are socket calls, keyed by root", () => {
  it("index and query reach the daemon with the root, and answers arrive", async () => {
    const fake = mount({
      "retrieval.reindex": { chunksWritten: 9, corpus: "code", costKnown: true, deleted: 1, embedded: 3, embeddedUsd: 0.0021, files: 4, skips: 0 },
      "search.query": { hits: [{ lineStart: 16, lineEnd: 24, score: 0.87, source: "internal/order/process.go" }], keywordOnly: true },
      "retrieval.status": STATUS_FOUR_REASONS,
    });
    setRoot(ROOT);
    fireEvent.click(screen.getByTestId("retrieval-index-btn"));
    await waitFor(() => expect(fake.methods()).toContain("retrieval.reindex"));
    // The reindex answer's figures land on the page, verbatim.
    await waitFor(() => expect(screen.getByText(/9 chunks/)).not.toBeNull());

    fireEvent.change(screen.getByTestId("retrieval-search"), { target: { value: "ProcessOrder" } });
    fireEvent.click(screen.getByTestId("retrieval-query-btn"));
    await waitFor(() => expect(fake.methods()).toContain("search.query"));
    // The hit renders as a file:line RANGE with a score — locations only.
    await waitFor(() => expect(screen.getByTestId("retrieval-hit").textContent).toContain("process.go:16"));
    // The keyword-only flag is spoken out loud (D4), not swallowed.
    await waitFor(() => expect(screen.getAllByText(/keyword-only/).length).toBeGreaterThan(0));
    expect((fake.paramsOf("search.query") as { componentRoot: string }).componentRoot).toBe(ROOT);
  });
});

describe("no answer box — every text input is a parameter the daemon defines", () => {
  it("there is no textarea and no free-text answer; only parameters render", () => {
    mount({});
    const inputs = screen.getAllByRole("textbox");
    const ids = inputs.map((i) => i.getAttribute("data-testid"));
    // The search term is a QUESTION the person asks, not a reply — it is a
    // parameter. A reply box would be an answer the page invented.
    for (const id of ids) expect(["retrieval-root", "retrieval-search", "retrieval-scope-key", "retrieval-rank"]).toContain(id);
    // No free-text answer anywhere: no textarea, no content pane.
    expect(document.querySelector("textarea")).toBeNull();
    expect(screen.queryByTestId("retrieval-answer")).toBeNull();
  });
});

describe("the roots are the graph's (B2 — reuse, do not re-list)", () => {
  it("the selector is graph.roots; no retrieval.roots is invented", async () => {
    const fake = mount({});
    await waitFor(() => expect(screen.getByTestId("retrieval-root")).not.toBeNull());
    // The page asks for the graph's roots on mount — the one roots source.
    expect(fake.methods()).toContain("graph.roots");
    // It never invents a second roots list for retrieval.
    expect(fake.methods().some((m) => m === "retrieval.roots")).toBe(false);
  });
});

describe("the learnings door is separate from the query (not fused)", () => {
  it("learnings.query reaches the daemon with scope + optional rank, and the set renders", async () => {
    const fake = mount({
      "learnings.query": {
        learnings: [
          { id: 7, scope: "project", scopeKey: ROOT, body: "the retry backoff is capped", provenance: "observed", observedCount: 3, createdAt: "2026-10-31T00:00:00Z" },
        ],
      },
    });
    fireEvent.change(screen.getByTestId("retrieval-scope"), { target: { value: "project" } });
    fireEvent.change(screen.getByTestId("retrieval-scope-key"), { target: { value: ROOT } });
    fireEvent.click(screen.getByTestId("retrieval-learnings-btn"));
    await waitFor(() => expect(fake.methods()).toContain("learnings.query"));
    await waitFor(() => expect(screen.getByTestId("retrieval-learning").textContent).toContain("the retry backoff is capped"));
    expect((fake.paramsOf("learnings.query") as { scopeKey: string }).scopeKey).toBe(ROOT);
  });
});
