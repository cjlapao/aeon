import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { ProductApp } from "../src/app/router";

// The Home composer's Component, Flow and Provider selectors are the kit's `Picker` — a
// trigger button inside a labelled field. Queried by the field's own `data-testid` (not the
// label text or accessible name) so these tests don't ride on label copy that's still moving.

function component(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    id: "c1", name: "aeon-core-engine", slug: "aeon-core-engine", root: "/work/aeon-core-engine",
    icon: "", owner: "", remote: "", remoteKey: "", repoKey: "", branchPrefix: "", defaultBranch: "main",
    description: "", shortDescription: "", tags: [], projectId: "p1", createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z", ...overrides,
  };
}

function pickerButton(fieldTestId: string) {
  return within(screen.getByTestId(fieldTestId)).getByRole("button");
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  window.location.hash = "#/";
});
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("the home composer's pickers", () => {
  it("arrives with the component from ?component= already picked, and drops the parameter", async () => {
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [], all: true },
        "component.list": { components: [component(), component({ id: "c2", name: "auth-service", slug: "auth-service", root: "/work/auth-service" })] },
        "blueprint.list": { names: ["two-step"] }, "blueprint.validate": { diagnostics: [] },
        "routes.list": { routes: [], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    window.location.hash = "#/?component=%2Fwork%2Fauth-service";
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    await waitFor(() => expect(within(pickerButton("composer-component-field")).getByText("auth-service")).toBeTruthy());
    await waitFor(() => expect(window.location.hash).toBe("#/"));
  });

  it("picks a component, a flow and (from Advanced) a provider, then starts a loop", async () => {
    const daemon = installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [], all: true },
        "component.list": { components: [component(), component({ id: "c2", name: "auth-service", slug: "auth-service", root: "/work/auth-service" })] },
        "blueprint.list": { names: ["two-step"] },
        "blueprint.validate": { diagnostics: [] },
        "routes.list": { routes: [{ id: "replay", displayName: "Replay", provider: "anthropic", model: "claude" }], providers: [], persisted: true },
        "loop.start": { id: "l1", slug: "l1", status: "running" },
        "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Ship it", root: "/work/auth-service", status: "running",
          position: { lane: "survey" }, lanes: [{ id: "survey", status: "running", steps: [] }] },
        "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");

    // Component: open, pick the second row.
    fireEvent.click(pickerButton("composer-component-field"));
    fireEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: /auth-service/ }));
    await waitFor(() => expect(within(pickerButton("composer-component-field")).getByText("auth-service")).toBeTruthy());

    // Flow: only one, pick it.
    fireEvent.click(pickerButton("composer-flow-field"));
    fireEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: "two-step" }));
    await waitFor(() => expect(within(pickerButton("composer-flow-field")).getByText("two-step")).toBeTruthy());

    fireEvent.change(screen.getByPlaceholderText(/Describe a task/), { target: { value: "Ship it" } });

    // Provider lives behind Advanced, and already defaulted itself to "replay" before the
    // row is ever opened — opening it is just a look, not a requirement to launch.
    expect(screen.queryByTestId("composer-provider-field")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Advanced options ▾" }));
    await waitFor(() => expect(within(pickerButton("composer-provider-field")).getByText("Replay")).toBeTruthy());

    await waitFor(() => expect(screen.getByRole("button", { name: "Launch loop" }).hasAttribute("disabled")).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Launch loop" }));
    await waitFor(() => expect(daemon.paramsOf("loop.start")).toEqual({
      blueprint: "two-step", route: "replay", workspaceRoot: "/work/auth-service", goal: "Ship it", mode: "standard", attended: true,
    }));
  });

  it("offers thinking and effort in Advanced only for a route that has them, and sends the choice", async () => {
    const daemon = installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [], all: true },
        "component.list": { components: [component()] },
        "blueprint.list": { names: ["two-step"] }, "blueprint.validate": { diagnostics: [] },
        "routes.list": { routes: [{ id: "qwen", displayName: "Qwen", provider: "openai", model: "qwen", capabilities: ["thinking"], reasoningEfforts: ["low", "high"] },
          { id: "plain", displayName: "Plain", provider: "openai", model: "plain" }], providers: [], persisted: true },
        "loop.start": { id: "l1", slug: "l1", status: "running" },
        "loop.show": { id: "l1", slug: "l1", blueprint: "two-step", goal: "Ship it", root: "/work/aeon-core-engine", status: "running",
          position: { lane: "survey" }, lanes: [{ id: "survey", status: "running", steps: [] }] },
        "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    fireEvent.click(pickerButton("composer-component-field"));
    fireEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: /aeon-core-engine/ }));
    fireEvent.click(pickerButton("composer-flow-field"));
    fireEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: "two-step" }));
    fireEvent.change(screen.getByPlaceholderText(/Describe a task/), { target: { value: "Ship it" } });
    fireEvent.click(screen.getByRole("button", { name: "Advanced options ▾" }));
    // The first route (qwen) declares thinking and efforts: both controls show.
    await screen.findByTestId("composer-thinking-field");
    expect(screen.getByTestId("composer-effort-field")).toBeTruthy();
    const pick = (field: string, value: string) => fireEvent.change(within(screen.getByTestId(field)).getByRole("combobox"), { target: { value } });
    pick("composer-thinking-field", "off");
    pick("composer-effort-field", "low");
    fireEvent.click(screen.getByRole("button", { name: "Launch loop" }));
    await waitFor(() => expect(daemon.paramsOf("loop.start")).toMatchObject({ route: "qwen", thinking: "off", reasoningEffort: "low" }));
  });

  it("hides both controls for a route without them", async () => {
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [], all: true }, "component.list": { components: [component()] },
        "blueprint.list": { names: ["two-step"] }, "blueprint.validate": { diagnostics: [] },
        "routes.list": { routes: [{ id: "plain", displayName: "Plain", provider: "openai", model: "plain" }], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    fireEvent.click(screen.getByRole("button", { name: "Advanced options ▾" }));
    await screen.findByTestId("composer-provider-field");
    expect(screen.queryByTestId("composer-thinking-field")).toBeNull();
    expect(screen.queryByTestId("composer-effort-field")).toBeNull();
  });

  it("shows a flow's own diagnostics as a tag on its picker row", async () => {
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [], all: true }, "component.list": { components: [] },
        "blueprint.list": { names: ["broken"] },
        "blueprint.validate": { diagnostics: [{ state: "invalid", kind: "schema", path: "steps[0]" }] },
        "routes.list": { routes: [], providers: [], persisted: true },
        "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");
    fireEvent.click(pickerButton("composer-flow-field"));
    const row = await within(screen.getByRole("listbox")).findByRole("option", { name: /broken/ });
    expect(within(row).getByText("invalid")).toBeTruthy();
  });

  it("disables a picker and links to where it's fixed when its list is empty", async () => {
    installFakeDaemon({
      negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
      responses: {
        "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 },
      },
    });
    render(<ProductApp />);
    await screen.findByTestId("product-home");

    expect(pickerButton("composer-component-field")).toHaveProperty("disabled", true);
    expect(screen.getByText("No components yet.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add one in Projects" }).getAttribute("href")).toBe("#/projects");

    fireEvent.click(screen.getByRole("button", { name: "Advanced options ▾" }));
    await waitFor(() => expect(screen.getByTestId("composer-provider-field")).toBeTruthy());
    expect(pickerButton("composer-provider-field")).toHaveProperty("disabled", true);
    expect(screen.getByText("No providers configured yet.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add one in Models" }).getAttribute("href")).toBe("#/models");
  });
});
