import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { RoadmapPage, STATUS_LABELS } from "../src/features/test-bench/pages/RoadmapPage";
import { RoadmapPage as GanttPage } from "../src/features/roadmap/RoadmapPage";
import { BENCH_PAGES, isBenchPage } from "../src/app/routes";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { ROADMAP_STATUSES, type RoadmapItemView, type RoadmapListResult } from "../src/lib/protocol";

// S26 T5/T6 + the user-requested bench CRUD — the roadmap pages held to
// what they are FOR: the eight statuses are eight (R1), `unknown` priority
// renders distinctly from known/assumed on the MARKUP (D2 — the gate the
// task names), an estimate is `min–max (basis)` or an explicit "not
// estimated" (D3 — never a bare figure, never a blank), provenance says
// where the item came from (D5), the CRUD is the machine's own verbs
// (cancel-never-deletes, D7), the half-estimate is refused at the form as
// at the door, and every call rides the generic bridge — no new Tauri
// command.
//
// The rows mirror `conformance/roadmap/*.json` (the pointer-style estimate
// trio, priorityLabel null unless stated) — the wire, not this file's
// invention.

const ROOT = "/srv/projects/ledger";

function mkItem(id: number, status: string, over: Partial<RoadmapItemView> = {}): RoadmapItemView {
  return {
    id,
    componentRoot: ROOT,
    title: `item number ${id}`,
    status: status as RoadmapItemView["status"],
    priorityState: "unknown",
    priorityLabel: null,
    estimateMin: null,
    estimateMax: null,
    estimateBasis: null,
    provenance: "authored",
    createdAt: "2026-10-29T09:00:00Z",
    updatedAt: "2026-10-29T09:00:00Z",
    ...over,
  };
}

function board(items: RoadmapItemView[]): RoadmapListResult {
  return { componentRoot: ROOT, items };
}

const OK = { item: mkItem(1, "idea") };

let fake: FakeDaemon;

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetBridge();
});

function daemonWith(responses: Record<string, unknown>): FakeDaemon {
  fake = installFakeDaemon({ responses });
  return fake;
}

async function openBoard(withItems: RoadmapItemView[] | null, page: () => ReactElement = () => <RoadmapPage />) {
  render(page());
  fireEvent.change(screen.getByLabelText("component root"), { target: { value: ROOT } });
  fireEvent.click(screen.getByText(/Load the board|Look at the future/));
  if (withItems !== null) {
    await waitFor(() => expect(fake.methods()).toContain("roadmap.list"));
  }
}

