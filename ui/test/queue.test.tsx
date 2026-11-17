import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { QueuePage, STATUS_LABELS } from "../src/features/test-bench/pages/QueuePage";
import { BENCH_PAGES, isBenchPage } from "../src/app/routes";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { QUEUE_STATUSES, type QueueItemView, type QueueListResult } from "../src/lib/protocol";

// S24 T8 — the queue bench, held to what the page is FOR: seven distinct
// statuses (the gate the task names), size as range-plus-basis or an honest
// "not sized yet" (never a bare number, never blank — D8), an append form
// that collects exactly the loop.start payload (CG3) with attendedness
// DECLARED and never defaulted to the watcher (D5/G14), remove-that-cancels
// staying on the board (D12), the settings write carrying both knobs
// (read-merge-write), and the advance door being ONE call (CG4).
//
// The rows below mirror the shape `conformance/queue/*.json` pins (the
// flattened size triple, `attended` as true/false/null) — the wire, not a
// private invention of this file.

const ROOT = "/srv/projects/ledger";

function mkItem(id: number, status: string, over: Partial<QueueItemView> = {}): QueueItemView {
  return {
    id,
    position: id,
    status: status as QueueItemView["status"],
    blueprint: "two-step",
    goal: `intent number ${id}`,
    workspaceRoot: "/srv/repos/ledger",
    attended: null,
    sizeMin: null,
    sizeMax: null,
    sizeBasis: null,
    createdAt: "2026-10-29T09:00:00Z",
    updatedAt: "2026-10-29T09:00:00Z",
    ...over,
  };
}

function board(items: QueueItemView[]): QueueListResult {
  return { componentRoot: ROOT, items };
}

const SETTINGS = { componentRoot: ROOT, known: true, queueAdvance: "ask", sizingMode: "manual" };

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

async function openBoard(withItems: QueueItemView[] | null) {
  render(<QueuePage />);
  fireEvent.change(screen.getByLabelText("component root"), { target: { value: ROOT } });
  fireEvent.click(screen.getByText("Load the board"));
  if (withItems !== null) {
    await waitFor(() => expect(fake.methods()).toContain("queue.list"));
  }
}

describe("QueuePage — the seven statuses are seven", () => {
  it("renders each CG1 status distinctly, asserted not merely visually different", async () => {
    daemonWith({
      "queue.list": board(QUEUE_STATUSES.map((s, i) => mkItem(11 + i, s))),
      "queue.settings": SETTINGS,
    });
    await openBoard(QUEUE_STATUSES.map((s, i) => mkItem(11 + i, s)));
    const labels = QUEUE_STATUSES.map((s) => STATUS_LABELS[s]);
    expect(new Set(labels).size).toBe(7); // none collapses into another
    for (const s of QUEUE_STATUSES) {
      const row = document.querySelector(`tr[data-status="${s}"]`);
      expect(row, `row for ${s}`).not.toBeNull();
      expect(row!.textContent).toContain(STATUS_LABELS[s]);
    }
    // blocked says the HALT aloud (CG2), not just a color.
    expect(document.querySelector(`tr[data-status="blocked"]`)!.textContent).toContain("nothing starts past this row");
    // cancel offered on exactly the four cancellable, refused on the rest (D11/D12 terminals).
    for (const s of ["pending", "sizing", "ready", "blocked"]) {
      const i = QUEUE_STATUSES.indexOf(s as (typeof QUEUE_STATUSES)[number]);
      expect(document.querySelector(`[data-testid="remove-${11 + i}"]`), `cancel for ${s}`).not.toBeNull();
    }
    for (const s of ["running", "done", "cancelled"]) {
      const i = QUEUE_STATUSES.indexOf(s as (typeof QUEUE_STATUSES)[number]);
      expect(document.querySelector(`[data-testid="remove-${11 + i}"]`), `no cancel for ${s}`).toBeNull();
    }
  });

  it("the empty board SPEAKS (D4) — and says what does not happen", async () => {
    daemonWith({ "queue.list": board([]), "queue.settings": SETTINGS });
    await openBoard([]);
    expect(screen.getByTestId("queue-board").textContent).toContain("EMPTY");
    expect(screen.getByTestId("queue-board").textContent).toContain("appending starts nothing");
  });
});

