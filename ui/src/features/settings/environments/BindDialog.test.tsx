import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { notifications } from "../../../lib/notifications";
import { EnvironmentsSection } from "./EnvironmentsSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { environmentBindDryRun, environmentsBaseResponses } from "../../../../test/environment-fixtures";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

const ENVIRONMENT_ID = "env_7f2a9b8c6d5e";
const DRY_STATEMENT = (environmentBindDryRun as { statement: string }).statement;

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

async function openBind(): Promise<void> {
  await screen.findByTestId(`environment-row-${ENVIRONMENT_ID}`);
  fireEvent.click(screen.getByTestId(`environment-row-toggle-${ENVIRONMENT_ID}`));
  fireEvent.click(await screen.findByTestId(`environment-bind-${ENVIRONMENT_ID}`)); // the row opens a side panel
  await screen.findByTestId("environments-bind-dialog");
}

describe("BindDialog (S59 T11)", () => {
  it("shows the daemon's statement from a dry run before anything is asked", async () => {
    const daemon = renderSection();
    await openBind();
    // The FIRST call is the dry run — and it is the door's own preview.
    await waitFor(() => expect(daemon.paramsOf("environment.bind")).toMatchObject({ dryRun: true }));
    const statement = await screen.findByTestId("environments-bind-statement");
    expect(statement.textContent).toBe(DRY_STATEMENT);
    // Nothing applied: only the dry run went out.
    expect(daemon.calls.filter((call) => call.method === "environment.bind")).toHaveLength(1);
  });

  it("declining the confirmation sends NOTHING", async () => {
    const daemon = renderSection();
    await openBind();
    await screen.findByTestId("environments-bind-statement");
    fireEvent.click(screen.getByTestId("environments-bind-ask"));
    // The confirm modal says the daemon's sentence, not the panel's.
    const confirmed = await screen.findByTestId("environments-bind-confirm-statement");
    expect(confirmed.textContent).toBe(DRY_STATEMENT);

    fireEvent.click(screen.getByRole("button", { name: "No, leave it" }));
    await waitFor(() => expect(screen.queryByTestId("environments-bind-confirm-statement")).toBeNull());
    const binds = daemon.calls.filter((call) => call.method === "environment.bind");
    // Exactly one, and it is the dry run: no applied write was ever sent.
    expect(binds).toHaveLength(1);
    expect((binds[0]?.params as { dryRun: boolean }).dryRun).toBe(true);
    expect(screen.queryByTestId("environments-bind-applied")).toBeNull();
  });

  it("confirming applies with dryRun false and the row updates FROM THE RESPONSE, never optimistically", async () => {
    const applied = { changed: true, dryRun: false, statement: `${DRY_STATEMENT} · applied` };
    const push = vi.spyOn(notifications, "push");
    const daemon = renderSection({
      ...environmentsBaseResponses,
      // Only the SECOND call (dryRun false) is allowed to say "applied".
      "environment.bind": (params: unknown) =>
        (params as { dryRun: boolean }).dryRun === true ? environmentBindDryRun : applied,
    });
    await openBind();
    await screen.findByTestId("environments-bind-statement");
    fireEvent.click(screen.getByTestId("environments-bind-ask"));
    fireEvent.click(screen.getByRole("button", { name: "Yes, bind it" }));

    // The words the daemon used for the applied write — not the panel's — as a toast.
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain(applied.statement);
    const binds = daemon.calls.filter((call) => call.method === "environment.bind");
    expect(binds.map((call) => (call.params as { dryRun: boolean }).dryRun)).toEqual([true, false]);
    // The row is refreshed from the daemon rather than edited in place.
    expect(daemon.calls.filter((call) => call.method === "environment.list").length).toBeGreaterThanOrEqual(2);
  });

  it("a refused bind is a verbatim toast and keeps the person's scope and component", async () => {
    const push = vi.spyOn(notifications, "push");
    const daemon = renderSection({
      ...environmentsBaseResponses,
      "environment.bind": (params: unknown) => {
        if ((params as { dryRun: boolean }).dryRun === true) return environmentBindDryRun;
        throw JSON.stringify({
          code: -31880,
          message: "the environment or secret-reference operation was refused",
          data: {
            kind: "binding-unknown-component",
            what: "there is no component cmp_nobody to bind an environment to — name a component that exists",
            means: "the bind would be a row pointing at nothing",
            todo: "pick the component from component.list",
            fatal: false,
          },
        });
      },
    });
    await openBind();
    fireEvent.click(screen.getByTestId("environments-bind-scope"));
    fireEvent.change(screen.getByTestId("environments-bind-scope"), { target: { value: "component" } });
    fireEvent.change(screen.getByTestId("environments-bind-component"), { target: { value: "cmp_42" } });
    await screen.findByTestId("environments-bind-statement");
    fireEvent.click(screen.getByTestId("environments-bind-ask"));
    fireEvent.click(screen.getByRole("button", { name: "Yes, bind it" }));

    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain("there is no component cmp_nobody to bind an environment to");
    // The choices that provoked it are still on the screen.
    expect((screen.getByTestId("environments-bind-scope") as HTMLSelectElement).value).toBe("component");
    expect((screen.getByTestId("environments-bind-component") as HTMLSelectElement).value).toBe("cmp_42");
    // And the panel never claimed it was applied.
    expect(screen.queryByTestId("environments-bind-applied")).toBeNull();
    expect(daemon.calls.filter((call) => call.method === "environment.bind").length).toBeGreaterThanOrEqual(2);
  });
});
