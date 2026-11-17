import { afterEach, beforeEach, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ComponentsScreen } from "../src/features/components/ComponentsScreen";
import { LiveController } from "../src/lib/live";
import { appSettings } from "../src/lib/appSettings";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); window.location.hash = "#/loops"; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("S9 product surfaces", () => {
  it("groups recorded loops by workspace and opens one component's loop history", async () => {
    installFakeDaemon({ responses: { "component.list": { components: [] }, "loop.list": { all: true, loops: [
      { id: "a", slug: "a", title: "Discovery", componentRoot: "/tmp/repo-a", status: "done", phase: "survey", updatedAt: "2026-10-19T08:00:00Z" },
      { id: "b", slug: "b", title: "Security", componentRoot: "/tmp/repo-a", status: "blocked", phase: "review", updatedAt: "2026-10-19T09:00:00Z" },
      { id: "c", slug: "c", title: "Other", componentRoot: "/tmp/repo-b", status: "done", phase: "end", updatedAt: "2026-10-19T07:00:00Z" },
    ] } } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/components"]}><Routes><Route path="/components" element={<ComponentsScreen controller={new LiveController()} live={live} />} /></Routes></MemoryRouter>);
    await screen.findByText("2 loops");
    expect(screen.getByText("1 blocked")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Current loop activity" })).toBeNull();
    fireEvent.click(screen.getByRole("link", { name: /repo-a/ }));
    expect(await screen.findByText("Loops in this component")).toBeTruthy();
    expect(screen.getByText("Discovery")).toBeTruthy();
    expect(screen.getByText("Security")).toBeTruthy();
    expect(screen.queryByText("Other")).toBeNull();
    expect(screen.getByRole("link", { name: "Start a loop here" }).getAttribute("href")).toContain("componentRoot=%2Ftmp%2Frepo-a");
  });

  it("shows every active loop and lets a waiting gate answer from the components dashboard", async () => {
    const loops = [
      { id: "waiting", slug: "waiting", title: "Survey approval", componentRoot: "/tmp/qa", status: "waiting", phase: "approve", updatedAt: "2026-10-20T10:00:00Z" },
      { id: "running", slug: "running", title: "Build", componentRoot: "/tmp/build", status: "running", phase: "implement", updatedAt: "2026-10-20T09:00:00Z" },
    ];
    const daemon = installFakeDaemon({ responses: {
      "component.list": { components: [] }, "loop.list": { loops },
      "loop.show": (params: unknown) => {
        const id = (params as { id: string }).id;
        return { id, slug: id, blueprint: "two-step", goal: "Inspect the workspace", root: id === "waiting" ? "/tmp/qa" : "/tmp/build",
          status: id, position: { lane: id === "waiting" ? "approve" : "implement" },
          gateAnswers: id === "waiting" ? ["approve", "revise"] : [], gatePrompt: "Approve the survey?",
          lanes: [{ id: id === "waiting" ? "approve" : "implement", structure: "direct", status: id,
            steps: [{ id: id === "waiting" ? "approve" : "implement", kind: id === "waiting" ? "gate" : "model-step", status: id }] }] };
      },
      "loop.answer": { accepted: true },
    } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/components"]}><Routes><Route path="/components"
      element={<ComponentsScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    await waitFor(() => expect(screen.getAllByTestId("active-loop-workflow")).toHaveLength(2));
    const stats = screen.getByTestId("component-stats");
    expect(stats.style.gridTemplateColumns).toContain("auto-fit");
    const tiles = within(stats).getAllByTestId("component-metric");
    expect(tiles).toHaveLength(5);
    expect(tiles.map((tile) => tile.getAttribute("data-tone"))).toEqual([
      appSettings.getSnapshot().controlTone, appSettings.getSnapshot().controlTone,
      appSettings.getSnapshot().controlTone, "red", appSettings.getSnapshot().controlTone,
    ]);
    act(() => appSettings.set({ controlTone: "violet" }));
    expect(tiles.map((tile) => tile.getAttribute("data-tone"))).toEqual(["violet", "violet", "violet", "red", "violet"]);
    expect(screen.getAllByTestId("active-loop-card")).toHaveLength(2);
    expect(screen.getAllByTestId("component-card")).toHaveLength(2);
    const waiting = screen.getAllByTestId("active-loop-card")[0]!;
    fireEvent.click(within(waiting).getAllByRole("button", { name: "Approve" })[0]!);
    await waitFor(() => expect(daemon.paramsOf("loop.answer")).toEqual({ id: "waiting", answer: "approve" }));
    fireEvent.click(screen.getByRole("button", { name: "Attention 1" }));
    expect(screen.getAllByTestId("component-card")).toHaveLength(1);
    expect(screen.getAllByTestId("active-loop-card")).toHaveLength(2);
    act(() => appSettings.reset());
  });

  it("creates and displays a saved component before its first loop", async () => {
    const created = { id: "p1", name: "Aeon QA", slug: "aeon-qa", root: "/tmp/aeon-qa-workspace",
      remote: "https://example.test/aeon.git", remoteKey: "origin", repoKey: "aeon",
      owner: "Platform", icon: "", tags: ["go", "qa"], createdAt: "2026-10-20T09:00:00Z", updatedAt: "2026-10-20T09:00:00Z" };
    const daemon = installFakeDaemon({ responses: {
      "component.list": { components: [] }, "loop.list": { loops: [] }, "component.create": created,
    } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/components"]}><Routes><Route path="/components"
      element={<ComponentsScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    expect(await screen.findByText("No components yet")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "New component" }));
    expect(screen.getByRole("heading", { name: "Component details" })).toBeTruthy();
    const resize = screen.getByRole("separator", { name: "Resize panel" });
    const initialWidth = Number(resize.getAttribute("aria-valuenow"));
    fireEvent.keyDown(resize, { key: "ArrowLeft" });
    expect(Number(resize.getAttribute("aria-valuenow"))).toBe(initialWidth + 20);
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Aeon QA" } });
    expect((screen.getByRole("textbox", { name: "Slug" }) as HTMLInputElement).value).toBe("aeon-qa");
    fireEvent.change(screen.getByRole("textbox", { name: "Workspace root" }), { target: { value: created.root } });
    fireEvent.change(screen.getByRole("textbox", { name: /Remote URL/ }), { target: { value: created.remote } });
    fireEvent.change(screen.getByRole("textbox", { name: /Owner/ }), { target: { value: created.owner } });
    fireEvent.change(screen.getByRole("textbox", { name: /Tags/ }), { target: { value: "go, qa, go" } });
    fireEvent.click(screen.getByText("Advanced identity fields"));
    fireEvent.change(screen.getByRole("textbox", { name: /Remote key/ }), { target: { value: created.remoteKey } });
    fireEvent.change(screen.getByRole("textbox", { name: /Repository key/ }), { target: { value: created.repoKey } });
    fireEvent.click(screen.getByRole("button", { name: "Create component" }));
    await waitFor(() => expect(daemon.paramsOf("component.create")).toEqual({ name: created.name, slug: created.slug,
      root: created.root, remote: created.remote, remoteKey: created.remoteKey, repoKey: created.repoKey,
      owner: created.owner, icon: "", tags: ["go", "qa"] }));
    expect(await screen.findByText("No loops yet")).toBeTruthy();
    expect(screen.getByText(created.remote)).toBeTruthy();
    expect(screen.getByText("go, qa")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Start a loop here" }).getAttribute("href")).toContain("componentRoot=%2Ftmp%2Faeon-qa-workspace");
  });

  it("dismisses the component side panel without creating a component", async () => {
    const daemon = installFakeDaemon({ responses: { "component.list": { components: [] }, "loop.list": { loops: [] } } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/components"]}><Routes><Route path="/components"
      element={<ComponentsScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    await screen.findByText("No components yet");
    fireEvent.click(screen.getByRole("button", { name: "New component" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Discard me" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByRole("button", { name: "New component" })).toBeTruthy();
    expect(daemon.methods()).not.toContain("component.create");
    fireEvent.click(screen.getByRole("button", { name: "New component" }));
    expect((screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("");
  });

  it("edits component metadata and icon without changing its workspace identity", async () => {
    const saved = { id: "p1", name: "Aeon QA", slug: "aeon-qa", root: "/tmp/aeon-qa-workspace",
      remote: "", remoteKey: "", repoKey: "", owner: "", icon: "", tags: ["qa"],
      createdAt: "2026-10-20T09:00:00Z", updatedAt: "2026-10-20T09:00:00Z" };
    const daemon = installFakeDaemon({ responses: { "component.list": { components: [saved] }, "loop.list": { loops: [] },
      "component.update": (params: unknown) => ({ ...saved, ...(params as object), updatedAt: "2026-10-20T10:00:00Z" }) } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={[`/components?root=${encodeURIComponent(saved.root)}`]}><Routes><Route path="/components"
      element={<ComponentsScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    expect(await screen.findByText("AQ")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit component" }));
    expect(screen.getAllByText(saved.root).length).toBeGreaterThan(1);
    expect(screen.queryByRole("textbox", { name: "Workspace root" })).toBeNull();
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Aeon Platform" } });
    fireEvent.change(screen.getByRole("textbox", { name: /Icon emoji/ }), { target: { value: "🚀" } });
    fireEvent.change(screen.getByRole("textbox", { name: /Owner/ }), { target: { value: "Platform" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(daemon.paramsOf("component.update")).toEqual({ id: saved.id, name: "Aeon Platform",
      slug: saved.slug, remote: "", remoteKey: "", repoKey: "", owner: "Platform", icon: "🚀", tags: ["qa"] }));
    expect(await screen.findByRole("heading", { name: "Aeon Platform" })).toBeTruthy();
    expect(screen.getAllByText("🚀").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "Start a loop here" }).getAttribute("href")).toContain("componentRoot=%2Ftmp%2Faeon-qa-workspace");
    fireEvent.click(screen.getByRole("button", { name: "Edit component" }));
    fireEvent.click(screen.getByRole("button", { name: "Use initials" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect((daemon.paramsOf("component.update") as { icon: string }).icon).toBe(""));
    expect((await screen.findAllByText("AP")).length).toBeGreaterThan(0);
  });

  it("uploads a small component icon as data in the create request", async () => {
    const daemon = installFakeDaemon({ responses: { "component.list": { components: [] }, "loop.list": { loops: [] },
      "component.create": (params: unknown) => ({ ...(params as object), id: "p2", createdAt: "2026-10-20T09:00:00Z", updatedAt: "2026-10-20T09:00:00Z" }) } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={["/components"]}><Routes><Route path="/components"
      element={<ComponentsScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    expect(await screen.findByText("No components yet")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "New component" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Icon Component" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Workspace root" }), { target: { value: "/tmp/icon-component" } });
    const file = new File([new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])], "mark.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Upload image"), { target: { files: [file] } });
    expect(screen.getByRole("button", { name: "Loading image…" }).hasAttribute("disabled")).toBe(true);
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toMatch(/^data:image\/png;base64,/));
    fireEvent.click(screen.getByRole("button", { name: "Create component" }));
    await waitFor(() => expect((daemon.paramsOf("component.create") as { icon: string }).icon).toMatch(/^data:image\/png;base64,/));
  });

  it("saves an uploaded icon when optional component fields are absent from the daemon response", async () => {
    const saved = { id: "p3", name: "Bare component", slug: "bare-component", root: "/tmp/bare-component",
      icon: "", tags: [], createdAt: "2026-10-20T09:00:00Z", updatedAt: "2026-10-20T09:00:00Z" };
    const daemon = installFakeDaemon({ responses: { "component.list": { components: [saved] }, "loop.list": { loops: [] },
      "component.update": (params: unknown) => ({ ...saved, ...(params as object) }) } });
    const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
    render(<MemoryRouter initialEntries={[`/components?root=${encodeURIComponent(saved.root)}`]}><Routes><Route path="/components"
      element={<ComponentsScreen controller={new LiveController()} live={live} canWrite />} /></Routes></MemoryRouter>);
    await screen.findByText("Bare component");
    fireEvent.click(screen.getByRole("button", { name: "Edit component" }));
    const tooLarge = new File([new Uint8Array(256 * 1024 + 1)], "large.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Upload image"), { target: { files: [tooLarge] } });
    expect(screen.getByRole("alert").textContent).toContain("no larger than 256 KiB");
    const file = new File([new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])], "mark.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Upload image"), { target: { files: [file] } });
    await waitFor(() => expect(document.querySelector("img")?.getAttribute("src")).toMatch(/^data:image\/png;base64,/));
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(daemon.paramsOf("component.update")).toMatchObject({
      id: saved.id, remote: "", owner: "", remoteKey: "", repoKey: "",
      icon: expect.stringMatching(/^data:image\/png;base64,/),
    }));
  });
});
