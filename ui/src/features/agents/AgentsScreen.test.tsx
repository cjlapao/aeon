import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider, ToastProvider, ToastViewport } from "@cjlapao/ui-kit";
import { LiveController, type LiveState } from "../../lib/live";
import { resetBridge } from "../../lib/bridge";
import { APP_LOCALES } from "../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { AgentsScreen } from "./AgentsScreen";
import {
  INTERNAL_DELETE_REFUSAL,
  INTERNAL_EDIT_REFUSAL,
  refusalFor,
  type AgentRowsParams,
  type AgentRowsResult,
} from "./agentsModel";

// A row's avatar can be a minion, and the minion animates through
// `matchMedia`, which jsdom has never heard of. Same stub the rest of the
// suite uses (AgentPanel.test.tsx).
vi.stubGlobal("matchMedia", () => ({
  matches: false,
  addEventListener() {},
  removeEventListener() {},
}));

// The management page's own behaviour test, co-located with the component (the
// suite's shape). What is under test is WHOSE ANSWER EACH NUMBER IS: the page, the
// order, the filter and the count all come back from `agent.rows`, and this file
// reddens if the browser ever starts computing one of them.
//
// The fake daemon below slices, filters and counts because the real one does;
// the assertions never trust it, they read what the page SENT and what the DOM
// SHOWS. No jest-dom here (this suite has none), so a disabled control is read
// as the property it is.

const liveCurrent: LiveState = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "1.0",
  protocolVersion: 1,
  generation: 1,
};

/** 60 rows: with a chunk of 50 the second chunk is real, and has ten rows in it. */
const ALL = Array.from({ length: 60 }, (_, i) => {
  const n = String(i + 1).padStart(2, "0");
  return {
    slug: `agent-${n}`,
    name: `Agent ${n}`,
    description: `The ${i + 1}th agent in the store`,
    version: "1.0.0",
    // A first-party row near the top: the read-only rule has to be visible without scrolling (CU6).
    internal: `agent-${n}` === "agent-02",
    signed: i % 3 === 0,
    // agent-03 arrived through import, unsigned (i % 3 === 0 is signed, and agent-03 is i = 2).
    origin: `agent-${n}` === "agent-02" ? "seed" : `agent-${n}` === "agent-03" ? "import" : "manual",
    createdAt: `2026-11-0${(i % 7) + 1}T08:00:00Z`,
    updatedAt: `2026-11-0${(i % 7) + 1}T09:00:00Z`,
  };
});

/**
 * The window the way the daemon answers it: the ask echoed, the slice taken. For `sort=slug,
 * dir=asc` the slice comes back REVERSED — a stand-in for a SQL collation that is not the
 * browser's. A page that sorted what it was given would show the browser's order, and the DOM
 * assertion would see the difference.
 */
function windowFor(raw: unknown): AgentRowsResult {
  const p = (raw ?? {}) as AgentRowsParams;
  const page = typeof p.page === "number" ? p.page : 1;
  const pageSize = typeof p.pageSize === "number" ? p.pageSize : 50;
  const sort = typeof p.sort === "string" ? p.sort : "name";
  const dir = p.dir === "desc" ? "desc" : "asc";
  const q = typeof p.q === "string" ? p.q.trim() : "";
  const matched = q === "" ? ALL : ALL.filter((row) => row.slug.includes(q) || row.name.toLowerCase().includes(q.toLowerCase()));
  const key = sort === "slug" ? "slug" : "name";
  const ordered = [...matched].sort((a, b) => {
    const cmp = a[key as "name"].localeCompare(b[key as "name"]);
    return dir === "desc" ? -cmp : cmp;
  });
  const slice = ordered.slice((page - 1) * pageSize, page * pageSize);
  return { agents: sort === "slug" && dir === "asc" ? [...slice].reverse() : slice, page, pageSize, total: matched.length, sort, dir, q };
}

/** A refusal, in the shape the bridge turns into an `RpcError`. */
const REFUSAL = 'invalid params: sort "nonsense" is not a sort';

function mount(live: LiveState = liveCurrent, canWrite = live.state === "current", at = "/config/agents") {
  return render(
    <I18nProvider locales={APP_LOCALES}>
      <ToastProvider>
      <ToastViewport />
      <MemoryRouter initialEntries={[at]}>
        <AgentsScreen controller={new LiveController()} live={live} canWrite={canWrite} />
      </MemoryRouter>
      </ToastProvider>
    </I18nProvider>,
  );
}

