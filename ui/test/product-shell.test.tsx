import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { LiveController } from "../src/lib/live";
import { ProductApp } from "../src/app/router";
import { SettingsScreen } from "../src/features/settings/SettingsScreen";
import { registerConfigPage } from "../src/features/config/lib/configRegistry";
import { appSettings, startAppSettings } from "../src/lib/appSettings";
import { StrictMode } from "react";
import { backupStatus, installFakeDaemon, resetFakeDaemon, chooseAppMenu } from "./helpers";

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); window.location.hash = "#/work"; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("S9 product surfaces", () => {
  it("switches light, dark and system themes and saves the choice", () => {
    const original = window.matchMedia;
    const listeners = new Set<() => void>();
    let prefersDark = true;
    window.matchMedia = vi.fn().mockImplementation(() => ({ get matches() { return prefersDark; },
      addEventListener: (_: string, listener: () => void) => listeners.add(listener),
      removeEventListener: (_: string, listener: () => void) => listeners.delete(listener) }));
    window.localStorage.removeItem("aeon-app-settings-v1");
    act(() => appSettings.reset());
    const stop = startAppSettings();
    try {
      render(<MemoryRouter><SettingsScreen controller={new LiveController()} live={{ state: "connecting", endpoint: "unknown" }} /></MemoryRouter>);
      expect(document.documentElement.classList.contains("dark")).toBe(true);
      fireEvent.click(screen.getByRole("radio", { name: "Light" }));
      expect(document.documentElement.classList.contains("dark")).toBe(false);
      expect(JSON.parse(window.localStorage.getItem("aeon-app-settings-v1") || "{}").theme).toBe("light");
      fireEvent.click(screen.getByRole("radio", { name: "Dark" }));
      expect(document.documentElement.classList.contains("dark")).toBe(true);
      fireEvent.click(screen.getByRole("radio", { name: "System" }));
      prefersDark = false;
      act(() => listeners.forEach((listener) => listener()));
      expect(document.documentElement.classList.contains("dark")).toBe(false);
      expect(JSON.parse(window.localStorage.getItem("aeon-app-settings-v1") || "{}").theme).toBe("system");
    } finally {
      cleanup(); stop(); window.matchMedia = original; window.localStorage.removeItem("aeon-app-settings-v1");
      appSettings.reset();
      document.documentElement.classList.remove("dark");
    }
  });

  it("applies selected control, text and surface tones across kit controls", () => {
    appSettings.reset();
    render(<MemoryRouter><SettingsScreen controller={new LiveController()} live={{ state: "connecting", endpoint: "unknown" }} /></MemoryRouter>);
    fireEvent.click(within(screen.getByRole("group", { name: "Controls" })).getByRole("button", { name: "red" }));
    fireEvent.click(within(screen.getByRole("group", { name: "Text" })).getByRole("button", { name: "blue" }));
    fireEvent.click(within(screen.getByRole("group", { name: "Surfaces" })).getByRole("button", { name: "amber" }));
    expect(appSettings.getSnapshot()).toMatchObject({ controlTone: "red", textTone: "blue", surfaceTone: "amber" });
    expect(screen.getByRole("button", { name: "Primary action" }).getAttribute("data-color")).toBe("red");
    expect(document.documentElement.dataset.textTone).toBe("blue");
    expect(document.documentElement.dataset.surfaceTone).toBe("amber");
    cleanup(); appSettings.reset();
  });

  it("opens the product loops view and keeps the test bench available", async () => {
    installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "blueprint.list": { names: ["two-step"] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 },
        // The settings screen (opened via the user menu below) mounts the
        // backups and seed panels, which render contract-required structs the
        // fake's generic `{}` fallback cannot stand in for — see the fixture's
        // own doc comment in helpers.
        "backup.status": backupStatus(), "seed.status": { sets: [] } } });
    render(<ProductApp />);
    expect(await screen.findByTestId("work-screen")).toBeTruthy();
    // The Aeon mark, as the Logo component mounts it. The flag is Logo's own
    // contract (one img, picked once); the asset itself is inlined as a data
    // URI under vitest, so its file name is not a stable thing to assert on.
    expect(document.querySelector("img[data-animated]")).toBeTruthy();
    for (const label of ["Work", "Flows", "System", "Test bench"]) {
      expect(screen.getAllByRole("link", { name: label }).length).toBeGreaterThan(0);
    }
    // Configuration is no longer an App-group rail link: it rides the footer
    // section beside the daemon-status LED, as a button (asserted on `rail`
    // below, where the footer lives).
    expect(screen.queryByRole("link", { name: "Configuration" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Settings" })).toBeNull();
    expect(screen.getByRole("button", { name: "User menu" }).getAttribute("aria-expanded")).toBe("false");
    const page = screen.getByRole("region", { name: "Page content" });
    expect(page.getAttribute("data-slot")).toBe("scrollarea");
    expect(page.querySelector('[data-slot="scrollarea-viewport"]')?.contains(screen.getByTestId("work-screen"))).toBe(true);
    // S41's joined-menu shell retired the header banner and its wordmark
    // link (the wordmark now rides the splash): the chrome is the rail —
    // the aside carries the mark, and the daemon status sits in its footer.
    const rail = screen.getAllByRole("complementary")[0]!;
    expect(rail.contains(document.querySelector("img[data-animated]"))).toBe(true);
    expect(rail.contains(screen.getByTestId("daemon-status-trigger"))).toBe(true);
    // The Configuration entry sits in that same footer section, beside the LED.
    expect(rail.contains(screen.getByTestId("configuration-entry"))).toBe(true);
    chooseAppMenu("Settings");
    expect(await screen.findByTestId("product-settings")).toBeTruthy();
    expect(screen.queryByRole("menu", { name: "User menu" })).toBeNull();
    // The theme radios belong to Settings' ThemeControl, never to the rail
    // chrome that once carried a header copy of them.
    expect(within(rail).queryByRole("radio", { name: "Light" })).toBeNull();
    fireEvent.click(within(screen.getByRole("group", { name: "Controls" })).getByRole("button", { name: "red" }));
    chooseAppMenu("System");
    expect(await screen.findByTestId("product-system")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("link", { name: "Work" })[0]!);
    expect(await screen.findByTestId("work-screen")).toBeTruthy();
    expect(document.documentElement.dataset.controlTone).toBe("red");
    act(() => appSettings.reset());
  });

  it("opens the product home view by default, with the composer and a link into Work", async () => {
    window.location.hash = "#/";
    installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<ProductApp />);
    expect(await screen.findByTestId("product-home")).toBeTruthy();
    expect(screen.getByText("What should we build?")).toBeTruthy();
    expect(screen.getByText(/Aeon Engine v1\.0/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Home" })).toBeTruthy();
    fireEvent.click(screen.getAllByRole("link", { name: "Work" })[0]!);
    expect(await screen.findByTestId("work-screen")).toBeTruthy();
  });

  it("closes the mobile menu after navigating while keeping one page viewport", async () => {
    const original = window.matchMedia;
    window.matchMedia = vi.fn().mockImplementation(() => ({ matches: true,
      addEventListener: () => {}, removeEventListener: () => {} }));
    try {
      installFakeDaemon({ responses: { "loop.list": { loops: [], all: true },
        "blueprint.list": { names: [] }, "routes.list": { routes: [], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 }, "backup.status": backupStatus(),
        "seed.status": { sets: [] } } });
      render(<ProductApp />);
      await screen.findByTestId("work-screen");
      const menuButton = screen.getByRole("button", { name: "Open menu" });
      const drawer = document.querySelector("[data-joined-menu]") as HTMLElement;
      expect(drawer.hasAttribute("inert")).toBe(true);
      fireEvent.click(menuButton);
      await waitFor(() => expect(drawer.hasAttribute("inert")).toBe(false));
      fireEvent.click(within(drawer).getByTestId("daemon-status-trigger"));
      expect(await screen.findByTestId("daemon-status-state")).toHaveProperty("textContent", "Connected");
      chooseAppMenu("Settings");
      expect(await screen.findByTestId("product-settings")).toBeTruthy();
      await waitFor(() => expect(drawer.hasAttribute("inert")).toBe(true));
      expect(screen.getAllByRole("main")).toHaveLength(1);
      expect(screen.getAllByRole("region", { name: "Page content" })).toHaveLength(1);
    } finally {
      cleanup(); window.matchMedia = original;
    }
  });

  it("opens the user menu from the keyboard and dismisses it without navigating", async () => {
    installFakeDaemon({ responses: { subscribe: { subscriptionId: "s1", from: 0 } } });
    render(<ProductApp />);
    await screen.findByTestId("work-screen");
    const avatar = screen.getByRole("button", { name: "User menu" });
    avatar.focus();
    fireEvent.keyDown(avatar, { key: "ArrowDown" });
    const settings = await screen.findByRole("menuitem", { name: "Settings" });
    expect(screen.getByRole("menu", { name: "User menu" })).toBeTruthy();
    expect(avatar.getAttribute("aria-expanded")).toBe("true");
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Models" })));
    fireEvent.keyDown(settings, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(avatar);
    expect(screen.getByTestId("work-screen")).toBeTruthy();
    fireEvent.click(avatar);
    expect(screen.getByRole("menu", { name: "User menu" })).toBeTruthy();
    fireEvent.pointerDown(screen.getByRole("region", { name: "Page content" }));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("keeps a direct Settings URL available with the user menu closed", async () => {
    installFakeDaemon({ responses: { subscribe: { subscriptionId: "s1", from: 0 }, "backup.status": backupStatus(), "seed.status": { sets: [] } } });
    window.location.hash = "#/settings";
    render(<ProductApp />);
    expect(await screen.findByTestId("product-settings")).toBeTruthy();
    expect(screen.getByRole("button", { name: "User menu" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.queryByRole("link", { name: "Settings" })).toBeNull();
  });

  it("restores a current product connection under React StrictMode", async () => {
    const daemon = installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<StrictMode><ProductApp /></StrictMode>);
    await waitFor(() => expect(daemon.countOf("connect")).toBeGreaterThan(0));
    // The rail's status button carries the state in its accessible name
    // ("Daemon: Connected"); S41 moved the old footer words into
    // DaemonStatusButton's labels. The stale state is set synchronously on
    // close, so "Reconnecting" is present before the first retry fires.
    await waitFor(() => expect(screen.getByRole("button", { name: "Daemon: Connected" })).toBeTruthy());
    act(() => daemon.close("test drop"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Daemon: Reconnecting" })).toBeTruthy());
  });

  it("opens the test bench on the product session without a second connect", async () => {
    const daemon = installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<ProductApp />);
    await screen.findByTestId("work-screen");
    const viewport = screen.getByRole("region", { name: "Page content" }).querySelector('[data-slot="scrollarea-viewport"]') as HTMLElement;
    viewport.scrollTop = 320;
    const before = daemon.countOf("connect");
    const openDaemonStatus = () => { if (!screen.queryByTestId("daemon-status-panel")) fireEvent.click(screen.getByTestId("daemon-status-trigger")); };
    openDaemonStatus();
    expect(screen.getByTestId("daemon-status-state").textContent).toBe("Connected");
    expect(screen.getByTestId("daemon-status-version").textContent).toBe("1.0");
    expect(screen.getByTestId("daemon-status-protocol").textContent).toBe("1");
    chooseAppMenu("Test bench");
    expect(await screen.findByTestId("test-bench-badge")).toBeTruthy();
    openDaemonStatus();
    expect(screen.getByTestId("daemon-status-state").textContent).toBe("Connected");
    expect(screen.getByTestId("daemon-status-version").textContent).toBe("1.0");
    expect(screen.getByRole("region", { name: "Page content" }).contains(screen.getByTestId("test-bench-badge"))).toBe(true);
    const nextViewport = screen.getByRole("region", { name: "Page content" }).querySelector('[data-slot="scrollarea-viewport"]') as HTMLElement;
    expect(nextViewport).not.toBe(viewport);
    expect(nextViewport.scrollTop).toBe(0);
    expect(screen.getAllByRole("main")).toHaveLength(1);
    expect(daemon.countOf("connect")).toBe(before);
  });

  it("opens a saved bench page and navigates through the secondary menu", async () => {
    const daemon = installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 },
        "proposal.list": { proposals: [] }, "mcp.status": { configPath: "/x/mcp.json", present: false }, "policy.list": { policies: [] } } });
    window.location.hash = "#/bench/providers";
    render(<ProductApp />);
    expect(await screen.findByTestId("test-bench-badge")).toBeTruthy();
    expect(window.location.hash).toBe("#/bench/providers");
    expect(screen.queryByRole("tablist")).toBeNull();
    // S34 T5 / S35 T8: Agents/MCP/Proposals sit immediately after Policy
    // (TB11/TD11 — the order is the authorization story: Policy → Agents →
    // MCP → Proposals).
    const labels = ["Status", "Providers", "Ask", "Chat", "Store", "Step", "Recovery", "Blueprints", "Policy", "Agents", "MCP", "Proposals", "Files", "Agent chat", "Notifications", "Demos"];
    const paths = ["/bench", "/bench/providers", "/bench/ask", "/bench/chat", "/bench/store", "/bench/step", "/bench/recovery", "/bench/blueprints", "/bench/policy", "/bench/agents", "/bench/mcp", "/bench/proposals", "/bench/files", "/bench/agent-chat", "/bench/notifications", "/bench/demos"];
    for (const [index, label] of labels.entries()) {
      const link = screen.getAllByRole("link", { name: label }).find((item) => item.getAttribute("href") === `#${paths[index]}`);
      expect(link, label).toBeTruthy();
    }
    // TestBenchMenuHasOneTrustGroupAboveFourRows: the "Trust" group label
    // renders exactly once, immediately above Policy/Agents/MCP/Proposals
    // in that order — a DOM-order assertion, not just presence.
    // getByText throws unless exactly one match exists, which is the
    // "exactly once" half of the assertion.
    const trustGroupLabel = screen.getByText("Trust");
    const trustRowOrder = ["Policy", "Agents", "MCP", "Proposals"].map((label) => screen.getByRole("link", { name: label }));
    // The group label's position in the document precedes all four rows,
    // and no non-Trust tab sits between the group label and the fourth row.
    for (const row of trustRowOrder) {
      expect(trustGroupLabel.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    // TestBenchMenuOtherEightTabsStillFlat: none of the pre-existing tabs
    // carry a group header anywhere near them — exactly one "Trust" text
    // node exists in the whole menu (asserted above), so a tab outside the
    // four cannot have picked up a stray header.
    for (const label of ["Status", "Providers", "Ask", "Chat", "Store", "Step", "Recovery", "Blueprints"]) {
      const row = screen.getByRole("link", { name: label });
      expect(row.previousElementSibling?.textContent?.trim()).not.toBe("Trust");
    }
    expect(screen.getByRole("link", { name: "Providers" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Test bench" }).getAttribute("aria-current")).toBe("page");
    fireEvent.click(await screen.findByRole("link", { name: "Store" }));
    await waitFor(() => expect(window.location.hash).toBe("#/bench/store"));
    expect(screen.getByRole("link", { name: "Store" }).getAttribute("aria-current")).toBe("page");
    act(() => window.history.back());
    await waitFor(() => expect(screen.getByRole("link", { name: "Providers" }).getAttribute("aria-current")).toBe("page"));
    act(() => window.history.forward());
    await waitFor(() => expect(screen.getByRole("link", { name: "Store" }).getAttribute("aria-current")).toBe("page"));
    fireEvent.click(screen.getByRole("link", { name: "Status" }));
    await waitFor(() => expect(window.location.hash).toBe("#/bench"));
    expect(screen.getByRole("link", { name: "Status" }).getAttribute("aria-current")).toBe("page");
    // S34 T5 / S35 T8: Agents, then MCP, then Proposals navigate the same
    // way Store did above.
    fireEvent.click(screen.getByRole("link", { name: "Agents" }));
    await waitFor(() => expect(window.location.hash).toBe("#/bench/agents"));
    expect(screen.getByRole("link", { name: "Agents" }).getAttribute("aria-current")).toBe("page");
    fireEvent.click(screen.getByRole("link", { name: "MCP" }));
    await waitFor(() => expect(window.location.hash).toBe("#/bench/mcp"));
    expect(screen.getByRole("link", { name: "MCP" }).getAttribute("aria-current")).toBe("page");
    fireEvent.click(screen.getByRole("link", { name: "Proposals" }));
    await waitFor(() => expect(window.location.hash).toBe("#/bench/proposals"));
    expect(screen.getByRole("link", { name: "Proposals" }).getAttribute("aria-current")).toBe("page");
    expect(daemon.countOf("connect")).toBe(1);
  });

  it.each(["/bench/status", "/bench/missing"])("handles the saved bench page %s", async (path) => {
    installFakeDaemon({ responses: { subscribe: { subscriptionId: "s1", from: 0 } } });
    window.location.hash = `#${path}`;
    render(<ProductApp />);
    if (path === "/bench/status") {
      expect(await screen.findByTestId("connection")).toBeTruthy();
      expect(screen.getByRole("link", { name: "Status" }).getAttribute("aria-current")).toBe("page");
    } else {
      expect(await screen.findByRole("heading", { name: "Page not found" })).toBeTruthy();
      expect(screen.getByRole("link", { name: "Status" }).getAttribute("aria-current")).toBeNull();
    }
  });

  it("opens Configuration on the first ready page with a grouped panel and an inert planned row", async () => {
    installFakeDaemon({ negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: { "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 },
        "settings.list": { settings: [] } } });
    // No shipped page is planned any more (Skills landed), so the inert-row behaviour is
    // exercised with a planned row of its own.
    registerConfigPage({ id: "demo-planned", label: "Demo planned", group: "access", order: 900, status: "planned", summary: "Its UI is not built yet." });
    window.location.hash = "#/config";
    render(<ProductApp />);
    // Acceptance 1: the entry (now the footer button beside the LED, not a
    // rail link) opens /config, the panel shows, and the first ready page —
    // daemon-settings, general order 10 — is selected.
    expect(await screen.findByTestId("daemon-settings-panel")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Configuration" }).getAttribute("aria-current")).toBe("page");
    // CF4: one group header per group that has pages, each above its own
    // links. Scoped to the secondary panel — the page frame's own breadcrumb
    // line also carries the group word.
    const panel = screen.getAllByRole("complementary").find((aside) => aside.textContent?.includes("Demo planned")) as HTMLElement;
    expect(panel).toBeTruthy();
    expect(within(panel).getByText("General")).toBeTruthy();
    expect(within(panel).getByText("Agents & access")).toBeTruthy();
    const daemonLink = screen.getByRole("link", { name: "Daemon settings" });
    expect(daemonLink.getAttribute("href")).toBe("#/config/daemon-settings");
    expect(daemonLink.getAttribute("aria-current")).toBe("page");
    // Acceptance 5 / CF4 Q1: the planned row is never clickable — the kit has
    // no disabled link, so its click is swallowed (onItemSelect cancels it
    // before the router acts) — and its row says why. A deep link still tells
    // the truth (CF5's planned state), never a Home detour.
    expect(within(panel).getByText("planned")).toBeTruthy();
    const skillsRow = within(panel).getByRole("link", { name: /Demo planned/ });
    fireEvent.click(skillsRow);
    expect(window.location.hash).toBe("#/config/daemon-settings"); // inert: no navigation
    act(() => { window.location.hash = "#/config/demo-planned"; });
    expect(await screen.findByTestId("config-page-missing")).toBeTruthy();
    expect(screen.getByText(/Demo planned is planned — its page is not wired yet/)).toBeTruthy();
    // Unknown pages still show the shell, never a blank (CF4's explicit slug,
    // CF5's not-found): no panel row highlights.
    act(() => { window.location.hash = "#/config/no-such-page"; });
    expect(await screen.findByTestId("config-page-missing")).toBeTruthy();
    expect(daemonLink.getAttribute("aria-current")).toBeNull();
  });

  it("shows an unknown saved URL inside the product shell", async () => {
    installFakeDaemon({ responses: { "subscribe": { subscriptionId: "s1", from: 0 } } });
    window.location.hash = "#/does-not-exist";
    render(<ProductApp />);
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Page content" }).contains(screen.getByRole("heading", { name: "Page not found" }))).toBe(true);
    fireEvent.click(screen.getByRole("link", { name: /Back to Work/ }));
    expect(await screen.findByTestId("work-screen")).toBeTruthy();
  });
});
