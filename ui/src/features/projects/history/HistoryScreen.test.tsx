import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { HistoryScreen } from "./HistoryScreen";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import {
  FLOOR_STATEMENT,
  HISTORY_COVERAGE_HEALTHY,
  HISTORY_COVERAGE_WARNING,
  HISTORY_EMPTY_BLANK,
  HISTORY_EMPTY_WITH_STATEMENT,
  HISTORY_REFUSAL,
  HISTORY_VOCABULARY,
  historyBaseResponses,
} from "../../../../test/history-fixtures";

/**
 * S53 T5 — the History surface, on the fake daemon (no daemon needed, the
 * triage/roadmap idiom), answered with the REAL conformance fixtures
 * (`test/history-fixtures.ts`). What is asserted here is the slice's own
 * acceptance shape: the three coverage states render distinctly and the floor
 * sentence is verbatim; the lane switch is the daemon's vocabulary and a
 * change re-asks the daemon rather than re-slicing a client-side list; the
 * empty is the daemon's statement or an explicit absence of one; the person's
 * export is the one door and it is inert when there is no person at a current
 * connection. No write door is called, and the whole call log says so.
 */

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

const offlineLive = {
  state: "disconnected",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  reason: "the daemon closed the socket",
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
  vi.restoreAllMocks();
});

function renderScreen(
  responses: Record<string, unknown> = historyBaseResponses(),
  live: LiveState = currentLive,
  canWrite?: boolean,
): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <HistoryScreen controller={new LiveController()} live={live} {...(canWrite === undefined ? {} : { canWrite })} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

/** The five doors plus the borrowed roots read, and nothing else: the whole
 *  call log, checked as a set. A sixth name — a write door — fails here. */
const DOORS = ["component.list", "history.coverage", "history.export", "history.list", "history.show", "history.vocabulary"];