/** The page's asks, in order — the evidence every assertion below reads. */
function asked(daemon: ReturnType<typeof installFakeDaemon>): AgentRowsParams[] {
  return daemon.calls.filter((c) => c.method === "agent.rows").map((c) => c.params as AgentRowsParams);
}
function lastAsk(daemon: ReturnType<typeof installFakeDaemon>): AgentRowsParams {
  const all = asked(daemon);
  return all[all.length - 1] as AgentRowsParams;
}
/** The rows the DOM is showing, in DOM order — what a person actually sees. */
function shownSlugs(): string[] {
  return Array.from(document.querySelectorAll('[data-testid^="agents-cell-name-"]')).map((el) => (el.getAttribute("data-testid") ?? "").replace("agents-cell-name-", ""));
}
/** The search box, by its `<label>` — the accessible name the page gives it. */
async function searchBox(): Promise<HTMLElement> {
  return screen.findByRole("textbox", { name: "Search agents" });
}

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("AgentsScreen — the list", () => {
  it("opens on the daemon's first chunk and asks for nothing else", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();

    await screen.findByTestId("agents-screen");
    await waitFor(() => expect(shownSlugs()).toContain("agent-01"));
    expect(shownSlugs().length).toBe(50);
    expect(lastAsk(daemon)).toEqual({ page: 1, pageSize: 50, sort: "name", dir: "asc" });
    // `agent.view` is only ever the per-slug avatar read; no other method belongs to a page that lists.
    expect(daemon.methods().filter((m) => m !== "agent.rows" && m !== "agent.view")).toEqual([]);
    expect(screen.getByTestId("agents-count").textContent).toBe("Showing 50 of 60");
  });

  it("draws its own title: the frame stands down for the list and for an agent's page", async () => {
    installFakeDaemon({ responses: { "agent.rows": windowFor } });
    const view = mount();
    await screen.findByTestId("agents-table");
    expect(view.container.querySelector("h1")?.textContent).toBe("Agents");
    expect(screen.getByTestId("agents-action-bar")).toBeTruthy();
  });

  it("a header click ASKS for that order, replaces the rows from page 1 and reorders nothing at home (PG2)", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));

    const header = within(screen.getByTestId("agents-table"))
      .getAllByRole("columnheader")
      .find((cell) => (cell.textContent ?? "").startsWith("Updated"));
    expect(header, "no Updated header rendered").toBeTruthy();
    fireEvent.click(within(header as HTMLElement).getByRole("button"));

    await waitFor(() => {
      const ask = lastAsk(daemon);
      expect(ask.sort).toBe("updatedAt");
      expect(ask.dir).toBe("asc");
      expect(ask.page).toBe(1);
    });
    await waitFor(() =>
      expect(shownSlugs()).toEqual(windowFor({ page: 1, pageSize: 50, sort: "updatedAt", dir: "asc" }).agents.map((r) => r.slug)),
    );
  });

  it("tags an agent that was imported without a signature, and does not call it \"yours\"", async () => {
    installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    expect(screen.getByTestId("agents-cell-imported-agent-03").textContent).toBe("Imported");
    expect(screen.getByTestId("agents-cell-unsigned-agent-03").textContent).toBe("unsigned");
    expect(screen.queryByTestId("agents-cell-internal-agent-03")).toBeNull();
    // A signed import carries the first tag only; your own agents carry neither.
    expect(screen.getByTestId("agents-cell-internal-agent-01")).toBeTruthy();
    expect(screen.queryByTestId("agents-cell-imported-agent-01")).toBeNull();
  });

  it("loads the next chunk on demand and appends it, then has nothing more to load", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));

    // The kit's table asks as the body nears its end (jsdom lays nothing out), and offers its own
    // button for the same ask.
    fireEvent.click(await within(screen.getByTestId("agents-table")).findByRole("button", { name: /load more/i }));
    await waitFor(() => expect(lastAsk(daemon).page).toBe(2));
    await waitFor(() => expect(shownSlugs().length).toBe(60));
    // Appended in the daemon's order, nothing sorted or de-duplicated here.
    expect(shownSlugs()).toEqual(ALL.map((r) => r.slug));
    expect(screen.getByTestId("agents-count").textContent).toBe("Showing 60 of 60");
    expect(within(screen.getByTestId("agents-table")).queryByRole("button", { name: /load more/i })).toBeNull();
  });

  it("hands the free height to the table so the rows scroll inside the card, not the page", async () => {
    installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    const card = screen.getByTestId("agents-table");
    expect(card.className).toContain("flex-1");
    expect(card.className).toContain("min-h-0");
    expect(screen.getByTestId("agents-action-bar").className).toContain("shrink-0");
    const viewport = card.querySelector('[data-slot="table-viewport"]');
    expect(viewport, "the kit's fullHeight viewport is gone").toBeTruthy();
    expect(viewport?.getAttribute("style")).toContain("height: 100%");
  });

  it("draws each row's avatar in its name cell, asked per slug of agent.view", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": windowFor,
        "agent.view": (raw: unknown) => {
          const slug = (raw as { slug: string }).slug;
          if (slug === "agent-01") return { slug, name: "Agent 01", avatarType: "minion", avatarColor: "yellow", avatarMood: "working" };
          if (slug === "agent-02") return { slug, name: "Agent 02", avatarType: "cat", avatarColor: "#334155", avatarMood: "" };
          return { slug, name: "Agent", avatarType: "", avatarColor: "", avatarMood: "" };
        },
      },
    });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    const askedSlugs = () => daemon.calls.filter((c) => c.method === "agent.view").map((c) => (c.params as { slug: string }).slug);
    await waitFor(() => expect(new Set(askedSlugs()).size).toBe(50));
    expect(daemon.calls.filter((c) => c.method === "agent.view").every((c) => Object.keys(c.params as object).length === 1)).toBe(true);

    const minion = screen.getByTestId("agents-row-avatar-agent-01");
    await waitFor(() => expect(minion.querySelector('[class*="minion-avatar"]')).toBeTruthy());
    const dot = screen.getByTestId("agents-row-avatar-agent-02");
    await waitFor(() => expect(dot.querySelector('[style*="background"]')).toBeTruthy());
    expect(dot.textContent).toBe("");
    expect(screen.getByTestId("agents-row-avatar-agent-03").firstChild).toBeNull();
  });

  it("marks a row only when it is the exception: Yours, not a badge on every row", async () => {
    installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    expect(screen.queryByTestId("agents-cell-internal-agent-02")).toBeNull(); // shipped: no mark
    expect(screen.getByTestId("agents-cell-internal-agent-01").textContent).toBe("yours");
  });

  it("a search asks with q from page 1, rides the address, and is one ask for the term (CU3)", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs()).toContain("agent-01"));

    fireEvent.change(await searchBox(), { target: { value: "agent-0" } });
    await waitFor(() => {
      const ask = lastAsk(daemon);
      expect(ask.q).toBe("agent-0");
      expect(ask.page).toBe(1);
    });
    // The kit's bar debounces; the page adds no timer of its own.
    expect(asked(daemon).filter((a) => a.q === "agent-0").length).toBe(1);
  });

  it("opens on the search the address already carries", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount(liveCurrent, true, "/config/agents?q=agent-1");
    await waitFor(() => expect(asked(daemon).some((a) => a.q === "agent-1")).toBe(true));
    await waitFor(() => expect(shownSlugs().every((s) => s.includes("agent-1"))).toBe(true));
  });

  it("keeps the rows the person was reading when the daemon refuses, and says so verbatim", async () => {
    installFakeDaemon({
      responses: {
        "agent.rows": (raw: unknown) => {
          const p = (raw ?? {}) as AgentRowsParams;
          if (typeof p.q === "string" && p.q !== "") throw JSON.stringify({ code: -32602, message: REFUSAL });
          return windowFor(raw);
        },
      },
    });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    fireEvent.change(await searchBox(), { target: { value: "zz" } });
    const error = await screen.findByTestId("agents-read-error");
    expect(error.textContent).toContain(REFUSAL);
  });

  it("says an empty window with a term as a search that found nothing", async () => {
    installFakeDaemon({ responses: { "agent.rows": (raw: unknown) => ({ ...windowFor(raw), agents: [], total: 0 }) } });
    mount();
    await screen.findByTestId("agents-screen");
    await waitFor(() => expect(screen.getByTestId("agents-empty-none")).toBeTruthy());
    fireEvent.change(await searchBox(), { target: { value: "nothing-matches-this" } });
    await waitFor(() => expect(screen.getByTestId("agents-empty-search").textContent).toContain("nothing-matches-this"), { timeout: 2000 });
    expect(screen.queryByTestId("agents-empty-none")).toBeNull();
  });

  it("shows a first-party row's Edit and Delete disabled, with the daemon's own sentence (CU6)", async () => {
    installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs()).toContain("agent-02"));

    fireEvent.click(await screen.findByTestId("agents-row-menu-agent-02"));
    const edit = await screen.findByRole("menuitem", { name: /^Edit/ });
    expect(edit.getAttribute("aria-disabled")).toBe("true");
    expect(edit.textContent).toContain(refusalFor(INTERNAL_EDIT_REFUSAL, "agent-02"));
    const del = screen.getByRole("menuitem", { name: /^Delete/ });
    expect(del.getAttribute("aria-disabled")).toBe("true");
    expect(del.textContent).toContain(refusalFor(INTERNAL_DELETE_REFUSAL, "agent-02"));
    expect(screen.getByRole("menuitem", { name: /^Duplicate/ }).getAttribute("aria-disabled")).toBeNull();
  });

  it("refuses nothing on a row the daemon would let change", async () => {
    installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await waitFor(() => expect(shownSlugs()).toContain("agent-01"));
    fireEvent.click(await screen.findByTestId("agents-row-menu-agent-01"));
    const edit = await screen.findByRole("menuitem", { name: /^Edit/ });
    expect(edit.getAttribute("aria-disabled")).toBeNull();
    expect(screen.getByRole("menuitem", { name: /^Delete/ }).getAttribute("aria-disabled")).toBeNull();
  });

  it("refuses its own writes while the shell is not current, and says why in the shell's words", async () => {
    mount(liveCurrent, false);
    await screen.findByTestId("agents-screen");
    expect(screen.getByTestId("agents-not-current").textContent).toContain("There is nothing to configure or call");
    for (const id of ["agents-create", "agents-import", "agents-migrate"]) {
      expect((screen.getByTestId(id) as HTMLButtonElement).disabled).toBe(true);
    }
  });

  it("opens the panel a verb leads to, and sends nothing the daemon would act on", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor } });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-create"));
    expect(await screen.findByTestId("agent-panel-create")).toBeTruthy();
    // Only reads: the closed lists the form offers (`handlers.list`), never a write.
    expect(daemon.methods().filter((m) => m !== "agent.rows" && m !== "agent.view" && m !== "handlers.list")).toEqual([]);
  });
});

