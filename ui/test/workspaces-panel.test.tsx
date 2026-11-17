import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { WorkspacesPanel } from "../src/features/system/WorkspacesPanel";
import { resetBridge, setInvoker } from "../src/lib/bridge";

type Workspace = { id: string; name: string; kind: "local" | "remote"; host?: string; port?: number };

function installFakeShell(initial: { workspaces: Workspace[]; activeId: string }) {
  const state = { workspaces: initial.workspaces, activeId: initial.activeId };
  const calls: { cmd: string; args: Record<string, unknown> }[] = [];
  setInvoker(async (cmd, args) => {
    calls.push({ cmd, args: args ?? {} });
    switch (cmd) {
      case "workspace_list":
        return state.workspaces;
      case "workspace_active":
        return state.workspaces.find((w) => w.id === state.activeId);
      case "workspace_add": {
        const { name, host, port } = args as { name: string; host: string; port: number };
        const added: Workspace = { id: `remote-${state.workspaces.length}`, name, kind: "remote", host, port };
        state.workspaces.push(added);
        return added;
      }
      case "workspace_remove": {
        const { id } = args as { id: string };
        state.workspaces = state.workspaces.filter((w) => w.id !== id);
        if (state.activeId === id) state.activeId = "local";
        return null;
      }
      case "workspace_select": {
        const { id } = args as { id: string };
        state.activeId = id;
        return state.workspaces.find((w) => w.id === id);
      }
      default:
        throw new Error(`unexpected ${cmd}`);
    }
  });
  return { calls, state };
}

beforeEach(() => {
  Object.defineProperty(window, "location", { value: { ...window.location, reload: vi.fn() }, writable: true });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); resetBridge(); vi.unstubAllGlobals(); });

describe("the workspaces panel", () => {
  it("lists Local as active by default", async () => {
    installFakeShell({ workspaces: [{ id: "local", name: "Local", kind: "local" }], activeId: "local" });
    render(<WorkspacesPanel />);
    expect(await screen.findByText("Local")).toBeTruthy();
    expect(screen.getByText("active")).toBeTruthy();
    expect(screen.getByText("this machine's bundled daemon")).toBeTruthy();
  });

  it("adds a remote workspace and lists it", async () => {
    installFakeShell({ workspaces: [{ id: "local", name: "Local", kind: "local" }], activeId: "local" });
    render(<WorkspacesPanel />);
    await screen.findByText("Local");

    fireEvent.change(screen.getByRole("textbox", { name: "Workspace name" }), { target: { value: "Dev box" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Workspace host" }), { target: { value: "192.168.1.50" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Workspace port" }), { target: { value: "7777" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByText("Dev box")).toBeTruthy();
    expect(screen.getByText("192.168.1.50:7777")).toBeTruthy();
  });

  it("refuses to add without a valid port, naming what's wrong", async () => {
    installFakeShell({ workspaces: [{ id: "local", name: "Local", kind: "local" }], activeId: "local" });
    render(<WorkspacesPanel />);
    await screen.findByText("Local");

    fireEvent.change(screen.getByRole("textbox", { name: "Workspace name" }), { target: { value: "Dev box" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Workspace host" }), { target: { value: "192.168.1.50" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect((await screen.findByRole("alert")).textContent).toMatch(/port/);
  });

  it("switching to a workspace selects it on the shell and hard-reloads", async () => {
    const { calls } = installFakeShell({
      workspaces: [{ id: "local", name: "Local", kind: "local" }, { id: "remote-1", name: "Dev box", kind: "remote", host: "h", port: 1 }],
      activeId: "local",
    });
    render(<WorkspacesPanel />);
    await screen.findByText("Dev box");

    fireEvent.click(screen.getByRole("button", { name: "Switch to this" }));

    await waitFor(() => expect(window.location.reload).toHaveBeenCalledTimes(1));
    expect(calls.some((c) => c.cmd === "workspace_select" && c.args.id === "remote-1")).toBe(true);
  });

  it("removing a remote workspace asks for its name to be typed, and refuses until it matches", async () => {
    installFakeShell({
      workspaces: [{ id: "local", name: "Local", kind: "local" }, { id: "remote-1", name: "Dev box", kind: "remote", host: "h", port: 1 }],
      activeId: "local",
    });
    render(<WorkspacesPanel />);
    await screen.findByText("Dev box");

    fireEvent.click(screen.getByRole("button", { name: "Remove Dev box" }));
    const dialog = within(await screen.findByRole("alertdialog"));
    const confirmButton = dialog.getByRole("button", { name: "Remove" });
    const typeField = dialog.getByPlaceholderText("Dev box");

    // Wrong text keeps the destructive button disabled.
    fireEvent.change(typeField, { target: { value: "not the name" } });
    expect(confirmButton).toHaveProperty("disabled", true);

    fireEvent.change(typeField, { target: { value: "Dev box" } });
    expect(confirmButton).toHaveProperty("disabled", false);
    fireEvent.click(confirmButton);

    await waitFor(() => expect(screen.queryByText("Dev box")).toBeNull());
  });

  it("cancelling the confirm leaves the workspace in place", async () => {
    installFakeShell({
      workspaces: [{ id: "local", name: "Local", kind: "local" }, { id: "remote-1", name: "Dev box", kind: "remote", host: "h", port: 1 }],
      activeId: "local",
    });
    render(<WorkspacesPanel />);
    await screen.findByText("Dev box");

    fireEvent.click(screen.getByRole("button", { name: "Remove Dev box" }));
    const dialog = within(await screen.findByRole("alertdialog"));
    fireEvent.click(dialog.getByRole("button", { name: "Cancel" }));

    expect(screen.getByText("Dev box")).toBeTruthy();
  });

  it("reports a failure to load without crashing", async () => {
    setInvoker(async () => { throw new Error("not connected to the daemon"); });
    render(<WorkspacesPanel />);
    expect((await screen.findByRole("alert")).textContent).toMatch(/not connected/);
  });
});
