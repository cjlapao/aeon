import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { notifications } from "../../../lib/notifications";
import { ProvisionsTab } from "./ProvisionsTab";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { environmentsBaseResponses, stuckProvision } from "../../../../test/environment-fixtures";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

const STUCK = stuckProvision();

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

function renderTab(responses: Record<string, unknown> = environmentsBaseResponses, canWrite = true): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <ProvisionsTab controller={new LiveController()} live={currentLive} canWrite={canWrite} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("ProvisionsTab (S59 T11)", () => {
  it("the ledger is the daemon's rows: state, loop, backend, handle, ages, last_error", async () => {
    renderTab();
    await screen.findByTestId("provision-row-prv_2c4d6e8a");
    expect((await screen.findByTestId("provision-state-prv_2c4d6e8a")).textContent).toBe("ready");
    expect((await screen.findByTestId("provision-handle-prv_2c4d6e8a")).textContent).toContain("aeon-s59-acme-api-8f2b1c");
    expect((await screen.findByTestId("provision-loop-prv_2c4d6e8a")).textContent).toContain("release-2026-11");

    // The failed removal keeps the driver's own sentence — verbatim, because a
    // ledger that records the failure it could not complete is the honest one.
fireEvent.click(await screen.findByTestId("provision-row-prv_stuck_0001")); // the row opens a side panel
    expect((await screen.findByTestId("provision-error-prv_stuck_0001")).textContent).toContain(STUCK.lastError);
  });

  it("tear down is a human door: the confirm names the row, and declining sends nothing", async () => {
    const daemon = renderTab();
    await screen.findByTestId("provision-row-prv_stuck_0001");
    fireEvent.click(screen.getByTestId("provision-row-prv_stuck_0001"));
    fireEvent.click(await screen.findByTestId("provision-teardown-prv_stuck_0001"));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("prv_stuck_0001");
    // The confirm button is gated on typing the id; closing sends nothing.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(daemon.methods()).not.toContain("environment.teardown");
  });

  it("a torn row is replaced by the ANSWER, said as a toast", async () => {
    const push = vi.spyOn(notifications, "push");
    const daemon = renderTab({
      ...environmentsBaseResponses,
      "environment.teardown": () =>
        JSON.parse(JSON.stringify((environmentsBaseResponses["environment.teardown"] as Record<string, unknown>) ?? {})),
    });
    await screen.findByTestId("provision-row-prv_stuck_0001");
    fireEvent.click(screen.getByTestId("provision-row-prv_stuck_0001"));
    fireEvent.click(await screen.findByTestId("provision-teardown-prv_stuck_0001"));
    await screen.findByRole("alertdialog");
    fireEvent.change(screen.getByPlaceholderText("prv_stuck_0001"), { target: { value: "prv_stuck_0001" } });
    fireEvent.click(screen.getByRole("button", { name: "Tear it down" }));

    await waitFor(() => expect(push).toHaveBeenCalled());
    // The state said is the one the daemon answered with (`removed`), not the one the row carried before.
    const said = `${push.mock.calls[0]?.[0]?.title} ${push.mock.calls[0]?.[0]?.body}`;
    expect(said).toContain("prv_2c4d6e8a");
    expect(said).toContain("removed");
    expect(daemon.paramsOf("environment.teardown")).toEqual({ provisionId: "prv_stuck_0001" });
    expect(daemon.calls.filter((call) => call.method === "environment.provisions").length).toBeGreaterThanOrEqual(2);
  });

  it("an unknown provision is refused with the daemon's own three parts, as a toast", async () => {
    const push = vi.spyOn(notifications, "push");
    const daemon = renderTab({
      ...environmentsBaseResponses,
      "environment.teardown": () => {
        throw JSON.stringify({
          code: -31880,
          message: "the environment or secret-reference operation was refused",
          data: {
            kind: "provision-unknown",
            what: "there is no provision prv_stuck_0001 to mark — the ledger is the only thing that knows a resource exists, so a mark against nothing means the resource was created without a row (EV12)",
            means: "the container, if it exists, has no ledger row — or the row was removed and something still runs",
            todo: "list the ledger with environment.provisions and name the row you meant",
            fatal: false,
          },
        });
      },
    });
    await screen.findByTestId("provision-row-prv_stuck_0001");
    fireEvent.click(screen.getByTestId("provision-row-prv_stuck_0001"));
    fireEvent.click(await screen.findByTestId("provision-teardown-prv_stuck_0001"));
    await screen.findByRole("alertdialog");
    fireEvent.change(screen.getByPlaceholderText("prv_stuck_0001"), { target: { value: "prv_stuck_0001" } });
    fireEvent.click(screen.getByRole("button", { name: "Tear it down" }));

    await waitFor(() => expect(push).toHaveBeenCalled());
    const body = String(push.mock.calls[0]?.[0]?.body ?? "");
    expect(body).toContain("the ledger is the only thing that knows a resource exists");
    expect(body).toContain("list the ledger with environment.provisions and name the row you meant");
    expect(screen.queryByTestId("environments-provision-error-panel")).toBeNull();
    expect(daemon.methods()).toContain("environment.teardown");
  });

  it("offline offers no tear-down door", async () => {
    const daemon = renderTab(environmentsBaseResponses, false);
    fireEvent.click(await screen.findByTestId("provision-row-prv_stuck_0001"));
    await screen.findByTestId("provision-ready-prv_stuck_0001");
    expect(screen.queryByTestId("provision-teardown-prv_stuck_0001")).toBeNull();
    expect(daemon.methods()).not.toContain("environment.teardown");
  });
});
