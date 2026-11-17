import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { EnvironmentsSection } from "./EnvironmentsSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { environmentsBaseResponses } from "../../../../test/environment-fixtures";
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

function renderSection(
  responses: Record<string, unknown> = environmentsBaseResponses,
  live: LiveState = currentLive,
  canWrite?: boolean,
): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <EnvironmentsSection controller={new LiveController()} live={live} {...(canWrite === undefined ? {} : { canWrite })} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("EnvironmentsSection (S59 T11)", () => {
  it("the four tabs are Environments · Secret references · Provisions · Effective, and each opens its own door", async () => {
    const daemon = renderSection();
    await screen.findByTestId("environments-section");
    for (const label of ["Environments", "Secret references", "Provisions", "Effective"]) {
      expect(screen.getByRole("tab", { name: label })).toBeTruthy();
    }
    await waitFor(() => expect(daemon.methods()).toContain("environment.list"));

    fireEvent.click(screen.getByRole("tab", { name: "Secret references" }));
    await waitFor(() => expect(daemon.methods()).toContain("secretref.list"));
    fireEvent.click(screen.getByRole("tab", { name: "Provisions" }));
    await waitFor(() => expect(daemon.methods()).toContain("environment.provisions"));
    fireEvent.click(screen.getByRole("tab", { name: "Effective" }));
    await waitFor(() => expect(daemon.methods()).toContain("environment.effective"));
  });

  it("the vocabulary is read once for the section and never retyped by a tab", async () => {
    const daemon = renderSection();
    await screen.findByTestId("environments-section");
    fireEvent.click(screen.getByRole("tab", { name: "Provisions" }));
    await screen.findByTestId("environments-provision-list");
    expect(daemon.calls.filter((call) => call.method === "environment.vocabulary")).toHaveLength(1);
    expect(daemon.calls.find((call) => call.method === "environment.vocabulary")?.params).toEqual({});
  });

  it("offline disables writes", async () => {
    const daemon = renderSection(environmentsBaseResponses, currentLive, false);
    await screen.findByTestId(`environment-row-env_7f2a9b8c6d5e`);
    // The rows are still there — a read is a read — but no door is offered.
    expect((screen.getByTestId("environments-create") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("environment-row-toggle-env_7f2a9b8c6d5e"));
    await screen.findByTestId("environment-detail-env_7f2a9b8c6d5e");
    expect(screen.queryByTestId(`environment-edit-env_7f2a9b8c6d5e`)).toBeNull();
    expect(screen.queryByTestId(`environment-bind-env_7f2a9b8c6d5e`)).toBeNull();
    expect(screen.queryByTestId(`environment-archive-env_7f2a9b8c6d5e`)).toBeNull();
    expect(daemon.methods()).not.toContain("environment.create");
    expect(daemon.methods()).not.toContain("environment.bind");
  });

  it("a stale connection sends nothing at all and says so", async () => {
    const daemon = renderSection(environmentsBaseResponses, staleLive);
    expect(await screen.findByTestId("environments-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("environment.list");
    expect(daemon.methods()).not.toContain("environment.vocabulary");
  });

  it("a daemon answering bare {} is survivable: an honest empty, not a crash", async () => {
    renderSection({});
    await screen.findByTestId("environments-section");
    expect(screen.getByTestId("environments-list-empty")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Provisions" }));
    expect(await screen.findByTestId("environments-provisions-empty")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Secret references" }));
    expect(await screen.findByTestId("environments-secret-empty")).toBeTruthy();
  });

  it("the section moved into Work: /config/environments redirects to its Work tab", async () => {
    // S72 T2: pinned "mounted inside SettingsScreen, beside the sections it
    // joined"; the mount moved to the Configuration page (CF3/CF8) and the
    // assertion follows — recorded in the slice Notes, step 4.
    installFakeDaemon({
      responses: {
        ...environmentsBaseResponses,
      },
    });
    mountConfigPage("environments", currentLive);
    expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/guardrails");
  });
});
