import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configure, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { notifications } from "../../../lib/notifications";
import { SecretRefsTab } from "./SecretRefsTab";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { environmentsBaseResponses, refusedFixture } from "../../../../test/environment-fixtures";

// The side panels mount after a frame; under a loaded full-suite run the default waits are tight.
configure({ asyncUtilTimeout: 4000 });
vi.setConfig({ testTimeout: 15000 });

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

const REFUSAL_SENTENCE =
  "refused: secret-unresolvable · the daemon's environment has no DEPLOY_TOKEN to read · do: set it in the daemon's environment, or point the reference at a key file or a stored credential";

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

/** The page header owns the create verb; the tab only receives whether the form is open. */
function Harness({ canWrite = true, vocabulary }: { canWrite?: boolean; vocabulary: never }) {
  const [creating, setCreating] = useState(false);
  return (
    <>
      {canWrite && <button type="button" data-testid="environments-secret-create" onClick={() => setCreating(true)}>new</button>}
      <SecretRefsTab controller={new LiveController()} live={currentLive} canWrite={canWrite} vocabulary={vocabulary} creating={creating} setCreating={setCreating} />
    </>
  );
}

function renderTab(responses: Record<string, unknown> = environmentsBaseResponses): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <Harness vocabulary={responses["environment.vocabulary"] as never} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("SecretRefsTab (S59 T11)", () => {
  it("a check answers two booleans and the refusal sentence — never a value, never its length", async () => {
    renderTab();
    await screen.findByTestId("secretref-row-deploy-token");
    fireEvent.click(screen.getByTestId("secretref-toggle-deploy-token"));
    fireEvent.click(await screen.findByTestId("secretref-check-deploy-token"));

    const result = await screen.findByTestId("secretref-check-result-deploy-token");
    expect(result.textContent).toContain("Resolvable: false");
    expect(result.textContent).toContain("Source present: false");
    // The daemon's sentence, verbatim.
    expect(screen.getByTestId("secretref-check-refusal-deploy-token").textContent).toContain(REFUSAL_SENTENCE);
    // No value, no length: the answer has no field for one and nothing renders a count.
    expect(result.textContent).not.toMatch(/\d+\s*(chars|characters|bytes)/);
  });

  it("the row shows the LOCATION (a human needs it) and the names, never a value", async () => {
    renderTab();
    await screen.findByTestId("secretref-row-deploy-token", {}, { timeout: 4000 });
    expect((await screen.findByTestId("secretref-source-deploy-token", {}, { timeout: 4000 })).textContent).toBe("DEPLOY_TOKEN");
    expect(screen.getByTestId("secretref-injectas-deploy-token").textContent).toContain("DEPLOY_TOKEN");
    fireEvent.click(screen.getByTestId("secretref-toggle-deploy-token"));
    await waitFor(() => expect(screen.getByTestId("secretref-usedby-deploy-token").textContent).toContain("ci-default")); // the show read lands after the panel
  }, 15000);

  it("the create form has no value field, and the door's key-refusal is a verbatim toast with the draft kept", async () => {
    const push = vi.spyOn(notifications, "push");
    const daemon = renderTab({
      ...environmentsBaseResponses,
      "secretref.create": () => {
        throw refusedFixture("secretref", "create-refuses-a-value-field.json");
      },
    });
    await screen.findByTestId("secretref-row-deploy-token");
    fireEvent.click(screen.getByTestId("environments-secret-create"));
    await screen.findByTestId("secretref-create-form");

    // The note says what a reference is; no input on this form could hold a value.
    expect(screen.getByTestId("secretref-create-note").textContent).toContain("There is no field for a value here");
    for (const input of Array.from(document.querySelectorAll("input"))) {
      const haystack = `${input.getAttribute("type") ?? ""} ${input.getAttribute("name") ?? ""} ${
        input.getAttribute("aria-label") ?? ""
      } ${input.getAttribute("data-testid") ?? ""}`.toLowerCase();
      expect(haystack).not.toMatch(/password|token|credential|secret value|api_key/);
      expect(input.getAttribute("type")).not.toBe("password");
    }

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "deploy-token" } });
    fireEvent.change(screen.getByLabelText("Location (an environment variable name or a file path)"), {
      target: { value: "DEPLOY_TOKEN" },
    });
    fireEvent.click(screen.getByTestId("secretref-form-save"));

    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain('secretref.create params carry "token"');
    // The draft survived the refusal.
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("deploy-token");
    expect(
      (screen.getByLabelText("Location (an environment variable name or a file path)") as HTMLInputElement).value,
    ).toBe("DEPLOY_TOKEN");
    // And nothing value-shaped ever left the app.
    await waitFor(() => expect(daemon.paramsOf("secretref.create")).toBeTruthy());
    const keys = Object.keys(daemon.paramsOf("secretref.create") as Record<string, unknown>);
    // The pointer, and only the pointer: `sourceKind` + `source` (+ the labels a
    // person typed). An empty optional is omitted rather than sent blank, and no
    // value-shaped key exists to send.
    expect(keys.sort()).toEqual(["description", "name", "source", "sourceKind"]);
  });

  it("the source-kind filter is the vocabulary's list and travels as a secretref.list param", async () => {
    const daemon = renderTab();
    await screen.findByTestId("secretref-row-deploy-token");
    const select = screen.getByLabelText("Source kind") as HTMLSelectElement;
    const kinds = (environmentsBaseResponses["environment.vocabulary"] as { sourceKinds: string[] }).sourceKinds;
    expect(Array.from(select.options).map((option) => option.value)).toEqual(["", ...kinds]);
    fireEvent.change(select, { target: { value: "key-file" } });
    await waitFor(() => expect(daemon.paramsOf("secretref.list")).toMatchObject({ sourceKind: "key-file" }));
  });

  it("offline disables every secret-reference write", async () => {
    const daemon = renderTab();
    await screen.findByTestId("secretref-row-deploy-token");
    // Rendered without canWrite: no create door at all.
    cleanup();
    resetFakeDaemon();
    const offline = installFakeDaemon({ responses: environmentsBaseResponses });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <Harness canWrite={false} vocabulary={environmentsBaseResponses["environment.vocabulary"] as never} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByTestId("secretref-row-deploy-token");
    expect(screen.queryByTestId("environments-secret-create")).toBeNull();
    fireEvent.click(screen.getByTestId("secretref-toggle-deploy-token"));
    await screen.findByTestId("secretref-detail-deploy-token");
    expect(screen.queryByTestId(`secretref-archive-deploy-token`)).toBeNull();
    expect(offline.methods()).not.toContain("secretref.archive");
    expect(daemon.methods()).not.toContain("secretref.create");
  });
});
