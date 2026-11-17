import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DemosPage } from "../src/features/test-bench/pages/DemosPage";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// S17 T10 — the "Full ladder (S17)" panel contract (QAC-93 in
// docs/test_suites/aeon-loop.md, numbered after T9's QAC-80..92).
//
// The method itself is gates and send-backs — the things a click-through
// would corrupt. Three ways this panel can appear to pass while lying,
// each pre-empted:
//
//  1. AN APPROVE BUTTON IN DISGUISE, TWICE OVER. The plan gate is THE one
//     human decision the method is built around, and its answer set
//     [approve, revise, abort] carries abort as a first-class OUTCOME. A
//     button that answers it turns the method into a wizard that approves
//     itself. The panel READS gates and never answers them.
//  2. A FABRICATED MERGE. No fake daemon merged anything, and a fabricated
//     merge fabricates A FEATURE THAT EXISTS NOWHERE. Every success-shaped
//     row is labeled REPLAY citing the Go test; the model/GitHub halves
//     were OWED on the T9 walk (no credential, no token on this box) and
//     the owed rows say `owed`.
//  3. A COUNTERS FICTION. D2's claim — task 3 keeps its own budget — must
//     render FROM THE DAEMON'S loop.show ROWS, not the page's imagination,
//     and `unverified` renders as the third state it is, never a green
//     tick (I5 applies to pixels too).
//
// The sentences below are copied from the 2026-10-26 T9 walk
// (docs/aeon/S17-full-ladder/DEMO.md walk record — every one printed by a
// real daemon, the store, or a named Go test). The panel quotes them; this
// test quotes the quote.

const GATE_BODY = "Approve the plan and start building?";
const REDO_SENTENCE = 'redo of "verify": retry 1 of 2 — the json output omits the flag field that the spec requires';
const UNVERIFIED_SENTENCE = 'redo of "verify": sent back — unverified: the linter was killed before it ran — could not determine';

const VALIDATE_REPORT = {
  blueprint: "dev-loop", schemaVersion: 1, version: 1, validCount: 98, diagnostics: [],
};

// The store's OWN counter rows as `loop show --counters` printed them on
// the walked day (lad5: the defect charged t2's row alone... in the walk's
// numbering t1; the shape is the contract — per-task rows, one ledger each).
const LADDER_VIEW = {
  id: "9fc1d3fa77987f0769ffdcaadd9f3b8f", slug: "lad-demo", blueprint: "dev-loop",
  status: "waiting", questionId: 11,
  lanes: [
    { id: "plan-gate", structure: "single", status: "waiting" },
    {
      id: "build", structure: "ladder", status: "running", iterate: "tasks", itemCount: 3,
      items: [
        { id: "t1", status: "done", steps: [{ id: "implement", kind: "model-step", status: "done", outcome: "ok", stepId: 29 }] },
        {
          id: "t2", status: "in-review", steps: [
            { id: "implement", kind: "model-step", status: "done", outcome: "ok", stepId: 31 },
            { id: "verify", kind: "model-step", status: "done", outcome: "unverified", stepId: 30, reason: UNVERIFIED_SENTENCE },
          ],
        },
        { id: "t3", status: "pending", steps: [] },
      ],
    },
  ],
  loopCounters: { total_failed_gates: 1 },
  loopCounterCaps: { total_failed_gates: 10, splits_done: 0, qa_rounds: 2 },
  taskCounters: [
    { task: "t1", counter: "retries", level: "task", value: 0, cap: 2, capExceeded: false },
    { task: "t2", counter: "retries", level: "task", value: 1, cap: 2, capExceeded: false },
    { task: "t3", counter: "retries", level: "task", value: 0, cap: 2, capExceeded: false },
  ],
};

const REDO_STEP = {
  id: 31, loopSlug: "lad-demo", phase: "build", agent: "golang-engineer",
  status: "done", outcome: "ok",
  messages: [{ role: "user", content: [
    { kind: "text", text: `## Task\n\nGoal: walk §1–§7\nCurrent blueprint lane: build\nTask item: t2\n${REDO_SENTENCE}` },
  ] }],
};

