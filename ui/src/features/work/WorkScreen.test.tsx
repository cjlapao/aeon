import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../lib/i18n";
import { LiveController } from "../../lib/live";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { WorkScreen } from "./WorkScreen";
import { LegacyRedirect } from "./LegacyRedirect";

const live = { state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 } as const;
const comp = (id: string, name: string, root: string, parentId = "") => ({ id, name, slug: id, root, parentId, shortDescription: `${name} desc` });
const loop = (id: string, root: string, status: string) => ({ id, slug: id, title: `loop ${id}`, componentRoot: root, status, phase: "implement", updatedAt: "2026-11-14T08:00:00Z", startedAt: "2026-11-14T07:00:00Z", awaiting: false, mode: "standard" });

function Where() { const l = useLocation(); return <div data-testid="where">{l.pathname}{l.search}</div>; }
function mount(path: string) {
  installFakeDaemon({ responses: {
    "component.list": { components: [comp("a", "aeon", "/w/aeon"), comp("ui", "ui", "/w/aeon/ui", "a"), comp("kit", "kit", "/w/aeon/ui/kit", "ui"), comp("h", "helper", "/w/helper")] },
    "loop.list": { all: true, loops: [loop("l1", "/w/aeon/ui", "running"), loop("l2", "/w/aeon/ui/kit", "waiting"), loop("l3", "/w/helper", "blocked")] },
    "task.list": { tasks: [
      { id: "t1", taskid: "T1", title: "Wire the board", status: "in-progress", componentId: "ui", manual: false, assignee: "react-engineer", assigneeKind: "agent", loopSlug: "l1", costUsd: 0, costIsFloor: false, updatedAt: "2026-11-14T08:00:00Z" },
      { id: "t2", taskid: "T2", title: "Other project", status: "merged", componentId: "h", manual: false, assignee: "", assigneeKind: "", loopSlug: "", costUsd: 0, costIsFloor: false, updatedAt: "2026-11-14T08:00:00Z" },
    ] },
    "goal.list": [],
    "roadmap.list": { componentRoot: "/w/aeon/ui", items: [] },
    "subscribe": { subscriptionId: "s1", from: 0 },
  } });
  return render(
    <I18nProvider locales={APP_LOCALES}><MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/work/:component?/:tab?" element={<><WorkScreen controller={new LiveController()} live={live} canWrite /><Where /></>} />
        <Route path="/" element={<Where />} />
        <Route path="/loops" element={<LegacyRedirect routeId="loops" controller={new LiveController()} live={live} />} />
        <Route path="/components/:id" element={<LegacyRedirect routeId="component" controller={new LiveController()} live={live} />} />
      </Routes>
    </MemoryRouter></I18nProvider>,
  );
}

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); });
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("WorkScreen", () => {
  it("Start a loop opens Home with this component picked, and pins nothing", async () => {
    mount("/work/ui");
    fireEvent.click(await screen.findByTestId("work-start"));
    expect((await screen.findByTestId("where")).textContent).toBe("/?component=%2Fw%2Faeon%2Fui");
  });

  it("all projects: counts every loop and lists the projects", async () => {
    mount("/work");
    expect(await screen.findByTestId("work-overview")).toBeTruthy();
    expect(within(await screen.findByTestId("work-stat-loops")).getByText("3")).toBeTruthy();
    expect(within(screen.getByTestId("work-stat-running")).getByText("2")).toBeTruthy();
    expect(within(screen.getByTestId("work-stat-needs")).getByText("2")).toBeTruthy();
    const inside = await screen.findByTestId("work-inside");
    expect(within(inside).getByText("aeon")).toBeTruthy();
    expect(within(inside).getByText("helper")).toBeTruthy();
  });

  it("a component with sub components counts them in by default and can leave them out", async () => {
    mount("/work/ui/loops");
    expect(await screen.findByTestId("work-loop-l1")).toBeTruthy();
    expect(screen.getByTestId("work-loop-l2")).toBeTruthy();
    expect(screen.queryByTestId("work-loop-l3")).toBeNull();
    fireEvent.click(screen.getByTestId("work-include-sub"));
    expect(screen.queryByTestId("work-loop-l2")).toBeNull();
    expect(screen.getByTestId("work-loop-l1")).toBeTruthy();
  });

  it("shows a Component column only when the scope spans several components", async () => {
    mount("/work/all/loops");
    await screen.findByTestId("work-loop-l1");
    expect(screen.getByText("Component")).toBeTruthy();
    cleanup();
    mount("/work/kit/loops");
    await screen.findByTestId("work-loop-l2");
    expect(screen.queryByText("Component")).toBeNull();
  });

  it("names where you are in the breadcrumb, every part a link", async () => {
    mount("/work/kit/loops");
    const crumb = await screen.findByTestId("work-breadcrumb");
    expect(within(crumb).getByRole("link", { name: "aeon" }).getAttribute("href")).toBe("/work/a");
    expect(within(crumb).getByRole("link", { name: "ui" })).toBeTruthy();
    expect(within(crumb).getByRole("link", { name: "kit" })).toBeTruthy();
  });

  it("says so for a component the daemon does not know", async () => {
    mount("/work/nope");
    expect(await screen.findByTestId("work-unknown")).toBeTruthy();
  });

  it("switching tab changes the address, keeping the component", async () => {
    mount("/work/ui");
    await screen.findByTestId("work-overview");
    fireEvent.click(screen.getByRole("tab", { name: /Loops/ }));
    expect((await screen.findByTestId("where")).textContent).toBe("/work/ui/loops");
  });

  it("redirects the old loops address, resolving ?root= to a component", async () => {
    mount("/loops?root=%2Fw%2Faeon%2Fui");
    expect((await screen.findByTestId("where")).textContent).toBe("/work/ui/loops");
  });

  it("redirects /components/:id to that component's overview", async () => {
    mount("/components/ui");
    expect((await screen.findByTestId("where")).textContent).toBe("/work/ui");
  });

  it("guardrails ask for one component on all projects, and open the sections on a picked one", async () => {
    mount("/work/all/guardrails");
    expect(await screen.findByTestId("work-guardrails-needs-one")).toBeTruthy();
    cleanup(); resetFakeDaemon();
    mount("/work/ui/guardrails");
    expect(await screen.findByTestId("work-guardrails")).toBeTruthy();
    expect(await screen.findByTestId("gates-section")).toBeTruthy();
    expect(screen.queryByTestId("gates-picker")).toBeNull();
  });

  it("plan opens on the task board with the daemon's real statuses, narrowed to the component", async () => {
    mount("/work/ui/plan");
    expect(await screen.findByTestId("work-tasks")).toBeTruthy();
    expect(await screen.findByText("Wire the board")).toBeTruthy();
    expect(screen.queryByText("Other project")).toBeNull();
    expect(screen.getAllByText("in-progress").length).toBeGreaterThan(0);
  });
});