describe("AgentsScreen — the Activity view", () => {
  const LEDGER = [
    { id: 2, op: "migrate", slug: null, summary: "migrate derived 34 rows from plugin_agent", actorKind: "system", actorId: "seed-engine", occurredAt: "2026-11-04T08:15:00Z" },
    { id: 1, op: "edit", slug: "agent-02", summary: "edit agent-02 (1 content field)", actorKind: "user", actorId: "1000", occurredAt: "2026-11-03T08:15:00Z" },
  ];
  const ledger = (raw: unknown) => {
    const p = (raw ?? {}) as { page?: number; slug?: string };
    return { changes: LEDGER, page: p.page ?? 1, pageSize: 50, total: LEDGER.length, slug: p.slug ?? "" };
  };

  it("is the second tab of the list, rides the address, and asks the whole ledger (no slug)", async () => {
    const daemon = installFakeDaemon({ responses: { "agent.rows": windowFor, "agent.changes": ledger } });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    expect(screen.queryByTestId("agents-activity")).toBeNull();

    fireEvent.click(screen.getByRole("tab", { name: "Activity" }));
    await screen.findByTestId("agents-activity");
    await screen.findByTestId("agent-change-2-subject");
    expect(screen.getByTestId("agent-change-2-subject").textContent).toBe("Every agent");
    expect(daemon.calls.filter((c) => c.method === "agent.changes").every((c) => (c.params as { slug?: string }).slug === undefined)).toBe(true);
    // The list's own table is not behind it.
    expect(screen.queryByTestId("agents-table")).toBeNull();

    // An agent's name opens that agent's page.
    fireEvent.click(screen.getByTestId("agent-change-1-subject"));
    await screen.findByTestId("agent-detail");
  });
});

