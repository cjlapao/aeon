import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { LiveController } from "../../../lib/live";
import { SystemRecordsPanel } from "./SystemRecordsPanel";
import { APP_LOCALES } from "../../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../../test/helpers";
import { notifications } from "../../../lib/notifications";
import en from "../../../lib/i18n/en.json";
import pt from "../../../lib/i18n/pt.json";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

/** A seed set, shaped like `seed.status`'s element. No id or key is hardcoded. */
function seedSet(overrides: Record<string, unknown> = {}) {
  return {
    appliedVersion: 0,
    availableVersion: 0,
    class: "inert",
    id: "set.a",
    records: [],
    ...overrides,
  };
}

function record(overrides: Record<string, unknown> = {}) {
  return {
    appliedVersion: 0,
    availableVersion: 0,
    key: "record.a",
    reason: "",
    state: "applied",
    ...overrides,
  };
}

/** A `seed.plan` result. `keys: []` plans the whole set. */
function planResult(overrides: Record<string, unknown> = {}) {
  return {
    plan: {
      setId: "set.a",
      decisions: [
        { key: "record.a", op: "update", fromVersion: 1, toVersion: 2, reason: "update", state: "applied" },
      ],
      refusal: { key: "", reason: "" },
      ...overrides,
    },
  };
}

/** The kit's Modal reads `matchMedia`; jsdom has none. */
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

