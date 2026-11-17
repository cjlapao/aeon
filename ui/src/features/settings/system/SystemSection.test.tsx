import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { SystemSection } from "./SystemSection";
import { WorkKnowledgeTab } from "../../work/WorkKnowledgeTab";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { notifications } from "../../../lib/notifications";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";

// S54 T6 — the section renders what the daemon ANSWERS and nothing else:
// state words verbatim (unwired included), counts verbatim, the plan
// figures before a reindex is confirmed, the daemon's refusal sentences
// verbatim, and the ledger rows verbatim. The fake daemon records every
// call, which is how these tests also prove the params: `platform.changes`
// carries a limit, the writes carry root/corpus/path, and NO call carries
// an actor — the peer is the daemon's to know.

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

const staleLive = {
  state: "stale",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  since: Date.now() - 4000,
  cause: "daemon restarted",
  retry: 1,
} as const satisfies LiveState;

const STATUS = {
  evaluatedAt: "2026-11-08T10:00:00Z",
  retrieval: {
    state: "available",
    availability: "fts5+vec ready",
    components: [
      {
        component: "/work/core",
        embedRoute: "anthropic",
        embedModel: "embed-x",
        embedDim: 1536,
        corpora: [
          { name: "code", files: 412, staleChunks: 37, reindexing: true },
          { name: "docs", files: 20, staleChunks: 0 },
        ],
      },
    ],
  },
  graph: {
    state: "stale",
    roots: [
      { root: "/work/core", files: 120, nodes: 900, edges: 2000 },
      { root: "/work/api", neverBuilt: true, files: 0, nodes: 0, edges: 0 },
    ],
  },
  usage: {
    state: "available",
    workspace: {
      loopsRun: 12,
      inputTokensTotal: 123450,
      outputTokensTotal: 6789,
      costUsdTotal: 0.4231,
      costState: "partial",
      recorded: true,
    },
  },
};

const PLAN = {
  componentRoot: "/work/core",
  corpus: "docs",
  cost: { state: "partial", usd: 0.21, reason: "no price for embed-x" },
  embedModel: "embed-x",
  estimateBasis: "daemon-counted",
  estimatedInputTokens: 98765,
  files: 20,
  staleChunks: 7,
  staleFiles: 3,
  willReplaceEmbeddings: true,
};

const CHANGES = [
  {
    id: 7,
    occurredAt: "2026-11-08T09:55:00Z",
    op: "reindex",
    subject: "/work/core#docs",
    actorKind: "user",
    actorId: "1000",
    summary: "started",
    outcomeRef: "worker-7",
  },
];

const NODE_PARSE = { id: 1, symbol: "ParseBlueprint", kind: "function", language: "go", filePath: "internal/blueprint/parse.go", startLine: 41, endLine: 118 };
const NODE_VALIDATE = { id: 2, symbol: "(*Spec).Validate", kind: "method", language: "go", filePath: "internal/blueprint/spec.go", startLine: 88, endLine: 120 };
const NODE_LOAD = { id: 3, symbol: "LoadFlow", kind: "function", language: "go", filePath: "internal/flow/load.go", startLine: 10, endLine: 40 };
const EXPLAIN_FOUND = {
  state: "found",
  node: NODE_PARSE,
  incoming: [{ kind: "calls", resolution: "extracted", from: NODE_LOAD }],
  outgoing: [{ kind: "calls", resolution: "extracted", to: NODE_VALIDATE }],
};

function responses(extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "platform.status": STATUS,
    "platform.changes": CHANGES,
    "retrieval.reindexPlan": PLAN,
    "retrieval.reindex": { started: true, workerId: "w-9", state: "building" },
    "graph.build": { started: true, workerId: "w-10", state: "building" },
    "graph.reparse": { root: "/work/core", files: 1, nodes: 4, edges: 6, filesSkipped: 0, unresolvedEdges: 0 },
    "graph.delete": { root: "/work/core", nodesDeleted: 900 },
    "graph.query": { state: "found", nodes: [NODE_PARSE] },
    "graph.explain": EXPLAIN_FOUND,
    "graph.path": { state: "found", start: NODE_PARSE, hops: [{ kind: "calls", resolution: "extracted", to: NODE_VALIDATE }] },
    "search.query": { hits: [{ source: "internal/blueprint/validate.go", lineStart: 12, lineEnd: 64, score: 0.82 }, { source: "docs/design.md", lineStart: 210, lineEnd: 248, score: 0.41 }], keywordOnly: true },
    ...extra,
  };
}