describe("QueuePage — size is a fact or it is nothing (D8)", () => {
  it("renders range plus basis together, never a bare number", async () => {
    daemonWith({
      "queue.list": board([mkItem(3, "ready", { sizeMin: 1, sizeMax: 3, sizeBasis: "one handler, one door" })]),
      "queue.settings": SETTINGS,
    });
    await openBoard([]);
    const cell = await screen.findByTestId("size-3");
    expect(cell.textContent).toBe("1–3 (one handler, one door)");
    expect(cell.textContent ?? "").not.toMatch(/^\s*\d+\s*$/); // never a bare number
  });

  it("an unsized row says NOT SIZED YET, and what the mode says about it — three renderings, none blank", async () => {
    daemonWith({
      "queue.list": board([mkItem(4, "pending")]),
      "queue.settings": { ...SETTINGS, sizingMode: "manual" },
    });
    await openBoard([]);
    const cell = await screen.findByTestId("size-4");
    expect(cell.textContent).toContain("not sized yet");
    expect(cell.textContent).toContain("set-size is the door"); // manual: this page is the door
    expect((cell.textContent ?? "").trim()).not.toBe("");
  });

  it("under skill mode the absence names report_size, not silence", async () => {
    daemonWith({
      "queue.list": board([mkItem(5, "sizing")]),
      "queue.settings": { ...SETTINGS, sizingMode: "skill" },
    });
    await openBoard([]);
    const cell = await screen.findByTestId("size-5");
    expect(cell.textContent).toContain("not sized yet");
    expect(cell.textContent).toContain("report_size");
  });
});

describe("QueuePage — the append form is the loop.start payload (CG3)", () => {
  it("collects blueprint, goal, tech, with, and a DECLARED attendedness", async () => {
    daemonWith({
      "queue.list": board([]),
      "queue.settings": SETTINGS,
      "queue.append": { item: mkItem(9, "pending") },
    });
    await openBoard([]);
    fireEvent.change(screen.getByPlaceholderText("two-step"), { target: { value: "dev-loop" } });
    fireEvent.change(screen.getByPlaceholderText("Surface the retry counter"), { target: { value: "Surface the retry counter" } });
    fireEvent.change(screen.getByPlaceholderText("go"), { target: { value: "go" } });
    fireEvent.change(screen.getByPlaceholderText("ticket=LEDGER-9; owner=ana"), { target: { value: "ticket=LEDGER-9; owner=ana" } });
    fireEvent.change(screen.getByRole("combobox", { name: /attendedness/i }), { target: { value: "unattended" } });
    fireEvent.click(screen.getByText("Append to the queue"));
    await waitFor(() => expect(fake.methods()).toContain("queue.append"));
    expect(fake.paramsOf("queue.append")).toEqual({
      componentRoot: ROOT,
      blueprint: "dev-loop",
      goal: "Surface the retry counter",
      tech: "go",
      with: { ticket: "LEDGER-9", owner: "ana" },
      attended: false,
    });
  });

  it("leaving attendedness untouched sends NOTHING — the project's declared default decides (D5/G14)", async () => {
    daemonWith({
      "queue.list": board([]),
      "queue.settings": SETTINGS,
      "queue.append": { item: mkItem(9, "pending") },
    });
    await openBoard([]);
    fireEvent.change(screen.getByPlaceholderText("two-step"), { target: { value: "two-step" } });
    fireEvent.change(screen.getByPlaceholderText("Surface the retry counter"), { target: { value: "g" } });
    fireEvent.click(screen.getByText("Append to the queue"));
    await waitFor(() => expect(fake.methods()).toContain("queue.append"));
    const params = fake.paramsOf("queue.append") as Record<string, unknown>;
    expect("attended" in params).toBe(false); // not true, not false — absent, by declaration
    // and the default option is the DEFAULT: never "whoever is watching".
    expect((screen.getByRole("combobox", { name: /attendedness/i }) as HTMLSelectElement).value).toBe("default");
  });

  it("an incomplete payload cannot be appended — blueprint AND goal are required", () => {
    daemonWith({ "queue.list": board([]), "queue.settings": SETTINGS });
    render(<QueuePage />);
    expect(screen.getByText("Append to the queue").hasAttribute("disabled")).toBe(true);
  });
});

