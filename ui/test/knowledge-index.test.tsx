import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { GraphTab } from "../src/features/settings/system/GraphTab";
import { RetrievalIndexAll } from "../src/features/settings/system/RetrievalIndexAll";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

const plan = (files: number) => ({ files, staleFiles: files, staleChunks: files * 3, estimatedInputTokens: 1000, estimateBasis: "chars/4", willReplaceEmbeddings: false, cost: { state: "known", usd: 0.01 } });

describe("indexing a project's knowledge", () => {
  beforeEach(() => {
    resetFakeDaemon();
    window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
  });
  afterEach(() => cleanup());

  it("the graph of a project has no root to pick, and its Index button builds that project's root", async () => {
    const daemon = installFakeDaemon({ responses: { "graph.build": { started: true } } });
    let changed = 0;
    render(<GraphTab block={{ state: "ready", roots: [] }} canWrite onChanged={() => { changed += 1; }} projectRoot="/r/new-project" />);
    expect(screen.queryByTestId("graph-root-select")).toBeNull();
    expect(screen.getByTestId("graph-project-root").textContent).toBe("/r/new-project");
    const button = screen.getByTestId("graph-index");
    expect(button.textContent).toContain("actions.indexAll"); // nothing built yet (no catalog here, so the key shows)
    fireEvent.click(button);
    await waitFor(() => expect(changed).toBe(1));
    expect(daemon.paramsOf("graph.build")).toEqual({ componentRoot: "/r/new-project" });
  });

  it("says Reindex once the graph exists, and serves a sub project from its repository's index", () => {
    installFakeDaemon({});
    render(<GraphTab block={{ state: "ready", roots: [{ root: "/r/aeon", files: 10, nodes: 5, edges: 4 }] }} canWrite onChanged={() => undefined} projectRoot="/r/aeon/ui" />);
    expect(screen.getByTestId("graph-project-root").textContent).toBe("/r/aeon");
    expect(screen.getByTestId("graph-index").textContent).toContain("actions.reindexAll");
  });

  it("the search Index button asks each corpus's plan first, shows the money, and starts both passes on confirm", async () => {
    const daemon = installFakeDaemon({ responses: { "routes.list": { routes: [{ id: "embed", capabilities: ["embedding"] }], providers: [], persisted: true }, "retrieval.reindexPlan": plan(12), "retrieval.reindex": { started: true } } });
    render(<RetrievalIndexAll componentRoot="/r/p" block={{ state: "ready", components: [{ component: "/r/p", corpora: [{ name: "code", files: 0, staleChunks: 0 }] }] } as never} canWrite onChanged={() => undefined} />);
    const button = screen.getByTestId("search-index-all");
    expect(button.textContent).toContain("actions.indexAll");
    fireEvent.click(button);
    expect(await screen.findByTestId("index-all-code")).toBeTruthy();
    expect(screen.getByTestId("index-all-history")).toBeTruthy();
    expect(daemon.methods().filter((m) => m === "retrieval.reindex")).toHaveLength(0); // asking a plan starts nothing
    const buttons = screen.getAllByRole("button", { name: /actions\.indexAll/ });
    fireEvent.click(buttons[buttons.length - 1]!); // the confirm
    await waitFor(() => expect(daemon.methods().filter((m) => m === "retrieval.reindex")).toHaveLength(2));
    expect(daemon.methods().filter((m) => m === "retrieval.reindexPlan")).toHaveLength(2); // one plan per corpus, before anything started
  });

  it("follows a started pass to its end and says how it went, so a pass that fails at once is not silent", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { notifications } = await import("../src/lib/notifications");
    const pushed: { title: string; body: string }[] = [];
    const spy = vi.spyOn(notifications, "push").mockImplementation(((n: { title: string; body: string }) => { pushed.push(n); return undefined; }) as never);
    installFakeDaemon({ responses: {
      "routes.list": { routes: [{ id: "embed", capabilities: ["embedding"] }], providers: [], persisted: true },
      "retrieval.reindexPlan": plan(2),
      "retrieval.reindex": { started: true, workerId: "w1" },
      "asyncworkers.get": { state: "failed", error: "no embedding route is configured" },
    } });
    render(<RetrievalIndexAll componentRoot="/r/p" block={{ state: "ready", components: [] } as never} canWrite onChanged={() => undefined} />);
    fireEvent.click(screen.getByTestId("search-index-all"));
    await screen.findByTestId("index-all-code");
    const buttons = screen.getAllByRole("button", { name: /actions\.indexAll/ });
    fireEvent.click(buttons[buttons.length - 1]!);
    await vi.advanceTimersByTimeAsync(3000);
    await waitFor(() => expect(pushed.some((n) => n.title.includes("failed") && n.body.includes("no embedding route"))).toBe(true));
    spy.mockRestore();
    vi.useRealTimers();
  });

  it("says what is missing, before asking anything, when no model can embed", async () => {
    const { notifications } = await import("../src/lib/notifications");
    const pushed: { title: string; body: string }[] = [];
    const spy = vi.spyOn(notifications, "push").mockImplementation(((n: { title: string; body: string }) => { pushed.push(n); return undefined; }) as never);
    const daemon = installFakeDaemon({ responses: { "routes.list": { routes: [{ id: "chat", capabilities: ["text"] }], providers: [], persisted: true } } });
    render(<RetrievalIndexAll componentRoot="/r/p" block={{ state: "ready", components: [] } as never} canWrite onChanged={() => undefined} />);
    fireEvent.click(screen.getByTestId("search-index-all"));
    await waitFor(() => expect(pushed.some((n) => n.title.includes("No embedding model") && n.body.includes("Embedding"))).toBe(true));
    expect(daemon.methods()).not.toContain("retrieval.reindexPlan");
    expect(screen.queryByTestId("index-all-plan")).toBeNull();
    spy.mockRestore();
  });
});
