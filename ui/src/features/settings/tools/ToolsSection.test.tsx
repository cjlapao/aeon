import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ToolsSection } from "./ToolsSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";

/**
 * The Tools/Surface section (S47 T10). The daemon is the mock; the words it sends
 * here — capabilities, families, states, doors, summaries, reasons — are TEST data
 * on purpose, because the section's own sources are forbidden to hold them
 * (`toolsRpc.guard.test.ts`), and a fixture that repeats a source constant proves
 * the source was consulted.
 */

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

// The real `stale` shape from `lib/live.ts` — a connection the controller has
// lost but whose last answer it still holds. The state name is the daemon
// contract's, not a word this test may invent.
const staleLive = {
  state: "stale",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  since: 1_730_000_000_000,
  cause: "the daemon closed the socket",
  retry: 2,
} as const satisfies LiveState;

function toolRow(overrides: Record<string, unknown> = {}) {
  return {
    name: "read",
    family: "workspace",
    source: "system",
    description: "Reads a file under the workspace root.",
    schema: { type: "object" },
    schemaHash: "sha256:aaa",
    rerunClass: "safe",
    capabilities: ["workspace_read"],
    governed: ["tool_access"],
    conditional: false,
    available: true,
    classified: true,
    version: 3,
    origin: "builtin",
    rowCurrent: true,
    ...overrides,
  };
}

const listAnswer = {
  tools: [
    toolRow(),
    toolRow({
      name: "bash",
      family: "process",
      rerunClass: "unsafe",
      capabilities: ["spawn_process"],
      governed: ["tool_access", "command_access"],
      aliases: ["run"],
    }),
    toolRow({
      name: "browser_navigate",
      family: "browser",
      conditional: true,
      available: false,
      capabilities: [],
    }),
    toolRow({
      name: "acme.search",
      family: "acme",
      source: "mcp",
      server: "acme",
      tier: "private",
      classified: false,
      rerunClass: "",
      capabilities: [],
      origin: "declared",
      rowCurrent: false,
    }),
  ],
};

const vocabAnswer = {
  capabilities: [
    { name: "workspace_read", meaning: "Reads bytes under the step's workspace root through the one resolver." },
    { name: "spawn_process", meaning: "Starts a process, and only behind a policy-issued allow token." },
  ],
  families: ["workspace", "process", "browser", "acme"],
  sources: ["system", "mcp"],
  rerunClasses: ["safe", "detectable", "unsafe"],
};

const statusCurrent = {
  counts: { conditionalUnwired: 1, mcp: 1, system: 3 },
  drift: [],
  seed: { appliedVersion: 7, availableVersion: 7, set: "tools.core", state: "applied" },
};

const statusDrifted = {
  ...statusCurrent,
  seed: { appliedVersion: 7, availableVersion: 8, set: "tools.core", state: "partial" },
  drift: [
    { name: "bash", state: "differs", fields: ["capabilities", "rerun_class"] },
    { name: "gone_tool", state: "missing_row" },
  ],
};

const getAnswer = {
  tool: toolRow({ name: "bash", family: "process", rerunClass: "unsafe", capabilities: ["spawn_process"], governed: ["tool_access", "command_access"], aliases: ["run"], twin: "bash.run", schema: { type: "object", properties: { command: { type: "string" } }, required: ["command"] } }),
  agents: [
    { agent: "atlas", tool: "bash", state: "granted_denied", dispatchable: true, reason: "refused: no-shell-in-review · lane-type:review", granted_by: ["handler"] },
    { agent: "sentry", tool: "bash", state: "not_granted", dispatchable: true },
    { agent: "cursitor", tool: "bash", state: "not_granted", dispatchable: false, reason: "custom agents are not dispatchable from the app" },
  ],
  policies: [
    { id: "tc-deny-bash-atlas", action: "deny", source: "operator", origin: "agent", target: "agent:atlas", toolName: "bash", describe: "no shell in a review lane", expired: false, link: { kind: "policy", id: "tc-deny-bash-atlas" } },
  ],
};

