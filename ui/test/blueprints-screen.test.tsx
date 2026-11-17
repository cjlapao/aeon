import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { BlueprintsScreen } from "../src/features/blueprints/BlueprintsScreen";
import { APP_LOCALES } from "../src/lib/i18n";
import { LiveController } from "../src/lib/live";
import { resetBridge } from "../src/lib/bridge";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

// S48 T12 — the flow library as a management screen.
//
// The assertions that matter here are about WHO DECIDES. A filter is a param the
// daemon answers, not a client-side `Array.filter` (BP13); a copy is a name the
// daemon returns, not YAML rewritten in the browser; the row on screen is the row
// the URL named, so the side menu and this screen agree. The screen itself keeps
// no verdict, no revision and no usage count — it renders what it was handed.

const source = `id: two-step
schemaVersion: 1
version: 2
lanes:
  - id: start
    step: {kind: model-step, agent: default}
  - id: review
    step: {kind: gate, question: approval}
questions:
  approval: {prompt: Approve?, answers: [approve, revise]}
`;

/** The structure the daemon reads the text into (blueprint.model) and takes back (blueprint.render). */
const model = {
  id: "two-step", description: "Ask an agent, then a person.", schemaVersion: 1, version: 2,
  lanes: [{ id: "start", step: { kind: "model-step", agent: "default" } }, { id: "review", step: { kind: "gate", question: "approval" } }],
  questions: { approval: { prompt: "Approve?", answers: ["approve", "revise"] } },
};
const vocabulary = {
  stepKinds: ["model-step", "code-action", "gate"], after: ["finish", "converse"], thenActions: ["escalate", "fail-item", "block"], operators: ["eq", "neq", "in"],
  decides: ["goal", "brief"], emitTypes: ["goal", "epic", "brief", "task"], requireTypes: ["goal", "task"], requireStates: ["approved"],
  fields: [{ path: "mode", type: "enum", enum: ["simple", "standard", "poc"], operators: ["eq", "neq", "in"], doc: "Loop execution mode" }, { path: "tasks", type: "collection", operators: ["eq", "neq"], doc: "Tasks" }],
  actions: [{ name: "git.createBranch", args: [{ name: "which", type: "string", required: false, source: "code" }] }],
};

const live = { state: "current" as const, endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 };
const report = { blueprint: "two-step", diagnostics: [], schemaVersion: 1, validCount: 2, version: 2 };

/** The library the fake holds: two shipped rows, one the person wrote. */
const library = [
  { contentHash: "h1", description: "The full development protocol.", divergedFromShipped: false, name: "dev-loop", origin: "system", revision: 1, updatedAt: "2026-11-01T00:00:00Z", updatedBy: "seed", usedByCount: 2, version: 1 },
  { contentHash: "h2", name: "two-step", origin: "user", revision: 3, updatedAt: "2026-11-02T00:00:00Z", updatedBy: "cli", usedByCount: 0, version: 2 },
];

function baseResponses(extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    "blueprint.list": (params: { origin?: string; q?: string }) => ({
      blueprints: library.filter(
        (row) => ((params?.origin ?? "") === "" || row.origin === params?.origin) && ((params?.q ?? "") === "" || row.name.includes(params.q!)),
      ),
      names: library.map((row) => row.name),
    }),
    "blueprint.show": (params: { name: string }) => ({
      content: source,
      contentHash: "h2",
      name: params.name,
      origin: params.name === "two-step" || params.name === "two-step-copy" ? "user" : "system",
      path: `/state/blueprints/${params.name}.yaml`,
      references: [],
      report: { blueprint: params.name, lanes: [], ok: true },
      revision: 3,
      updatedAt: "2026-11-02T00:00:00Z",
      updatedBy: "cli",
      version: 2,
    }),
    "blueprint.validate": report,
    "blueprint.model": { model },
    "blueprint.render": { content: source },
    "blueprint.vocabulary": vocabulary,
    "agent.list": { agents: [{ name: "default", provenance: "first-party", state: "active" }, { name: "investigator", provenance: "first-party", state: "active" }] },
    "blueprint.save": { changed: true, name: "two-step", origin: "user", references: [], report, revision: 4, version: 2 },
    "blueprint.history": { revisions: [] },
    "blueprint.usage": { actions: [], agents: [], loopsStarted: 0, queueItems: [], schedules: [], skills: [], triggers: [] },
    "blueprint.lint": { advisories: [], evaluatedAt: "2026-11-02T00:00:00Z" },
    "blueprint.duplicate": { changed: true, name: "two-step-copy", origin: "user", references: [], report, revision: 1, version: 1 },
    "proposal.list": { proposals: [] },
    ...extra,
  };
}

