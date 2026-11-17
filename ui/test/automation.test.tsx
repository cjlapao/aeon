import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AutomationPage } from "../src/features/test-bench/pages/AutomationPage";
import { TestBench } from "../src/features/test-bench/TestBench";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { FIRE_OUTCOMES } from "../src/lib/protocol";

// S40 T9 — the automation bench, held to the four things it is for: the five
// outcomes as five pills with zero shown as zero (AU12/P31), reasons printed
// as the daemon spoke them (AU12), the two human writes and nothing else
// (AU1/AU15), and an honest note when the daemon predates the slice.
//
// The payloads below are **T7/T8's conformance fixtures**, copied verbatim —
// `conformance/automation/trigger-list.json` and `trigger-fires.json` — so a
// Go-side re-wording that breaks the page shows up here, not in a screenshot
// review. The create-refusal test quotes the sentence T8's bad-match fixture
// measured.

/** `conformance/automation/trigger-list.json` — the two triggers, unedited. */
const LIST = {
  triggers: [
    {
      id: "tr-intake",
      name: "Intake",
      source: "manual",
      projectId: "prj-platform",
      componentId: "cmp-alpha",
      match: { event: "goal.approved", goal_type: ["feature"] },
      blueprint: "dev-loop",
      kickoff: "Approved: {{.Goal}}",
      maxRunsPerHour: 20,
      maxCostPerDayUsd: 25.5,
      enabled: true,
      disabledReason: "",
      createdByKind: "user",
      createdById: "operator",
      createdAt: "2026-10-26T09:00:00Z",
      updatedAt: "2026-10-27T08:00:00Z",
      lastFire: {
        id: 1,
        triggerId: "tr-intake",
        outboxId: 4,
        subjectKind: "goal",
        subjectId: "g0",
        depth: 0,
        outcome: "started",
        reason: "",
        loopId: "l1",
        loopSlug: "auto-tr-intake-4",
        costNote: "",
        claimedAt: "2026-10-27T08:10:00Z",
        decidedAt: "2026-10-27T08:10:01Z",
      },
    },
    {
      id: "tr-nightly",
      name: "Nightly triage",
      source: "manual",
      projectId: "prj-platform",
      componentId: "",
      match: { event: "task.approved", task_type: ["triage"] },
      blueprint: "dev-loop",
      kickoff: "",
      maxRunsPerHour: 4,
      maxCostPerDayUsd: null,
      enabled: false,
      disabledReason: "operator paused it",
      createdByKind: "user",
      createdById: "operator",
      createdAt: "2026-10-25T09:00:00Z",
      updatedAt: "2026-10-26T18:00:00Z",
      lastFire: null,
    },
  ],
};

/** `conformance/automation/trigger-fires.json` — the rows and the five counts, unedited. */
const FIRES = {
  fires: [
    {
      id: 2,
      triggerId: "tr1",
      outboxId: 7,
      subjectKind: "goal",
      subjectId: "g1",
      depth: 0,
      outcome: "limited",
      reason: "max-runs-per-hour: 2 started since 2026-10-27T08:00:00Z, limit 2",
      loopId: "",
      loopSlug: "",
      costNote: "cost-unknown",
      claimedAt: "2026-10-27T08:30:00Z",
      decidedAt: "2026-10-27T08:30:00Z",
    },
    {
      id: 1,
      triggerId: "tr1",
      outboxId: 4,
      subjectKind: "goal",
      subjectId: "g0",
      depth: 0,
      outcome: "started",
      reason: "",
      loopId: "l1",
      loopSlug: "auto-tr1-4",
      costNote: "",
      claimedAt: "2026-10-27T08:10:00Z",
      decidedAt: "2026-10-27T08:10:01Z",
    },
  ],
  counts: { started: 1, refused: 0, limited: 1, skipped: 0, "could-not-say": 0 },
};

/** The six methods this page may ever call — the closed set of AU14's bench half. */
const THE_SIX = [
  "automation.trigger.list",
  "automation.trigger.fires",
  "automation.trigger.show",
  "automation.trigger.create",
  "automation.trigger.enable",
  "automation.trigger.disable",
];

