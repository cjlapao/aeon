import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { LiveController } from "../src/lib/live";
import { DaemonSettingsPanel } from "../src/features/settings/DaemonSettingsPanel";
import { SettingsScreen } from "../src/features/settings/SettingsScreen";
import { APP_LOCALES } from "../src/lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { notifications } from "../src/lib/notifications";
import en from "../src/lib/i18n/en.json";
import pt from "../src/lib/i18n/pt.json";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

/** A declared setting, shaped like `settings.list`'s element. */
function view(overrides: Record<string, unknown> = {}) {
  return {
    area: "daemon",
    default: "false",
    description: "",
    envInvalid: "",
    key: "x",
    sensitive: false,
    shadowedBy: "",
    source: "default",
    stored: null,
    storedInvalid: false,
    type: "bool",
    updatedAt: "",
    updatedBy: "",
    value: "false",
    ...overrides,
  };
}

/** The kit's Modal/Responsive components read `matchMedia`; jsdom has none. */
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
        <DaemonSettingsPanel controller={new LiveController()} live={live} canWrite={canWrite} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

/** Open a setting's side panel by clicking its table row. */
async function openSetting(key: string): Promise<HTMLElement> {
  fireEvent.click(await screen.findByTestId(`setting-row-${key}`));
  return screen.findByTestId("daemon-setting-panel");
}

/** The whole table row a testid-carrying cell sits in. */
const rowOf = (testid: string): HTMLElement => screen.getByTestId(testid).closest("tr") as HTMLElement;