describe("AgentsScreen — one agent's page", () => {
  /** `agent.view`'s whole answer, named by the contract's 24 keys. */
  function viewRow(slug: string): Record<string, unknown> {
    return {
      id: 1,
      slug,
      name: `Viewed ${slug}`,
      description: "",
      functionality: "",
      prompt: "",
      skills: "",
      metadata: "",
      tools: "",
      handoff: "",
      policyAgent: "",
      temperature: 0,
      avatarType: "",
      avatarColor: "",
      avatarMood: "",
      version: "1.0.0",
      signed: false,
      internal: false,
      origin: "manual",
      certificate: "",
      signature: "",
      bodyHash: "",
      createdAt: "2026-11-01T08:00:00Z",
      updatedAt: "2026-11-01T09:00:00Z",
    };
  }

  const responses = () => ({ "agent.rows": windowFor, "agent.view": (raw: unknown) => viewRow((raw as { slug: string }).slug) });

  it("a row click opens that agent's page — an address, not a drawer — and Back returns to the list", async () => {
    const daemon = installFakeDaemon({ responses: responses() });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));

    fireEvent.click(screen.getByTestId("agents-cell-name-agent-05"));
    await screen.findByTestId("agent-detail");
    expect(daemon.calls.some((c) => c.method === "agent.view" && (c.params as { slug: string }).slug === "agent-05")).toBe(true);
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Viewed agent-05"));
    // The list is not behind it: one or the other.
    expect(screen.queryByTestId("agents-table")).toBeNull();

    fireEvent.click(screen.getByTestId("agent-back"));
    await screen.findByTestId("agents-table");
    expect(screen.queryByTestId("agent-detail")).toBeNull();
  });

  it("opens straight onto an agent when the address names one", async () => {
    installFakeDaemon({ responses: responses() });
    mount(liveCurrent, true, "/config/agents?agent=agent-07");
    await screen.findByTestId("agent-detail");
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Viewed agent-07"));
  });

  it("opening a row's menu is not a row click — the menu opens alone", async () => {
    installFakeDaemon({ responses: responses() });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    fireEvent.click(screen.getByTestId("agents-row-menu-agent-01"));
    await screen.findByRole("menuitem", { name: /^View/ });
    expect(screen.queryByTestId("agent-detail")).toBeNull();
  });

  it("choosing a menu verb is not a row click either — the verb's panel opens, not the page", async () => {
    installFakeDaemon({ responses: responses() });
    mount();
    await waitFor(() => expect(shownSlugs().length).toBe(50));
    fireEvent.click(screen.getByTestId("agents-row-menu-agent-01"));
    const menu = await screen.findByRole("menu");
    fireEvent.click(within(menu).getByRole("menuitem", { name: /^Duplicate/ }));
    await screen.findByTestId("agent-duplicate-panel");
    expect(screen.queryByTestId("agent-detail")).toBeNull();
  });

  it("opens to be read, and Edit turns the same sections into the form", async () => {
    installFakeDaemon({ responses: responses() });
    mount(liveCurrent, true, "/config/agents?agent=agent-07");
    await screen.findByTestId("agent-detail");
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Viewed agent-07"));
    expect(screen.queryByTestId("agent-panel-save")).toBeNull();

    fireEvent.click(screen.getByTestId("agent-edit"));
    await screen.findByTestId("agent-done");
    fireEvent.click(screen.getByTestId("agent-done"));
    await waitFor(() => expect(screen.queryByTestId("agent-done")).toBeNull());
  });

  it("a shipped agent's page offers no Edit and no Delete", async () => {
    installFakeDaemon({ responses: { ...responses(), "agent.view": (raw: unknown) => ({ ...viewRow((raw as { slug: string }).slug), internal: true }) } });
    mount(liveCurrent, true, "/config/agents?agent=agent-02");
    await screen.findByTestId("agent-detail");
    // Not possible on this row, so not offered: no Edit, no Delete, and no sentence about a refusal
    // that never happened.
    await screen.findByTestId("agent-duplicate");
    expect(screen.queryByTestId("agent-locked-note")).toBeNull();
    expect(screen.queryByTestId("agent-edit")).toBeNull();
    expect(screen.queryByTestId("agent-delete")).toBeNull();
    // What is still possible stays.
    expect(screen.getByTestId("agent-duplicate")).toBeTruthy();
    expect(screen.getByTestId("agent-export")).toBeTruthy();
  });

  it("offers no write at all and says so once, instead of a row of dead buttons", async () => {
    installFakeDaemon({ responses: responses() });
    mount(liveCurrent, false, "/config/agents?agent=agent-02");
    await screen.findByTestId("agent-detail");
    expect(await screen.findByTestId("agent-offline-note")).toBeTruthy();
    // The actions wait for the row, so wait for the one that stays before asserting the ones that went.
    expect(await screen.findByTestId("agent-export")).toBeTruthy();
    expect(screen.queryByTestId("agent-edit")).toBeNull();
    expect(screen.queryByTestId("agent-duplicate")).toBeNull();
    expect(screen.queryByTestId("agent-delete")).toBeNull();
  });
});