const byAgentAnswer = {
  agent: "atlas",
  dispatchable: true,
  tools: [
    { agent: "atlas", tool: "bash", state: "granted_denied", dispatchable: true, reason: "refused: no-shell-in-review · lane-type:review", granted_by: ["handler"] },
    { agent: "atlas", tool: "read", state: "granted_allowed", dispatchable: true, granted_by: ["default"] },
    { agent: "atlas", tool: "edit", state: "granted_no_rule", dispatchable: true, reason: "no allow matched; the empty set denies" },
  ],
};

const surfaceAnswer = {
  counts: { byDoor: { socket: 2, public: 1, narrow: 1, agentService: 0 }, byKind: { read: 1, write: 0, control: 1, diagnostic: 0 }, total: 2, unfiltered: 272 },
  methods: [
    {
      method: "loop.show",
      kind: "read",
      summary: "One loop as the store holds it, with the ladder beneath it.",
      humanDoorOnly: false,
      debug: false,
      requiresInitialize: true,
      doors: { socket: true, public: true, narrow: "aeon_loop_show", agentService: false },
    },
    {
      method: "loop.cancel",
      kind: "control",
      summary: "Ends a loop for good, and runs nothing on the way out.",
      humanDoorOnly: true,
      debug: false,
      requiresInitialize: true,
      doors: { socket: true, public: false, narrow: "unknown", agentService: false },
    },
  ],
};

const surfaceGetAnswer = {
  entry: {
    method: "loop.cancel",
    kind: "control",
    summary: "Ends a loop for good, and runs nothing on the way out.",
    humanDoorOnly: true,
    debug: false,
    requiresInitialize: true,
    doors: { socket: true, public: false, narrow: "unknown", agentService: false, toolTwin: "loop_control_state" },
  },
};

const baseResponses: Record<string, unknown> = {
  "agent.list": { agents: [{ slug: "atlas", name: "atlas", provenance: "first-party", state: "active" }] },
  "tools.list": listAnswer,
  "tools.status": statusCurrent,
  "tools.vocabulary": vocabAnswer,
  "tools.get": getAnswer,
  "tools.byAgent": byAgentAnswer,
  "surface.list": surfaceAnswer,
  "surface.get": surfaceGetAnswer,
};

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