describe("DaemonSettingsPanel", () => {
  it("lists each declared setting as a row with its value and the layer it comes from", async () => {
    const settings = [
      view({ key: "daemon.a", type: "bool" }),
      view({ key: "daemon.b", type: "enum", enumValues: ["x", "y"], value: "x", default: "x", source: "database", stored: "x" }),
      view({ key: "daemon.c", type: "int", min: 0, max: 100, value: "5", default: "5", source: "environment", shadowedBy: "AEON_C" }),
    ];
    renderPanel({ "settings.list": { settings } });
    await screen.findByTestId("setting-row-daemon.a");
    expect(screen.getByTestId("setting-row-daemon.b")).toBeTruthy();
    expect(screen.getByTestId("setting-row-daemon.c")).toBeTruthy();
    expect(screen.getByTestId("setting-layer-daemon.a").textContent).toBe("Default");
    expect(screen.getByTestId("setting-layer-daemon.b").textContent).toBe("Saved");
    expect(screen.getByTestId("setting-layer-daemon.c").textContent).toBe("Environment");
    expect(rowOf("setting-value-daemon.c").textContent).toContain("5");
    expect(screen.getByTestId("daemon-settings-count").textContent).toContain("3");
  });

  it("search, area and layer narrow the rows already fetched", async () => {
    renderPanel({
      "settings.list": {
        settings: [
          view({ key: "backup.dir", area: "backup", type: "string", description: "Where copies go", value: "/x", default: "/x" }),
          view({ key: "scheduler.tick", area: "scheduler", type: "int", value: "5", default: "5", source: "database", stored: "5" }),
        ],
      },
    });
    await screen.findByTestId("setting-row-backup.dir");
    fireEvent.change(screen.getByTestId("daemon-filter-area"), { target: { value: "scheduler" } });
    await waitFor(() => expect(screen.queryByTestId("setting-row-backup.dir")).toBeNull());
    expect(screen.getByTestId("setting-row-scheduler.tick")).toBeTruthy();
    fireEvent.change(screen.getByTestId("daemon-filter-area"), { target: { value: "" } });
    fireEvent.change(screen.getByTestId("daemon-filter-layer"), { target: { value: "saved" } });
    await waitFor(() => expect(screen.queryByTestId("setting-row-backup.dir")).toBeNull());
    expect(screen.getByTestId("setting-row-scheduler.tick")).toBeTruthy();
  });

  it("toggling a non-sensitive bool in the panel calls settings.set with 'true'", async () => {
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.debug", type: "bool", value: "false", default: "false" })] },
      "settings.set": (params: { value: string }) => ({
        changed: true,
        setting: view({ key: "daemon.debug", value: String(params.value), source: "database", stored: String(params.value) }),
      }),
    });
    await openSetting("daemon.debug");
    fireEvent.click(screen.getByRole("radio", { name: "true" }));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "settings.set", params: { key: "daemon.debug", value: "true" } }));
    // the table row takes the response: now saved.
    await waitFor(() => expect(screen.getByTestId("setting-layer-daemon.debug").textContent).toBe("Saved"));
  });

  it("the value comes from the response, not from the draft", async () => {
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.port", type: "int", min: 0, max: 65535, value: "8080", default: "8080" })] },
      // The daemon answers a different value than the draft: the panel must take the response.
      "settings.set": () => ({
        changed: true,
        setting: view({ key: "daemon.port", type: "int", min: 0, max: 65535, value: "3", source: "database", stored: "3" }),
      }),
    });
    await openSetting("daemon.port");
    const input = screen.getByTestId("daemon-setting-input") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "4" } });
    fireEvent.click(screen.getByTestId("daemon-setting-save"));
    await waitFor(() => expect(input.value).toBe("3"));
    expect(daemon.calls).toContainEqual({ method: "settings.set", params: { key: "daemon.port", value: "4" } });
  });

  it("a malformed draft is shown inline and never sent", async () => {
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.port", type: "int", min: 0, max: 65535, value: "8080", default: "8080" })] },
    });
    await openSetting("daemon.port");
    fireEvent.change(screen.getByTestId("daemon-setting-input"), { target: { value: "99999" } });
    fireEvent.click(screen.getByTestId("daemon-setting-save"));
    expect((await screen.findByTestId("daemon-setting-parse-error")).textContent).toContain("between 0 and 65535");
    expect(daemon.calls.filter((c) => c.method === "settings.set")).toHaveLength(0);
  });

  it("asks first for a sensitive key and sends nothing on decline", async () => {
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.secret", type: "bool", value: "false", sensitive: true })] },
      "settings.set": () => ({ changed: true, setting: view({ key: "daemon.secret", value: "true", sensitive: true }) }),
    });
    await openSetting("daemon.secret");
    fireEvent.click(screen.getByRole("radio", { name: "true" }));
    // The type-to-confirm modal opens (the panel steps aside first), naming the value it must be typed.
    await screen.findByPlaceholderText("daemon.secret");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    // Declining sends nothing.
    expect(daemon.calls.filter((c) => c.method === "settings.set")).toHaveLength(0);
  });

  it("confirms then sets a sensitive key", async () => {
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.secret", type: "bool", value: "false", sensitive: true })] },
      "settings.set": () => ({ changed: true, setting: view({ key: "daemon.secret", value: "true", sensitive: true }) }),
    });
    await openSetting("daemon.secret");
    fireEvent.click(screen.getByRole("radio", { name: "true" }));
    const confirmInput = await screen.findByPlaceholderText("daemon.secret");
    fireEvent.change(confirmInput, { target: { value: "daemon.secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "settings.set", params: { key: "daemon.secret", value: "true" } }));
  });

  it("an environment-controlled setting is read-only with the variable named", async () => {
    renderPanel({
      "settings.list": {
        settings: [view({ key: "daemon.locked", type: "bool", value: "env-1", source: "environment", shadowedBy: "AEON_LOCKED", stored: "saved-1" })],
      },
    });
    await openSetting("daemon.locked");
    expect(screen.getByRole("radiogroup", { name: "daemon.locked" }).getAttribute("aria-disabled")).toBe("true");
    expect(screen.getByTestId("daemon-setting-lock").textContent).toContain("AEON_LOCKED");
  });

  it("an invalid environment or saved value is flagged in the table and explained in the panel", async () => {
    renderPanel({
      "settings.list": {
        settings: [
          view({ key: "daemon.badEnv", type: "bool", envInvalid: "AEON_BADENV", source: "default" }),
          view({ key: "daemon.badSaved", type: "bool", storedInvalid: true, stored: "old" }),
        ],
      },
    });
    await screen.findByTestId("setting-invalid-daemon.badEnv");
    expect(screen.getByTestId("setting-invalid-daemon.badSaved")).toBeTruthy();
    await openSetting("daemon.badEnv");
    expect(screen.getByText(/not valid for this setting/i)).toBeTruthy();
    cleanup();
    resetFakeDaemon();
    renderPanel({ "settings.list": { settings: [view({ key: "daemon.badSaved", type: "bool", storedInvalid: true, stored: "old" })] } });
    await openSetting("daemon.badSaved");
    expect(screen.getByText(/no longer valid/i)).toBeTruthy();
  });

  it("reset calls settings.unset and the row takes the response view", async () => {
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.r", type: "bool", value: "true", source: "database", stored: "true" })] },
      "settings.unset": () => ({ changed: true, setting: view({ key: "daemon.r", value: "false", source: "default", stored: null }) }),
    });
    await openSetting("daemon.r");
    fireEvent.click(screen.getByTestId("daemon-setting-reset"));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "settings.unset", params: { key: "daemon.r" } }));
    await waitFor(() => expect(screen.getByTestId("setting-layer-daemon.r").textContent).toBe("Default"));
  });

  it("reset is disabled when nothing is stored", async () => {
    renderPanel({ "settings.list": { settings: [view({ key: "daemon.none", type: "bool", source: "default", stored: null })] } });
    await openSetting("daemon.none");
    expect((screen.getByTestId("daemon-setting-reset") as HTMLButtonElement).disabled).toBe(true);
  });

  it("offline disables the controls", async () => {
    installFakeDaemon({ responses: { "settings.list": { settings: [view({ key: "daemon.off", type: "bool", value: "false" })] } } });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <DaemonSettingsPanel controller={new LiveController()} live={currentLive} canWrite={false} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await openSetting("daemon.off");
    expect(screen.getAllByText(/Reconnecting — changes are disabled/i).length).toBeGreaterThan(0);
    expect(screen.getByRole("radiogroup", { name: "daemon.off" }).getAttribute("aria-disabled")).toBe("true");
  });

  it("a refused write is a toast in the daemon's words", async () => {
    const toast = vi.spyOn(notifications, "push");
    renderPanel({
      "settings.list": { settings: [view({ key: "daemon.x", type: "bool", value: "false" })] },
      "settings.set": () => {
        throw Object.assign(new Error("settings.set refuses daemon.x — the key is read-only here"), { code: -32000, kind: "rpc" });
      },
    });
    await openSetting("daemon.x");
    fireEvent.click(screen.getByRole("radio", { name: "true" }));
    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls.map((c) => c[0]))).toContain("read-only here");
  });

  it("writes nothing to localStorage for a daemon value", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {});
    const daemon = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.persist", type: "int", value: "8080", default: "8080" })] },
      "settings.set": () => ({
        changed: true,
        setting: view({ key: "daemon.persist", type: "int", value: "9090", source: "database", stored: "9090" }),
      }),
    });
    await openSetting("daemon.persist");
    fireEvent.change(screen.getByTestId("daemon-setting-input"), { target: { value: "9090" } });
    fireEvent.click(screen.getByTestId("daemon-setting-save"));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "settings.set", params: { key: "daemon.persist", value: "9090" } }));
    // The daemon value is never written to browser storage. A success toast (which names the key)
    // may be persisted by the notifications system — that is not a daemon value, so it is fine.
    expect(setItem.mock.calls.filter(([, value]) => /9090/.test(String(value)))).toHaveLength(0);
    setItem.mockRestore();
  });

  it("only calls the registered settings methods", async () => {
    const daemon = renderPanel({
      "settings.list": {
        settings: [
          view({ key: "daemon.one", type: "bool" }),
          view({ key: "daemon.two", type: "bool", value: "true", source: "database", stored: "true" }),
        ],
      },
      "settings.set": () => ({ changed: true, setting: view({ key: "daemon.one", type: "bool", value: "true", source: "database", stored: "true" }) }),
      "settings.unset": () => ({ changed: true, setting: view({ key: "daemon.two", type: "bool", value: "false", source: "default", stored: null }) }),
    });
    await openSetting("daemon.one");
    fireEvent.click(screen.getByRole("radio", { name: "true" }));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "settings.set", params: { key: "daemon.one", value: "true" } }));
    cleanup();
    resetFakeDaemon();
    const second = renderPanel({
      "settings.list": { settings: [view({ key: "daemon.two", type: "bool", value: "true", source: "database", stored: "true" })] },
      "settings.unset": () => ({ changed: true, setting: view({ key: "daemon.two", type: "bool", value: "false", source: "default", stored: null }) }),
    });
    await openSetting("daemon.two");
    fireEvent.click(screen.getByTestId("daemon-setting-reset"));
    await waitFor(() => expect(second.calls).toContainEqual({ method: "settings.unset", params: { key: "daemon.two" } }));
    const allowed = new Set(["settings.list", "settings.set", "settings.unset", "settings.get", "settings.history"]);
    expect([...daemon.methods(), ...second.methods()].filter((m) => !allowed.has(m))).toHaveLength(0);
  });

  it("shows an error panel on a failed list, never a blank panel", async () => {
    installFakeDaemon({
      responses: {
        "settings.list": () => {
          throw Object.assign(new Error("the daemon refused this request"), { code: -32000, kind: "rpc" });
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <DaemonSettingsPanel controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    // The message is shown; the panel is not blank.
    expect(await screen.findByText(/the daemon refused this request/i, {}, { timeout: 2000 })).toBeTruthy();
  });

  it("shows an empty state when there are no settings", async () => {
    renderPanel({ "settings.list": { settings: [] } });
    expect(await screen.findByText(/No daemon settings are declared yet/i)).toBeTruthy();
  });

  it("keeps the existing appearance panels intact", async () => {
    const live = { state: "connecting", endpoint: "/run/user/1000/aeon/rpc.sock" } as const;
    installFakeDaemon({ responses: {} });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <SettingsScreen controller={new LiveController()} live={live} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(await screen.findByText("Appearance")).toBeTruthy();
    expect(screen.getByText("Language")).toBeTruthy();
    expect(screen.getByText("Default tones")).toBeTruthy();
  });

  it("keeps the en and pt catalogs in parity for settings.daemon", () => {
    const keys = (node: unknown, prefix = ""): string[] => {
      if (typeof node !== "object" || node === null) return [prefix];
      return Object.entries(node).flatMap(([k, v]) => keys(v, `${prefix}.${k}`));
    };
    const enKeys = keys(en.settings?.daemon ?? {}).sort();
    const ptKeys = keys(pt.settings?.daemon ?? {}).sort();
    expect(enKeys).toEqual(ptKeys);
  });
});