function ladderDaemon(extra: Record<string, unknown | ((p: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({ responses: {
    "routes.list": [],
    "blueprint.validate": VALIDATE_REPORT,
    "loop.show": LADDER_VIEW,
    "step.show": REDO_STEP,
    ...extra,
  } });
}

function runLadderDemo() {
  fireEvent.change(screen.getByTestId("ladder-loop"), { target: { value: "9fc1d3fa77987f0769ffdcaadd9f3b8f" } });
  fireEvent.click(screen.getByRole("button", { name: /Run ladder demo/ }));
}

afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("S17 full-ladder panel", () => {
  it("§1 as read: blueprint.validate for dev-loop answers ZERO invalid, ZERO unverifiable — the row quotes the counts", async () => {
    const daemon = ladderDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run ladder demo/ }));
    const rows = await screen.findAllByTestId("ladder-step");
    const joined = () => rows.map((r) => r.textContent ?? "").join("\n");
    await waitFor(() => { expect(joined()).toContain("met"); });
    expect(joined()).toContain("98");
    expect(joined()).toContain("0 invalid");
    expect(joined()).toContain("0 unverifiable");
    expect(daemon.methods()).toContain("blueprint.validate");
  });

  it("the plan gate renders VERBATIM with [approve, revise, abort] as TEXT — and nothing on the page answers it", async () => {
    const daemon = ladderDaemon();
    render(<DemosPage />);
    runLadderDemo();
    await screen.findAllByTestId("ladder-step");
    await waitFor(() => { expect(daemon.subscribes().length).toBeGreaterThanOrEqual(1); });
    // The gate prompt arrives as the daemon publishes it — a store question
    // row of kind "gate", the walk's row verbatim.
    daemon.emit({ subscriptionId: "s1", seq: 1, kind: "question.asked", at: new Date().toISOString(),
      payload: { id: 11, stepId: 0, kind: "gate", body: GATE_BODY, options: ["approve", "revise", "abort"] } });
    const rows = await screen.findAllByTestId("ladder-step");
    await waitFor(() => {
      const joined = rows.map((r) => r.textContent ?? "").join("\n");
      expect(joined).toContain(GATE_BODY);
      expect(joined).toContain("approve");
      expect(joined).toContain("revise");
      expect(joined).toContain("abort");
    });
    // abort is an OUTCOME, not a fault — and none of the three is a control
    // this page may fire. The answer door belongs to a person.
    for (const verb of [/^approve$/i, /^revise$/i, /^abort$/i, /answer/i]) {
      expect(screen.queryAllByRole("button", { name: verb })).toEqual([]);
    }
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toContain("aeon answer 9fc1d3fa77987f0769ffdcaadd9f3b8f");
  });

  it("the counters table renders FROM THE loop.show RESPONSE — task 3 at 0/2 beside task 2 at 1/2 (D2 from the app)", async () => {
    ladderDaemon();
    render(<DemosPage />);
    runLadderDemo();
    const rows = await screen.findAllByTestId("ladder-step");
    await waitFor(() => {
      const joined = rows.map((r) => r.textContent ?? "").join("\n");
      expect(joined).toMatch(/t2 · retries 1\/2/);
      expect(joined).toMatch(/t1 · retries 0\/2/);
      expect(joined).toMatch(/t3 · retries 0\/2/);
      expect(joined).toMatch(/loop · total_failed_gates 1\/10/);
    });
  });

  it("the redo's reason is IN the input the daemon served (retry 1 of 2), and the starved check reads unverified — never pass", async () => {
    ladderDaemon({
      "step.show": (p: unknown) => {
        const id = (p as { id?: number }).id;
        if (id === 30) return { id: 30, loopSlug: "lad-demo", phase: "build", agent: "verifier-engineer", status: "done", outcome: "unverified" };
        return REDO_STEP;
      },
    });
    render(<DemosPage />);
    runLadderDemo();
    const rows = await screen.findAllByTestId("ladder-step");
    await waitFor(() => {
      const joined = rows.map((r) => r.textContent ?? "").join("\n");
      // The reason the model gave, read back from the transcript — and the
      // attempt claim the counter granted (the unverified variant carries
      // "sent back" with NO attempt claim — rows below cite it).
      expect(joined).toContain("redo of \"verify\": retry 1 of 2 — the json output omits the flag field");
      expect(joined).toMatch(/stored outcome: unverified/);
    });
    const unverifiedRow = rows.find((r) => (r.textContent ?? "").includes("stored outcome: unverified"));
    expect(unverifiedRow?.textContent ?? "").not.toMatch(/\bpass(ed)?\b(?! nor fail)/);
  });

  it("no loop started yet reads `not runnable here` naming DEMO §1 and the operator's loop start command", async () => {
    ladderDaemon({ "loop.show": () => { throw new Error(JSON.stringify({ code: -32000, message: "store: not found" })); } });
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run ladder demo/ }));
    const unavailables = await screen.findAllByTestId("ladder-unavailable");
    // Every read the cold store denies says the same honest thing.
    const text = unavailables.map((el) => el.textContent ?? "").join("\n");
    expect(text).toContain("not runnable here");
    expect(text).toContain("docs/aeon/S17-full-ladder/DEMO.md");
    expect(text).toContain("§1");
    expect(text).toContain("aeon loop start");
    expect(text).not.toContain("error (");
    // The same sentence must not print twice in one row (validation pass
    // 2026-10-26): the row-level pre and the unavailable block used to
    // render v.text each, so every cold row read its own sentence twice.
    for (const el of unavailables) {
      const row = el.closest('[data-testid="ladder-step"]') as HTMLElement;
      expect((row.textContent ?? "").match(/not runnable here/g)).toHaveLength(1);
    }
  });

  it("no mutation EVER leaves the page — live plus replay; the ban list is asserted, not inferred", async () => {
    const daemon = ladderDaemon();
    render(<DemosPage />);
    runLadderDemo();
    await screen.findAllByTestId("ladder-step");
    fireEvent.click(screen.getByRole("button", { name: /Run ladder replay/ }));
    await screen.findAllByTestId("ladder-replay-step");
    const sent = daemon.methods();
    for (const banned of ["question.answer", "loop.start", "loop.answer", "gate.answer", "step.continue",
      "loop.stop", "loop.retry", "question.reply", "blueprint.save", "step.answer"]) {
      expect(sent).not.toContain(banned);
    }
    for (const m of sent) {
      expect(["blueprint.validate", "loop.show", "step.show", "routes.list"]).toContain(m);
    }
  });

  it("every replay row is labeled with a grep-verified Go citation; the model/GitHub rows say `owed`, never their own verdict", async () => {
    ladderDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run ladder replay/ }));
    const rows = await screen.findAllByTestId("ladder-replay-step");
    expect(rows.length).toBeGreaterThanOrEqual(8);
    for (const row of rows) {
      const t = row.textContent ?? "";
      expect(t).toContain("replay");
      expect(t).toMatch(/_test\.go|Test[A-Z]/);
      expect(t).not.toContain("error (");
    }
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toMatch(/depends_on|cycle/);                          // plan load
    expect(joined).toMatch(/task 3|t3.*(0\/2|own)/i);                    // D2 — its own budget
    expect(joined).toMatch(/collapsed to block.*(cap|unattended)/i);     // §4 unattended names both
    expect(joined).toMatch(/unverified/i);                               // the third state
    expect(joined).toMatch(/bracket|per-item/i);                         // per-item ladder state
    expect(joined).toMatch(/review.?state|thread/i);                     // the merge gate
    expect(joined).toMatch(/append/i);                                   // the backlog
    expect(joined).toMatch(/abort(ed)?/i);                               // the terminal pair
    // The owed rows: they say owed, carry the replay label, and never claim
    // a verdict of their own.
    const owedRows = rows.filter((r) => (r.textContent ?? "").includes("owed"));
    expect(owedRows.length).toBeGreaterThanOrEqual(1);
    for (const row of owedRows) {
      expect(row.textContent ?? "").toContain("replay");
      expect(row.textContent ?? "").not.toMatch(/\bmet\b/);
    }
  });
});
