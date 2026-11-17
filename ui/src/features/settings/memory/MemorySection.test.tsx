import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { notifications } from "../../../lib/notifications";
import { MemorySection } from "./MemorySection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import {
  MEMORY_BOARD_RETRACTED_ROW,
  MEMORY_VOCABULARY,
  memoryBaseResponses,
} from "../../../../test/memory-fixtures";

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
  // jsdom has no canvas: the kit's map draws on one, so give it a context that accepts every call.
  const noop = new Proxy({}, { get: (_t, key) => (key === "measureText" ? () => ({ width: 10 }) : key === "createRadialGradient" ? () => ({ addColorStop() {} }) : () => {}) });
  HTMLCanvasElement.prototype.getContext = (() => noop) as never;
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

function renderSection(
  responses: Record<string, unknown> = memoryBaseResponses(),
  live: LiveState = currentLive,
  canWrite?: boolean,
): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <MemorySection controller={new LiveController()} live={live} {...(canWrite === undefined ? {} : { canWrite })} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

/** The explorer's cards and the map's list both name a memory by its title; either selects it. */
const CGO = /CGO shim header/;
const RELAYED = /24h retry window/;
const RETRACTED = /2019 TLS stack/;

async function loadBoard(daemon: FakeDaemon): Promise<void> {
  await screen.findByTestId("memory-section");
  await screen.findAllByRole("button", { name: CGO });
  expect(daemon.calls.some((c) => c.method === "learnings.board")).toBe(true);
}

/** Select a memory the way a person does: click its card. Its detail and acts open beside the map. */
async function pick(title: RegExp): Promise<void> {
  fireEvent.click((await screen.findAllByRole("button", { name: title }))[0] as HTMLElement);
  await screen.findByTestId("memory-drawer");
}

/** Choose the first roster agent in the panel's tag picker: open it, click the first option. */
async function pickAgent(panel: HTMLElement): Promise<void> {
  const trigger = await waitFor(() => {
    const found = panel.querySelector('[data-testid="agent-tag-picker"] button[aria-haspopup="listbox"]:not([disabled])');
    if (found === null) throw new Error("the agent picker is not ready");
    return found as HTMLElement;
  });
  fireEvent.click(trigger);
  const option = await waitFor(() => {
    const found = document.querySelector('li[role="option"]');
    if (found === null) throw new Error("the roster is not in the list yet");
    return found as HTMLElement;
  });
  fireEvent.click(option);
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
}