function renderScreen(initialPath = "/blueprints") {
  return render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter initialEntries={[initialPath]}>
        <BlueprintsScreen controller={new LiveController()} live={live} canWrite />
      </MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => {
  resetBridge();
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
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
});

afterEach(() => {
  cleanup();
  resetFakeDaemon();
  resetBridge();
  vi.unstubAllGlobals();
});

describe("the flow library", () => {
  it("shows every flow once, as a card that says what it is for, with the origin badge it earned", async () => {
    installFakeDaemon({ responses: baseResponses() });
    renderScreen();
    const cards = await screen.findAllByTestId("flow-card");
    expect(cards).toHaveLength(2);
    const shipped = cards.find((c) => c.textContent?.includes("dev-loop"))!;
    expect(shipped.textContent).toContain("The full development protocol.");
    expect(shipped.textContent).toContain("Shipped");
    // A flow with no description says so rather than showing an empty line.
    expect(cards.find((c) => c.textContent?.includes("two-step"))!.textContent).toContain("No description yet");
    // `divergedFromShipped` is absent on the user row: no Edited badge, because absent is not a verdict (BP14).
    expect(cards.find((c) => c.textContent?.includes("two-step"))!.textContent).not.toContain("Edited");
  });

  it("filters by origin and by name as PARAMS, so the answer is the daemon's (BP13)", async () => {
    const daemon = installFakeDaemon({ responses: baseResponses() });
    renderScreen();
    await screen.findAllByTestId("flow-card");
    fireEvent.click(screen.getByRole("radio", { name: "Shipped" }));
    await waitFor(() => expect(daemon.paramsOf("blueprint.list")).toMatchObject({ origin: "system" }));
    await waitFor(() => expect(screen.getAllByTestId("flow-card")).toHaveLength(1));
    fireEvent.change(screen.getByPlaceholderText("Search flows by name"), { target: { value: "dev" } });
    await waitFor(() => expect(daemon.paramsOf("blueprint.list")).toMatchObject({ origin: "system", q: "dev" }));
  });

  it("opens the flow the link named, so the side menu and this screen agree", async () => {
    installFakeDaemon({ responses: baseResponses() });
    renderScreen("/blueprints?name=dev-loop");
    const workspace = await screen.findByTestId("flow-workspace");
    await waitFor(() => expect(workspace.textContent).toContain("dev-loop"));
    // Opening a flow replaces the library: the list is not drawn twice.
    expect(screen.queryByTestId("flow-gallery")).toBeNull();
  });

  it("starts a new flow from a copy through the daemon and opens what it named", async () => {
    const daemon = installFakeDaemon({ responses: baseResponses() });
    renderScreen();
    fireEvent.click(await screen.findByTestId("new-flow-button"));
    const dialog = await screen.findByTestId("new-flow");
    fireEvent.change(dialog.querySelector("input")!, { target: { value: "two-step-copy" } });
    fireEvent.click(screen.getByRole("radio", { name: "Copy of…" }));
    fireEvent.click(screen.getByTestId("create-flow"));
    await waitFor(() => expect(daemon.paramsOf("blueprint.duplicate")).toEqual({ from: "dev-loop", name: "two-step-copy" }));
    await waitFor(() => expect(daemon.paramsOf("blueprint.show")).toMatchObject({ name: "two-step-copy" }));
  });

  it("creates a flow from a template: the daemon renders it and it is saved create-only", async () => {
    const daemon = installFakeDaemon({ responses: baseResponses({ "blueprint.save": { changed: true, name: "triage", origin: "user", references: [], report, revision: 1, version: 1 } }) });
    renderScreen();
    fireEvent.click(await screen.findByTestId("new-flow-button"));
    const dialog = await screen.findByTestId("new-flow");
    fireEvent.change(dialog.querySelector("input")!, { target: { value: "triage" } });
    fireEvent.change(dialog.querySelector("textarea")!, { target: { value: "Sorts reports." } });
    fireEvent.click(screen.getByRole("radio", { name: "Plan and approve" }));
    fireEvent.click(screen.getByTestId("create-flow"));
    await waitFor(() => expect(daemon.paramsOf("blueprint.save")).toMatchObject({ name: "triage", createOnly: true }));
    const sent = daemon.paramsOf("blueprint.render") as { model: { id: string; description: string; lanes: { id: string }[] } };
    expect(sent.model).toMatchObject({ id: "triage", description: "Sorts reports." });
    expect(sent.model.lanes.map((l) => l.id)).toEqual(["plan", "plan-gate"]);
  });

  it("refuses a name the daemon would refuse, before asking it", async () => {
    installFakeDaemon({ responses: baseResponses() });
    renderScreen();
    fireEvent.click(await screen.findByTestId("new-flow-button"));
    const dialog = await screen.findByTestId("new-flow");
    fireEvent.change(dialog.querySelector("input")!, { target: { value: "dev-loop" } });
    expect(await screen.findByText("A flow with this name already exists.")).toBeTruthy();
    expect((screen.getByTestId("create-flow") as HTMLButtonElement).disabled).toBe(true);
  });

  it("offers the proposals pile for this slice's kind, still answered by proposal.list", async () => {
    const daemon = installFakeDaemon({ responses: baseResponses() });
    renderScreen();
    fireEvent.click(await screen.findByTestId("proposals-toggle"));
    await waitFor(() => expect(daemon.methods()).toContain("proposal.list"));
    expect(daemon.methods()).not.toContain("blueprint.save");
  });

  it("diagnostic labels tell the truth after S13 and S16 closed (validation pass 2026-10-26)", async () => {
    installFakeDaemon({
      responses: baseResponses({
        "blueprint.validate": {
          blueprint: "two-step",
          validCount: 2,
          diagnostics: [
            { state: "unverifiable", kind: "agent-registry", path: "lanes[triage].step.agent", message: "agent body not shipped — S13 named it a follow-up (no body file in the registry)", slice: "S13" },
            { state: "unverifiable", kind: "action-registry", path: "lanes[ship].step.action", message: "the code action registry exists (S16 shipped) — this validator was not given the closed list; verdict withheld, not guessed", slice: "S16" },
          ],
        },
      }),
    });
    renderScreen("/blueprints?name=two-step");
    fireEvent.click(await screen.findByRole("tab", { name: "Source" }));
    const box = await screen.findByTestId("blueprint-diagnostics");
    const text = box.textContent ?? "";
    expect(text).not.toMatch(/until S1[36]/);
    expect(text).toContain("Agent body not shipped");
    expect(text).toContain("not given");
    expect(text).toContain("verdict withheld, not guessed");
  });
});

