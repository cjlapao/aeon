import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { compactTokens, contextSteps, contextUse, newest } from "../src/features/loop-workspace/contextModel";
import { ContextPanel } from "../src/features/loop-workspace/inspector/ContextPanel";
import { DocumentsTab } from "../src/features/loop-workspace/inspector/DocumentsTab";
import { StepTab } from "../src/features/loop-workspace/inspector/StepTab";
import type { StepSummary } from "../src/features/loop-workspace/stepSummaries";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

const sum = (o: Partial<StepSummary>): StepSummary => ({ stepId: 1, loopId: "L", lane: "x", agent: "a", status: "done", turns: 1, toolCalls: 0, toolErrors: 0,
  inputTokens: 0, outputTokens: 0, reasoningTokens: 0, wallMs: 0, costState: "known", firstEvent: 1, lastEvent: 2, contextTokens: 0, contextWindow: 100000, route: "qwen", ...o });

describe("context model", () => {
  it("reads pressure from tokens and the route's window, and never invents a share without one", () => {
    expect(contextUse(sum({ contextTokens: 50000 })).tone).toBe("ok");
    expect(contextUse(sum({ contextTokens: 75000 })).tone).toBe("warm");
    expect(contextUse(sum({ contextTokens: 95000 })).tone).toBe("hot");
    const none = contextUse(sum({ contextTokens: 5000, contextWindow: 0 }));
    expect(none.pct).toBeUndefined();
    expect(none.tone).toBe("unknown");
  });

  it("picks the running step as the current one, else the latest, and skips steps that never called a model", () => {
    const { all, current } = contextSteps([sum({ stepId: 1, contextTokens: 10 }), sum({ stepId: 2, contextTokens: 20, status: "running" }), sum({ stepId: 3, contextTokens: 30 }), sum({ stepId: 4, contextTokens: 0 })]);
    expect(all.map((u) => u.stepId)).toEqual([3, 2, 1]);
    expect(current?.stepId).toBe(2);
    expect(contextSteps([sum({ stepId: 1, contextTokens: 10 }), sum({ stepId: 3, contextTokens: 30 })]).current?.stepId).toBe(3);
  });

  it("keeps the newest N of a list, with the rest counted as hidden", () => {
    const items = Array.from({ length: 25 }, (_, i) => i);
    expect(newest(items, 10, false)).toEqual({ shown: items.slice(15), hidden: 15 });
    expect(newest(items, 10, true).hidden).toBe(0);
    expect(newest([1, 2], 10, false).hidden).toBe(0);
    expect(compactTokens(25211)).toBe("25.2k");
    expect(compactTokens(512)).toBe("512");
  });
});

describe("the inspector's context, lists and documents", () => {
  beforeEach(() => { resetFakeDaemon(); });
  afterEach(() => cleanup());

  it("draws the running agent's context and lists the others, each one selectable", () => {
    render(<ContextPanel summaries={[sum({ stepId: 1, agent: "investigator", contextTokens: 95000 }), sum({ stepId: 2, agent: "planner", status: "running", contextTokens: 20000 })]} />);
    expect(screen.getByTestId("context-panel")).toBeTruthy();
    // The running agent is the one in view; the other is one click away.
    expect(screen.getByRole("button", { name: /planner · running/ }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: /investigator/ }));
    expect(screen.getByRole("button", { name: /investigator/ }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("status").textContent).toContain("nearly full");
  });

  it("says plainly when the route has no context window, instead of drawing a made-up share", () => {
    render(<ContextPanel summaries={[sum({ contextTokens: 5000, contextWindow: 0 })]} />);
    expect(screen.getByTestId("context-unknown-window")).toBeTruthy();
  });

  it("shows the newest ten tool calls and sub-steps, and the rest on request", () => {
    const tools = Array.from({ length: 14 }, (_, i) => ({ tool: `t${i}`, summary: "", status: "done" as const, gated: false }));
    const subSteps = Array.from({ length: 12 }, (_, i) => ({ id: `tool:${i}`, label: `s${i}`, status: "done" as const }));
    render(<MemoryRouter><StepTab telemetry={{ status: "running", activeAgents: 1, tools, filesChanged: [] }} step={{ id: "x", title: "x", status: "running", subSteps } as never}
      position={{ index: 1, total: 1 }} onOpenFile={() => undefined} /></MemoryRouter>);
    expect(screen.queryByText("t0")).toBeNull();
    expect(screen.getByText(/t13/)).toBeTruthy();
    fireEvent.click(screen.getByTestId("tools-more"));
    expect(screen.getByText(/t0/)).toBeTruthy();
    expect(screen.getByTestId("sub-steps-more").textContent).toContain("2 earlier");
  });

  it("lists the loop's internal files and opens one", async () => {
    installFakeDaemon({ responses: {
      "loop.documents": { slug: "s", componentRoot: "/r", documents: [{ file: "discovery.md", rev: 2, bytes: 2048, writtenBy: "investigator", writtenAt: "2026-11-16T12:00:00Z" }] },
      "loop.read": { content: "# Findings\n\nhello" },
    } });
    render(<DocumentsTab loopId="L" />);
    const row = await screen.findByRole("button", { name: /discovery\.md/ });
    expect(row.textContent).toContain("rev 2");
    fireEvent.click(row);
    await waitFor(() => expect(screen.getByTestId("document-body").textContent).toContain("Findings"));
  });
});

vi.mock("../src/components/kit", async (orig) => {
  const actual = await orig<typeof import("../src/components/kit")>();
  // The chart needs a layout engine the test DOM does not have; its data and centre are what matter here.
  const Svg = ({ children }: { children: unknown }) => <div data-testid="chart">{children as never}</div>;
  const Pie = () => null;
  const PieCenter = ({ value, subtitle }: { value: string; subtitle: string }) => <span data-testid="chart-center">{value} {subtitle}</span>;
  return { ...actual, Chart: { ...actual.Chart, Svg, Pie, PieCenter } };
});