describe("HistoryScreen (S53 T5)", () => {
  it("asks its four reads on a current connection, and nothing that is not a registered door", async () => {
    const daemon = renderScreen();
    await screen.findByTestId("history-row-live");
    for (const door of ["history.vocabulary", "history.coverage", "history.list"]) {
      expect(daemon.methods(), `${door} was never asked`).toContain(door);
    }
    // The export is a person's click, not a mount side effect:
    expect(daemon.methods()).not.toContain("history.export");
    expect([...new Set(daemon.methods())].filter((m) => !DOORS.includes(m))).toEqual([]);
    // An untouched scope sends nothing at all (omitempty on every filter).
    expect(daemon.paramsOf("history.list")).toEqual({});
  });

  it("the lane switch offers the daemon’s served words, in its order, plus the default that sends nothing", async () => {
    renderScreen();
    const switcher = (await screen.findByTestId("history-scope-lane")) as HTMLSelectElement;
    const offered = [...switcher.options].map((option) => option.value);
    // "" (ask nothing) then the served lanes — no lane word is this screen's.
    expect(offered).toEqual(["", ...HISTORY_VOCABULARY.lanes]);
  });

  it("choosing a lane re-asks the DAEMON with that grouping; the screen never re-slices a fetched list", async () => {
    const daemon = renderScreen();
    await screen.findByTestId("history-row-live");
    fireEvent.change(screen.getByTestId("history-scope-lane"), { target: { value: "phase" } });
    await waitFor(() => {
      expect(daemon.paramsOf("history.list")).toEqual({ groupBy: "phase" });
    });
    // Untouched dials still never ride the wire:
    const params = daemon.paramsOf("history.list") as Record<string, unknown>;
    expect(params).not.toHaveProperty("projectRoot");
    expect(params).not.toHaveProperty("since");
    // and the second scope key is never sent, because naming both is refused:
    expect(params).not.toHaveProperty("componentRoot");
  });

  it("naming a root sends projectRoot only — one scope, asked one way", async () => {
    const daemon = renderScreen();
    await screen.findByTestId("history-row-live");
    fireEvent.change(screen.getByTestId("history-scope-root"), { target: { value: "/home/cjlapao/code/acme/api" } });
    await waitFor(() => {
      expect(daemon.paramsOf("history.coverage")).toEqual({ projectRoot: "/home/cjlapao/code/acme/api" });
    });
    expect(daemon.paramsOf("history.list")).toEqual({ projectRoot: "/home/cjlapao/code/acme/api" });
  });

  it("coverage with nothing undecodable renders its own state, the counts it was given, and the floor VERBATIM", async () => {
    renderScreen(historyBaseResponses({ "history.coverage": HISTORY_COVERAGE_HEALTHY }));
    const banner = await screen.findByTestId("history-coverage");
    expect(banner.getAttribute("data-coverage-state")).toBe("healthy");
    expect(within(banner).getByTestId("history-coverage-live").textContent).toContain("1");
    expect(within(banner).getByTestId("history-coverage-archived").textContent).toContain("1");
    expect(within(banner).getByTestId("history-coverage-healthy")).toBeTruthy();
    expect(within(banner).queryByTestId("history-coverage-undecodable")).toBeNull();
    // The span is the daemon's own stamps, unformatted:
    expect(within(banner).getByTestId("history-coverage-span").textContent).toContain("2026-11-01T09:00:00Z");
    expect(within(banner).getByTestId("history-coverage-span").textContent).toContain("2026-11-04T10:00:00Z");
    // P32's sentence, byte for byte — not a paraphrase of it:
    expect(within(banner).getByTestId("history-floor-statement").textContent).toBe(FLOOR_STATEMENT);
  });

  it("coverage with something undecodable is a WARNING that names the ids, and the notShown reason keeps its count", async () => {
    renderScreen(historyBaseResponses({ "history.coverage": HISTORY_COVERAGE_WARNING }));
    const banner = await screen.findByTestId("history-coverage");
    expect(banner.getAttribute("data-coverage-state")).toBe("warning");
    const warning = within(banner).getByTestId("history-coverage-undecodable");
    expect(warning.textContent).toContain("3");
    // Named, not summarised away — P31's third state with a name on it:
    for (const id of ["loop_ghost_1", "loop_ghost_2", "loop_ghost_3"]) {
      expect(within(banner).getByTestId(`history-coverage-id-${id}`)).toBeTruthy();
    }
    // The healthy line is gone, so the two states cannot read the same:
    expect(within(banner).queryByTestId("history-coverage-healthy")).toBeNull();
    // The reason is the daemon's sentence and the count is its number:
    expect(within(banner).getByTestId("history-notshown-reason-0").textContent).toBe("archived loops carry no steps");
    expect(within(banner).getByTestId("history-notshown-count-0").textContent).toBe("4");
    // The floor statement is present in BOTH states — always:
    expect(within(banner).getByTestId("history-floor-statement").textContent).toBe(FLOOR_STATEMENT);
  });

  it("a coverage read that was refused is said, not skipped", async () => {
    const daemon = renderScreen(
      historyBaseResponses({
        "history.coverage": () => {
          throw JSON.stringify({ code: -31800, message: "the coverage read was refused", data: {} });
        },
      }),
    );
    await screen.findByTestId("history-coverage-failed");
    expect(screen.getByTestId("history-coverage-failed").textContent).toContain("the coverage read was refused");
    // …and the rows still stand, with the banner saying they are unqualified:
    expect(screen.getByTestId("history-rows-panel")).toBeTruthy();
    expect(daemon.methods()).toContain("history.list");
  });

  it("the empty is the daemon’s own statement, quoted; and when it sent none, the screen says so", async () => {
    renderScreen(historyBaseResponses({ "history.list": HISTORY_EMPTY_WITH_STATEMENT }));
    const empty = await screen.findByTestId("history-rows-empty");
    expect(empty.textContent).toBe("no history — no live or archived loops in scope");
    // No row was invented to fill it:
    expect(document.querySelector("[data-testid^='history-row-']")).toBeNull();

    cleanup();
    renderScreen(historyBaseResponses({ "history.list": HISTORY_EMPTY_BLANK }));
    const blank = await screen.findByTestId("history-rows-empty");
    expect(blank.textContent).toContain("no statement");
    expect(blank.textContent).not.toContain("no history — no live or archived loops in scope");
  });

  it("a row renders the daemon’s describe fields verbatim and its links as routes", async () => {
    renderScreen();
    const row = await screen.findByTestId("history-row-live");
    expect(within(row).getByTestId("history-lane-live").textContent).toContain("live");
    expect(within(row).getByTestId("history-outcome-live").textContent).toContain("running");
    expect(within(row).getByTestId("history-cost-live").textContent).toContain("0.001");
    expect(within(row).getByTestId("history-cost-state-live").textContent).toBe("partial");
    // The kind word is the row's own; the route is the app's:
    expect(within(row).getByTestId("history-link-live-loop").textContent).toContain("loop");
    expect(within(row).getByTestId("history-link-live-loop").querySelector("a")?.getAttribute("href")).toBe("/loops/live");
    // A row's status/timeline are the wire's, not a re-derivation:
    expect(row.textContent).toContain("2026-11-04T10:00:00Z");
  });

  it("the drill-down is history.show for ONE entry, and its transcript renders as the daemon sent it", async () => {
    const daemon = renderScreen();
    await screen.findByTestId("history-row-live");
    fireEvent.click(screen.getByTestId("history-open-live"));
    await screen.findByTestId("history-transcript");
    expect(daemon.methods()).toContain("history.show");
    expect(daemon.paramsOf("history.show")).toEqual({ id: "live" });
    expect(screen.getByTestId("history-message-role-0").textContent).toBe("user");
    expect(screen.getByTestId("history-part-0-0").textContent).toBe("build the live loop");
  });

  it("a refused drill-down travels verbatim instead of reading as an empty entry", async () => {
    // The fixture's own bytes: code -31800 and the `what` that names the id
    // the scope does not hold (conformance/history/refused.json — DB19).
    renderScreen(historyBaseResponses({ "history.show": () => {
      throw HISTORY_REFUSAL;
    } }));
    await screen.findByTestId("history-row-live");
    fireEvent.click(screen.getByTestId("history-open-live"));
    const refused = await screen.findByTestId("history-drilldown-refused");
    expect(refused.textContent).toContain("the history read was refused");
    expect(screen.queryByTestId("history-transcript")).toBeNull();
  });

  it("the export is a person’s door: a read-only viewer sees it disabled and is told why", async () => {
    const daemon = renderScreen(historyBaseResponses(), currentLive, false);
    const button = (await screen.findByTestId("history-export")) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(screen.getByTestId("history-export-person-only")).toBeTruthy();
    // A click on an inert control asks nothing — the door is not bypassed by
    // reaching past its disabled state.
    fireEvent.click(button);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(daemon.methods()).not.toContain("history.export");
  });

  it("offline and reconnecting ask nothing at all, and the export is inert in both", async () => {
    for (const [live, testId] of [
      [staleLive, "history-reconnecting"],
      [offlineLive, "history-offline"],
    ] as const) {
      const daemon = renderScreen(historyBaseResponses(), live);
      await screen.findByTestId(testId);
      expect(daemon.methods()).toEqual([]);
      expect((screen.getByTestId("history-export") as HTMLButtonElement).disabled).toBe(true);
      cleanup();
    }
  });

  it("the person’s export asks history.export with the SAME scope and writes the payload it answers with", async () => {
    const created: unknown[] = [];
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      writable: true,
      value: (blob: unknown) => {
        created.push(blob);
        return "blob:aeon-history";
      },
    });
    const revoked = vi.fn();
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, writable: true, value: revoked });
    const clicked: Array<{ download: string; href: string }> = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function click(this: HTMLAnchorElement) {
      clicked.push({ download: this.download, href: this.href });
    });

    const daemon = renderScreen(historyBaseResponses());
    await screen.findByTestId("history-row-live");
    fireEvent.change(screen.getByTestId("history-scope-root"), { target: { value: "/home/cjlapao/code/acme/api" } });
    await waitFor(() => expect(daemon.paramsOf("history.list")).toEqual({ projectRoot: "/home/cjlapao/code/acme/api" }));

    fireEvent.click(screen.getByTestId("history-export"));
    await waitFor(() => expect(daemon.methods()).toContain("history.export"));
    // The same question the rows were answered with (H3: one List, two doors):
    expect(daemon.paramsOf("history.export")).toEqual({ projectRoot: "/home/cjlapao/code/acme/api" });
    // The payload the daemon answered with, written out — not a re-query, not
    // a re-shape, and never read back from disk:
    await waitFor(() => expect(created.length).toBe(1));
    expect(clicked[0]?.download).toBe("aeon-history-home-cjlapao-code-acme-api.ndjson");
    expect(revoked).toHaveBeenCalledTimes(1);
    // jsdom's Blob answers no `.text()`; the FileReader is how a browser
    // would read it back, and it is what the test reads with.
    const written = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(reader.error);
      reader.readAsText(created[0] as Blob);
    });
    expect(written).toContain('"id":"ghost"');
    // The rows written out are the EXPORT door's, one per line:
    expect(written.trim().split("\n")).toHaveLength(1);
    expect(screen.getByTestId("history-exported").textContent).toContain("1");
    // The rows on screen are still the LIST's answer, not the export's:
    expect(screen.getByTestId("history-row-live")).toBeTruthy();
    expect(document.querySelector("[data-testid='history-row-ghost']")).toBeNull();
  });

  it("an export the daemon refuses is said in its own words", async () => {
    renderScreen(historyBaseResponses({ "history.export": () => {
      // The same fixture refusal, met at the export door instead of the
      // drill-down's: the parts the daemon sent come out with it.
      throw HISTORY_REFUSAL;
    } }));
    await screen.findByTestId("history-row-live");
    fireEvent.click(screen.getByTestId("history-export"));
    const refusal = await screen.findByTestId("history-export-refusal");
    expect(refusal.textContent).toContain("the history read was refused");
    expect(refusal.textContent).toContain('no loop or archive with id "no-such-loop" in scope');
  });
});