function install(responses: Record<string, unknown | ((params: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({
    responses: {
      "automation.trigger.list": LIST,
      "automation.trigger.fires": FIRES,
      ...responses,
    },
  });
}

/** A daemon refusal in the shape the shell actually serialises. */
function refusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("the automation bench (S40 T9)", () => {
  it("shows all five outcomes as distinct pills, zero included", async () => {
    install();
    render(<AutomationPage />);
    for (const o of FIRE_OUTCOMES) {
      const pill = await screen.findByTestId(`pill-${o}`);
      expect(pill.textContent).toContain(o);
      expect(pill.textContent).toContain(String(FIRES.counts[o])); // zero SHOWN, not hidden
    }
    // could-not-say is its own answer, never skipped's (P31): different tone.
    const cns = screen.getByTestId("pill-could-not-say");
    const skipped = screen.getByTestId("pill-skipped");
    expect(cns.className).not.toBe(skipped.className);
    expect(cns.textContent).toContain("could-not-say");
    expect(screen.getByTestId("could-not-say-gloss").textContent).toContain("we cannot prove whether this started");
  });

  it("renders each reason verbatim and the cost note", async () => {
    install();
    render(<AutomationPage />);
    // Byte-identical to trigger-fires.json — no trimming, no re-casing.
    expect(
      await screen.findByText("max-runs-per-hour: 2 started since 2026-10-27T08:00:00Z, limit 2"),
    ).toBeTruthy();
    expect(screen.getByTestId("cost-note-2").textContent).toContain("cost-unknown");
  });

  it("a claimed fire is labelled claimed, not given an outcome", async () => {
    install({
      "automation.trigger.fires": {
        fires: [{ ...FIRES.fires[1], outcome: "" }],
        counts: FIRES.counts,
      },
    });
    render(<AutomationPage />);
    const pill = await screen.findByTestId("fire-outcome-1");
    expect(pill.textContent).toBe("claimed"); // the transient, never borrowed from the five
    for (const o of FIRE_OUTCOMES) {
      expect(pill.textContent).not.toContain(o);
    }
  });

  it("lists triggers with event, blueprint and last outcome; a disabled one shows its reason", async () => {
    install();
    render(<AutomationPage />);
    expect(await screen.findByTestId("trigger-row-tr-intake")).toBeTruthy();
    expect(screen.getByTestId("trigger-row-tr-intake").textContent).toContain("goal.approved");
    expect(screen.getByTestId("trigger-row-tr-intake").textContent).toContain("dev-loop");
    expect(screen.getByTestId("last-fire-tr-intake").textContent).toContain("started");
    expect(screen.getByTestId("last-fire-tr-nightly").textContent).toContain("never fired");
    // The pause speaks its own reason (AU15: a switch nobody can explain is not a decision).
    expect(screen.getByTestId("disabled-reason-tr-nightly").textContent).toBe("operator paused it");
  });

  it("disable needs a reason and sends exactly it", async () => {
    const d = install();
    render(<AutomationPage />);
    fireEvent.click(await screen.findByTestId("trigger-row-tr-intake"));
    const button = (await screen.findByTestId("disable")) as HTMLButtonElement;
    expect(button.disabled).toBe(true); // no reason typed
    fireEvent.change(screen.getByTestId("disable-reason"), { target: { value: "sunset" } });
    expect((screen.getByTestId("disable") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("disable"));
    await waitFor(() => expect(d.paramsOf("automation.trigger.disable")).toEqual({ id: "tr-intake", reason: "sunset" }));
    // A write reloads the desk: list was called again.
    await waitFor(() => expect(d.methods().filter((m) => m === "automation.trigger.list").length).toBeGreaterThanOrEqual(2));
  });

  it("enable sends only the id", async () => {
    const d = install();
    render(<AutomationPage />);
    fireEvent.click(await screen.findByTestId("trigger-row-tr-nightly"));
    fireEvent.click(await screen.findByTestId("enable"));
    await waitFor(() => expect(d.paramsOf("automation.trigger.enable")).toEqual({ id: "tr-nightly" }));
  });

  it("the create box sends nothing on invalid JSON", async () => {
    const d = install();
    render(<AutomationPage />);
    fireEvent.change(await screen.findByTestId("create-json"), { target: { value: "{oops" } });
    fireEvent.click(screen.getByTestId("create"));
    expect((await screen.findByTestId("create-refusal")).textContent).toContain("not valid JSON");
    expect(d.methods()).not.toContain("automation.trigger.create");
  });

  it("the create box shows the daemon's refusal verbatim", async () => {
    // The sentence T8's bad-match fixture measured, quoted whole.
    const sentence =
      'invalid params: match: event "" is not an automation event (the set is goal.approved, task.created, task.approved, task.completed)';
    install({ "automation.trigger.create": refusal(-32602, sentence) });
    render(<AutomationPage />);
    fireEvent.change(await screen.findByTestId("create-json"), {
      target: { value: '{"name":"Nope","match":{"event":null},"blueprint":"dev-loop","maxRunsPerHour":5}' },
    });
    fireEvent.click(screen.getByTestId("create"));
    const refusalBox = await screen.findByTestId("create-refusal");
    expect(refusalBox.textContent).toContain(sentence); // whole, unedited — the sentence is the product
  });

  it("renders an honest unwired note on a daemon without the methods", async () => {
    install({ "automation.trigger.list": refusal(-32601, 'method "automation.trigger.list" is not registered') });
    render(<AutomationPage />);
    expect((await screen.findByTestId("unwired")).textContent).toContain("predates S40");
    expect(screen.queryByTestId("triggers-empty")).toBeNull(); // never a lying empty table
  });

  it("the page has no delete, approve, run, fire or retry control", async () => {
    install();
    render(<AutomationPage />);
    await screen.findByTestId("automation-page");
    for (const name of [/delete/i, /approve/i, /run/i, /fire/i, /retry/i]) {
      expect(screen.queryByRole("button", { name })).toBeNull();
    }
  });

  it("the page only ever calls the six trigger methods", async () => {
    const d = install();
    render(<AutomationPage />);
    fireEvent.click(await screen.findByTestId("trigger-row-tr-nightly"));
    fireEvent.click(screen.getByTestId("enable"));
    fireEvent.click(screen.getByTestId("refresh"));
    await waitFor(() => expect(d.methods().length).toBeGreaterThanOrEqual(4));
    expect(d.methods().every((m) => THE_SIX.includes(m))).toBe(true);
  });

  it("TestBench mounts it as its own tab", async () => {
    install();
    render(<TestBench activePage="automation" />);
    expect(await screen.findByTestId("automation-page")).toBeTruthy();
    expect(await screen.findByTestId("fire-log")).toBeTruthy();
  });
});