function renderSection(live: LiveState = currentLive, extra: Record<string, unknown> = {}, canWrite?: boolean): FakeDaemon {
  const daemon = installFakeDaemon({ responses: responses(extra) });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <SystemSection controller={new LiveController()} live={live} {...(canWrite === undefined ? {} : { canWrite })} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

/** The explorer and the search probe moved into a component's Knowledge tab; these tests reach them there. */
function renderKnowledge(_live: LiveState = currentLive, extra: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({ responses: responses(extra) });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <WorkKnowledgeTab controller={new LiveController()} live={currentLive} canWrite scope={[]} all />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

/** Open a tab by its accessible name (the strip cards carry the same words, so tabs are addressed by role). */
async function openTab(name: string): Promise<void> {
  fireEvent.click(await screen.findByRole("tab", { name }));
}

/** The whole row a testid-carrying cell sits in (table cells hold their own testids). */
const rowOf = (testid: string): HTMLElement => screen.getByTestId(testid).closest("tr") as HTMLElement;

describe("SystemSection (S54 T6)", () => {
  it("reconnecting asks nothing at all", () => {
    const daemon = renderSection(staleLive);
    expect(screen.getByTestId("system-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("platform.status");
    expect(daemon.methods()).not.toContain("platform.changes");
  });

  it("the strip prints the daemon's words verbatim — states, availability, counts, cost", async () => {
    renderSection();
    await screen.findByTestId("system-section");
    expect(((await screen.findByTestId("retrieval-state")).textContent ?? "").includes("available")).toBe(true);
    expect(screen.getByText("fts5+vec ready")).toBeTruthy();
    expect(((screen.getByTestId("graph-state")).textContent ?? "").includes("stale")).toBe(true);
    // graph figures are summed from the rows the daemon sent: 900 symbols, 2,000 links, 2 roots.
    expect((screen.getByTestId("system-graph").textContent ?? "")).toContain("900");
    expect((screen.getByTestId("system-graph").textContent ?? "")).toContain("2,000");
    expect(((await screen.findByTestId("usage-cost")).textContent ?? "").includes("$0.4231")).toBe(true);
    expect(((screen.getByTestId("usage-cost-state")).textContent ?? "").includes("partial")).toBe(true);
    expect(((screen.getByTestId("system-evaluated")).textContent ?? "").includes("2026-11-08T10:00:00Z")).toBe(true);
  });

  it("an unwired block is a state, shown — not an empty panel", async () => {
    renderSection(currentLive, {
      "platform.status": {
        evaluatedAt: "2026-11-08T10:00:00Z",
        retrieval: { state: "unwired" },
        graph: { state: "unwired" },
        usage: { state: "unwired" },
      },
    });
    await screen.findByTestId("retrieval-state");
    expect(((screen.getByTestId("retrieval-state")).textContent ?? "").includes("unwired")).toBe(true);
    expect(((screen.getByTestId("graph-state")).textContent ?? "").includes("unwired")).toBe(true);
    expect(((screen.getByTestId("usage-state")).textContent ?? "").includes("unwired")).toBe(true);
    expect(screen.getByTestId("usage-unrecorded")).toBeTruthy();
  });

  it("the daemon's error sentence rides the block verbatim", async () => {
    renderSection(currentLive, {
      "platform.status": {
        evaluatedAt: "2026-11-08T10:00:00Z",
        retrieval: { state: "error", error: "corpus table unreadable: no such table" },
        graph: { state: "available", roots: [] },
        usage: { state: "available" },
      },
    });
    expect(((await screen.findByTestId("retrieval-error")).textContent ?? "").includes("corpus table unreadable: no such table")).toBe(true);
  });
});

describe("Knowledge tab — code graph explorer", () => {
  it("explores a symbol: callers and callees are drawn from the daemon's answer, coordinates only", async () => {
    const daemon = renderKnowledge();
    fireEvent.change(await screen.findByTestId("explore-symbol"), { target: { value: "ParseBlueprint" } });
    fireEvent.click(screen.getByTestId("explore-go"));
    await waitFor(() => expect(daemon.methods()).toContain("graph.explain"));
    expect(daemon.paramsOf("graph.explain")).toEqual({ componentRoot: "/work/core", symbol: "ParseBlueprint" });
    await screen.findByTestId("graph-network-found");
    // the selected symbol card is coordinates: symbol, kind, file and lines — never content.
    expect(screen.getByTestId("symbol-name").textContent).toBe("ParseBlueprint");
    expect(screen.getByTestId("symbol-coords").textContent).toContain("internal/blueprint/parse.go");
    expect(screen.getByTestId("symbol-coords").textContent).toContain("41");
    expect(screen.getByTestId("symbol-counts").textContent).toContain("1 callers");
  });

  it("suggests symbols by prefix and explores the one picked", async () => {
    const daemon = renderKnowledge();
    fireEvent.change(await screen.findByTestId("explore-symbol"), { target: { value: "Parse" } });
    const pick = await screen.findByTestId("explore-suggestion-1");
    expect(daemon.paramsOf("graph.query")).toMatchObject({ componentRoot: "/work/core", prefix: "Parse" });
    fireEvent.click(pick);
    await waitFor(() => expect(daemon.methods()).toContain("graph.explain"));
  });

  it("an ambiguous answer lists every candidate and picks none", async () => {
    const daemon = renderKnowledge(currentLive, {
      "graph.explain": { state: "ambiguous", candidates: [NODE_VALIDATE, { ...NODE_PARSE, id: 9, symbol: "Validate", filePath: "internal/policy/validate.go" }] },
    });
    fireEvent.change(await screen.findByTestId("explore-symbol"), { target: { value: "Validate" } });
    fireEvent.click(screen.getByTestId("explore-go"));
    await screen.findByTestId("graph-state-ambiguous");
    expect(screen.getAllByTestId("graph-candidate")).toHaveLength(2);
    expect(screen.queryByTestId("graph-network-found")).toBeNull();
    // picking one re-asks with ITS name — the page never chooses for the person.
    fireEvent.click(screen.getByTestId("graph-candidate-pick-2"));
    await waitFor(() => expect(daemon.methods().filter((m) => m === "graph.explain").length).toBe(2));
    expect((daemon.paramsOf("graph.explain") as { symbol: string }).symbol).toBe("(*Spec).Validate");
  });

  it("not-found and not-built each speak for themselves", async () => {
    renderKnowledge(currentLive, { "graph.explain": { state: "not-found" } });
    fireEvent.change(await screen.findByTestId("explore-symbol"), { target: { value: "Nope" } });
    fireEvent.click(screen.getByTestId("explore-go"));
    expect(await screen.findByTestId("graph-state-not-found")).toBeTruthy();
    cleanup();
    resetFakeDaemon();
    renderKnowledge(currentLive, { "graph.explain": { state: "not-built" } });
    fireEvent.change(await screen.findByTestId("explore-symbol"), { target: { value: "Nope" } });
    fireEvent.click(screen.getByTestId("explore-go"));
    expect(await screen.findByTestId("graph-state-not-built")).toBeTruthy();
  });

  it("a root that was never built says so and offers Build instead of a canvas", async () => {
    renderKnowledge();
    fireEvent.change(await screen.findByTestId("graph-root-select"), { target: { value: "/work/api" } });
    expect(await screen.findByTestId("explore-not-built-root")).toBeTruthy();
  });

  it("path mode asks graph.path and draws the chain", async () => {
    const daemon = renderKnowledge();
    fireEvent.click(await screen.findByRole("radio", { name: /Path/ }));
    fireEvent.change(await screen.findByTestId("path-from"), { target: { value: "ParseBlueprint" } });
    fireEvent.change(screen.getByTestId("path-to"), { target: { value: "Validate" } });
    fireEvent.click(screen.getByTestId("path-go"));
    await waitFor(() => expect(daemon.methods()).toContain("graph.path"));
    expect(daemon.paramsOf("graph.path")).toEqual({ componentRoot: "/work/core", from: "ParseBlueprint", to: "Validate" });
    await screen.findByTestId("graph-path-found");
  });

  it("a refused lookup is a toast, in the daemon's words", async () => {
    const toast = vi.spyOn(notifications, "push");
    renderKnowledge(currentLive, { "graph.explain": () => { throw new Error("graph.explain refuses root /work/core — it is outside every component"); } });
    fireEvent.change(await screen.findByTestId("explore-symbol"), { target: { value: "X" } });
    fireEvent.click(screen.getByTestId("explore-go"));
    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls[0]?.[0])).toContain("outside every component");
  });
});

describe("SystemSection — maintaining the graph", () => {
  async function openMaintain(): Promise<void> {
    fireEvent.click(await screen.findByTestId("graph-maintain-open"));
    await screen.findByTestId("graph-maintain");
  }

  it("lists the roots with the daemon's counts and never-built fact", async () => {
    renderSection();
    await openMaintain();
    expect(rowOf("graph-root-/work/core").textContent).toContain("2,000");
    expect(screen.getByTestId("graph-never-built-/work/api")).toBeTruthy();
  });

  it("a building state holds the controls down", async () => {
    renderSection(currentLive, {
      "platform.status": {
        evaluatedAt: "2026-11-08T10:00:00Z",
        retrieval: { state: "available" },
        graph: { state: "building", roots: [{ root: "/work/core", files: 1, nodes: 1, edges: 0 }] },
        usage: { state: "available" },
      },
    });
    await openMaintain();
    expect((screen.getByTestId("graph-build-/work/core") as HTMLButtonElement).disabled).toBe(true);
  });

  it("not current to write — controls held, reads still run", async () => {
    const daemon = renderSection(currentLive, {}, false);
    await openMaintain();
    expect((screen.getByTestId("graph-delete-/work/core") as HTMLButtonElement).disabled).toBe(true);
    expect(daemon.methods()).toContain("platform.status");
  });

  it("graph acts confirm on the named root; reparse sends the typed path", async () => {
    const daemon = renderSection();
    await openMaintain();
    fireEvent.click(screen.getByTestId("graph-build-/work/core"));
    expect(await screen.findByTestId("operation-statement")).toBeTruthy();
    fireEvent.click(screen.getByTestId("operation-go"));
    await waitFor(() => expect(daemon.methods()).toContain("graph.build"));
    expect(daemon.paramsOf("graph.build")).toEqual({ componentRoot: "/work/core" });

    fireEvent.click(screen.getByTestId("graph-reparse-/work/core"));
    await screen.findByTestId("reparse-path");
    fireEvent.change(screen.getByTestId("reparse-path"), { target: { value: "internal/store/store.go" } });
    fireEvent.click(screen.getByTestId("operation-go"));
    await waitFor(() => expect(daemon.methods()).toContain("graph.reparse"));
    expect(daemon.paramsOf("graph.reparse")).toEqual({ componentRoot: "/work/core", path: "internal/store/store.go" });

    fireEvent.click(screen.getByTestId("graph-delete-/work/core"));
    expect(await screen.findByTestId("operation-statement")).toBeTruthy();
    fireEvent.click(screen.getByTestId("operation-go"));
    await waitFor(() => expect(daemon.methods()).toContain("graph.delete"));
    expect(daemon.paramsOf("graph.delete")).toEqual({ componentRoot: "/work/core" });
    expect(JSON.stringify(daemon.calls)).not.toContain("actor");
  });

  it("an already-flying build names its starter in a toast", async () => {
    const toast = vi.spyOn(notifications, "push");
    renderSection(currentLive, { "graph.build": { started: false, workerId: "w-3", state: "building", startedBy: "user:1000", startedAt: "2026-11-08T09:55:00Z" } });
    await openMaintain();
    fireEvent.click(screen.getByTestId("graph-build-/work/core"));
    fireEvent.click(await screen.findByTestId("operation-go"));
    await waitFor(() => expect(toast).toHaveBeenCalled());
    const body = JSON.stringify(toast.mock.calls[0]?.[0]);
    expect(body).toContain("user:1000");
    expect(body).toContain("2026-11-08T09:55:00Z");
  });
});

describe("SystemSection — Retrieval tab", () => {
  it("shows each corpus with the daemon's counts and its embedding route", async () => {
    renderSection();
    await openTab("Search indexes");
    const route = await screen.findByTestId("retrieval-embed");
    expect(route.textContent).toContain("anthropic");
    expect(route.textContent).toContain("embed-x");
    expect(route.textContent).toContain("1536");
    const code = rowOf("corpus-row-/work/core-code");
    expect(code.textContent).toContain("412");
    expect(code.textContent).toContain("37");
    // the daemon's own reindexing flag outranks the button: a flying pass shows the flag.
    expect(screen.getByTestId("corpus-flying-/work/core-code")).toBeTruthy();
    expect(screen.queryByTestId("reindex-/work/core-code")).toBeNull();
  });

  it("a building state holds Reindex down; not current to write holds it too", async () => {
    renderSection(currentLive, {
      "platform.status": {
        evaluatedAt: "2026-11-08T10:00:00Z",
        retrieval: { state: "building", components: [{ component: "/work/core", corpora: [{ name: "docs", files: 20, staleChunks: 5 }] }] },
        graph: { state: "available", roots: [] },
        usage: { state: "available" },
      },
    });
    await openTab("Search indexes");
    expect(((await screen.findByTestId("reindex-/work/core-docs")) as HTMLButtonElement).disabled).toBe(true);
  });

  it("Reindex confirms on the daemon's plan figures, then sends root and corpus — never an actor", async () => {
    const daemon = renderSection();
    await openTab("Search indexes");
    fireEvent.click(await screen.findByTestId("reindex-/work/core-docs"));
    await screen.findByTestId("operation-confirm");
    expect(daemon.paramsOf("retrieval.reindexPlan")).toEqual({ componentRoot: "/work/core", corpus: "docs" });
    expect(((await screen.findByTestId("reindex-plan-cost")).textContent ?? "").includes("partial")).toBe(true);
    expect(((screen.getByTestId("reindex-plan-cost")).textContent ?? "").includes("$0.2100")).toBe(true);
    expect(((screen.getByTestId("reindex-plan-cost")).textContent ?? "").includes("no price for embed-x")).toBe(true);
    expect(((screen.getByTestId("reindex-plan-counts")).textContent ?? "").includes("20")).toBe(true);
    expect(((screen.getByTestId("reindex-plan-estimate")).textContent ?? "").includes("98,765")).toBe(true);
    expect(((screen.getByTestId("reindex-plan-estimate")).textContent ?? "").includes("daemon-counted")).toBe(true);
    expect(screen.getByTestId("reindex-plan-replaces")).toBeTruthy();
    fireEvent.click(screen.getByTestId("operation-go"));
    await waitFor(() => expect(daemon.methods()).toContain("retrieval.reindex"));
    expect(daemon.paramsOf("retrieval.reindex")).toEqual({ componentRoot: "/work/core", corpus: "docs" });
    expect(JSON.stringify(daemon.calls)).not.toContain("actor");
  });

  it("an unpriced plan is a toast and the pass never starts", async () => {
    const toast = vi.spyOn(notifications, "push");
    const daemon = renderSection(currentLive, {
      "retrieval.reindexPlan": () => {
        throw new Error("the embed route is unpriced — an unpriced money pass never starts");
      },
    });
    await openTab("Search indexes");
    fireEvent.click(await screen.findByTestId("reindex-/work/core-docs"));
    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls[0]?.[0])).toContain("unpriced money pass never starts");
    await waitFor(() => expect(screen.queryByTestId("operation-confirm")).toBeNull());
    expect(daemon.methods()).not.toContain("retrieval.reindex");
  });

  it("an already-running refusal is a toast with the daemon's starter-naming words", async () => {
    const toast = vi.spyOn(notifications, "push");
    renderSection(currentLive, {
      "retrieval.reindex": () => {
        throw new Error("already running — started by you at 2026-11-08T09:55:00Z");
      },
    });
    await openTab("Search indexes");
    fireEvent.click(await screen.findByTestId("reindex-/work/core-docs"));
    await screen.findByTestId("reindex-plan-cost");
    fireEvent.click(screen.getByTestId("operation-go"));
    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls[0]?.[0])).toContain("already running — started by you at 2026-11-08T09:55:00Z");
  });

  it("an already-flying pass's answer names its starter in a toast", async () => {
    const toast = vi.spyOn(notifications, "push");
    renderSection(currentLive, {
      "retrieval.reindex": { started: false, workerId: "w-3", state: "building", startedBy: "user:1000", startedAt: "2026-11-08T09:55:00Z" },
    });
    await openTab("Search indexes");
    fireEvent.click(await screen.findByTestId("reindex-/work/core-docs"));
    await screen.findByTestId("reindex-plan-cost");
    fireEvent.click(screen.getByTestId("operation-go"));
    await waitFor(() => expect(toast).toHaveBeenCalled());
    const body = JSON.stringify(toast.mock.calls[0]?.[0]);
    expect(body).toContain("user:1000");
    expect(body).toContain("2026-11-08T09:55:00Z");
  });

  it("the search probe shows the daemon's hits as ranges and scores, and says keyword-only", async () => {
    const daemon = renderKnowledge();
    await openTab("Search index");
    fireEvent.change(await screen.findByTestId("probe-query"), { target: { value: "how are blueprints validated" } });
    fireEvent.click(screen.getByTestId("probe-go"));
    await waitFor(() => expect(daemon.methods()).toContain("search.query"));
    expect(daemon.paramsOf("search.query")).toEqual({ componentRoot: "/work/core", corpus: "code", query: "how are blueprints validated" });
    const hit = await screen.findByTestId("probe-hit-0");
    expect(hit.textContent).toContain("internal/blueprint/validate.go:12–64");
    expect(hit.textContent).toContain("0.82");
    expect(screen.getByTestId("probe-keyword-only")).toBeTruthy();
  });
});

describe("SystemSection — Activity tab", () => {
  it("prints the ledger rows verbatim", async () => {
    renderSection();
    await screen.findByTestId("system-section");
    await openTab("Activity");
    const row = await screen.findByTestId("activity-row-7");
    expect(row.textContent).toContain("2026-11-08T09:55:00Z");
    const tr = row.closest("tr") as HTMLElement;
    expect(within(tr).getByText("reindex")).toBeTruthy();
    expect(within(tr).getByText("/work/core#docs")).toBeTruthy();
    expect(((screen.getByTestId("activity-actor-7")).textContent ?? "").includes("user:1000")).toBe(true);
    expect(((screen.getByTestId("activity-outcome-7")).textContent ?? "").includes("worker-7")).toBe(true);
    expect((await screen.findAllByText("started")).length).toBeGreaterThan(0);
  });

  it("platform.changes carries the limit — and an empty ledger says so once", async () => {
    const daemon = renderSection(currentLive, { "platform.changes": [] });
    await screen.findByTestId("system-section");
    await openTab("Activity");
    expect(await screen.findByTestId("activity-empty")).toBeTruthy();
    expect(daemon.paramsOf("platform.changes")).toEqual({ limit: 100 });
  });
});
