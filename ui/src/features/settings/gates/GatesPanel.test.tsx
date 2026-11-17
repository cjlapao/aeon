import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { GatesSection } from "./GatesPanel";
import { APP_LOCALES } from "../../../lib/i18n";
import type { LiveState } from "../../../lib/live";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { gatesBaseResponses, listGates } from "../../../../test/gate-fixtures";
import { mountConfigPage } from "../../../../test/configPage";

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


function renderSection(responses: Record<string, unknown> = gatesBaseResponses, live: LiveState = currentLive): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <GatesSection controller={new LiveController()} live={live} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

async function pickComponent(): Promise<void> {
  await screen.findByTestId("gates-picker");
  fireEvent.change(screen.getByTestId("gates-picker"), { target: { value: "c-aeon" } });
}

describe("GatesSection (S57 T11)", () => {
  it("a chosen component opens its gates, and the tabs open their own doors", async () => {
    const daemon = renderSection();
    await pickComponent();
    await screen.findByTestId(`gate-row-${listGates[0]?.id}`);
    expect(daemon.methods()).toContain("verify.list");
    fireEvent.click(screen.getByRole("tab", { name: "Merge rule" }));
    await waitFor(() => expect(daemon.methods()).toContain("mergerule.get"));
    fireEvent.click(screen.getByRole("tab", { name: "Runs" }));
    await waitFor(() => expect(daemon.methods()).toContain("verify.runs"));
  });

  it("a confirmed try shows the answer honestly: ran-in path and the loops that were live", async () => {
    renderSection();
    await pickComponent();
    await screen.findByTestId(`gate-row-${listGates[0]?.id}`);
    fireEvent.click(screen.getByTestId(`gate-row-toggle-${listGates[0]?.id}`));
    fireEvent.click(await screen.findByTestId(`gate-try-${listGates[0]?.id}`));
    fireEvent.click(await screen.findByRole("button", { name: "Run it now" }));
    const panel = await screen.findByTestId("gates-try-result");
    expect(panel.textContent).toContain("/home/cjlapao/src/aeon"); // ranIn, said out loud
    expect(within(panel).getByTestId("gates-try-loops").textContent).toContain("fix-the-login"); // the named race
  });

  it("a stale connection refuses to write and says so — no door opens", async () => {
    const daemon = renderSection(gatesBaseResponses, staleLive);
    expect(await screen.findByTestId("gates-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("verify.list");
    expect(daemon.methods()).not.toContain("mergerule.get");
  });

  it("a daemon answering bare {} is survivable: an honest empty, not a crash", async () => {
    renderSection({});
    await pickComponent();
    await screen.findByTestId("gates-picker");
    // Whatever renders, no row was invented and the panel stands.
    expect(screen.queryByTestId(`gate-row-${listGates[0]?.id}`)).toBeNull();
    expect(screen.getByTestId("gates-section")).toBeTruthy();
  });

  it("the section moved into Work: /config/gates redirects to its Work tab", async () => {
    // S72 T2: pinned "mounted inside SettingsScreen"; the mount moved to the
    // Configuration page (CF3/CF8) and the assertion follows — recorded in
    // the slice Notes, step 4.
    mountConfigPage("gates", currentLive);
    expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/guardrails");
  });
});
