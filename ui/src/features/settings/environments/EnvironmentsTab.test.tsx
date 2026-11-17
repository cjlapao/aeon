import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { EnvironmentsSection } from "./EnvironmentsSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import {
  environmentVocabulary,
  environmentsBaseResponses,
  legacyRow,
} from "../../../../test/environment-fixtures";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

const ENVIRONMENT_ID = "env_7f2a9b8c6d5e";

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

function renderSection(responses: Record<string, unknown> = environmentsBaseResponses): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <EnvironmentsSection controller={new LiveController()} live={currentLive} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

const vocabulary = environmentVocabulary as {
  backends: Array<{ name: string; capabilities: string[] }>;
  knownLimits: Array<{ key: string; sentence: string; active: boolean }>;
};

/** Every capability any backend declares — the chip set, read from the fixture. */
const universe = [...new Set(vocabulary.backends.flatMap((entry) => entry.capabilities))];

describe("the Environments tab (S59 T11)", () => {
  it("lists environments with capability chips and bindings", async () => {
    renderSection();
    await screen.findByTestId(`environment-row-${ENVIRONMENT_ID}`);
    // Bound at the component scope, named by `component.list` — not by id.
    expect(screen.getByTestId(`environment-binding-${ENVIRONMENT_ID}`).textContent).toContain("acme-api");
    // The last change is the audit door's actor, beside a relative time.
    expect(screen.getByTestId(`environment-changed-${ENVIRONMENT_ID}`).textContent).toContain("peer-1");

    // The capability chips live in the row's side panel: one per capability the PLATFORM can enforce, in the vocabulary's own
    // names — none of them typed here.
    fireEvent.click(screen.getByTestId(`environment-row-toggle-${ENVIRONMENT_ID}`));
    await screen.findByTestId(`environment-detail-${ENVIRONMENT_ID}`);
    const chips = screen.getAllByTestId(new RegExp(`^environment-chip-${ENVIRONMENT_ID}-`));
    expect(chips.map((chip) => chip.getAttribute("data-testid"))).toEqual(
      universe.map((capability) => `environment-chip-${ENVIRONMENT_ID}-${capability}`),
    );
    // The row's backend is host-scrubbed: it declares the allowlist and the
    // redaction, and does NOT declare filesystem isolation.
    expect(screen.getByTestId(`environment-chip-${ENVIRONMENT_ID}-secrets-redacted`).getAttribute("data-enforced")).toBe("true");
    expect(screen.getByTestId(`environment-chip-${ENVIRONMENT_ID}-filesystem-isolation`).getAttribute("data-enforced")).toBe("false");
  });

  it("badges the inheriting row and shows the workspace-floor banner in the daemon's words", async () => {
    const note =
      "this step inherits the daemon's own environment: what it can read is every name this process has, not a list (EV1). Bind a host-scrubbed environment to narrow it.";
    renderSection({
      ...environmentsBaseResponses,
      "environment.list": [...(environmentsBaseResponses["environment.list"] as unknown[]), legacyRow()],
      "environment.effective": { ...(environmentsBaseResponses["environment.effective"] as Record<string, unknown>), downgradeNote: note },
    });
    await screen.findByTestId("environment-row-env_legacy_0001");
    expect(screen.getByTestId("environment-legacy-env_legacy_0001")).toBeTruthy();
    // An inheriting row enforces nothing but the wall limit: the chips (in its panel) say so.
    fireEvent.click(screen.getByTestId("environment-row-toggle-env_legacy_0001"));
    await screen.findByTestId("environment-detail-env_legacy_0001");
    expect(screen.getByTestId("environment-chip-env_legacy_0001-wall-limit").getAttribute("data-enforced")).toBe("true");
    expect(screen.getByTestId("environment-chip-env_legacy_0001-env-allowlist").getAttribute("data-enforced")).toBe("false");
    const banner = await screen.findByTestId("environments-banner-legacy");
    // VERBATIM from `environment.effective`'s downgrade note — not a string this file owns.
    expect(banner.textContent).toContain(note);
  });

  it("renders the EV15 limit verbatim while the daemon says it is active, and not otherwise", async () => {
    const limit = vocabulary.knownLimits.find((entry) => entry.active === true);
    if (!limit) throw new Error("the vocabulary fixture carries no active known limit");
    renderSection();
    const banner = await screen.findByTestId(`environments-limit-${limit.key}`);
    expect(banner.textContent).toContain(limit.sentence);

    cleanup();
    resetFakeDaemon();
    renderSection({
      ...environmentsBaseResponses,
      "environment.vocabulary": { ...(environmentVocabulary as Record<string, unknown>), knownLimits: [{ ...limit, active: false }] },
    });
    await screen.findByTestId("environments-section");
    expect(screen.queryByTestId(`environments-limit-${limit.key}`)).toBeNull();
  });

  it("the filters travel as environment.list PARAMS — the rows are the daemon's answer", async () => {
    const daemon = renderSection();
    await screen.findByTestId(`environment-row-${ENVIRONMENT_ID}`);
    fireEvent.change(screen.getByLabelText("Backend"), { target: { value: "container" } });
    await waitFor(() => expect(daemon.paramsOf("environment.list")).toMatchObject({ backend: "container" }));
    fireEvent.change(screen.getByLabelText("Origin"), { target: { value: "system" } });
    await waitFor(() => expect(daemon.paramsOf("environment.list")).toMatchObject({ origin: "system" }));
    fireEvent.click(screen.getByLabelText("Include archived"));
    await waitFor(() => expect(daemon.paramsOf("environment.list")).toMatchObject({ includeArchived: true }));
  });

  it("opening a row reads environment.show and prints names, the audit diff and the honesty line verbatim", async () => {
    renderSection();
    await screen.findByTestId(`environment-row-${ENVIRONMENT_ID}`);
    fireEvent.click(screen.getByTestId(`environment-row-toggle-${ENVIRONMENT_ID}`));
    const detail = await screen.findByTestId(`environment-detail-${ENVIRONMENT_ID}`);
    expect(detail.textContent).toContain("deploy-token"); // the reference NAME
    expect(detail.textContent).toContain("passthrough"); // the audit summary, verbatim
    const honesty = (environmentVocabulary as { honesty: string }).honesty;
    expect(screen.getByTestId(`environment-honesty-${ENVIRONMENT_ID}`).textContent).toContain(honesty);
    // The revision carries a diff computed from the recorded specAfter: +secrets, ~TERM…
    const diff = await screen.findByTestId(`environment-revision-diff-${ENVIRONMENT_ID}-2`);
    expect(diff.textContent).toMatch(/[+−~]/);
  });
});
