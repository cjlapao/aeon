import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { PolicyForm } from "./PolicyForm";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon } from "../../../../test/helpers";
import type { Vocabulary } from "./policyModel";

// S74: the guided grant — who, what, what happens — builds the same draft the
// full form does, from the daemon's own descriptors.

const vocabulary: Vocabulary = {
  types: ["tool_access", "command_access"],
  targetKinds: ["all-agents", "agent", "loop"],
  sources: ["operator"],
  actions: ["allow", "deny", "ask"],
  authorship: [
    { source: "operator", action: "allow", allowed: true },
    { source: "operator", action: "deny", allowed: true },
    { source: "operator", action: "ask", allowed: true },
  ],
  specs: [
    {
      type: "tool_access",
      fields: [
        { name: "toolName", kind: "string", required: true, description: "the tool", picker: "tool-address" },
        { name: "state", kind: "enum", required: true, enumValues: ["allow", "deny", "ask"], description: "verdict" },
      ],
    },
  ],
};

const live = { state: "current", endpoint: "/run/user/1000/aeon/rpc.sock", daemonVersion: "9.9.9", protocolVersion: 7, generation: 1 } as const satisfies LiveState;

const addOk = {
  policy: { id: "tool_access.agent.brainstormer", describe: "deny tools *.*.* for agent brainstormer", action: "deny", source: "operator", origin: "user", target: "agent:brainstormer", type: "tool_access", spec: {}, notAfter: null, createdAt: "2026-01-01T00:00:00Z", authored: true },
  before: { answers: [], target: "agent:brainstormer" },
  after: { answers: [], target: "agent:brainstormer" },
  warnings: [],
};

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {},
    addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); resetFakeDaemon(); });

/** Choose the first agent in the tag picker: open it, click the first option. */
async function pickFirstAgent(): Promise<void> {
  const trigger = await waitFor(() => {
    const found = document.querySelector('[data-testid="agent-tag-picker"] button[aria-haspopup="listbox"]:not([disabled])');
    if (found === null) throw new Error("the agent picker is not ready");
    return found as HTMLElement;
  });
  fireEvent.click(trigger);
  // The roster loads after the trigger exists: wait for a real option, not the loading line.
  const option = await waitFor(() => {
    // The picker's rows are list items; a native <option> elsewhere on the page is not one of them.
    const found = document.querySelector('li[role="option"]');
    if (found === null) throw new Error("the roster is not in the list yet");
    return found as HTMLElement;
  });
  fireEvent.click(option);
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
}

describe("PolicyForm quick", () => {
  it("grants by choosing an agent, a tool preset and a decision — and sends the derived policy", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.list": { agents: [{ name: "brainstormer", provenance: "first_party", state: "running" }] },
        "tools.list": { tools: [], total: 0 },
        "tools.matchPattern": (p: unknown) => ({ pattern: (p as { pattern: string }).pattern, matches: ["workspace.glob.read"], count: 1 }),
        "policy.add": addOk,
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <PolicyForm quick vocabulary={vocabulary} canWrite controller={new LiveController()} live={live} busy={false} onCancel={() => {}} onAdded={() => {}} />
        </MemoryRouter>
      </I18nProvider>,
    );

    await screen.findByTestId("quick-grant");
    await pickFirstAgent();
    fireEvent.click(screen.getByTestId("quick-preset-*.*.*"));
    fireEvent.click(screen.getByText("deny"));
    expect((await screen.findByTestId("quick-sentence")).textContent).toContain("brainstormer");

    await waitFor(() => expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => expect(daemon.methods()).toContain("policy.add"));
    expect(daemon.paramsOf("policy.add")).toMatchObject({
      type: "tool_access",
      target: { kind: "agent", id: "brainstormer" },
      action: "deny",
      spec: { toolName: "*.*.*", state: "deny" },
    });
  });

  it("keeps the full form one click away", async () => {
    installFakeDaemon({ responses: { "agent.list": { agents: [] }, "tools.list": { tools: [], total: 0 } } });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <PolicyForm quick vocabulary={vocabulary} canWrite controller={new LiveController()} live={live} busy={false} onCancel={() => {}} onAdded={() => {}} />
        </MemoryRouter>
      </I18nProvider>,
    );
    fireEvent.click(await screen.findByTestId("quick-advanced"));
    expect(screen.getByLabelText("Type")).toBeTruthy();
  });
});