describe("the builder", () => {
  async function openBuilder(extra: Record<string, unknown> = {}) {
    const daemon = installFakeDaemon({ responses: baseResponses(extra) });
    renderScreen("/blueprints?name=two-step");
    fireEvent.click(await screen.findByTestId("edit-flow"));
    fireEvent.click(await screen.findByTestId("edit-view-list"));   // editing opens on the graph; the list is the other view
    await screen.findAllByTestId("lane-card");
    return daemon;
  }

  it("draws the lanes in order, each saying what it does", async () => {
    await openBuilder();
    const cards = screen.getAllByTestId("lane-card");
    expect(cards.map((c) => c.getAttribute("data-lane"))).toEqual(["start", "review"]);
    expect(cards[0]!.textContent).toContain("default");
    expect(cards[1]!.textContent).toContain("Approve?");
  });

  it("opens to the graph read-only; Edit makes the graph itself editable, and the list is one click away", async () => {
    installFakeDaemon({ responses: baseResponses({}) });
    renderScreen("/blueprints?name=two-step");
    expect(await screen.findByTestId("flow-graph")).toBeTruthy();
    expect(screen.queryByTestId("lane-card")).toBeNull();
    expect(screen.queryByTestId("flow-graph-editor")).toBeNull();        // read-only: no inspector beside it
    expect(screen.queryByTestId("save-blueprint")).toBeNull();
    fireEvent.click(screen.getByTestId("edit-flow"));
    expect(await screen.findByTestId("flow-graph-editor")).toBeTruthy(); // editing is on the connection graph
    expect(screen.getByTestId("flow-graph")).toBeTruthy();
    expect(screen.getByTestId("save-blueprint")).toBeTruthy();
    expect(screen.queryByTestId("lane-card")).toBeNull();
    fireEvent.click(screen.getByTestId("edit-view-list"));
    expect((await screen.findAllByTestId("lane-card")).length).toBe(2);
    expect(screen.queryByTestId("flow-graph")).toBeNull();
    fireEvent.click(screen.getByTestId("edit-view-graph"));
    expect(await screen.findByTestId("flow-graph-editor")).toBeTruthy();
  });

  it("moves a lane by dragging it: a ghost marks the landing, the drop reorders through the daemon", async () => {
    const daemon = await openBuilder();
    const [first, second] = screen.getAllByTestId("lane-card");
    const dataTransfer = { setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: "" };
    fireEvent.dragStart(first!, { dataTransfer });
    fireEvent.dragOver(second!, { dataTransfer, clientY: 10 });
    const ghost = await screen.findByTestId("lane-ghost");
    // Two lanes, the first carried: the "+" above and below it hide, the one after the second stays.
    expect(screen.queryAllByRole("button", { name: "Add a lane here" })).toHaveLength(1); // the ghost's own "+" is drawn but not a control
    fireEvent.drop(ghost, { dataTransfer });
    await waitFor(() => expect(daemon.paramsOf("blueprint.render")).toBeTruthy());
    const sent = daemon.paramsOf("blueprint.render") as { model: { lanes: { id: string }[] } };
    expect(sent.model.lanes.map((l) => l.id)).toEqual(["review", "start"]);
    expect(screen.queryByTestId("lane-ghost")).toBeNull();
  });

  it("offers no ghost where a drop would leave the lane exactly where it is", async () => {
    await openBuilder();
    const [first, second] = screen.getAllByTestId("lane-card");
    const dataTransfer = { setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: "" };
    // The second lane dropped just after the first is the second lane, where it already is.
    fireEvent.dragStart(second!, { dataTransfer });
    fireEvent.dragOver(first!, { dataTransfer });
    expect(screen.queryByTestId("lane-ghost")).toBeNull();
  });

  it("changing an agent rewrites the flow through the daemon and offers Save", async () => {
    const daemon = await openBuilder({ "blueprint.render": { content: "id: two-step\nschemaVersion: 1\nversion: 2\nchanged: yes\n" } });
    expect((screen.getByTestId("save-blueprint") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(screen.getAllByTestId("lane-card")[0]!).getAllByRole("button")[0]!);
    const form = await screen.findByTestId("step-form");
    fireEvent.change(within(form).getByLabelText("Agent", { selector: "select" }), { target: { value: "investigator" } });
    await waitFor(() => expect(daemon.paramsOf("blueprint.render")).toBeTruthy());
    const sent = daemon.paramsOf("blueprint.render") as { model: { lanes: { step: { agent: string } }[] } };
    expect(sent.model.lanes[0]!.step.agent).toBe("investigator");
    await waitFor(() => expect((screen.getByTestId("save-blueprint") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByTestId("save-blueprint"));
    await waitFor(() => expect(daemon.paramsOf("blueprint.save")).toMatchObject({ name: "two-step", content: expect.stringContaining("changed: yes") }));
  });

  it("adds a lane between two others from the + between them", async () => {
    const daemon = await openBuilder();
    fireEvent.click(screen.getAllByRole("button", { name: "Add a lane here" })[1]!);
    fireEvent.click(await screen.findByRole("menuitem", { name: /Approval/ }));
    await waitFor(() => expect(daemon.paramsOf("blueprint.render")).toBeTruthy());
    const sent = daemon.paramsOf("blueprint.render") as { model: { lanes: { id: string }[] } };
    expect(sent.model.lanes.map((l) => l.id)).toEqual(["start", "approval", "review"]);
  });

  it("a rejection that loops back asks for its bound and its fallback", async () => {
    const daemon = await openBuilder();
    fireEvent.click(within(screen.getAllByTestId("lane-card")[1]!).getAllByRole("button")[0]!);
    const form = await screen.findByTestId("reject-form");
    fireEvent.change(within(form).getByLabelText("Send the flow back to", { selector: "select" }), { target: { value: "start" } });
    await waitFor(() => expect(daemon.paramsOf("blueprint.render")).toBeTruthy());
    const sent = daemon.paramsOf("blueprint.render") as { model: { lanes: { id: string; onReject?: unknown }[] } };
    expect(sent.model.lanes[1]!.onReject).toEqual({ goto: "start", counter: "review_retries", max: 2, then: "escalate" });
    expect(await within(await screen.findByTestId("reject-form")).findByLabelText("Try at most")).toBeTruthy();
  });

  it("shows what the daemon found on the lane it belongs to, and the problem list opens that lane", async () => {
    await openBuilder({ "blueprint.validate": { blueprint: "two-step", validCount: 1, diagnostics: [{ state: "invalid", kind: "unknown-agent", path: "lanes[start].step.agent", message: 'unknown agent "default"' }] } });
    await waitFor(() => expect(screen.getAllByTestId("lane-card")[0]!.textContent).toContain("1 issue"));
    expect(screen.getByTestId("verdict").textContent).toContain("1 problem");
    fireEvent.click(within(screen.getByTestId("problem-list")).getByRole("button"));
    expect(await screen.findByTestId("lane-inspector")).toBeTruthy();
  });

  it("explains a flow's settings in the flow's own words, and keeps the name fixed", async () => {
    await openBuilder();
    fireEvent.click(screen.getByRole("button", { name: /Ask an agent|Flow/ }));
    const settings = await screen.findByTestId("flow-settings");
    // The title is editable; the name beside it is the identifier and stays fixed.
    const boxes = within(settings).getAllByRole("textbox") as HTMLInputElement[];
    expect(boxes[0]!.disabled).toBe(false);
    expect(boxes[1]!.disabled).toBe(true);
    expect(within(settings).getByText("Keep the conversation open")).toBeTruthy();
  });
});