function renderSection(responses: Record<string, unknown> = {}, live: LiveState = currentLive): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { ...baseResponses, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <ToolsSection controller={new LiveController()} live={live} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

async function openTab(label: RegExp): Promise<void> {
  fireEvent.click(await screen.findByRole("tab", { name: label }));
}

// --- the Tools tab ---------------------------------------------------------------

describe("ToolsSection — Tools tab", () => {
  it("renders one row per tool with the daemon's own words", async () => {
    renderSection();
    const row = (await screen.findByTestId("tool-row-bash")).closest("tr") as HTMLElement;
    expect(within(row).getByText("bash")).toBeTruthy();
    expect(within(row).getByText("process")).toBeTruthy();
    expect(within(row).getByText("system")).toBeTruthy();
    // The row stays quiet: capabilities and the re-run class live in the panel.
    expect(within(row).queryByTestId("tool-caps-bash")).toBeNull();
    expect(within(row).getByText(/also run/)).toBeTruthy();
    fireEvent.click(within(row).getByTestId("tool-row-bash"));
    const drawer = await screen.findByTestId("tools-drawer");
    expect(within(drawer).getByTestId("tools-drawer-cap-spawn_process").textContent).toContain("spawn_process");
    expect(within(drawer).getByTestId("tools-drawer-rerun").textContent).toBe("unsafe");
  });

  it("shows a conditional tool that cannot run as UNWIRED, not absent", async () => {
    renderSection();
    const row = (await screen.findByTestId("tool-row-browser_navigate")).closest("tr") as HTMLElement;
    expect(within(row).getByTestId("tool-unwired-browser_navigate").textContent).toBe("UNWIRED");
  });

  it("shows an unclassified declared MCP tool as UNCLASSIFIED", async () => {
    renderSection();
    const row = (await screen.findByTestId("tool-row-acme.search")).closest("tr") as HTMLElement;
    expect(within(row).getByTestId("tool-unclassified-acme.search").textContent).toBe("UNCLASSIFIED");
    expect(within(row).getByTestId("tool-source-acme.search").textContent).toBe("mcp");
  });

  it("shows a row the catalog disagrees with as stale", async () => {
    renderSection();
    const row = (await screen.findByTestId("tool-row-acme.search")).closest("tr") as HTMLElement;
    expect(within(row).getByTestId("tool-stale-acme.search").textContent).toBe("catalog entry stale");
  });

  it("sends every chosen filter as a param and does not filter the answer itself", async () => {
    const daemon = renderSection();
    await screen.findByTestId("tools-list");
    fireEvent.change(screen.getByLabelText("Family"), { target: { value: "process" } });
    fireEvent.change(screen.getByLabelText("Search"), { target: { value: "shell" } });
    await waitFor(() => {
      const params = daemon.paramsOf("tools.list") as Record<string, unknown>;
      expect(params).toMatchObject({ family: "process", q: "shell" });
    });
    // The rows are what the daemon answered, unpruned: a client that filtered
    // here would be a client entitled to disagree about what the catalog holds.
    expect(document.querySelectorAll('[data-testid^="tool-row-"]').length).toBe(4);
  });

  it("builds its filter selects from the vocabulary the daemon sent", async () => {
    renderSection();
    const family = (await screen.findByLabelText("Family")) as HTMLSelectElement;
    expect(Array.from(family.options).map((o) => o.value)).toEqual(["", "workspace", "process", "browser", "acme"]);
    const capability = (await screen.findByLabelText("Capability")) as HTMLSelectElement;
    expect(Array.from(capability.options).map((o) => o.value)).toEqual(["", "workspace_read", "spawn_process"]);
  });

  it("offers no filter option before the vocabulary door has answered", async () => {
    renderSection({ "tools.vocabulary": { capabilities: [], families: [], sources: [], rerunClasses: [] } });
    const family = (await screen.findByLabelText("Family")) as HTMLSelectElement;
    expect(family.disabled).toBe(true);
    expect(Array.from(family.options).map((o) => o.value)).toEqual([""]);
  });

  it("quotes the daemon's own sentence when a filter matched nothing", async () => {
    renderSection({ "tools.list": { tools: [], statement: "no methods match this filter." } });
    const empty = await screen.findByTestId("tools-empty");
    expect(within(empty).getByTestId("tools-empty-statement").textContent).toBe("no methods match this filter.");
  });

  it("says the catalog is current, with the version, when nothing drifts", async () => {
    renderSection();
    const line = await screen.findByTestId("tools-drift-current");
    expect(line.textContent).toContain("catalog current");
    expect(line.textContent).toContain("7");
  });

  it("lists every drift line with its state and the field names the daemon reported", async () => {
    renderSection({ "tools.status": statusDrifted });
    const banner = await screen.findByTestId("tools-drift");
    expect(within(banner).getByTestId("tools-drift-row-bash").textContent).toContain("differs");
    expect(within(banner).getByTestId("tools-drift-fields-bash").textContent).toContain("capabilities");
    expect(within(banner).getByTestId("tools-drift-row-bash").textContent).toContain("without a catalog version bump");
    expect(within(banner).getByTestId("tools-drift-row-gone_tool").textContent).toContain("no catalog row at all");
  });
});

// --- the drawer ------------------------------------------------------------------

describe("ToolsSection — the tool drawer", () => {
  it("opens on a row click with the schema as read-only code and the capability meanings the daemon sent", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("tool-row-bash"));
    const drawer = await screen.findByTestId("tools-drawer");
    expect(within(drawer).getByTestId("tools-drawer-description").textContent).toBe("Reads a file under the workspace root.");
    const schema = within(drawer).getByTestId("tools-drawer-schema").textContent ?? "";
    expect(schema).toContain('"command"');
    expect(schema).toContain('\n  "required"');
    expect(drawer.querySelector("input, textarea, select")).toBeNull();
    expect(within(drawer).getByTestId("tools-drawer-cap-spawn_process").textContent).toContain("Starts a process, and only behind a policy-issued allow token.");
  });

  it("shows the per-agent matrix with the reasons verbatim and the grant origins as chips", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("tool-row-bash"));
    const row = await screen.findByTestId("tools-matrix-row-atlas");
    expect(within(row).getByTestId("tools-matrix-state-atlas").textContent).toBe("granted_denied");
    expect(within(row).getByTestId("tools-matrix-reason-atlas").textContent).toBe("refused: no-shell-in-review · lane-type:review");
    expect(within(row).getByTestId("tools-matrix-granted-by-atlas").textContent).toContain("handler");
  });

  it("shows a non-dispatchable agent as such, with no grant state beside it", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("tool-row-bash"));
    const row = await screen.findByTestId("tools-matrix-row-cursitor");
    expect(within(row).getByTestId("tools-matrix-undispatchable-cursitor").textContent).toContain("not dispatchable");
    expect(within(row).queryByTestId("tools-matrix-state-cursitor")).toBeNull();
  });

  it("links the policy row to where rules are authored, and links an MCP tool to its server", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("tool-row-bash"));
    const link = (await screen.findByTestId("tools-drawer-policy-link-tc-deny-bash-atlas")) as HTMLAnchorElement;
    expect(link.getAttribute("href")).toContain("/settings?policy=tc-deny-bash-atlas");
    expect(within(screen.getByTestId("tools-drawer-policy-tc-deny-bash-atlas")).getByText("no shell in a review lane")).toBeTruthy();
  });

  it("links an MCP tool to its server", async () => {
    renderSection({
      "tools.get": {
        ...getAnswer,
        tool: toolRow({ name: "acme.search", family: "acme", source: "mcp", server: "acme", tier: "private", classified: false, capabilities: [] }),
        mcp: { server: "acme", tier: "private", link: { kind: "mcp_server", id: "acme" } },
        policies: [],
      },
    });
    fireEvent.click(await screen.findByTestId("tool-row-acme.search"));
    const link = (await screen.findByTestId("tools-drawer-server-link")) as HTMLAnchorElement;
    expect(link.getAttribute("href")).toContain("/settings?server=acme");
  });

  it("states that it changes nothing", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("tool-row-bash"));
    expect((await screen.findByTestId("tools-drawer-readonly-note")).textContent).toContain("Read-only");
  });
});

