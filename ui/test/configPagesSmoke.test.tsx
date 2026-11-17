import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon, backupStatus } from "./helpers";
import { mountConfigPage, configLiveCurrent } from "./configPage";
import { gatesBaseResponses } from "./gate-fixtures";
import { environmentsBaseResponses } from "./environment-fixtures";
import { automationBaseResponses } from "./automation-fixtures";
import { triageBaseResponses } from "./triage-fixtures";

// S72 CF10 (T2 step 5): one smoke render per migrated Configuration page —
// the page mounts through the real registry, the real ConfigScreen and the
// fake daemon, and shows its section's root. Each section's own suite owns
// its behaviour; this proves the MOVING moved. (The Agents page's smoke
// lives with its own extraction test — T3.)

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  });
});
afterEach(() => {
  cleanup();
  resetFakeDaemon();
  vi.unstubAllGlobals();
});

const base = {
  "backup.status": backupStatus(),
  "seed.status": { sets: [] },
  "settings.list": { settings: [] },
  "policy.list": { policies: [] },
  "agent.list": { agents: [] },
};

const MOVED = { gates: 1, environments: 1, automation: 1, triage: 1, memory: 1 } as Record<string, number>;
const pages: { id: string; assert: () => Promise<void> | void }[] = [
  { id: "daemon-settings", assert: async () => { await screen.findByTestId("daemon-settings-panel"); } },
  { id: "backups", assert: async () => { await screen.findByTestId("backups-section"); } },
  // SystemRecordsPanel carries no root test id; its i18n subtitle is the handle.
  { id: "system-records", assert: async () => { await screen.findByText(/What the app ships as rows/); } },
  { id: "policy", assert: async () => { await screen.findByTestId("policy-section"); } },
  { id: "mcp", assert: async () => { await screen.findByTestId("mcp-section"); } },
  { id: "tools", assert: async () => { await screen.findByTestId("tools-section"); } },
  { id: "gates", assert: async () => { expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/guardrails"); } },
  { id: "environments", assert: async () => { expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/guardrails"); } },
  { id: "automation", assert: async () => { expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/automation"); } },
  { id: "variables", assert: async () => { await screen.findByTestId("variables-section"); } },
  { id: "triage", assert: async () => { expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/guardrails"); } },
  { id: "memory", assert: async () => { expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/knowledge"); } },
  { id: "continuous", assert: async () => { await screen.findByTestId("continuous-section"); } },
  { id: "platform-status", assert: async () => { await screen.findByTestId("system-section"); } },
];

describe("Configuration pages smoke (S72 T2/CF10)", () => {
  it.each(pages)("/config/$id mounts its section, or sends you to where it moved in Work", async ({ id, assert }) => {
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon-test.sock" },
      responses: {
        ...base,
        ...gatesBaseResponses,
        ...environmentsBaseResponses,
        ...automationBaseResponses(),
        ...triageBaseResponses(),
      },
    });
    mountConfigPage(id, configLiveCurrent);
    await assert();
    if (id in MOVED) return; // it left Configuration: the redirect is the whole contract
    // The frame holds the page (CF6, bare): the section lives inside it.
    const frame = screen.getByTestId("config-page-frame");
    expect(frame.getAttribute("data-page")).toBe(id);
    expect(frame.querySelector('[data-testid$="-section"], [data-testid="daemon-settings-panel"], [data-testid="tab-boundary"]')
      ?? /What the app ships/.test(frame.textContent ?? "")).toBeTruthy();
  });

  it("a page with no connection shows the shell's not-connected state, never a section", async () => {
    resetFakeDaemon();
    installFakeDaemon({});
    mountConfigPage("policy", { state: "connecting", endpoint: "/tmp/aeon-test.sock" });
    expect(await screen.findByTestId("tab-needs-connection")).toBeTruthy();
    expect(screen.queryByTestId("policy-section")).toBeNull();
  });
});