describe("MemorySection (S52 T6)", () => {
  it("reconnecting asks nothing at all", async () => {
    const daemon = renderSection(memoryBaseResponses(), staleLive);
    expect(await screen.findByTestId("memory-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("learnings.board");
    expect(daemon.methods()).not.toContain("learnings.vocabulary");
  });

  it("the board asks the daemon, and untouched dials never ride the wire", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    const first = daemon.calls.filter((c) => c.method === "learnings.board")[0];
    // An empty filter set is the daemon's own default read — an empty
    // params object, not seven empty strings, and never a retracted set.
    expect(first?.params).toEqual({});
  });

  it("the filters are PARAMS — the daemon filters, the panel never sweeps", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    fireEvent.change(screen.getByTestId("memory-filter-provenance"), { target: { value: "observed" } });
    fireEvent.change(screen.getByTestId("memory-filter-tag"), { target: { value: "cgo" } });
    await waitFor(() => {
      const last = daemon.paramsOf("learnings.board") as Record<string, unknown>;
      expect(last).toMatchObject({ provenance: "observed", tag: "cgo" });
    });
    // The rows on screen are whatever the fake daemon answered — the
    // panel NEVER filters them further (no client-side sweep to catch).
    expect(screen.getAllByRole("button", { name: CGO }).length).toBeGreaterThan(0);
  });

  it("include-retracted is a dial the PERSON turns, and it rides the params", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    expect(daemon.paramsOf("learnings.board")).not.toHaveProperty("includeRetracted");
    fireEvent.click(screen.getByTestId("memory-filter-include-retracted").querySelector("input") as HTMLInputElement);
    await waitFor(() =>
      expect(daemon.paramsOf("learnings.board")).toMatchObject({ includeRetracted: true }),
    );
  });

  it("the vocabulary is read once, and its words are the filter dials", async () => {
    const daemon = renderSection();
    await screen.findByTestId("memory-section");
    expect(daemon.calls.filter((c) => c.method === "learnings.vocabulary")).toHaveLength(1);
    const provenance = screen.getByTestId("memory-filter-provenance") as HTMLSelectElement;
    expect([...provenance.options].map((o) => o.value).filter((v) => v !== "")).toEqual(MEMORY_VOCABULARY.provenances);
    const scope = screen.getByTestId("memory-filter-scope") as HTMLSelectElement;
    expect([...scope.options].map((o) => o.value).filter((v) => v !== "")).toEqual(MEMORY_VOCABULARY.scopes);
  });

  it("row 11: its provenance word, its strength ×N, its tags, and its daemon sentence verbatim", async () => {
    renderSection();
    await pick(CGO);
    const drawer = screen.getByTestId("memory-drawer");
    expect(within(drawer).getByTestId("memory-drawer-provenance").textContent).toBe("observed");
    expect(drawer.textContent).toContain("×3");
    expect(within(drawer).getByText("cgo")).toBeTruthy();
    // The daemon's own sentence is the memory's summary — verbatim.
    expect(screen.getAllByText("observed (×3) — a loop saw it 3 times").length).toBeGreaterThan(0);
  });

  it("relayed is its own word and colour, and a retracted row shows its stamp", async () => {
    renderSection();
    await pick(RELAYED);
    const relayedPill = within(screen.getByTestId("memory-drawer")).getByTestId("memory-drawer-provenance");
    expect(relayedPill.textContent).toBe("relayed");
    const relayedClass = relayedPill.getAttribute("class");
    await pick(CGO);
    const observedPill = within(screen.getByTestId("memory-drawer")).getByTestId("memory-drawer-provenance");
    expect(observedPill.getAttribute("class")).not.toBe(relayedClass);

    await pick(RETRACTED);
    const stamp = await screen.findByTestId("memory-drawer-retraction");
    expect(stamp.textContent).toContain("2026-11-03T10:00:00Z");
    expect(stamp.textContent).toContain("user:1000");
  });

  it("retract is a two-click act that quotes the daemon's OWN law, byte for byte", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    await pick(CGO);
    fireEvent.click(screen.getByTestId("memory-ask-retract"));
    // The confirm shows the law and NOTHING has run yet:
    expect(screen.getByTestId("memory-confirm-law").textContent).toBe(MEMORY_VOCABULARY.retractLaw);
    expect(daemon.methods()).not.toContain("learnings.retract");
    fireEvent.change(screen.getByTestId("memory-confirm-reason"), { target: { value: "moved into BUILD.md" } });
    fireEvent.click(screen.getByTestId("memory-confirm-retract"));
    await waitFor(() => expect(daemon.methods()).toContain("learnings.retract"));
    // Only the id and the reason CAN ride — the actor is the peer's:
    const params = daemon.paramsOf("learnings.retract") as Record<string, unknown>;
    expect(Object.keys(params).sort()).toEqual(["id", "reason"]);
    expect(params.reason).toBe("moved into BUILD.md");
    // The answer's facts keep their own words; the board RE-READS the
    // daemon (never a locally patched copy):
    expect((await screen.findByTestId("memory-answer")).textContent).toContain("changed: true");
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "learnings.board").length).toBeGreaterThan(1));
  });

  it("declining the confirm sends NOTHING", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    await pick(CGO);
    fireEvent.click(screen.getByTestId("memory-ask-retract"));
    fireEvent.click(screen.getByTestId("memory-confirm-decline"));
    expect(daemon.methods()).not.toContain("learnings.retract");
  });

  it("a refusal is a verbatim toast, keeps the reason draft, and names nothing of its own", async () => {
    const push = vi.spyOn(notifications, "push");
    const sentence = "the reason must be between 1 and 256 bytes — this one is 0";
    const daemon = installFakeDaemon({
      responses: {
        ...memoryBaseResponses(),
        "learnings.retract": () => {
          throw JSON.stringify({ code: -31910, message: sentence });
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <MemorySection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await loadBoard(daemon);
    await pick(CGO);
    fireEvent.click(screen.getByTestId("memory-ask-retract"));
    fireEvent.change(screen.getByTestId("memory-confirm-reason"), { target: { value: "my kept draft" } });
    fireEvent.click(screen.getByTestId("memory-confirm-retract"));
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain(sentence); // VERBATIM, as a toast
    expect(screen.queryByTestId("memory-act-refusal")).toBeNull();
    expect((screen.getByTestId("memory-confirm-reason") as HTMLInputElement).value).toBe("my kept draft"); // draft SURVIVES
  });

  it("a replayed retraction says so plainly — changed:false is a fact, not a failure", async () => {
    const daemon = installFakeDaemon({
      responses: {
        ...memoryBaseResponses(),
        "learnings.retract": { changed: false, learning: MEMORY_BOARD_RETRACTED_ROW },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <MemorySection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await loadBoard(daemon);
    await pick(CGO);
    fireEvent.click(screen.getByTestId("memory-ask-retract"));
    fireEvent.change(screen.getByTestId("memory-confirm-reason"), { target: { value: "again" } });
    fireEvent.click(screen.getByTestId("memory-confirm-retract"));
    expect(await screen.findByTestId("memory-act-replay")).toBeTruthy();
  });

  it("restore sends only the id, through the same confirm shape", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    await pick(CGO);
    fireEvent.click(screen.getByTestId("memory-ask-restore"));
    expect(screen.getByTestId("memory-confirm-law").textContent).toBe(MEMORY_VOCABULARY.retractLaw);
    fireEvent.click(screen.getByTestId("memory-confirm-restore"));
    await waitFor(() => expect(daemon.methods()).toContain("learnings.restore"));
    expect(Object.keys(daemon.paramsOf("learnings.restore") as Record<string, unknown>)).toEqual(["id"]);
  });

  it("without write permission the acts are locked — reads still show", async () => {
    const daemon = renderSection(memoryBaseResponses(), currentLive, false);
    await loadBoard(daemon);
    await pick(CGO);
    expect(await screen.findByTestId("memory-acts-locked")).toBeTruthy();
    // The acts are not rendered at all — there is nothing to click into a verb.
    expect(daemon.methods()).not.toContain("learnings.retract");
    expect(daemon.methods()).not.toContain("learnings.restore");
  });

  it("the drawer shows the audit career with the daemon's own summaries", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    await pick(CGO);
    await screen.findByTestId("memory-history-1");
    expect(screen.getByTestId("memory-history-1").textContent).toContain("added told learning scoped to project//srv/repo");
    expect(screen.getByTestId("memory-history-1").textContent).toContain("user:1000");
  });

  it("what would this agent see: the readable set with the WHY, and the M1 note", async () => {
    const daemon = renderSection();
    await screen.findByTestId("memory-section");
    fireEvent.click(await screen.findByRole("tab", { name: "What an agent sees" }));
    const panel = await screen.findByTestId("memory-agent-panel");
    await pickAgent(panel);
    await waitFor(() => expect(daemon.methods()).toContain("learnings.forAgent"));
    expect(daemon.paramsOf("learnings.forAgent")).toEqual({ agent: "golang-engineer" });
    const hit = await screen.findByTestId("memory-agent-hit-11");
    expect(within(hit.closest("tr") as HTMLElement).getByTestId("memory-agent-hit-11-provenance").textContent).toBe("observed");
    expect(screen.getByTestId("memory-agent-why-11").textContent).toContain("no query — scope order");
  });

  it("an unknown agent slug answers its OWN refusal, naming the slug", async () => {
    const sentence = 'learnings.forAgent refuses agent "ghost" — no agent row answers to that slug (S52 LM1\'s authority)';
    const daemon = installFakeDaemon({
      responses: {
        ...memoryBaseResponses(),
        "agent.list": { agents: [{ id: 9, name: "ghost", slug: "ghost", provenance: "imported", enabled: true }] },
        "learnings.forAgent": () => {
          throw JSON.stringify({ code: -31910, message: sentence });
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <MemorySection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByTestId("memory-section");
    fireEvent.click(await screen.findByRole("tab", { name: "What an agent sees" }));
    const panel = await screen.findByTestId("memory-agent-panel");
    await pickAgent(panel);
    const refusal = await screen.findByTestId("memory-agent-refusal");
    expect(refusal.textContent).toContain(sentence); // VERBATIM, slug named
    void daemon;
  });

  it("the desk shows the agent's pending learning_retract proposal — decided by a person", async () => {
    renderSection();
    await screen.findByTestId("memory-section");
    fireEvent.click(await screen.findByRole("tab", { name: "Proposals" }));
    const card = await screen.findByTestId("proposal-card-41");
    expect(within(card).getByText("learning_retract")).toBeTruthy();
    expect(screen.getByTestId("proposal-reason-41").textContent).toContain("no longer exists");
  });

  it("when the daemon placed the learnings by meaning, its own sentence says what the positions are", async () => {
    const basis = "positions are a 2-D principal-component projection of the stored 4-dimensional vectors (deterministic)";
    const daemon = renderSection({
      ...memoryBaseResponses(),
      "learnings.map": { projected: true, total: 3, embedded: 3, omitted: 0, canEmbed: true, dim: 4, basis, points: [{ id: 11, x: 1, y: 2 }], links: [] },
    });
    await loadBoard(daemon);
    expect((await screen.findByTestId("memory-map-note")).textContent).toBe(basis); // verbatim, not ours
    expect(daemon.paramsOf("learnings.map")).toEqual({}); // the board's own filters, none touched
    expect(screen.queryByTestId("memory-map-pending")).toBeNull();
  });

  it("memories without a vector are counted, and the way to place them is said (no embed door from here)", async () => {
    const daemon = renderSection({
      ...memoryBaseResponses(),
      "learnings.map": { projected: false, total: 3, embedded: 1, omitted: 0, canEmbed: true, dim: 0, basis: "only 1 of 3 learnings have a vector", points: [], links: [] },
    });
    await loadBoard(daemon);
    expect((await screen.findByTestId("memory-map-basis")).textContent).toContain("only 1 of 3");
    expect(screen.getByTestId("memory-map-pending").textContent).toContain("aeon learnings embed");
    expect(daemon.methods()).not.toContain("learnings.embed");
  });
});