// --- the By agent tab ------------------------------------------------------------

describe("ToolsSection — By agent tab", () => {
  it("asks tools.byAgent for the agent the picker chose", async () => {
    const daemon = renderSection();
    await openTab(/By agent/);
    fireEvent.click(await screen.findByTestId("agent-row-name-atlas"));
    await screen.findByTestId("tools-by-agent-groups");
    expect(daemon.paramsOf("tools.byAgent")).toEqual({ agent: "atlas" });
  });

  it("groups the rows by the states the daemon sent, with counts and the reasons verbatim", async () => {
    renderSection();
    await openTab(/By agent/);
    fireEvent.click(await screen.findByTestId("agent-row-name-atlas"));
    const denied = await screen.findByTestId("tools-by-agent-group-granted_denied");
    expect(within(denied).getByTestId("tools-by-agent-reason-bash").textContent).toBe("refused: no-shell-in-review · lane-type:review");
    expect(denied.textContent).toContain("1 tools");
    expect(await screen.findByTestId("tools-by-agent-group-granted_allowed")).toBeTruthy();
    expect((await screen.findByTestId("tools-by-agent-group-granted_no_rule")).textContent).toContain("no allow matched");
  });

  it("states a non-dispatchable agent as such and shows no table of grants", async () => {
    renderSection({ "tools.byAgent": { agent: "cursitor", dispatchable: false, tools: [] } });
    await openTab(/By agent/);
    fireEvent.click(await screen.findByTestId("agent-row-name-atlas"));
    const note = await screen.findByTestId("tools-by-agent-not-dispatchable");
    expect(note.textContent).toContain("cannot be dispatched");
    expect(screen.queryByTestId("tools-by-agent-groups")).toBeNull();
  });
});

// --- the Surface tab --------------------------------------------------------------