describe("the bench — the eight statuses are eight (R1)", () => {
  it("renders each status distinctly, asserted not merely visually different", async () => {
    daemonWith({ "roadmap.list": board(ROADMAP_STATUSES.map((s, i) => mkItem(21 + i, s))) });
    await openBoard(ROADMAP_STATUSES.map((s, i) => mkItem(21 + i, s)));
    const labels = ROADMAP_STATUSES.map((s) => STATUS_LABELS[s]);
    expect(new Set(labels).size).toBe(8); // none collapses into another
    for (const s of ROADMAP_STATUSES) {
      const row = document.querySelector(`tr[data-status="${s}"]`);
      expect(row, `row for ${s}`).not.toBeNull();
      expect(row!.textContent).toContain(STATUS_LABELS[s]);
    }
    // D7's words: cancelled says its own outcome, never a deletion.
    expect(document.querySelector(`tr[data-status="cancelled"]`)!.textContent).toContain("never a deletion");
  });

  it("cancel is offered on exactly the five pre-loop rungs, never on running/done/cancelled (D7)", async () => {
    daemonWith({ "roadmap.list": board(ROADMAP_STATUSES.map((s, i) => mkItem(21 + i, s))) });
    await openBoard(ROADMAP_STATUSES.map((s, i) => mkItem(21 + i, s)));
    for (const s of ["idea", "ideating", "brief-agreed", "planned", "ready"]) {
      const i = ROADMAP_STATUSES.indexOf(s as (typeof ROADMAP_STATUSES)[number]);
      expect(document.querySelector(`[data-testid="cancel-${21 + i}"]`), `cancel for ${s}`).not.toBeNull();
    }
    for (const s of ["running", "done", "cancelled"]) {
      const i = ROADMAP_STATUSES.indexOf(s as (typeof ROADMAP_STATUSES)[number]);
      expect(document.querySelector(`[data-testid="cancel-${21 + i}"]`), `no cancel for ${s}`).toBeNull();
    }
  });

  it("cancel calls the door and says the row is never deleted", async () => {
    daemonWith({ "roadmap.list": board([mkItem(1, "ready")]), "roadmap.cancel": OK });
    await openBoard([mkItem(1, "ready")]);
    fireEvent.click(screen.getByTestId("cancel-1"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.cancel"));
    expect(fake.paramsOf("roadmap.cancel")).toEqual({ id: 1 });
    await waitFor(() => expect(document.body.textContent).toContain("never deleted (D7)"));
  });

  it("the empty board SPEAKS (D8) — and names what does not happen", async () => {
    daemonWith({ "roadmap.list": board([]) });
    await openBoard([]);
    expect(screen.getByTestId("roadmap-board").textContent).toContain("EMPTY");
    expect(screen.getByTestId("roadmap-board").textContent).toContain("loop.start never asked");
  });
});

describe("the bench — priority is three states, `unknown` renders the WORD (D2's gate)", () => {
  it("unknown renders distinctly from known/assumed, asserted on markup", async () => {
    daemonWith({
      "roadmap.list": board([
        mkItem(1, "idea", { priorityState: "known", priorityLabel: "p1" }),
        mkItem(2, "idea", { priorityState: "assumed", priorityLabel: "maybe-p2" }),
        mkItem(3, "idea", { priorityState: "unknown" }),
      ]),
    });
    await openBoard([]);
    const known = await screen.findByTestId("priority-1");
    const assumed = screen.getByTestId("priority-2");
    const unknown = screen.getByTestId("priority-3");
    expect(known.textContent).toContain("priority known: p1");
    expect(assumed.textContent).toContain("priority assumed: maybe-p2");
    // The honest absence: the WORD, its capitalised shout, and it is not
    // a blank, not a dash, not a sort-last implied-low.
    expect(unknown.textContent).toContain("unknown");
    expect(unknown.textContent).toContain("NOT STATED (D2)");
    expect((unknown.textContent ?? "").trim()).not.toBe("");
    expect(unknown.getAttribute("data-state")).toBe("unknown");
    expect(known.getAttribute("data-state")).toBe("known");
    expect(unknown.textContent).not.toBe(known.textContent);
    expect(unknown.textContent).not.toMatch(/unknown priority:\s*$/);
  });

  it("the priority write is ONE decision — state with its label, never a half", async () => {
    daemonWith({ "roadmap.list": board([mkItem(1, "idea")]), "roadmap.priority": OK });
    await openBoard([mkItem(1, "idea")]);
    fireEvent.click(screen.getByTestId("select-1"));
    // The button is shut until the state is chosen (one decision whole).
    expect((screen.getByTestId("write-priority") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("priority state"), { target: { value: "known" } });
    fireEvent.change(screen.getByLabelText("priority label"), { target: { value: "p1" } });
    fireEvent.click(screen.getByTestId("write-priority"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.priority"));
    expect(fake.paramsOf("roadmap.priority")).toEqual({ id: 1, priorityState: "known", priorityLabel: "p1" });
  });
});

describe("the bench — an estimate is a range plus basis or nothing (D3's gate)", () => {
  it("renders `min–max (basis)`; never a bare number", async () => {
    daemonWith({ "roadmap.list": board([mkItem(3, "ready", { estimateMin: 2, estimateMax: 4, estimateBasis: "one handler, one door" })]) });
    await openBoard([]);
    const cell = await screen.findByTestId("estimate-3");
    expect(cell.textContent).toBe("2–4 (one handler, one door)");
    expect(cell.textContent ?? "").not.toMatch(/^\s*\d+\s*$/); // never a bare number
  });

  it("an unestimated row says NOT ESTIMATED — never a blank cell", async () => {
    daemonWith({ "roadmap.list": board([mkItem(4, "idea")]) });
    await openBoard([]);
    const cell = await screen.findByTestId("estimate-4");
    expect(cell.textContent).toContain("not estimated");
    expect(cell.textContent).toContain("all-present or all-absent (D3)");
    expect((cell.textContent ?? "").trim()).not.toBe("");
  });

  it("half a trio is refused at the FORM, the same law the door keeps", async () => {
    daemonWith({ "roadmap.list": board([]), "roadmap.add": OK });
    await openBoard([]);
    fireEvent.change(screen.getByPlaceholderText("Surface the retry counter"), { target: { value: "a plan" } });
    fireEvent.change(screen.getByLabelText("provenance"), { target: { value: "authored" } });
    fireEvent.change(screen.getByLabelText("add estimate min"), { target: { value: "2" } });
    fireEvent.click(screen.getByTestId("add-button"));
    await waitFor(() => expect(document.body.textContent).toContain("all-present or all-absent (D3)"));
    expect(fake.methods()).not.toContain("roadmap.add"); // nothing half-written crossed the wire
  });

  it("the estimate write carries the trio whole and only when whole", async () => {
    daemonWith({ "roadmap.list": board([mkItem(1, "idea")]), "roadmap.estimate": OK });
    await openBoard([mkItem(1, "idea")]);
    fireEvent.click(screen.getByTestId("select-1"));
    expect((screen.getByTestId("write-estimate") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("estimate min"), { target: { value: "2" } });
    fireEvent.change(screen.getByLabelText("estimate max"), { target: { value: "4" } });
    fireEvent.change(screen.getByLabelText("estimate basis"), { target: { value: "measured" } });
    fireEvent.click(screen.getByTestId("write-estimate"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.estimate"));
    expect(fake.paramsOf("roadmap.estimate")).toEqual({ id: 1, estimateMin: 2, estimateMax: 4, estimateBasis: "measured" });
  });
});

describe("the bench — create answers D5, and the rung buttons follow the machine", () => {
  it("provenance has NO default: Add is shut until a person answers where the item came from", async () => {
    daemonWith({ "roadmap.list": board([]), "roadmap.add": OK });
    await openBoard([]);
    fireEvent.change(screen.getByPlaceholderText("Surface the retry counter"), { target: { value: "a thing" } });
    expect((screen.getByTestId("add-button") as HTMLButtonElement).disabled).toBe(true); // D5: no default
    fireEvent.change(screen.getByLabelText("provenance"), { target: { value: "triaged" } });
    expect((screen.getByTestId("add-button") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("add-button"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.add"));
    const params = fake.paramsOf("roadmap.add") as Record<string, unknown>;
    expect(params.provenance).toBe("triaged"); // always sent — the writer answers
    expect(params.title).toBe("a thing");
  });

  it("every row says where it came from (D5 rendered)", async () => {
    daemonWith({
      "roadmap.list": board([
        mkItem(1, "idea", { provenance: "authored" }),
        mkItem(2, "idea", { provenance: "triggered" }),
        mkItem(3, "idea", { provenance: "triaged" }),
      ]),
    });
    await openBoard([]);
    const prov1 = await screen.findByTestId("provenance-1");
    expect(prov1.textContent).toBe("from authored");
    expect(screen.getByTestId("provenance-2").textContent).toBe("from triggered");
    expect(screen.getByTestId("provenance-3").textContent).toBe("from triaged");
  });

  it("ideate reaches an idea row, materialize reaches a ready row, both through their door", async () => {
    daemonWith({
      "roadmap.list": board([
        mkItem(1, "idea"),
        mkItem(2, "ready"),
        mkItem(3, "done", { loopSlug: "roadmap-item-3" }),
      ]),
      "roadmap.ideate": { started: true, loopSlug: "ideate-item-1", status: "ideating" },
      "roadmap.materialize": { started: true, loopSlug: "roadmap-item-2", stamped: true, status: "running" },
    });
    await openBoard([]);
    await screen.findByTestId("ideate-1");
    expect(screen.queryByTestId("ideate-2")).toBeNull();
    screen.getByTestId("materialize-2");
    expect(screen.queryByTestId("materialize-3")).toBeNull();
    expect(document.querySelector(`tr[data-status="done"]`)!.textContent).toContain("loop=roadmap-item-3");
    fireEvent.change(screen.getByLabelText("workspace root"), { target: { value: "/srv/repos/ledger" } });
    fireEvent.click(screen.getByTestId("ideate-1"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.ideate"));
    expect(fake.paramsOf("roadmap.ideate")).toEqual({ id: 1, workspaceRoot: "/srv/repos/ledger" });
    fireEvent.change(screen.getByLabelText("materialize blueprint"), { target: { value: "two-step" } });
    fireEvent.change(screen.getByLabelText("materialize goal"), { target: { value: "ship it" } });
    fireEvent.click(screen.getByTestId("materialize-2"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.materialize"));
    const params = fake.paramsOf("roadmap.materialize") as Record<string, unknown>;
    expect(params).toEqual({ id: 2, blueprint: "two-step", goal: "ship it", workspaceRoot: "/srv/repos/ledger" });
    expect(params).not.toHaveProperty("attended"); // declared or absent (D5/G14) — never defaulted
  });

  it("attendedness is DECLARED — the third option sends nothing at all", async () => {
    daemonWith({
      "roadmap.list": board([mkItem(2, "ready")]),
      "roadmap.materialize": { started: true, loopSlug: "roadmap-item-2", stamped: true, status: "running" },
    });
    await openBoard([]);
    fireEvent.change(screen.getByLabelText("workspace root"), { target: { value: "/srv/repos/ledger" } });
    fireEvent.change(screen.getByLabelText("materialize blueprint"), { target: { value: "two-step" } });
    fireEvent.change(screen.getByLabelText("materialize goal"), { target: { value: "ship it" } });
    fireEvent.change(screen.getByLabelText("materialize attended"), { target: { value: "attended" } });
    fireEvent.click(screen.getByTestId("materialize-2"));
    await waitFor(() => expect(fake.methods()).toContain("roadmap.materialize"));
    expect((fake.paramsOf("roadmap.materialize") as Record<string, unknown>).attended).toBe(true);
  });
});

describe("the bench and the app share the ONE set of honest words", () => {
  it("both pages render the same D2 sentence for unknown", async () => {
    const rows = board([mkItem(1, "idea", { priorityState: "unknown" })]);
    daemonWith({ "roadmap.list": rows });
    render(<RoadmapPage />);
    fireEvent.change(screen.getByLabelText("component root"), { target: { value: ROOT } });
    fireEvent.click(screen.getByText("Load the board"));
    await screen.findByTestId("priority-1");
    const benchWords = screen.getByTestId("priority-1").textContent;
    cleanup();
    daemonWith({ "roadmap.list": rows });
    render(<GanttPage />);
    fireEvent.change(screen.getByLabelText("component root"), { target: { value: ROOT } });
    fireEvent.click(screen.getByText("Look at the future"));
    await screen.findByTestId("priority-1");
    expect(screen.getByTestId("priority-1").textContent).toBe(benchWords);
  });
});

describe("the app's forward Gantt (T6)", () => {
  it("lanes are by epic and by priority — never by agent (the locked decision)", async () => {
    daemonWith({
      "roadmap.list": board([
        mkItem(1, "idea", { epic: "reliability" }),
        mkItem(2, "idea", { priorityState: "known", priorityLabel: "p1" }),
        mkItem(3, "idea", { priorityState: "assumed", priorityLabel: "maybe" }),
      ]),
    });
    await openBoard([], () => <GanttPage />);
    // By epic: the un-epicked item says the tag's absence aloud (R2).
    const epicLanes = [...document.querySelectorAll('[data-testid="lane-epic"]')];
    expect(epicLanes.map((l) => l.getAttribute("data-lane"))).toEqual(
      expect.arrayContaining(["epic: reliability", "no epic (a tag, not a row — R2)"]),
    );
    // Switch to priority lanes: known first, assumed next, unknown LAST.
    fireEvent.change(screen.getByLabelText("lane axis"), { target: { value: "priority" } });
    await waitFor(() => expect(document.querySelectorAll('[data-testid="lane-priority"]').length).toBeGreaterThan(0));
    const bands = [...document.querySelectorAll('[data-testid="lane-priority"]')].map((l) => l.getAttribute("data-lane"));
    expect(bands).toEqual(["priority known", "priority assumed", "priority unknown"]);
  });

  it("unknown renders distinctly on the Gantt too, and an estimate is range-or-absence", async () => {
    daemonWith({
      "roadmap.list": board([
        mkItem(1, "idea", { priorityState: "unknown" }),
        mkItem(2, "idea", { priorityState: "known", priorityLabel: "p1", estimateMin: 1, estimateMax: 3, estimateBasis: "three doors" }),
      ]),
    });
    await openBoard([], () => <GanttPage />);
    const unknown = await screen.findByTestId("priority-1");
    expect(unknown.textContent).toContain("unknown");
    expect(screen.getByTestId("priority-2").textContent).toContain("priority known: p1");
    expect(screen.getByTestId("estimate-2").textContent).toBe("1–3 (three doors)");
    expect(screen.getByTestId("estimate-1").textContent).toContain("not estimated");
  });

  it("the empty future speaks D8's sentence", async () => {
    daemonWith({ "roadmap.list": board([]) });
    await openBoard([], () => <GanttPage />);
    expect(screen.getByTestId("roadmap-gantt").textContent).toContain("EMPTY");
    expect(screen.getByTestId("roadmap-gantt").textContent).toContain("loop.start never asked");
  });
});

describe("the standing rules", () => {
  it("no new Tauri command — both pages ride the generic rpc() bridge only", () => {
    for (const rel of [
      "src/features/test-bench/pages/RoadmapPage.tsx",
      "src/features/roadmap/RoadmapPage.tsx",
    ]) {
      const src = readFileSync(join(__dirname, "..", rel), "utf8");
      expect(src, `${rel} imports the generic bridge`).toContain("lib/bridge\"");
      expect(src, `${rel} names no Tauri invoke`).not.toMatch(/invoke\s*\(/);
      expect(src, `${rel} stores no secret`).not.toMatch(/apiKey|localStorage/);
    }
  });

  it("the bench tab exists, right after the queue it complements", () => {
    expect(isBenchPage("roadmap")).toBe(true);
    const ids = BENCH_PAGES.map((p) => p.id);
    expect(ids.indexOf("roadmap")).toBe(ids.indexOf("queue") + 1);
  });

  it("the board is per component root — the door is asked with nothing else", async () => {
    daemonWith({ "roadmap.list": board([]) });
    await openBoard([]);
    expect(fake.paramsOf("roadmap.list")).toEqual({ componentRoot: ROOT });
  });
});
