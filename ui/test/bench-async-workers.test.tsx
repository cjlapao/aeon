import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AsyncWorkersPage } from "../src/features/test-bench/pages/AsyncWorkersPage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { getBenchPage } from "../src/features/test-bench/lib/benchRegistry";
import type { WorkerView } from "../src/lib/asyncworkers";

// S64's ledger page, held to what the slice is for:
//
//   * the SIX states are six renderings (P31) — `interrupted` is not
//     `failed` in the DOM either; the daemon dying and the work refusing
//     are different instructions, and a page that flattens them tells
//     the user a lie about what happened;
//   * progress is UNKNOWN unless the runner reported it — absent never
//     renders as a confident 0%;
//   * there is no start affordance anywhere — the ledger holds Go
//     functions; starting belongs to the owning service's own door;
//   * rows are truth: the mount read answers the reopen-mid-build
//     question, and events only repaint on top of it.

function row(overrides: Partial<WorkerView> & { workerId: string; state: WorkerView["state"] }): WorkerView {
  return {
    workerType: "graph.build",
    componentRoot: "/home/dev/shop",
    queuedAt: "2026-10-30T17:41:02.000Z",
    ...overrides,
  };
}

const LIVE_ROW = row({
  workerId: "aw_live1",
  state: "running",
  progress: 0.4,
  progressNote: "parsing cmd/aeon/serve.go",
  startedAt: "2026-10-30T17:41:03.000Z",
});

function mount(workers: WorkerView[], extra: Record<string, unknown> = {}): FakeDaemon {
  const fake = installFakeDaemon({
    responses: {
      "asyncworkers.list": { workers, types: ["graph.build"] },
      ...extra,
    },
  });
  render(<AsyncWorkersPage />);
  return fake;
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("registration", () => {
  it("registers itself under /bench/async-workers and needs the daemon", async () => {
    await import("../src/features/test-bench/pages/registerAll");
    const def = getBenchPage("async-workers");
    expect(def).toBeDefined();
    expect(def?.path).toBe("/bench/async-workers");
    expect(def?.needsConnection).toBe(true);
  });
});

describe("the mount read (the reopen-mid-build promise)", () => {
  it("asks the ledger on connect and paints the live row", async () => {
    const fake = mount([LIVE_ROW]);
    await waitFor(() => expect(fake.methods()).toContain("asyncworkers.list"));
    await waitFor(() => expect(screen.queryByTestId("aw-row-running")).not.toBeNull());
    expect(screen.getByTestId("aw-row-running").textContent).toContain("40%");
    expect(screen.getByTestId("aw-row-running").textContent).toContain("serve.go");
  });

  it("an empty ledger is the row's answer, not a failed read", async () => {
    mount([]);
    await waitFor(() => expect(screen.queryByTestId("aw-empty")).not.toBeNull());
    expect(screen.getByTestId("aw-empty").textContent).toContain("not a failed read");
  });

  it("progress the runner never reported renders as unknown, never 0%", async () => {
    mount([row({ workerId: "aw_noprogress", state: "running" })]);
    await waitFor(() => expect(screen.queryByTestId("aw-row-running")).not.toBeNull());
    expect(screen.getByTestId("aw-row-running").textContent).toContain("progress unknown");
    expect(screen.getByTestId("aw-row-running").textContent).not.toContain("0%");
  });
});

describe("the six states are six renderings", () => {
  it("every state keeps its own row testid and its own words", async () => {
    mount([
      row({ workerId: "aw_q", state: "queued" }),
      row({ workerId: "aw_r", state: "running" }),
      row({ workerId: "aw_f", state: "finished", finishedAt: "2026-10-30T17:51:47.000Z", result: null }),
      row({ workerId: "aw_x", state: "failed", error: "open /x: permission denied", finishedAt: "t" }),
      row({ workerId: "aw_c", state: "cancelled", cancelRequested: true, error: "cancelled", finishedAt: "t" }),
      row({ workerId: "aw_i", state: "interrupted", error: "daemon stopped before the worker finished", finishedAt: "t" }),
    ]);
    for (const s of ["queued", "running", "finished", "failed", "cancelled", "interrupted"]) {
      await waitFor(() => expect(screen.queryByTestId(`aw-row-${s}`)).not.toBeNull());
    }
    // failed carries the runner's sentence; interrupted carries its OWN
    // wording — never the other's.
    expect(screen.getByTestId("aw-row-failed").textContent).toContain("permission denied");
    expect(screen.getByTestId("aw-row-interrupted").textContent).toContain("daemon stopped before the worker finished");
    expect(screen.getByTestId("aw-row-interrupted").textContent).toContain("nothing restarted it on purpose");
    expect(screen.getByTestId("aw-row-cancelled").textContent).toContain("you asked, it stopped");
    // Live states offer cancel; terminal states never do.
    expect(screen.queryByTestId("aw-cancel-aw_r")).not.toBeNull();
    expect(screen.queryByTestId("aw-cancel-aw_x")).toBeNull();
  });
});

describe("cancel is the ledger's only write, and start does not exist", () => {
  it("cancel names the worker through asyncworkers.cancel", async () => {
    const fake = mount([LIVE_ROW]);
    await waitFor(() => expect(screen.queryByTestId("aw-cancel-aw_live1")).not.toBeNull());
    fireEvent.click(screen.getByTestId("aw-cancel-aw_live1"));
    await waitFor(() => expect(fake.methods()).toContain("asyncworkers.cancel"));
    expect(fake.paramsOf("asyncworkers.cancel")).toEqual({ workerId: "aw_live1" });
  });

  it("no start door: the page never offers one and never asks for one", async () => {
    const fake = mount([LIVE_ROW]);
    await waitFor(() => expect(screen.queryByTestId("aw-row-running")).not.toBeNull());
    for (const button of Array.from(document.querySelectorAll("button"))) {
      expect(button.textContent?.trim().toLowerCase()).not.toMatch(/^start|fire|run worker|dispatch/);
    }
    expect(fake.methods().some((m) => m.startsWith("asyncworkers.") && m !== "asyncworkers.list" && m !== "asyncworkers.cancel")).toBe(false);
  });
});

describe("events repaint, rows stay truth", () => {
  it("an asyncworker.finished event lands a new row without a re-read", async () => {
    const fake = mount([]);
    await waitFor(() => expect(screen.queryByTestId("aw-empty")).not.toBeNull());
    fake.emit({
      subscriptionId: "s-1",
      seq: 12,
      kind: "asyncworker.finished",
      at: "2026-10-30T17:51:47.123Z",
      payload: row({
        workerId: "aw_evt",
        state: "finished",
        finishedAt: "2026-10-30T17:51:47.000Z",
        result: { root: "/home/dev/shop", files: 214, filesSkipped: 9, nodes: 1893, edges: 5310, unresolvedEdges: 412 },
      }),
    });
    await waitFor(() => expect(screen.queryByTestId("aw-row-finished")).not.toBeNull());
    // The graph.build result prints as the BuildReport the socket used to
    // return inline — same numbers, one rendering.
    expect(screen.getByTestId("aw-row-finished").textContent).toContain("finished");
    fireEvent.click(screen.getByTestId("aw-show-aw_evt"));
    await waitFor(() => expect(document.body.textContent).toContain("214 files (9 skipped)"));
    expect(document.body.textContent).toContain("412 references unresolved");
  });
});
