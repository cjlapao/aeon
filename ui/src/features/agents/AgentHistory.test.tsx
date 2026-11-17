import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import type { LiveState } from "../../lib/live";
import { resetBridge } from "../../lib/bridge";
import { APP_LOCALES } from "../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { AgentChanges } from "./AgentChanges";
import { AgentsActivity } from "./AgentsActivity";
import { LEDGER_CHUNK } from "./agentsLedger";

// The agent's history is a timeline of ONE agent, and the whole ledger is the Agents page's
// Activity view. Both read the same call: `slug` present for the first, absent for the second —
// the absence IS the second view, because a migrate run names no single agent (a NULL slug)
// and only the unfiltered answer carries it. Every word on screen is the daemon's; what this
// file pins is which call each surface makes and that a short history is loaded whole.

const liveCurrent: LiveState = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "1.0",
  protocolVersion: 1,
  generation: 1,
};

type Change = { id: number; op: string; slug: string | null; summary: string; actorKind: string; actorId: string; occurredAt: string };

const ROWS: Change[] = [
  { id: 3, op: "edit", slug: "reviewer-plus", summary: "edit reviewer-plus (9 content fields)", actorKind: "user", actorId: "1000", occurredAt: "2026-11-06T10:00:00Z" },
  { id: 2, op: "import", slug: "reviewer-plus", summary: "import reviewer-plus v2 (created, proposal 12)", actorKind: "cli", actorId: "cli", occurredAt: "2026-11-05T09:30:00Z" },
  { id: 1, op: "migrate", slug: null, summary: "migrate derived 34 rows from plugin_agent", actorKind: "system", actorId: "seed-engine", occurredAt: "2026-11-04T08:15:00Z" },
];

type LedgerParams = { page?: number; pageSize?: number; slug?: string };

/** The ledger the way the daemon answers it: the ask echoed, newest first, windowed by the page the ask named. */
function ledgerOf(rows: Change[]) {
  return (raw: unknown) => {
    const p = (raw ?? {}) as LedgerParams;
    const page = typeof p.page === "number" ? p.page : 1;
    const pageSize = typeof p.pageSize === "number" ? p.pageSize : LEDGER_CHUNK;
    const slug = typeof p.slug === "string" ? p.slug : "";
    const matched = slug === "" ? rows : rows.filter((row) => row.slug === slug);
    const start = (page - 1) * pageSize;
    return { changes: matched.slice(start, start + pageSize), page, pageSize, total: matched.length, slug };
  };
}

function asked(daemon: ReturnType<typeof installFakeDaemon>): LedgerParams[] {
  return daemon.calls.filter((c) => c.method === "agent.changes").map((c) => c.params as LedgerParams);
}

function wrap(node: React.ReactNode) {
  return render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>{node}</MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("an agent's history (a timeline of one agent)", () => {
  it("asks with the agent's slug and shows each change in words, with the daemon's sentence", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.changes": ledgerOf(ROWS) } });
    wrap(<AgentChanges slug="reviewer-plus" live={liveCurrent} />);
    await screen.findByTestId("agent-change-3-op");

    expect(asked(daemon).every((ask) => ask.slug === "reviewer-plus")).toBe(true);
    expect(screen.getByTestId("agent-change-3-op").textContent).toBe("Edited");
    expect(screen.getByTestId("agent-change-3-summary").textContent).toBe("edit reviewer-plus (9 content fields)");
    expect(screen.getByTestId("agent-change-2-op").textContent).toBe("Imported");
    // The entry that names no single agent is not here: it lives on the Agents page.
    expect(screen.queryByTestId("agent-change-1-op")).toBeNull();
    expect(screen.getByTestId("agent-history-count").textContent).toBe("2 of 2 changes");
  });

  it("names the proposal an entry arrived through, beside the daemon's own sentence", async () => {
    installFakeDaemon({ responses: { "agent.changes": ledgerOf(ROWS) } });
    wrap(<AgentChanges slug="reviewer-plus" live={liveCurrent} />);
    await screen.findByTestId("agent-change-2-op");
    expect(screen.getByText("arrived through proposal 12")).toBeTruthy();
  });

  it("says plainly when an agent has never been touched", async () => {
    installFakeDaemon({ responses: { "agent.changes": ledgerOf(ROWS) } });
    wrap(<AgentChanges slug="nobody" live={liveCurrent} />);
    expect(await screen.findByTestId("agent-history-empty")).toBeTruthy();
    expect(screen.queryByTestId("agent-history-rows")).toBeNull();
  });

  it("prints a refusal whole and asks again when told to", async () => {
    let calls = 0;
    installFakeDaemon({
      responses: {
        "agent.changes": (raw: unknown) => {
          calls += 1;
          if (calls === 1) throw new Error("refused: the ledger is not readable");
          return ledgerOf(ROWS)(raw);
        },
      },
    });
    wrap(<AgentChanges slug="reviewer-plus" live={liveCurrent} />);
    const error = await screen.findByTestId("agent-history-error");
    expect(error.textContent).toContain("the ledger is not readable");
    fireEvent.click(screen.getByTestId("agent-history-retry"));
    await screen.findByTestId("agent-change-3-op");
  });

  it("loads every window of a long history, one chunk at a time, until the daemon's count is reached", async () => {
    const many: Change[] = Array.from({ length: LEDGER_CHUNK + 5 }, (_, i) => ({
      id: 1000 - i,
      op: "edit",
      slug: "reviewer-plus",
      summary: `edit ${i}`,
      actorKind: "user",
      actorId: "1000",
      occurredAt: "2026-11-06T10:00:00Z",
    }));
    const daemon = installFakeDaemon({ responses: { "agent.changes": ledgerOf(many) } });
    wrap(<AgentChanges slug="reviewer-plus" live={liveCurrent} />);
    await waitFor(() => expect(screen.getByTestId("agent-history-count").textContent).toBe(`${LEDGER_CHUNK + 5} of ${LEDGER_CHUNK + 5} changes`));
    expect(asked(daemon).map((ask) => ask.page)).toEqual([1, 2]);
  });
});

describe("the Agents page's Activity view (the whole ledger)", () => {
  it("asks with no slug, and is where the entry naming no single agent appears", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.changes": ledgerOf(ROWS) } });
    wrap(<AgentsActivity live={liveCurrent} onOpenAgent={() => undefined} />);
    await screen.findByTestId("agent-change-1-subject");

    expect(asked(daemon).every((ask) => ask.slug === undefined)).toBe(true);
    expect(screen.getByTestId("agent-change-1-subject").textContent).toBe("Every agent");
    expect(screen.getByTestId("agent-change-1-summary").textContent).toBe("migrate derived 34 rows from plugin_agent");
    expect(screen.getByTestId("agents-activity-count").textContent).toBe("Showing 3 of 3");
  });

  it("opens an agent from its name, and offers no door for the entry that names none", async () => {
    installFakeDaemon({ responses: { "agent.changes": ledgerOf(ROWS) } });
    const opened: string[] = [];
    wrap(<AgentsActivity live={liveCurrent} onOpenAgent={(slug) => opened.push(slug)} />);
    const name = await screen.findByTestId("agent-change-3-subject");
    fireEvent.click(name);
    expect(opened).toEqual(["reviewer-plus"]);
    expect(screen.getByTestId("agent-change-1-subject").tagName).not.toBe("BUTTON");
  });
});
