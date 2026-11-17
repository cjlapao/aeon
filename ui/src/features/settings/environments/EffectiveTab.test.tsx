import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { EffectiveTab } from "./EffectiveTab";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { environmentEffective, environmentsBaseResponses } from "../../../../test/environment-fixtures";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

/** A value the daemon could never answer, planted in the mocked answer. */
const SENTINEL = "sk-live-SENTINEL-must-never-reach-the-dom";

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

function renderTab(responses: Record<string, unknown> = environmentsBaseResponses): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <EffectiveTab
          controller={new LiveController()}
          live={currentLive}
          vocabulary={responses["environment.vocabulary"] as never}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("EffectiveTab (S59 T11)", () => {
  it("shows variable NAMES with their SOURCES, and a planted value never reaches the DOM", async () => {
    renderTab({
      ...environmentsBaseResponses,
      "environment.effective": {
        ...(environmentEffective as Record<string, unknown>),
        // A tampered answer that DOES carry values: the panel has nowhere to put
        // them, which is exactly the property this test exists for.
        variables: [
          { name: "PATH", source: "passthrough", value: SENTINEL },
          { name: "TERM", source: "set", value: SENTINEL },
          { name: "DEPLOY_TOKEN", source: "secret:deploy-token", value: SENTINEL },
        ],
        secretValue: SENTINEL,
      },
    });
    await screen.findByTestId("environments-effective-answer");

    expect(screen.getByTestId("environments-effective-variable-PATH").textContent).toContain("passthrough");
    expect(screen.getByTestId("environments-effective-variable-TERM").textContent).toContain("set");
    expect(screen.getByTestId("environments-effective-variable-DEPLOY_TOKEN").textContent).toContain("secret:deploy-token");
    // The names are there; no value anywhere on the screen.
    expect(document.body.textContent).toContain("DEPLOY_TOKEN");
    expect(document.body.textContent).not.toContain(SENTINEL);
  });

  it("says WHY this environment: the binding source the daemon named, and the honesty line verbatim", async () => {
    renderTab();
    await screen.findByTestId("environments-effective-answer");
    expect(screen.getByTestId("environments-effective-scope").textContent).toContain("workspace");
    expect(screen.getByTestId("environments-effective-honesty").textContent).toContain(
      (environmentEffective as { honesty: string }).honesty,
    );
    // The chips are the vocabulary's capabilities for the answered backend.
    expect(screen.getByTestId("environments-effective-chip-secrets-redacted").getAttribute("data-enforced")).toBe("true");
    expect(screen.getByTestId("environments-effective-chip-filesystem-isolation").getAttribute("data-enforced")).toBe("false");
  });

  it("the component picker travels as environment.effective's param, and the root is the daemon's", async () => {
    const daemon = renderTab();
    await screen.findByTestId("environments-effective-picker");
    fireEvent.change(screen.getByTestId("environments-effective-picker"), { target: { value: "cmp_42" } });
    await waitFor(() => expect(daemon.paramsOf("environment.effective")).toMatchObject({ componentId: "cmp_42" }));
    expect(daemon.methods()).toContain("component.show");
expect((await screen.findByTestId("environments-effective-root")).textContent).toContain("/home/cjlapao/code/acme/api");
  });

  it("the panel never sends attended — the daemon answers it (G14, S59 T12)", async () => {
    // The control that used to sit here let the person flip attendedness, which
    // is declared only at loop.start, the queue's append and the roadmap's
    // dispatch (internal/loop's TestQ4Recorded pins exactly three doors). A
    // preview must not become a fourth: the request carries componentId only,
    // and the note shown is the daemon's own answer, verbatim.
    const daemon = renderTab();
    await screen.findByTestId("environments-effective-picker");
    const params = daemon.paramsOf("environment.effective") as Record<string, unknown>;
    expect(Object.keys(params)).not.toContain("attended");
    expect(params).toMatchObject({ componentId: "" });
    expect(daemon.methods()).toContain("environment.effective");
    expect(screen.queryByTestId("environments-effective-attended")).toBeNull();
    expect(screen.getByTestId("environments-effective-attended-state").textContent).toContain("unattended");
  });
});