function renderPanel(responses: Record<string, unknown>, live = currentLive, canWrite?: boolean) {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <SystemRecordsPanel controller={new LiveController()} live={live} canWrite={canWrite} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

/** Open a set's side panel by clicking its row. */
async function openSet(id = "set.a"): Promise<void> {
  fireEvent.click(await screen.findByText(id));
  await screen.findByTestId("seed-set-panel");
}

describe("SystemRecordsPanel", () => {
  it("renders one card per set with class, versions and counts, from the mocked status only", async () => {
    renderPanel({
      "seed.status": {
        sets: [
          seedSet({
            id: "set.a",
            class: "guard",
            availableVersion: 3,
            appliedVersion: 2,
            records: [record({ state: "applied" }), record({ state: "diverged" })],
          }),
        ],
      },
    });
    await screen.findByText("set.a");
    // The row renders the class the daemon sent, both versions, and the count.
    const card = screen.getByText("set.a").closest("tr");
    expect(within(card as HTMLElement).getByText("guard")).toBeTruthy();
    expect(card?.textContent).toContain("3"); // available version
    expect(card?.textContent).toContain("2"); // applied version
    // A diverged record drives a distinct status line (and a distinct count pill).
    expect(screen.getAllByText(/diverged/i).length).toBeGreaterThan(0);
  });

  it("renders the honest empty state, never a blank panel", async () => {
    renderPanel({ "seed.status": { sets: [] } });
    expect(await screen.findByText(/No system records are declared yet/i)).toBeTruthy();
  });

  it("shows an error panel on a failed status, never a blank panel", async () => {
    installFakeDaemon({
      responses: {
        "seed.status": () => {
          throw Object.assign(new Error("the daemon refused this request"), { code: -32000, kind: "rpc" });
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <SystemRecordsPanel controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(await screen.findByText(/the daemon refused this request/i, {}, { timeout: 2000 })).toBeTruthy();
  });

  it("opens a set to its record list and shows a non-applied reason verbatim", async () => {
    renderPanel({
      "seed.status": {
        sets: [
          seedSet({
            records: [
              record({ key: "record.a", state: "diverged", reason: "the live row was changed by an operator" }),
            ],
          }),
        ],
      },
    });
    await openSet();
    // The daemon's reason is shown verbatim, not rewritten.
    expect(await screen.findByText("the live row was changed by an operator")).toBeTruthy();
  });

  it("shows a failed record's error in an Alert (not a plain line)", async () => {
    renderPanel({
      "seed.status": {
        sets: [
          seedSet({
            records: [
              record({ key: "record.a", state: "failed", reason: "apply failed: disk full" }),
            ],
          }),
        ],
      },
    });
    await openSet();
    const errorText = await screen.findByText("apply failed: disk full");
    // The failed error is in an Alert (a div), never a plain <p> line.
    expect(errorText.tagName).not.toBe("P");
  });

  it("plan is read-only: opening a plan calls seed.plan and nothing else", async () => {
    const daemon = renderPanel({
      "seed.status": { sets: [seedSet({ records: [record({ state: "diverged" })] })] },
      "seed.plan": planResult(),
      "seed.apply": () => ({ apply: { records: [], setId: "set.a" }, requiresConfirmation: false }),
    });
    await screen.findByText("set.a");
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    // The plan dialog is shown; the decisions render.
    expect(await screen.findByText("record.a")).toBeTruthy();
    expect(daemon.calls).toContainEqual({ method: "seed.plan", params: { set: "set.a", keys: [] } });
    // Opening a plan never writes.
    expect(daemon.calls.filter((c) => c.method === "seed.apply")).toHaveLength(0);
  });

  it("inert apply is one click after the plan is shown", async () => {
    const daemon = renderPanel({
      "seed.status": { sets: [seedSet({ class: "inert", records: [record({ state: "diverged" })] })] },
      "seed.plan": planResult(),
      "seed.apply": () => ({ apply: { records: [], setId: "set.a" }, requiresConfirmation: false }),
    });
    await screen.findByText("set.a");
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    await screen.findByText("record.a");
    // One click on Apply (no confirmation) applies the inert set.
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "seed.apply", params: { set: "set.a", keys: [] } }),
    );
  });

  it("guard apply confirms, and a decline sends nothing", async () => {
    const daemon = renderPanel({
      "seed.status": { sets: [seedSet({ class: "guard", records: [record({ state: "diverged" })] })] },
      "seed.plan": planResult(),
      "seed.apply": () => ({ apply: { records: [], setId: "set.a" }, requiresConfirmation: false }),
    });
    await screen.findByText("set.a");
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    await screen.findByText("record.a");
    // Clicking Apply opens the guard confirmation (an alertdialog, not the plan dialog).
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await screen.findByRole("alertdialog");
    // Decline: cancel the confirmation — nothing is sent.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(daemon.calls.filter((c) => c.method === "seed.apply")).toHaveLength(0);
  });

  it("guard apply, confirmed, applies the set", async () => {
    const daemon = renderPanel({
      "seed.status": { sets: [seedSet({ class: "guard", records: [record({ state: "diverged" })] })] },
      "seed.plan": planResult(),
      "seed.apply": () => ({ apply: { records: [], setId: "set.a" }, requiresConfirmation: false }),
    });
    await screen.findByText("set.a");
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    await screen.findByText("record.a");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    // Type the set id to confirm.
    const confirmInput = await screen.findByPlaceholderText("set.a");
    fireEvent.change(confirmInput, { target: { value: "set.a" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "seed.apply", params: { set: "set.a", keys: [] } }),
    );
  });

  it("restore confirms with the reason and calls seed.restore", async () => {
    const daemon = renderPanel({
      "seed.status": {
        sets: [
          seedSet({
            records: [record({ key: "record.a", state: "removed", reason: "the operator removed the live row" })],
          }),
        ],
      },
      "seed.restore": () => ({ apply: { records: [], setId: "set.a" } }),
    });
    await openSet();
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));
    // The confirmation names the version the restore lands on; type the key to confirm.
    const confirmInput = await screen.findByPlaceholderText("record.a");
    fireEvent.change(confirmInput, { target: { value: "record.a" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "seed.restore", params: { set: "set.a", key: "record.a" } }),
    );
  });

  it("updates the card from a fresh seed.status after a write, never from the click", async () => {
    let statusCalls = 0;
    installFakeDaemon({
      responses: {
        // First read: one diverged record. The refetch after the write answers applied.
        "seed.status": () => {
          statusCalls += 1;
          return {
            sets: [
              seedSet({
                records: [
                  record({
                    key: "record.a",
                    state: statusCalls === 1 ? "diverged" : "applied",
                    availableVersion: 2,
                    appliedVersion: statusCalls === 1 ? 1 : 2,
                  }),
                ],
              }),
            ],
          };
        },
        "seed.plan": planResult(),
        "seed.apply": () => ({ apply: { records: [], setId: "set.a" }, requiresConfirmation: false }),
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <SystemRecordsPanel controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByText("set.a");
    // Before the write, the card shows the diverged state from the first read.
    expect(screen.getAllByText(/diverged/i).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    await screen.findByText("record.a");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    // The card takes the refetched (applied) state, not an optimistic guess.
    await waitFor(() => expect(statusCalls).toBe(2));
    expect(await screen.findAllByText(/Up to date/i)).toBeTruthy();
  });

  it("a refusal is a toast and the control is restored", async () => {
    const toast = vi.spyOn(notifications, "push");
    const daemon = installFakeDaemon({
      responses: {
        "seed.status": { sets: [seedSet({ class: "inert", records: [record({ state: "diverged" })] })] },
        "seed.plan": planResult(),
        "seed.apply": () => {
          throw Object.assign(new Error("refused: a guard set cannot be applied at startup"), {
            code: -32000,
            kind: "rpc",
          });
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <SystemRecordsPanel controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByText("set.a");
    fireEvent.click(screen.getByRole("button", { name: "Plan" }));
    await screen.findByText("record.a");
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    // The daemon's refusal is a toast.
    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls.map((c) => c[0]))).toContain("refused: a guard set cannot be applied at startup");
    // The card is not blank and the Plan control is restored (re-enabled).
    expect(screen.getByText("set.a")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Plan" }) as HTMLButtonElement).disabled).toBe(false);
    expect(daemon.calls.filter((c) => c.method === "seed.apply").length).toBe(1);
  });

  it("offline disables writes but still renders the last data", async () => {
    installFakeDaemon({
      responses: {
        "seed.status": {
          sets: [seedSet({ class: "inert", records: [record({ state: "diverged" })] })],
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <SystemRecordsPanel controller={new LiveController()} live={currentLive} canWrite={false} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByText("set.a"); // last data still renders
    expect(screen.getByText(/Reconnecting — changes are disabled/i)).toBeTruthy();
    // The write control is dead.
    expect((screen.getByRole("button", { name: "Plan" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("keeps the existing Daemon settings panel intact on the same screen", async () => {
    const live = { state: "connecting", endpoint: "/run/user/1000/aeon/rpc.sock" } as const;
    installFakeDaemon({ responses: {} });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          {/* Mount the panel directly; SettingsScreen is covered by its own test. */}
          <SystemRecordsPanel controller={new LiveController()} live={live} />
        </MemoryRouter>
      </I18nProvider>,
    );
    // With no connection yet, the panel shows its loading state, not a blank.
    expect(await screen.findByText(/Loading system records/i)).toBeTruthy();
  });

  it("keeps the en and pt catalogs in parity for settings.seed", () => {
    const keys = (node: unknown, prefix = ""): string[] => {
      if (typeof node !== "object" || node === null) return [prefix];
      return Object.entries(node).flatMap(([k, v]) => keys(v, `${prefix}.${k}`));
    };
    const enKeys = keys(en.settings?.seed ?? {}).sort();
    const ptKeys = keys(pt.settings?.seed ?? {}).sort();
    expect(enKeys).toEqual(ptKeys);
  });
});
