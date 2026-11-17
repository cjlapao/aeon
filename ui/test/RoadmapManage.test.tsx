import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { RoadmapPage } from "../src/features/roadmap/RoadmapPage";
import { APP_LOCALES } from "../src/lib/i18n";
import { LiveController } from "../src/lib/live";
import type { LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";
import {
  IDEA_DESCRIBE,
  IDEA_BLOCKER,
  PROPOSAL_DIGEST,
  ROADMAP_DISPATCH_STATEMENT,
  roadmapBaseResponses,
  roadmapRefusal,
  RUNNING_DESCRIBE,
} from "./roadmap-fixtures";

// S50 T7 — the roadmap MANAGEMENT surface held to what it is FOR: the
// daemon's sentences (describe, blocker, refusal, authority statement)
// render BYTE FOR BYTE; filters travel as `roadmap.board` PARAMS and never
// as a client-side sweep; the path fields answer `pathEditableStatuses`
// membership, not a remembered status list; the dispatch confirm shows the
// daemon's statement with its `<item>` slot FILLED, and declining sends
// nothing at all; settings save through `settings.set`; proposals commit
// through S45's digest discipline. Same harness as AutomationSection.test.

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

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({
    matches: false,
    media: "",
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

function renderPage(responses: Record<string, unknown> = roadmapBaseResponses(), live: LiveState = currentLive): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <RoadmapPage controller={new LiveController()} live={live} canWrite />
    </I18nProvider>,
  );
  return daemon;
}

/** Type the component root so the board becomes loadable, and wait for the
 *  rows the daemon answered with. Pass the fake to also pin the params. */
async function loadBoard(daemon?: FakeDaemon): Promise<void> {
  fireEvent.change(screen.getByTestId("board-filter-root"), { target: { value: "/srv/repos/ledger" } });
  await screen.findByTestId("board-row-7");
  if (daemon !== undefined) expect(daemon.paramsOf("roadmap.board")).toEqual({ componentRoot: "/srv/repos/ledger" });
}

describe("the board (RM6, AN11)", () => {
  it("renders the daemon's describe and blocker sentences verbatim, counts as answered", async () => {
    const daemon = renderPage();
    await loadBoard(daemon);
    expect(screen.getByTestId("board-describe-7").textContent).toBe(IDEA_DESCRIBE);
    expect(screen.getByTestId("board-describe-8").textContent).toBe(RUNNING_DESCRIBE);
    // The blocker sentence byte-for-byte — the daemon names the missing thing.
    expect(screen.getByTestId("board-blocker-7").textContent).toBe(IDEA_BLOCKER);
    expect(screen.getByTestId("board-ready-8")).toBeTruthy();
    expect(screen.getByTestId("board-loop-8").textContent).toContain("ledger-reconcile-1102");
    expect(screen.getByTestId("board-groups").textContent).toContain("idea: 1");
  });

  it("filters ride the board call as PARAMS — never a client-side sweep", async () => {
    const daemon = renderPage();
    await loadBoard(daemon);
    fireEvent.change(screen.getByTestId("board-filter-status"), { target: { value: "running" } });
    await waitFor(() =>
      expect(daemon.paramsOf("roadmap.board")).toEqual({ componentRoot: "/srv/repos/ledger", status: "running" }),
    );
    fireEvent.change(screen.getByTestId("board-filter-epic"), { target: { value: "reporting" } });
    await waitFor(() =>
      expect(daemon.paramsOf("roadmap.board")).toEqual({ componentRoot: "/srv/repos/ledger", status: "running", epic: "reporting" }),
    );
    // The status dial is the vocabulary's OWN statuses array, daemon order.
    const options = (screen.getByTestId("board-filter-status") as HTMLSelectElement).options;
    expect(options[1]?.textContent).toBe("idea");
    expect(options[8]?.textContent).toBe("cancelled");
  });

  it("asks nothing until the root is named, and says so", () => {
    const daemon = renderPage();
    expect(screen.getByTestId("board-needs-root")).toBeTruthy();
    expect(daemon.methods()).not.toContain("roadmap.board");
  });

  it("a stale connection says so and asks nothing", async () => {
    const daemon = renderPage(roadmapBaseResponses(), staleLive);
    expect(await screen.findByTestId("roadmap-manage-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("roadmap.board");
    expect(daemon.methods()).not.toContain("roadmap.vocabulary");
  });
});

describe("the item drawer — edit (RM7)", () => {
  it("hides the path fields when the vocabulary's window excludes the status, shows them when it admits it", async () => {
    renderPage();
    await loadBoard();
    fireEvent.click(screen.getByTestId("board-row-7"));
    await screen.findByTestId("edit-brief-path"); // idea IS in pathEditableStatuses
    fireEvent.click(screen.getByTestId("drawer-close"));
    fireEvent.click(screen.getByTestId("board-row-8"));
    await screen.findByTestId("edit-paths-hidden"); // running is NOT
    expect(screen.queryByTestId("edit-brief-path")).toBeNull();
    expect(screen.queryByTestId("edit-plan-path")).toBeNull();
  });

  it("keeps the draft on a refusal and renders the daemon's sentence verbatim", async () => {
    const daemon = renderPage({
      ...roadmapBaseResponses(),
      "roadmap.update": roadmapRefusal(-32000, "roadmap: title must not be blank"),
    });
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    await screen.findByTestId("edit-title");
    fireEvent.change(screen.getByTestId("edit-title"), { target: { value: "Renamed burn-down" } });
    fireEvent.click(screen.getByTestId("edit-save"));
    const refusal = await screen.findByTestId("edit-refusal");
    expect(refusal.textContent).toContain("roadmap: title must not be blank");
    // The person's words SURVIVE the daemon's no.
    expect((screen.getByTestId("edit-title") as HTMLInputElement).value).toBe("Renamed burn-down");
    // And only touched fields travelled.
    expect(daemon.paramsOf("roadmap.update")).toEqual({ id: 7, title: "Renamed burn-down" });
  });

  it("answers changed:false honestly — nothing happened, said so", async () => {
    const daemon = renderPage({ ...roadmapBaseResponses(), "roadmap.update": { item: {}, changed: false } });
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    await screen.findByTestId("edit-title");
    fireEvent.change(screen.getByTestId("edit-title"), { target: { value: "  " } });
    fireEvent.click(screen.getByTestId("edit-save"));
    // (title only differs, so a touched-field param goes out; the fake
    // answers changed:false and the line says what it means.)
    await screen.findByTestId("edit-nochange");
  });

  it("displays the vocabulary's byte limits, not invented ones", async () => {
    const daemon = renderPage();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    await screen.findByTestId("edit-title");
    expect(screen.getByTestId("item-edit-form").textContent).toContain("200");
    expect(screen.getByTestId("item-edit-form").textContent).toContain("64");
  });
});

describe("the item drawer — history (RM6)", () => {
  it("keeps the daemon's order, marks system rows visibly, quotes summaries verbatim", async () => {
    const daemon = renderPage();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    fireEvent.click(screen.getByTestId("drawer-tab-history"));
    const systemRow = await screen.findByTestId("history-row-41");
    expect(systemRow.getAttribute("data-actor")).toBe("system");
    // Visibly distinct: labelled as the system's line, not dressed as a person's.
    expect(screen.getByTestId("history-actor-41").textContent).toBe("system");
    expect(screen.getByTestId("history-summary-41").textContent).toBe("reactor: idea → ideating (loop ledger-ideate-1103 started)");
    const personRow = screen.getByTestId("history-row-40");
    expect(personRow.getAttribute("data-actor")).toBe("person");
    // The person row names the actor the audit carries, verbatim.
    expect(screen.getByTestId("history-actor-40").textContent).toBe("cli:cjlapao");
    // Newest first AS ANSWERED — no re-sorting by the UI.
    const items = screen.getAllByTestId(/^history-row-/);
    expect(items[0]?.getAttribute("data-testid")).toBe("history-row-41");
    expect(daemon.paramsOf("roadmap.history")).toEqual({ id: 7 });
  });
});

describe("the item drawer — dispatch (RM9)", () => {
  it("fills the <item> slot with the daemon's describe and sends NOTHING when declining", async () => {
    const daemon = renderPage();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    fireEvent.click(screen.getByTestId("dispatch-open-ideate"));
    const statement = await screen.findByTestId("dispatch-statement");
    // The daemon's statement, slot replaced by the daemon's own describe.
    expect(statement.textContent).toContain(ROADMAP_DISPATCH_STATEMENT.replace("<item>", IDEA_DESCRIBE));
    expect(statement.textContent).not.toContain("<item>");
    // No root named — the go control is not offered; declining sends nothing.
    expect((screen.getByTestId("dispatch-go-ideate") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("dispatch-cancel"));
    fireEvent.click(screen.getByTestId("drawer-close"));
    expect(daemon.methods()).not.toContain("roadmap.ideate");
    expect(daemon.methods()).not.toContain("roadmap.plan");
    expect(daemon.methods()).not.toContain("roadmap.materialize");
  });

  it("confirms, names the workspace root, and the verb goes out with exactly the wire fields", async () => {
    const daemon = renderPage();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    fireEvent.click(screen.getByTestId("dispatch-open-ideate"));
    fireEvent.change(screen.getByTestId("dispatch-workspace-root"), { target: { value: "/srv/repos/ledger" } });
    fireEvent.click(screen.getByTestId("dispatch-go-ideate"));
    await waitFor(() => expect(daemon.methods()).toContain("roadmap.ideate"));
    expect(daemon.paramsOf("roadmap.ideate")).toEqual({ id: 7, workspaceRoot: "/srv/repos/ledger" });
    const answer = await screen.findByTestId("dispatch-answer");
    expect(answer.textContent).toContain("loopSlug: ledger-ideate-1103");
  });

  it("a refused dispatch shows the daemon's sentence whole", async () => {
    const daemon = renderPage({
      ...roadmapBaseResponses(),
      "roadmap.ideate": roadmapRefusal(-32000, 'blueprint "brainstorm" does not declare produces: brief.md'),
    });
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("board-row-7"));
    fireEvent.click(screen.getByTestId("dispatch-open-ideate"));
    fireEvent.change(screen.getByTestId("dispatch-workspace-root"), { target: { value: "/srv/repos/ledger" } });
    fireEvent.click(screen.getByTestId("dispatch-go-ideate"));
    const refusal = await screen.findByTestId("dispatch-refusal");
    expect(refusal.textContent).toContain('blueprint "brainstorm" does not declare produces: brief.md');
  });
});

describe("the dispatch settings card (RM8)", () => {
  it("reads both views, shows the health dots with the daemon's state WORDS, saves through settings.set", async () => {
    const daemon = renderPage();
    await screen.findByTestId("dispatch-setting-roadmap.ideationBlueprint");
    expect(daemon.calls.filter((c) => c.method === "settings.get").map((c) => (c.params as { key: string }).key).sort()).toEqual([
      "roadmap.ideationBlueprint",
      "roadmap.planBlueprint",
    ]);
    // State words verbatim — the tone is the UI's, the word is the daemon's.
    expect(screen.getByTestId("dispatch-health-roadmap.ideationBlueprint").textContent).toBe("ok");
    expect(screen.getByTestId("dispatch-health-roadmap.planBlueprint").textContent).toBe("missing");
    expect(screen.getByTestId("dispatch-counts-roadmap.ideationBlueprint").textContent).toBe("4/0/1");
    expect(screen.getByTestId("dispatch-evaluated-at").textContent).toContain("2026-11-03T12:00:00Z");
    // Save through the one settings write, under the right key.
    fireEvent.click(screen.getByTestId("dispatch-save-roadmap.ideationBlueprint"));
    await waitFor(() => expect(daemon.methods()).toContain("settings.set"));
    expect(daemon.paramsOf("settings.set")).toEqual({ key: "roadmap.ideationBlueprint", value: "brainstorm" });
  });

  it("a refused save shows the daemon's sentence whole", async () => {
    renderPage({
      ...roadmapBaseResponses(),
      "settings.set": roadmapRefusal(-32000, "setting roadmap.ideationBlueprint: unknown blueprint"),
    });
    await screen.findByTestId("dispatch-setting-roadmap.ideationBlueprint");
    fireEvent.click(screen.getByTestId("dispatch-save-roadmap.ideationBlueprint"));
    const refusal = await screen.findByTestId("dispatch-refusal-roadmap.ideationBlueprint");
    expect(refusal.textContent).toContain("setting roadmap.ideationBlueprint: unknown blueprint");
  });
});

describe("the proposals strip (S45's desk, narrowed)", () => {
  it("renders roadmap_idea rows only; commit carries the digest proposal.get answered", async () => {
    const daemon = renderPage();
    expect(await screen.findByTestId("proposal-row-12")).toBeTruthy();
    // The other kind is the shared desk's row — this strip never renders it.
    expect(screen.queryByTestId("proposal-row-13")).toBeNull();
    expect(screen.getByTestId("proposal-title-12").textContent).toBe("Weekly burn-down export");
    expect(screen.getByTestId("proposal-note-12").textContent).toBe("weekly export from the effort rows");
    // The door filters by STATUS only; the strip asked for the proposed set.
    expect(daemon.paramsOf("proposal.list")).toEqual({ status: "proposed" });

    // An unreasoned reject is stopped at the control, not sent.
    expect((screen.getByTestId("proposal-reject-12") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId("proposal-reason-12"), { target: { value: "already on the board" } });
    expect((screen.getByTestId("proposal-reject-12") as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByTestId("proposal-commit-12"));
    await waitFor(() => expect(daemon.methods()).toContain("proposal.commit"));
    // The digest the person READ is the digest the commit CARRIES (PM7).
    expect(daemon.paramsOf("proposal.get")).toEqual({ id: 12 });
    expect(daemon.paramsOf("proposal.commit")).toEqual({ id: 12, digest: PROPOSAL_DIGEST });

    fireEvent.click(screen.getByTestId("proposal-reject-12"));
    await waitFor(() => expect(daemon.methods()).toContain("proposal.reject"));
    expect(daemon.paramsOf("proposal.reject")).toEqual({ id: 12, reason: "already on the board" });
  });

  it("a refused decision shows the daemon's sentence whole", async () => {
    renderPage({
      ...roadmapBaseResponses(),
      "proposal.commit": roadmapRefusal(-32000, "the proposal changed since you read it"),
    });
    await screen.findByTestId("proposal-row-12");
    fireEvent.click(screen.getByTestId("proposal-commit-12"));
    const refusal = await screen.findByTestId("proposal-refusal-12");
    expect(refusal.textContent).toContain("the proposal changed since you read it");
  });
});

describe("the Gantt half", () => {
  it("stays exactly the S26 page when the live pair is not mounted with it", () => {
    installFakeDaemon({ responses: {} });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <RoadmapPage />
      </I18nProvider>,
    );
    expect(screen.getByTestId("roadmap-gantt")).toBeTruthy();
    expect(screen.queryByTestId("roadmap-manage")).toBeNull();
  });

  it("still sits above the management half on the real route", async () => {
    renderPage();
    expect(await screen.findByTestId("roadmap-manage")).toBeTruthy();
    expect(screen.getByTestId("roadmap-gantt")).toBeTruthy();
  });
});