describe("QueuePage — remove cancels; advance is one call; settings write the pair", () => {
  it("remove sends queue.remove and the board keeps showing cancelled rows (D12)", async () => {
    let removed = false; // the daemon's own memory: remove CANCELS, the row stays
    daemonWith({
      "queue.list": () => board([removed ? mkItem(7, "cancelled") : mkItem(7, "pending")]),
      "queue.settings": SETTINGS,
      "queue.remove": () => {
        removed = true;
        return { item: mkItem(7, "cancelled") };
      },
    });
    await openBoard([mkItem(7, "pending")]);
    fireEvent.click(await screen.findByTestId("remove-7"));
    await waitFor(() => expect(fake.methods()).toContain("queue.remove"));
    expect(fake.paramsOf("queue.remove")).toEqual({ id: 7 });
    expect(fake.methods().filter((m) => m === "queue.list").length).toBe(2); // the board reloaded
    expect(document.querySelector('tr[data-status="cancelled"]')).not.toBeNull(); // the row stays
    expect(screen.getByText(/row stays on the board/)).toBeTruthy();
  });

  it("advance is ONE call — the call itself is the decision (CG4)", async () => {
    daemonWith({
      "queue.list": board([]),
      "queue.settings": SETTINGS,
      "queue.advance": { started: true },
    });
    await openBoard([]);
    fireEvent.click(await screen.findByTestId("advance-button"));
    await waitFor(() => expect(fake.methods().filter((m) => m === "queue.advance").length).toBe(1));
    expect(fake.paramsOf("queue.advance")).toEqual({ componentRoot: ROOT });
  });

  it("settings writes BOTH knobs, the untouched one merged from the daemon's own read", async () => {
    daemonWith({
      "queue.list": board([]),
      "queue.settings": { componentRoot: ROOT, known: true, queueAdvance: "auto", sizingMode: "manual" },
    });
    await openBoard([]);
    await screen.findByTestId("save-settings");
    fireEvent.change(screen.getByRole("combobox", { name: /sizingMode/i }), { target: { value: "skill" } });
    fireEvent.click(screen.getByTestId("save-settings"));
    await waitFor(() => expect(fake.methods().filter((m) => m === "queue.settings").length).toBe(3));
    // calls: load-read, merge-read, write. The write carries the read's
    // queueAdvance UNTOUCHED (the socket refuses half a pair).
    const writes = fake.calls.filter((c) => c.method === "queue.settings");
    const write = writes[2] as { params: unknown } | undefined;
    expect(write, "the write call").toBeDefined();
    expect(write?.params).toEqual({ componentRoot: ROOT, queueAdvance: "auto", sizingMode: "skill" });
  });
});

describe("QueuePage — the standing habits and the bench wiring", () => {
  it("no localStorage, no credential-shaped anything in the page", () => {
    const src = readFileSync(join(__dirname, "../src/features/test-bench/pages/QueuePage.tsx"), "utf8");
    for (const banned of ["localStorage", "sessionStorage", "apiKey", "credential", "token"]) {
      expect(src, `QueuePage must not mention ${banned}`).not.toContain(banned);
    }
  });

  it("the bench registers the tab: routes carry queue, the trust-bench order is undisturbed", () => {
    expect(isBenchPage("queue")).toBe(true);
    const ids = BENCH_PAGES.map((p) => p.id);
    expect(ids).toContain("queue");
    // queue sits among the loop-mechanics tabs, before the trust bench —
    // asserted against the two anchors that exist in every tree, not the
    // trust-bench trio whose own order test (S34) already pins it.
    expect(ids.indexOf("queue")).toBeLessThan(ids.indexOf("policy"));
    expect(ids[ids.indexOf("queue") - 1]).toBe("blueprints");
  });

  it("the page says what it does not do (§4b): the ask gate question lives in the thread surface", () => {
    daemonWith({ "queue.list": board([]), "queue.settings": SETTINGS });
    render(<QueuePage />);
    expect(screen.getByTestId("queue-append").textContent).toContain("question/thread surface");
    expect(screen.getByTestId("queue-append").textContent).toContain("not inline on this page");
  });
});
