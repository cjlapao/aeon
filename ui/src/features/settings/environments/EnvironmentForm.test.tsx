import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configure, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { notifications } from "../../../lib/notifications";
import { EnvironmentsSection } from "./EnvironmentsSection";
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

const ENVIRONMENT_ID = "env_7f2a9b8c6d5e";

/** The refusal the real door answers with when a `set` name looks like a credential. */
const CREDENTIAL_REFUSAL = {
  code: -31880,
  message: "the environment or secret-reference operation was refused",
  data: {
    kind: "name-credential-shaped",
    what: 'refused: name-credential-shaped · "DEPLOY_API_KEY" is a credential-shaped name and this plane keeps credentials out of `set` · use a secret reference',
    means: "a variable that looks like a key is a key someone forgot to point at",
    todo: "create the reference with secretref.create and name it in spec.env.secrets",
    fatal: false,
  },
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

async function openEditor(): Promise<HTMLElement> {
  await screen.findByTestId(`environment-row-${ENVIRONMENT_ID}`);
  fireEvent.click(screen.getByTestId(`environment-row-toggle-${ENVIRONMENT_ID}`));
  fireEvent.click(await screen.findByTestId(`environment-edit-${ENVIRONMENT_ID}`)); // the row opens a side panel
  return screen.findByTestId("environment-form");
}


describe("EnvironmentForm (S59 T11)", () => {
  it("a refusal is a toast with its three parts, and the draft is kept", async () => {
    const push = vi.spyOn(notifications, "push");
    renderSection({
      ...environmentsBaseResponses,
      "environment.update": () => {
        throw JSON.stringify(CREDENTIAL_REFUSAL);
      },
    });
    const form = await openEditor();

    fireEvent.click(within(form).getByTestId("environment-form-set-add"));
    const names = () => within(form).getAllByLabelText("Variables set · Variable name") as HTMLInputElement[];
    const contents = () => within(form).getAllByLabelText("Variables set · Content") as HTMLInputElement[];
    // The row the person just added: the fixture's row (TERM) is still there,
    // so the fields are addressed by position, not by a shared label.
    const last = (list: HTMLInputElement[]): HTMLInputElement => {
      const entry = list[list.length - 1];
      if (!entry) throw new Error("the form has no such field");
      return entry;
    };
    fireEvent.change(last(names()), { target: { value: "DEPLOY_API_KEY" } });
    fireEvent.change(last(contents()), { target: { value: "keep-me" } });
    fireEvent.click(screen.getByTestId("environment-form-save"));

    await waitFor(() => expect(push).toHaveBeenCalled());
    // VERBATIM — the daemon's `what`, `means` and `todo`, not a paraphrase — and no panel stays on the page.
    const body = String(push.mock.calls[0]?.[0]?.body ?? "");
    expect(body).toContain(CREDENTIAL_REFUSAL.data.what);
    expect(body).toContain(CREDENTIAL_REFUSAL.data.means);
    expect(body).toContain(CREDENTIAL_REFUSAL.data.todo);
    expect(screen.queryByTestId("environment-form-refusal")).toBeNull();
    // And the draft that provoked it is exactly as the person left it.
    expect(last(names()).value).toBe("DEPLOY_API_KEY");
    expect(last(contents()).value).toBe("keep-me");
  });

  it("the passthrough chips are the daemon's default allowlist, one click away", async () => {
    renderSection();
    const form = await openEditor();
    fireEvent.click(within(form).getByTestId("environment-form-default-allowlist"));
    // Every word of the daemon's own list is now on.
    const allowed = (environmentsBaseResponses["environment.vocabulary"] as { defaultAllowlist: string[] }).defaultAllowlist;
    for (const word of allowed) {
      await waitFor(() =>
        expect(within(form).getByTestId(`environment-form-chip-${word}`).getAttribute("data-on")).toBe("true"),
      );
    }
  });

  it("a create carries no identity param and no value-shaped key, and an edit carries expectedRevision", async () => {
    const daemon = renderSection();
    const form = await openEditor();
    fireEvent.click(within(form).getByTestId("environment-form-save"));
    await waitFor(() => expect(daemon.paramsOf("environment.update")).toBeTruthy());
    const params = daemon.paramsOf("environment.update") as Record<string, unknown>;
    expect(Object.keys(params).sort()).toEqual(["expectedRevision", "id", "spec"]);
    // Stale edits are refused by the daemon, not overwritten: the revision travels.
    // Stale edits are refused by the daemon, not overwritten: the revision the
    // row carries travels unchanged.
    expect(params.expectedRevision).toBe(2);
    const spec = JSON.parse(String(params.spec)) as Record<string, unknown>;
    expect(Object.keys(spec).sort()).toEqual(["egress", "env", "home", "image", "limits", "preview", "services"]);
    expect(Object.keys(spec.env as Record<string, unknown>).sort()).toEqual(["passthrough", "secrets", "set"]);

    cleanup();
    resetFakeDaemon();
    const created = renderSection();
    await screen.findByTestId("environment-row-env_7f2a9b8c6d5e");
    fireEvent.click(screen.getByTestId("environments-create"));
    const blank = await screen.findByTestId("environment-form");
    fireEvent.click(within(blank).getByTestId("environment-form-save"));
    await waitFor(() => expect(created.paramsOf("environment.create")).toBeTruthy());
    const addParams = created.paramsOf("environment.create") as Record<string, unknown>;
    expect(Object.keys(addParams).sort()).toEqual(["backend", "name", "slug", "spec"]);
    expect(created.methods()).not.toContain("environment.update");
  });

  it("the form has no value field anywhere — nothing to type a secret into", async () => {
    renderSection();
    await openEditor();
    await screen.findByTestId("environment-form-secret-deploy-token"); // the references load after the form opens
    const inputs = Array.from(
      document.querySelectorAll<HTMLElement>('[data-testid="environment-form"] input'),
    );
    expect(inputs.length).toBeGreaterThan(0);
    // No password field anywhere, and the reference picker is the one place a
    // daemon-supplied NAME lands in a label — that is data, not a field kind.
    const pickerInputs = inputs.filter((input) => input.closest('[data-testid="environment-form-secrets"]'));
    expect(pickerInputs.length).toBeGreaterThan(0);
    for (const input of inputs) {
      expect(input.getAttribute("type")).not.toBe("password");
      if (input.closest('[data-testid="environment-form-secrets"]')) continue;
      const haystack = `${input.getAttribute("type") ?? ""} ${input.getAttribute("name") ?? ""} ${
        input.getAttribute("aria-label") ?? ""
      } ${input.getAttribute("data-testid") ?? ""}`.toLowerCase();
      expect(haystack).not.toMatch(/password|secret value|token|credential|apikey|api_key/);
    }
    expect(within(screen.getByTestId("environment-form-secrets")).getByText("deploy-token")).toBeTruthy();
  });

  it("the backend select is the vocabulary's list and nothing else", async () => {
    renderSection();
    const form = await openEditor();
    const select = within(form).getByLabelText("Backend") as HTMLSelectElement;
    const named = (environmentsBaseResponses["environment.vocabulary"] as { backends: Array<{ name: string }> }).backends;
    expect(Array.from(select.options).map((option) => option.value)).toEqual(named.map((entry) => entry.name));
  });

  it("a refusal the door answers by key (a stolen actor) is rendered verbatim", async () => {
    const push = vi.spyOn(notifications, "push");
    renderSection({
      ...environmentsBaseResponses,
      "environment.create": () => {
        throw refusedFixture("environment", "create-refuses-a-stolen-actor.json");
      },
    });
    await screen.findByTestId("environment-row-env_7f2a9b8c6d5e");
    fireEvent.click(screen.getByTestId("environments-create"));
    await screen.findByTestId("environment-form");
    fireEvent.click(screen.getByTestId("environment-form-save"));
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain('environment.create params carry "actor"');
  });
});