describe("ToolsSection — Surface tab", () => {
  it("renders each method with its kind pill, the daemon's summary and its door chips", async () => {
    renderSection();
    await openTab(/Surface/);
    const row = (await screen.findByTestId("surface-row-loop.show")).closest("tr") as HTMLElement;
    expect(within(row).getByTestId("surface-kind-loop.show").textContent).toBe("read");
    expect(within(row).getByTestId("surface-summary-loop.show").textContent).toBe("One loop as the store holds it, with the ladder beneath it.");
    expect(within(row).getByTestId("surface-door-loop.show-narrow").textContent).toContain("aeon_loop_show");
    expect(within(row).getByTestId("surface-door-loop.show-public").textContent).toBe("public");
  });

  it("renders an unknown narrow flag as unknown, never as not on narrow", async () => {
    renderSection();
    await openTab(/Surface/);
    const chip = await screen.findByTestId("surface-door-loop.cancel-narrow");
    expect(chip.textContent).toBe("narrow: unknown");
    expect(chip.textContent).not.toMatch(/not on narrow|none/);
  });

  it("carries the human-door-only and Debug badges from the entry", async () => {
    renderSection();
    await openTab(/Surface/);
    expect((await screen.findByTestId("surface-human-only-loop.cancel")).textContent).toBe("Human door only");
    expect(screen.queryByTestId("surface-debug-loop.cancel")).toBeNull();
  });

  it("keeps the counts beside the filtered answer, including the unfiltered total", async () => {
    renderSection();
    await openTab(/Surface/);
    expect((await screen.findByTestId("surface-count-total")).textContent).toContain("272");
    expect((await screen.findByTestId("surface-count-read")).textContent).toContain("1");
    expect((await screen.findByTestId("surface-count-socket")).textContent).toContain("2");
  });

  it("sends its filters as params to surface.list", async () => {
    const daemon = renderSection();
    await openTab(/Surface/);
    fireEvent.change(await screen.findByLabelText("Kind"), { target: { value: "control" } });
    fireEvent.change(screen.getByLabelText("Human door only"), { target: { value: "true" } });
    await waitFor(() => {
      expect(daemon.paramsOf("surface.list")).toMatchObject({ kind: "control", humanDoorOnly: true });
    });
  });

  it("opens a drawer with the surface.get entry, doors and all", async () => {
    renderSection();
    await openTab(/Surface/);
    fireEvent.click(await screen.findByTestId("surface-row-loop.cancel"));
    const drawer = await screen.findByTestId("surface-drawer");
    expect(within(drawer).getByTestId("surface-drawer-summary").textContent).toBe("Ends a loop for good, and runs nothing on the way out.");
    expect(within(drawer).getByTestId("surface-drawer-door-toolTwin").textContent).toContain("loop_control_state");
    expect(within(drawer).getByTestId("surface-drawer-door-narrow").textContent).toBe("narrow: unknown");
  });
});

// --- across the section ----------------------------------------------------------

describe("ToolsSection — connection and honesty", () => {
  it("keeps the last catalog on screen and says it is reconnecting", async () => {
    const daemon = installFakeDaemon({ responses: baseResponses });
    const view = render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <ToolsSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByTestId("tool-row-bash");
    view.rerender(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <ToolsSection controller={new LiveController()} live={staleLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(await screen.findByTestId("tools-reconnecting")).toBeTruthy();
    expect(screen.getByTestId("tool-row-bash")).toBeTruthy();
    expect(daemon.methods()).not.toContain("tools.update");
  });

  it("speaks only the catalog reads — no write method leaves this section", async () => {
    const daemon = renderSection();
    await openTab(/By agent/);
    fireEvent.click(await screen.findByTestId("agent-row-name-atlas"));
    await openTab(/Surface/);
    fireEvent.click(await screen.findByTestId("surface-row-loop.show"));
    // Settled before the assertion: a method that never left for the daemon
    // cannot be counted from a call that is still in flight.
    await screen.findByTestId("surface-drawer");
    const spoken = new Set(daemon.calls.map((c) => c.method));
    for (const method of spoken) {
      expect(["tools.list", "tools.get", "tools.byAgent", "tools.status", "tools.vocabulary", "surface.list", "surface.get", "agent.list"]).toContain(method);
    }
  });

  it("keeps nothing in browser storage", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    renderSection();
    await screen.findByTestId("tool-row-bash");
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
  });
});
