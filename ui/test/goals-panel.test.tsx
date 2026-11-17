import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { GoalsPanel } from "../src/features/goals/GoalsPanel";
import { APP_LOCALES } from "../src/lib/i18n";
import { LiveController } from "../src/lib/live";
import type { LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// A goal is a row with a review state and a version; it owns the documents its
// work produced and its tasks hang off it. The panel shows what the daemon
// answered and sends a decision as `goal.decide` with the goal's own version.

const live = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

const GOAL = {
  id: "goal-7a1f", componentId: "prj-1", version: 3, title: "Ship the org board", type: "feature", bodyNote: "criteria 6-11",
  sourceKind: "brainstorm", sourceRef: "", reviewState: "in_review", createdByKind: "agent", createdById: "brainstormer",
  createdAt: "2026-11-08T09:00:00Z", updatedAt: "2026-11-08T09:00:00Z",
};
const TASK = {
  id: "tsk-1", taskid: "T1", loopSlug: "plan-1", componentRoot: "/repo/alpha", title: "Write the migration", status: "pending",
  manual: false, assigneeKind: "agent", assignee: "sql-engineer", approvalState: "none", loopSlugs: ["plan-1"],
  costUsd: 0.42, costIsFloor: true, updatedAt: "2026-11-08T09:05:00Z", goalId: "goal-7a1f",
};
const PICTURE = {
  goal: { ...GOAL, rootId: "prj-1" }, components: [], attachments: [], reviews: [], loops: [{ id: "l1", slug: "work-1", componentRoot: "/repo/alpha", status: "running", blueprint: "feature-loop" }],
  executionState: "running", cost: { usd: 1.5, isFloor: true },
  documents: [{ name: "brief.md", rev: 2, bytes: 120, loopSlug: "brain-1", componentRoot: "/repo/alpha", writtenBy: "brainstormer", writtenAt: "2026-11-08T09:00:00Z" }],
};

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({
    matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {},
    addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});
afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

function renderPanel(extra: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({
    responses: {
      "goal.list": [GOAL],
      "task.list": { tasks: [TASK] },
      "goal.show": PICTURE,
      "loop.read": { file: "brief.md", rev: 2, bytes: 120, sha256: "x", writtenAt: "", writtenBy: "brainstormer", content: "# The brief\nbuild it" },
      "goal.decide": { changed: true, state: "approved", decidedBy: "1000", decidedAt: "2026-11-08T10:00:00Z" },
      ...extra,
    },
  });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <GoalsPanel controller={new LiveController()} live={live} canWrite />
    </I18nProvider>,
  );
  return daemon;
}

describe("goals and tasks on the roadmap", () => {
  it("lists the goals with their review state and version", async () => {
    renderPanel();
    const row = await screen.findByTestId("goal-row-goal-7a1f");
    expect(row.textContent).toContain("Ship the org board");
    const line = row.closest("tr")?.textContent ?? "";
    expect(line).toContain("in_review");
    expect(line).toContain("v3");
  });

  it("filters travel as params, never as a client sweep", async () => {
    const daemon = renderPanel();
    await screen.findByTestId("goal-row-goal-7a1f");
    fireEvent.change(screen.getByTestId("goals-state-filter"), { target: { value: "approved" } });
    await waitFor(() => expect(daemon.paramsOf("goal.list")).toEqual({ state: "approved" }));
  });

  it("opens a goal: its documents, loops, tasks, and the text of a document", async () => {
    const daemon = renderPanel();
    fireEvent.click(await screen.findByTestId("goal-row-goal-7a1f"));
    expect(await screen.findByTestId("goal-title")).toBeTruthy();
    expect(daemon.paramsOf("goal.show")).toEqual({ id: "goal-7a1f" });
    expect((await screen.findByTestId("goal-execution")).textContent).toBe("running");
    expect((await screen.findByTestId("goal-loops")).textContent).toContain("work-1");
    const doc = await screen.findByTestId("goal-doc-brief.md");
    expect(doc.textContent).toContain("rev 2");
    expect(doc.textContent).toContain("loop brain-1");
    fireEvent.click(doc);
    // The text is read from the loop that wrote it.
    expect((await screen.findByTestId("goal-doc-text")).textContent).toContain("build it");
    expect(daemon.paramsOf("loop.read")).toEqual({ slug: "brain-1", componentRoot: "/repo/alpha", name: "brief.md" });
    // The tasks in the drawer are the goal's own, asked of the daemon by goal.
    await waitFor(() => expect(daemon.calls.some((c) => c.method === "task.list" && JSON.stringify(c.params) === JSON.stringify({ goal: "goal-7a1f" }))).toBe(true));
  });

  it("approves with the goal's own version and the reason, and shows a refusal as the daemon wrote it", async () => {
    const daemon = renderPanel();
    fireEvent.click(await screen.findByTestId("goal-row-goal-7a1f"));
    fireEvent.change(await screen.findByTestId("goal-reason"), { target: { value: "brief matches the ask" } });
    fireEvent.click(screen.getByTestId("goal-approve"));
    await waitFor(() => expect(daemon.paramsOf("goal.decide")).toEqual({ id: "goal-7a1f", version: 3, decision: "approved", reason: "brief matches the ask" }));
  });

  it("a goal that is not in review offers no decision", async () => {
    renderPanel({ "goal.show": { ...PICTURE, goal: { ...GOAL, reviewState: "approved" } } });
    fireEvent.click(await screen.findByTestId("goal-row-goal-7a1f"));
    await screen.findByTestId("goal-title");
    await waitFor(() => expect(screen.getByTestId("goal-review-state").textContent).toBe("approved"));
    expect(screen.queryByTestId("goal-decision")).toBeNull();
  });
});
