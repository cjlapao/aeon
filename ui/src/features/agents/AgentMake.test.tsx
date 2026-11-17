import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { resetBridge } from "../../lib/bridge";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { VIEW_ROW, LEDGER, rowsAnswer, refusalEnvelope, viewAnswer, mount, chooseRowVerb } from "./agentPanelKit";

// The kit's `Modal` and `ConfirmModal` ask the browser what viewport they are in,
// and jsdom has never heard of `matchMedia`. Same stub the rest of the suite uses.
vi.stubGlobal("matchMedia", () => ({
  matches: false,
  addEventListener() {},
  removeEventListener() {},
}));

// The kit's markdown entry is a 588 KB editor chunk (react-markdown and all).
// jsdom does not need to run it — these tests own the PROMPT wiring, so the
// stand-in prints the two shapes the real editor takes: a viewer div that shows
// the value it was handed, and an editable textarea that reports every keystroke
// back through `onChange`. Which shape, the caller decides with `readOnly` and
// `preview`, exactly as `PromptMarkdown` decides.
vi.mock("@cjlapao/ui-kit/markdown", () => ({
  MarkdownEditor: ({
    value,
    readOnly,
    preview,
    onChange,
    height,
  }: {
    value: string;
    readOnly?: boolean;
    preview?: string;
    onChange?: (next: string | undefined) => void;
    height?: number;
  }) =>
    readOnly || preview === "preview" ? (
      <div data-testid="md-view" data-height={height}>{value}</div>
    ) : (
      <div data-testid="md-edit-shell" data-height={height}>
        <textarea data-testid="md-edit" value={value} onChange={(event) => onChange?.(event.target.value)} />
      </div>
    ),
}));


// The new-agent panel, the edit page's forms, and the doors that bring an agent in or copy one out
// (duplicate, export, import) — everything the person does to MAKE or MOVE an agent. The shared
// fixtures live in agentPanelKit.tsx.


beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("the agent panel: making, editing and moving agents", () => {
  // --- the new-agent panel: who, what it does, what it works with, what it may use -----------------
  const KITS = [
    { id: "read-code", label: "Read the code", description: "Open and search files.", effect: "reads", tools: ["read", "list", "glob", "grep"] },
    { id: "run-commands", label: "Run commands", description: "Run shell commands.", effect: "runs", tools: ["bash"] },
  ];
  const TAGS = [{ tag: "go", label: "Go", group: "language" }, { tag: "docs", label: "Documentation", group: "area" }];
  const REGISTRY = { handlers: [], resolvers: ["by-tech-tag"], techTags: TAGS, toolKits: KITS };
  // The catalog behind the picker: the kits' tools, one tool no kit holds, and one that is not available.
  const TOOLS = { tools: [
    { name: "read", family: "workspace", description: "Read a file", available: true },
    { name: "list", family: "workspace", description: "List a folder", available: true },
    { name: "glob", family: "workspace", description: "Find files by pattern", available: true },
    { name: "grep", family: "workspace", description: "Search files", available: true },
    { name: "bash", family: "process", description: "Run a command", available: true },
    { name: "goal_show", family: "goal", description: "Show a goal", available: true },
    { name: "graph", family: "code-graph", description: "Conditional", available: false },
  ] };
  /** Open the tools picker and wait for its list. */
  async function openTools() {
    // The picker's trigger: it shows the placeholder, or the chosen tools as pills once there are some.
    const trigger = await waitFor(() => {
      const found = document.querySelector('[data-testid="agent-tools-picker"] button[aria-haspopup="listbox"]:not([disabled])');
      if (found === null) throw new Error("the tools picker is not ready");
      return found as HTMLElement;
    });
    fireEvent.click(trigger);
    await screen.findByRole("listbox");
  }
  /** Choose a technology in the tag picker: open it, click the option. */
  async function pickTech(label: string) {
    const section = await screen.findByRole("region", { name: /work with/i });
    const trigger = await waitFor(() => {
      const found = section.querySelector('button[aria-haspopup="listbox"]:not([disabled])');
      if (found === null) throw new Error("the technology picker is not ready");
      return found as HTMLElement;
    });
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("option", { name: label }));
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  }
  const group = (name: RegExp) => screen.getByRole("checkbox", { name });

  it("creates with what the person chose — instructions, tech, a kit of tools, a colour — and opens the new agent", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "handlers.list": REGISTRY,
        "tools.list": TOOLS,
        "agent.create": { ...VIEW_ROW, slug: "reviewer-plus", name: "Reviews pull requests" },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    fireEvent.click(screen.getByTestId("agents-create"));
    await screen.findByTestId("agent-create");

    fireEvent.change(screen.getByTestId("agent-create-name"), { target: { value: "Reviews pull requests" } });
    fireEvent.change(screen.getByTestId("agent-create-description"), { target: { value: "Reads the diff, posts findings" } });
    fireEvent.click(screen.getByTestId("agent-create-skeleton"));
    expect((screen.getByTestId("agent-create-prompt") as HTMLTextAreaElement).value).toContain("\n\nRules\n- ");
    fireEvent.click(screen.getByTestId("agent-create-colour-pink"));
    await pickTech("Go");
    // A kit is one toggle for all its tools.
    await openTools();
    fireEvent.click(group(/Read the code/));
    expect(group(/Read the code/).getAttribute("aria-checked")).toBe("true");
    expect(screen.getByTestId("agent-tools-summary").textContent).toContain("4 tools chosen");
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    // What it must report back: one line per field, an optional one marked with a switch.
    fireEvent.click(screen.getByTestId("agent-handoff-add"));
    fireEvent.change(screen.getByTestId("agent-handoff-name-0"), { target: { value: "summary" } });
    fireEvent.change(screen.getByTestId("agent-handoff-purpose-0"), { target: { value: "What was reviewed" } });

    fireEvent.click(screen.getByTestId("agent-panel-create-run"));
    // The agent's own page opens, and the outcome is a toast, not a box in the panel.
    await screen.findByTestId("agent-detail");
    expect(await screen.findByText(/Created Reviews pull requests/)).toBeTruthy();

    const params = daemon.paramsOf("agent.create") as Record<string, unknown>;
    expect(params).toEqual({
      name: "Reviews pull requests",
      description: "Reads the diff, posts findings",
      prompt: expect.stringContaining("Rules"),
      tech: ["go"],
      tools: ["read", "list", "glob", "grep"],
      handoff: { summary: "What was reviewed" },
      avatarColor: "pink",
    });
    // No mood, no functionality, no slug: those are the daemon's.
    expect(Object.keys(params)).not.toContain("avatarMood");
    expect(Object.keys(params)).not.toContain("functionality");
    expect(Object.keys(params)).not.toContain("slug");
    expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(2);
  });

  it("sends only a name and a description when nothing else was chosen — the daemon's defaults are not ours to send", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "agent.create": { ...VIEW_ROW } },
    });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-create"));
    await screen.findByTestId("agent-create");
    fireEvent.change(screen.getByTestId("agent-create-name"), { target: { value: "Plain" } });
    fireEvent.click(screen.getByTestId("agent-panel-create-run"));
    await waitFor(() => expect(daemon.methods()).toContain("agent.create"));
    expect(daemon.paramsOf("agent.create")).toEqual({ name: "Plain", description: "" });
  });

  it("offers the tools in groups a person can take whole: the daemon's kits first, then the rest by family, and nothing that is not available", async () => {
    installFakeDaemon({ responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS } });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-create"));
    await openTools();
    // Kit groups come first, the rest by the daemon's own family names.
    const headers = within(screen.getByRole("listbox")).getAllByRole("checkbox").map((el) => el.textContent ?? "");
    expect(headers[0]).toContain("Read the code");
    expect(headers[0]).toContain("0/4");
    expect(headers.some((h) => h.includes("Run commands"))).toBe(true);
    expect(headers.some((h) => h.toLowerCase().includes("goal"))).toBe(true);
    expect(screen.queryByText("graph")).toBeNull();

    // A group header takes the whole group in…
    fireEvent.click(group(/Read the code/));
    expect(screen.getByTestId("agent-tools-summary").textContent).toContain("4 tools chosen");
    // …and one tool out of it leaves the group partly on.
    fireEvent.click(screen.getByRole("option", { name: /grep/ }));
    expect(group(/Read the code/).getAttribute("aria-checked")).toBe("mixed");
    expect(screen.getByTestId("agent-tools-summary").textContent).toContain("3 tools chosen");
    // The group header acts on what the search shows: only the matches go in.
    fireEvent.change(screen.getByRole("combobox", { name: /search/i }), { target: { value: "grep" } });
    fireEvent.click(group(/Read the code/));
    expect(screen.getByTestId("agent-tools-summary").textContent).toContain("4 tools chosen");
  });

  it("edits the tools of your own agent with the same kits, and posts the list with the rest of the row", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS, "agent.edit": { edited: "reviewer-plus" } },
    });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-edit-description");
    await openTools();
    // The agent already holds read, list, write: the read-code group is partly on (it has glob and grep too).
    expect(group(/Read the code/).getAttribute("aria-checked")).toBe("mixed");
    fireEvent.click(group(/Run commands/));
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    fireEvent.click(await screen.findByTestId("agent-panel-save"));
    await waitFor(() => expect(daemon.calls.some((c) => c.method === "agent.edit")).toBe(true));
    const sent = (daemon.paramsOf("agent.edit") as { fields: { tools: string } }).fields.tools;
    expect(JSON.parse(sent)).toEqual(["read", "list", "write", "bash"]);
  });

  it("says that bash always asks first, and only when bash is chosen", async () => {
    installFakeDaemon({ responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS } });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-create"));
    await screen.findByTestId("agent-create");
    expect(screen.queryByTestId("agent-tools-bash-asks")).toBeNull();
    await openTools();
    fireEvent.click(group(/Run commands/));
    expect(screen.getByTestId("agent-tools-bash-asks").textContent).toContain("asks for your approval");
  });

  it("changes which minion it is with a click, goes round back to the name, and sends only a chosen one", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS, "agent.create": { ...VIEW_ROW } },
    });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-create"));
    await screen.findByTestId("agent-create");
    fireEvent.change(screen.getByTestId("agent-create-name"), { target: { value: "Bopper" } });
    const chooser = screen.getByTestId("agent-minion-chooser");
    const shown = () => screen.getByTestId("agent-minion-name").textContent;
    expect(shown()).toBe("auto");
    for (const next of ["pip", "bop", "noodle", "auto"]) {
      fireEvent.click(chooser);
      expect(shown()).toBe(next);
    }
    fireEvent.click(chooser); // pip
    fireEvent.click(chooser); // bop
    fireEvent.click(screen.getByTestId("agent-panel-create-run"));
    await waitFor(() => expect(daemon.methods()).toContain("agent.create"));
    expect(daemon.paramsOf("agent.create")).toEqual({ name: "Bopper", description: "", avatarMinion: "bop" });
  });

  it("changes the minion of your own agent with the same click, kept in the metadata beside the other keys", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS, "agent.edit": { edited: "reviewer-plus" } },
    });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-edit-description");
    fireEvent.click(await screen.findByTestId("agent-minion-chooser"));
    expect(screen.getByTestId("agent-minion-name").textContent).toBe("pip");
    fireEvent.click(await screen.findByTestId("agent-panel-save"));
    await waitFor(() => expect(daemon.calls.some((c) => c.method === "agent.edit")).toBe(true));
    const sent = (daemon.paramsOf("agent.edit") as { fields: { metadata: string } }).fields.metadata;
    // The row's own metadata ({"owner":"cjlapao"}) is kept; the minion joins it.
    expect(JSON.parse(sent)).toEqual({ owner: "cjlapao", minion: "pip" });
  });

  it("builds what your agent reports back in the Overview, and posts it as the object the daemon reads", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS, "agent.edit": { edited: "reviewer-plus" } },
    });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-edit-description");
    fireEvent.click(await screen.findByTestId("agent-handoff-add"));
    fireEvent.change(screen.getByTestId("agent-handoff-name-0"), { target: { value: "links" } });
    fireEvent.change(screen.getByTestId("agent-handoff-purpose-0"), { target: { value: "Where to read it" } });
    fireEvent.click(screen.getByTestId("agent-handoff-optional-0").querySelector("input") as HTMLInputElement);
    fireEvent.click(await screen.findByTestId("agent-panel-save"));
    await waitFor(() => expect(daemon.calls.some((c) => c.method === "agent.edit")).toBe(true));
    const sent = (daemon.paramsOf("agent.edit") as { fields: { handoff: string } }).fields.handoff;
    expect(JSON.parse(sent)).toEqual({ "links?": "Where to read it" });
  });

  it("edits your agent the way it is made: the minion, the tag picker and the tools in one tab, the rest apart", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS, "agent.edit": { edited: "reviewer-plus" } },
    });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-edit-description");
    expect(screen.getByTestId("agent-edit-overview")).toBeTruthy();
    // The identity block, the tech picker and the tools picker are all on this tab.
    expect(screen.getByTestId("agent-edit-name")).toBeTruthy();
    expect(screen.getByTestId("agent-minion-chooser")).toBeTruthy();
    expect(screen.getByTestId("agent-tools-picker")).toBeTruthy();
    // The old way is gone from it: no raw metadata box, no avatar selects.
    expect(document.querySelector("#agent-field-metadata")).toBeNull();
    expect(document.querySelector("#agent-field-avatarMood")).toBeNull();

    await pickTech("Go");
    fireEvent.click(screen.getByTestId("agent-edit-colour-pink"));
    fireEvent.click(await screen.findByTestId("agent-panel-save"));
    await waitFor(() => expect(daemon.calls.some((c) => c.method === "agent.edit")).toBe(true));
    const sent = (daemon.paramsOf("agent.edit") as { fields: { metadata: string; avatarColor: string } }).fields;
    expect(sent.avatarColor).toBe("pink");
    // The row's own metadata is kept; the tech joins it.
    expect(JSON.parse(sent.metadata)).toEqual({ owner: "cjlapao", tech: ["go"] });
  });

  it("keeps the details most agents never need on their own tab, in the same ghost controls", async () => {
    installFakeDaemon({ responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY, "tools.list": TOOLS } });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-edit-description");
    fireEvent.click(screen.getByRole("tab", { name: /^Metadata$/ }));
    const advanced = await screen.findByTestId("agent-edit-advanced");
    for (const key of ["functionality", "skills", "metadata"]) {
      expect(advanced.querySelector(`#agent-field-${key}`), `${key} is on the advanced tab`).not.toBeNull();
    }
    expect(advanced.querySelector("#agent-field-name")).toBeNull();
    // The metadata is the kit's code block, edited in place: a keystroke marks the row changed.
    const box = advanced.querySelector("#agent-field-metadata textarea, textarea#agent-field-metadata") as HTMLTextAreaElement | null;
    expect(box, "metadata edits in the code block's textarea").not.toBeNull();
    fireEvent.change(box as HTMLTextAreaElement, { target: { value: '{"owner":"someone"}' } });
    await screen.findByTestId("agent-panel-save");
  });

  it("says what the daemon refused, in a toast that stays, and keeps the panel as it was", async () => {
    installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, "handlers.list": REGISTRY,
        "agent.create": () => { throw refusalEnvelope("refused: wrtie is not a registered tool"); },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-create"));
    await screen.findByTestId("agent-create");
    fireEvent.change(screen.getByTestId("agent-create-name"), { target: { value: "Broken" } });
    fireEvent.click(screen.getByTestId("agent-panel-create-run"));
    expect(await screen.findByText(/wrtie is not a registered tool/)).toBeTruthy();
    // Still the form, still the name — nothing was lost to the refusal.
    expect((screen.getByTestId("agent-create-name") as HTMLInputElement).value).toBe("Broken");
  });

  it("duplicates only with a name for the copy, says so in a toast, and opens the copy", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "agent.duplicate": { ...VIEW_ROW, slug: "reviews-plus-v2", name: "Reviewer Two" },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Duplicate/);
    await screen.findByTestId("agent-duplicate-panel");

    expect((screen.getByTestId("agent-duplicate-run") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId("agent-duplicate-name"), { target: { value: "Reviewer Two" } });
    expect((screen.getByTestId("agent-duplicate-run") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("agent-duplicate-run"));
    // The copy's own page opens, and the outcome is a toast.
    await screen.findByTestId("agent-detail");
    expect(await screen.findByText(/Created Reviewer Two, a copy of the agent/)).toBeTruthy();

    expect(daemon.paramsOf("agent.duplicate")).toEqual({ slug: "reviewer-plus", name: "Reviewer Two" });
    expect(daemon.paramsOf("agent.view")).toEqual({ slug: "reviews-plus-v2" });
  });

  it("offers Duplicate on a first-party row too (AM1) — the copy is the editable twin", async () => {
    installFakeDaemon({ responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER } });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("golang-engineer", /^Duplicate/);
    expect(await screen.findByTestId("agent-duplicate-panel")).toBeTruthy();
    expect(screen.getByTestId("agent-duplicate-note").textContent).toContain("The original is not touched");
  });

  it("exports to a file or to the screen: the daemon\'s two answers, told apart, and the card can be copied", async () => {
    const CARD_TEXT = '{"slug":"reviewer-plus","name":"Reviewer Plus"}';
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER,
        "agent.export": (raw: unknown) => ((raw as { path?: string }).path ? (raw as { path: string }).path : CARD_TEXT),
      },
    });
    const written: string[] = [];
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (text: string) => { written.push(text); } } });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Export/);
    await screen.findByTestId("agent-export-panel");

    // No path: the card itself comes back, in a block that can be copied.
    fireEvent.click(screen.getByTestId("agent-export-run"));
    expect((await screen.findByTestId("agent-export-answer-line")).textContent).toContain("The card, ready to copy");
    expect(screen.getByTestId("agent-export-answer-text").textContent).toContain("reviewer-plus");
    fireEvent.click(screen.getByTestId("agent-export-copy"));
    await waitFor(() => expect(written).toEqual([CARD_TEXT]));
    expect(daemon.paramsOf("agent.export")).toEqual({ slug: "reviewer-plus", path: "" });

    // A path: the daemon wrote a file and says where.
    fireEvent.change(screen.getByTestId("agent-export-path"), { target: { value: "/tmp/card.json" } });
    fireEvent.click(screen.getByTestId("agent-export-run"));
    await waitFor(() => expect(screen.getByTestId("agent-export-answer-line").textContent).toContain("wrote the card to"));
    expect(screen.queryByTestId("agent-export-copy")).toBeNull();
  });

  it("keeps a refused export in a toast that stays, and the panel as it was", async () => {
    installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER,
        "agent.export": () => { throw refusalEnvelope("refused: cannot write the card to /nope: permission denied"); },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");
    await chooseRowVerb("reviewer-plus", /^Export/);
    await screen.findByTestId("agent-export-panel");
    fireEvent.change(screen.getByTestId("agent-export-path"), { target: { value: "/nope" } });
    fireEvent.click(screen.getByTestId("agent-export-run"));
    expect(await screen.findByText(/permission denied/)).toBeTruthy();
    expect((screen.getByTestId("agent-export-path") as HTMLInputElement).value).toBe("/nope");
  });

  // --- the import panel: three steps, the card's contents (not a path) to the daemon ---------------
  const CARD = JSON.stringify({ card: { slug: "reviewer-plus", name: "Reviewer Plus", version: "2" }, signature: "c2ln", certificate: "pem" });
  const UNSIGNED_CARD = JSON.stringify({ card: { slug: "reviewer-plus", name: "Reviewer Plus", version: "2" }, signature: "", certificate: "" });

  /** Drop a file on the panel's file control, the way the browser reports it. */
  async function drop(text: string, name = "card.json") {
    const input = (await waitFor(() => {
      const found = document.querySelector('input[type="file"]');
      if (found === null) throw new Error("no file input yet");
      return found;
    })) as HTMLInputElement;
    const file = new File([text], name, { type: "application/json" });
    fireEvent.change(input, { target: { files: [file] } });
  }

  async function openImport(responses: Record<string, unknown>) {
    const daemon = installFakeDaemon({ responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER, ...responses } });
    mount();
    await screen.findByTestId("agents-screen");
    fireEvent.click(screen.getByTestId("agents-import"));
    await screen.findByTestId("agent-import");
    return daemon;
  }

  it("imports from a file: shows what is in the card first, sends its contents (never a path), and prints the daemon's word with a tone each", async () => {
    for (const word of ["created", "updated", "no-op (same version already stored)"]) {
      const daemon = await openImport({ "agent.import": { agent: VIEW_ROW, result: word } });
      // Nothing to import until a card is chosen.
      expect((screen.getByTestId("agent-panel-import-run") as HTMLButtonElement).disabled).toBe(true);

      await drop(CARD);
      await screen.findByTestId("agent-import-summary");
      expect(screen.getByTestId("agent-import-name").textContent).toContain("Reviewer Plus");
      expect(screen.getByTestId("agent-import-slug").textContent).toContain("reviewer-plus");
      expect(screen.getByTestId("agent-import-version").textContent).toContain("2");
      expect(screen.queryByTestId("agent-import-unsigned")).toBeNull();

      fireEvent.click(screen.getByTestId("agent-panel-import-run"));
      await screen.findByTestId("agent-panel-imported");
      expect(daemon.paramsOf("agent.import")).toEqual({ content: CARD });
      expect(screen.getByTestId("agent-import-word").textContent).toBe(word);
      // The word is also said as a sentence a person can read.
      expect(screen.getByTestId("agent-panel-imported-name").textContent).toMatch(/Added|Updated|already up to date/);
      cleanup();
      resetFakeDaemon();
    }
  });

  it("asks the person to confirm an unsigned card, sends their yes with that import alone, and marks the result", async () => {
    const daemon = await openImport({ "agent.import": { agent: { ...VIEW_ROW, signed: false, origin: "import" }, result: "created" } });
    await drop(UNSIGNED_CARD);
    expect((await screen.findByTestId("agent-import-unsigned")).textContent).toContain("Nobody vouches");
    fireEvent.click(screen.getByTestId("agent-panel-import-run"));

    // Nothing goes to the daemon until the person says yes.
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("may be unsafe");
    expect(daemon.methods()).not.toContain("agent.import");
    fireEvent.click(within(dialog).getByRole("button", { name: /Import anyway/i }));

    await screen.findByTestId("agent-panel-imported");
    expect(daemon.paramsOf("agent.import")).toEqual({ content: UNSIGNED_CARD, acceptUnsigned: true });
    expect(screen.getByTestId("agent-import-marked-unsigned").textContent).toContain("asks for your approval first");
  });

  it("does not import an unsigned card when the person says no", async () => {
    const daemon = await openImport({ "agent.import": { agent: VIEW_ROW, result: "created" } });
    await drop(UNSIGNED_CARD);
    await screen.findByTestId("agent-import-unsigned");
    fireEvent.click(screen.getByTestId("agent-panel-import-run"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /^Cancel$/i }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(daemon.methods()).not.toContain("agent.import");
    expect(screen.queryByTestId("agent-panel-imported")).toBeNull();
  });

  it("asks nothing of a signed card and never sends acceptUnsigned for it", async () => {
    const daemon = await openImport({ "agent.import": { agent: VIEW_ROW, result: "created" } });
    await drop(CARD);
    await screen.findByTestId("agent-import-summary");
    fireEvent.click(screen.getByTestId("agent-panel-import-run"));
    await screen.findByTestId("agent-panel-imported");
    expect(daemon.paramsOf("agent.import")).toEqual({ content: CARD });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("says in plain words what is wrong with a file that is not a card, and offers no import for it", async () => {
    await openImport({});
    await drop("this is not json");
    expect((await screen.findByText(/not valid JSON/)).textContent).toBeTruthy();
    expect((screen.getByTestId("agent-panel-import-run") as HTMLButtonElement).disabled).toBe(true);

    await drop(JSON.stringify({ hello: "world" }));
    expect(await screen.findByText(/no agent card in it/)).toBeTruthy();
    expect((screen.getByTestId("agent-panel-import-run") as HTMLButtonElement).disabled).toBe(true);
  });

  it("opens the agent it just added, from the panel", async () => {
    await openImport({ "agent.import": { agent: VIEW_ROW, result: "created" } });
    await drop(CARD);
    await screen.findByTestId("agent-import-summary");
    fireEvent.click(screen.getByTestId("agent-panel-import-run"));
    fireEvent.click(await screen.findByTestId("agent-import-open"));
    await screen.findByTestId("agent-detail");
  });

});
