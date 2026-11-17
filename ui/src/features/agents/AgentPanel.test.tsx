import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider, ToastProvider, ToastViewport } from "@cjlapao/ui-kit";
import { LiveController } from "../../lib/live";
import { APP_LOCALES } from "../../lib/i18n";
import { resetBridge } from "../../lib/bridge";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { AgentPanel } from "./AgentPanel";
import { AGENT_WRITABLE_KEYS, INTERNAL_EDIT_REFUSAL, refusalFor } from "./agentsModel";
import { VIEW_ROW, LEDGER, liveCurrent, rowsAnswer, refusalEnvelope, viewAnswer, mount, chooseRowVerb, closePanel, chooseTab, field } from "./agentPanelKit";

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

// ============================================================================
// S73 T7 — the panel, the delete modal, and the one hazard this whole task
// exists for.
//
// # The test's name is the rule
//
// `agentmgr.Edit` replaces nine content fields with whatever the request's
// `fields` blob carries. A form that shows three fields and posts three therefore
// blanks six, in a row that still looks like the agent you were reading. The
// case below plants content in fields the form does not render (`version`,
// `tools`, `bodyHash`, `policyAgent`), changes the description alone, and demands
// every one of them back in the posted `fields` — plus a key-for-key census of
// the row, which is the same claim with no room for "most of them".
//
// # Why the fixture's two answers disagree on purpose
//
// The table's row and `agent.view`'s answer describe the same slug with DIFFERENT
// values. A panel seeded from the table's cache would show one set and post the
// other, and the assertion would see it. This is AR5's whole argument in fixture
// form: the window's row is a summary, and opening a row means asking for it.
//
// # What these tests read
//
// The calls the page made and the params they carried (`daemon.calls`,
// `paramsOf`), and the DOM the person sees. No jest-dom in this suite, so a
// disabled control is read as the property it is.
// ============================================================================


beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("the agent panel (S73 T7)", () => {
  it("opens an edit by asking agent.view, and never from the table's cached row", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-panel-edit");
    // The body arrives a frame after the panel (the kit animates a panel that
    // opens, so it mounts closed): wait for the loaded form, not just the shell.
    await screen.findByTestId("agent-edit-description");

    expect(daemon.paramsOf("agent.view")).toEqual({ slug: "reviewer-plus" });
    // The form shows what `agent.view` said. The window said something else about
    // the same slug, and both are in this file on purpose.
    expect(field("Name").value).toBe(VIEW_ROW.name);
    // The page is the agent's own: its heading is what `agent.view` answered, and the list's
    // summary of the same slug is not on screen at all.
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(VIEW_ROW.name));
    expect(screen.getByTestId("agents-screen").textContent).not.toContain("Reviewer Plus (from agent.rows)");
    // Exactly two asks for this slug at the end state, each with one reason to
    // exist: the table's avatar read for the row, and the panel's own view —
    // which is why the form can say what `agent.view` said while the row still
    // says what the window said. A third would be someone asking twice.
    expect(
      daemon.calls.filter((c) => c.method === "agent.view" && (c.params as { slug: string }).slug === "reviewer-plus"),
    ).toHaveLength(2);
    expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(1);
  });

  it("the row body is a tab bar, and each tab shows its own face of the row", async () => {
    installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^View/);
    await screen.findByTestId("agent-panel-view");
    // Wait for the loaded body, not the animating shell.
    await screen.findByTestId("agent-tab-overview");

    const tabs = await screen.findAllByRole("tab");
    expect(tabs.map((entry) => entry.textContent?.trim())).toEqual([
      "Overview",
      "Prompt",
      "Metadata",
      "History",
    ]);

    // Overview: the identity as `agent.view` answered it — not the window's words —
    // and the stamps the daemon set.
    // The name is the page header's, drawn once; the tab does not say it again.
    await waitFor(() => expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(VIEW_ROW.name));
    expect(screen.queryByTestId("agent-detail-name")).toBeNull();
    expect(screen.getByTestId("agent-tab-id").textContent).toContain("12");
    // The body hash is a Details row, with a copy button for the whole value.
    expect(screen.getByTestId("agent-tab-bodyHash").textContent).toContain("sha256:1f4b0c9d");
    expect(within(screen.getByTestId("agent-tab-bodyHash")).getByRole("button")).toBeTruthy();
    // What it can do now lives here too: tools as words the row declared, the handoff text, the dispatch facts.
    expect(screen.getByTestId("agent-tab-tools").textContent).toContain("read");
    expect(screen.getByTestId("agent-tab-tools").textContent).toContain("write");
    // An empty handoff (`{}`) declares nothing, so the panel leaves it out rather than print braces.
    expect(screen.queryByTestId("agent-tab-handoff")).toBeNull();
    expect(screen.getByTestId("agent-tab-policyAgent").textContent).toContain("policy-reviewer");

    // Prompt: rendered by react-markdown, which shows the stored words (raw HTML stays text).
    await chooseTab(/^Prompt$/i);
    expect((await screen.findByTestId("agent-tab-prompt-view")).textContent).toContain(VIEW_ROW.prompt.trim());

    // Metadata: the blob pretty-printed, the proving bytes in blocks of their own —
    // and an empty one prints nothing, not a labelled blank.
    await chooseTab(/^Metadata$/i);
    expect(screen.getByTestId("agent-detail-metadata").textContent).toContain('"owner": "cjlapao"');
    expect(screen.getByTestId("agent-provenance-bodyHash").textContent).toContain("sha256:1f4b0c9d");
    expect(within(screen.getByTestId("agent-provenance-bodyHash")).getByRole("button")).toBeTruthy();
    expect(screen.queryByTestId("agent-provenance-signature")).toBeNull();

    // History: the ledger, where it always was.
    await chooseTab(/^History$/i);
    expect((await screen.findByTestId("agent-history")).textContent).toContain("edit reviewer-plus");
  });

  it("the ten-field hazard: editing the description alone posts the row back whole", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "agent.edit": { edited: "reviewer-plus" },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-panel-edit");
    // The body arrives a frame after the panel (the kit animates a panel that
    // opens, so it mounts closed): wait for the loaded form, not just the shell.
    await screen.findByTestId("agent-edit-description");

    fireEvent.change(screen.getByTestId("agent-edit-description"), { target: { value: "Reviews pull requests, and flags risky diffs" } });
    expect(screen.getByTestId("agent-cancel")).toBeTruthy();
    expect(screen.queryByTestId("agent-done")).toBeNull();
    fireEvent.click(await screen.findByTestId("agent-panel-save"));
    await waitFor(() => expect(daemon.calls.some((c) => c.method === "agent.edit")).toBe(true));

    const params = daemon.paramsOf("agent.edit") as { slug: string; fields: Record<string, unknown> };
    expect(params.slug).toBe("reviewer-plus");
    // The one field the form was used for.
    expect(params.fields.description).toBe("Reviews pull requests, and flags risky diffs");
    // …and everything the form never rendered, still there.
    expect(params.fields.version).toBe("7");
    expect(params.fields.tools).toBe('["read","list","write"]');
    expect(params.fields.bodyHash).toBe("sha256:1f4b0c9d");
    expect(params.fields.policyAgent).toBe("policy-reviewer");
    expect(params.fields.prompt).toBe(VIEW_ROW.prompt);
    expect(params.fields.skills).toBe(VIEW_ROW.skills);
    expect(params.fields.metadata).toBe(VIEW_ROW.metadata);
    expect(params.fields.avatarType).toBe("minion");
    expect(params.fields.avatarMood).toBe("idle");
    expect(params.fields.internal).toBe(false);
    expect(params.fields.createdAt).toBe(VIEW_ROW.createdAt);
    // The census: every key `agent.view` answered is in the blob, and nothing
    // else is. "Everything except the ones we forgot" is the shape of the bug.
    expect(Object.keys(params.fields).sort()).toEqual(Object.keys(VIEW_ROW).sort());
    // And the eleven this form does render are the eleven the door writes — the same
    // claim from the other side.
    expect([...AGENT_WRITABLE_KEYS].sort()).toEqual(
      ["name", "description", "functionality", "prompt", "skills", "metadata", "avatarType", "avatarColor", "avatarMood", "tools", "handoff"].sort(),
    );
    // The list is asked again; it was never edited in place.
    await waitFor(() => expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(2));
  });

  it("an internal row is not editable, and the reason on screen is the daemon's sentence", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER },
    });
    mount();
    await screen.findByTestId("agents-screen");

    // The panel mounted on its own rather than through the row menu, because T6
    // CU6 correctly disables `Edit` on a first-party row in the menu. This is the
    // defensive path: a window that still lists the row, and `agent.view` as the
    // authority on whether it can be touched.
    render(
      <I18nProvider locales={APP_LOCALES}>
        <ToastProvider><ToastViewport />
        <AgentPanel
          mode="edit"
          slug="golang-engineer"
          controller={new LiveController()}
          live={liveCurrent}
          canWrite
          onClose={() => undefined}
          onChanged={() => undefined}
        />
        </ToastProvider>
      </I18nProvider>,
    );
    await screen.findByTestId("agent-panel-first-party");

    expect(screen.getByTestId("agent-panel-first-party").textContent).toContain(
      refusalFor(INTERNAL_EDIT_REFUSAL, "golang-engineer"),
    );
    // No Save, so there is no way to ask the question the daemon already answered.
    expect(within(screen.getByTestId("agent-panel-edit")).queryByTestId("agent-panel-save")).toBeNull();
    expect(daemon.methods()).not.toContain("agent.edit");
    // The nine fields are still on screen — read-only, above the refusal's line —
    // because hiding the row is not the same as refusing to change it.
    expect(screen.getByTestId("agent-edit-overview")).toBeTruthy();
    expect((field("Name") as HTMLInputElement).disabled).toBe(true);
  });

  it("refuses to delete without the slug typed, then prints the refusal whole and keeps the row", async () => {
    const am4 =
      'refused: delete of reviewer-plus — 2 policy row(s) still reference it (policy "grant-read"); remove or re-point them first (AM4)';
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "agent.delete": () => {
          throw refusalEnvelope(am4);
        },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Delete/);
    // `DeleteConfirmModal` is the kit's `alertdialog`, not a plain dialog
    // (`dist/index.js:44259` onward), and the query says so.
    const dialog = await screen.findByRole("alertdialog");

    // The typed confirmation: a wrong slug cannot fire it (CU5).
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "reviewer-plu" } });
    expect(
      (within(dialog).getByRole("button", { name: /Delete this agent/i }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(daemon.methods()).not.toContain("agent.delete");

    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "reviewer-plus" } });
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete this agent/i }));

    await waitFor(() => expect(daemon.methods()).toContain("agent.delete"));
    expect(daemon.paramsOf("agent.delete")).toEqual({ slug: "reviewer-plus" });
    // Every reference the daemon named, whole — read out of `data.what`, the
    // only place the sentence travels — and the row still in the table.
    // ... in a toast that stays until it is dismissed (the toast's own test pins that).
    const shown = (await screen.findByTestId("toast-error-parts")).textContent ?? "";
    expect(shown).toContain('policy "grant-read"');
    expect(shown).toContain("remove or re-point them first (AM4)");
    // The daemon's own sentence is the toast's title.
    expect(screen.getByText(/the agent-services boundary refused/)).toBeTruthy();
    expect(screen.getByTestId("agents-cell-name-reviewer-plus")).toBeTruthy();
    // A refusal is not a change, so the table is not asked again for it.
    expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(1);
  });

  it("deletes with the daemon's sentence about what a delete means, and asks the table again", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "agent.delete": { deleted: "reviewer-plus" },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Delete/);
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("reviewer-plus");
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "reviewer-plus" } });
    fireEvent.click(within(dialog).getByRole("button", { name: /Delete this agent/i }));

    expect(await screen.findByText(/the daemon removed reviewer-plus/)).toBeTruthy();
    expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(2);
    // The OTHER row is untouched: the delete named one slug and moved one.
    expect(screen.getByTestId("agents-cell-name-golang-engineer")).toBeTruthy();
  });

  it("asks before throwing a draft away, and stays open when asked to keep it", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-panel-edit");
    // The body arrives a frame after the panel (the kit animates a panel that
    // opens, so it mounts closed): wait for the loaded form, not just the shell.
    await screen.findByTestId("agent-edit-description");
    // Nothing changed: Done, no Save, no Cancel.
    expect(screen.queryByTestId("agent-panel-save")).toBeNull();
    expect(screen.queryByTestId("agent-cancel")).toBeNull();
    expect(await screen.findByTestId("agent-done")).toBeTruthy();

    fireEvent.change(field("Name"), { target: { value: "Renamed in a draft" } });
    await waitFor(() => expect((screen.getByTestId("agent-panel-save") as HTMLButtonElement).disabled).toBe(false));

    closePanel();
    const ask = await screen.findByTestId("agents-discard-ask");
    fireEvent.click(within(ask).getByRole("button", { name: /Keep editing/i }));
    expect(screen.getByTestId("agent-panel-edit")).toBeTruthy();
    expect(daemon.methods()).not.toContain("agent.edit");

    closePanel();
    const askAgain = await screen.findByTestId("agents-discard-ask");
    fireEvent.click(within(askAgain).getByRole("button", { name: /Discard the draft/i }));
    await waitFor(() => expect(screen.queryByTestId("agent-panel-edit")).toBeNull());
  });

  it("keeps a write that was already flying when the panel closed, and re-reads the table when it lands", async () => {
    let release: ((value: unknown) => void) | undefined;
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "agent.edit": () =>
          new Promise((resolve) => {
            release = resolve;
          }),
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    await chooseRowVerb("reviewer-plus", /^Edit/);
    await screen.findByTestId("agent-panel-edit");
    // The body arrives a frame after the panel (the kit animates a panel that
    // opens, so it mounts closed): wait for the loaded form, not just the shell.
    await screen.findByTestId("agent-edit-description");
    // The prompt lives on the Prompt tab now, and that tab edits it with the
    // kit's markdown editor (stubbed above): the keystroke goes through the
    // draft exactly as the old textarea's did.
    await chooseTab(/^Prompt$/i);
    fireEvent.change(await screen.findByTestId("md-edit"), { target: { value: "A prompt nobody has read yet" } });
    fireEvent.click(await screen.findByTestId("agent-panel-save"));
    await waitFor(() => expect(daemon.methods()).toContain("agent.edit"));

    // Closing now cannot un-send it, and the page says nothing as if it could.
    closePanel();
    const ask = await screen.findByTestId("agents-discard-ask");
    fireEvent.click(within(ask).getByRole("button", { name: /Discard the draft/i }));
    expect(screen.queryByTestId("agent-panel-edit")).toBeNull();
    expect(screen.getByTestId("agents-screen").textContent).not.toMatch(/cancelled|canceled/i);

    release?.({ edited: "reviewer-plus" });
    await waitFor(() => expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(2));
  });

  it("migrates through a confirm and tells the count the daemon answered in a toast", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "agent.rows": rowsAnswer,
        "agent.view": viewAnswer,
        "agent.changes": LEDGER,
        "agent.migrate": { created: 4 },
      },
    });
    mount();
    await screen.findByTestId("agents-screen");

    fireEvent.click(screen.getByTestId("agents-migrate"));
    await screen.findByTestId("agent-migrate-modal");
    fireEvent.click(screen.getByTestId("agent-migrate-run"));

    // The count is a toast, and the dialog closes by itself.
    expect(await screen.findByText(/the daemon created 4 agents/)).toBeTruthy();
    await waitFor(() => expect(screen.queryByTestId("agent-migrate-modal")).toBeNull());
    expect(daemon.paramsOf("agent.migrate")).toEqual({});
    expect(daemon.methods().filter((m) => m === "agent.rows")).toHaveLength(2);
  });

  it("refuses every write the shell cannot make, and says whose refusal the words are", async () => {
    const daemon = installFakeDaemon({
      responses: { "agent.rows": rowsAnswer, "agent.view": viewAnswer, "agent.changes": LEDGER },
    });
    mount(false);
    await screen.findByTestId("agents-screen");

    // The first defence is that the doors are shut: with the shell not current,
    // the action bar's writes are disabled, so clicking one opens nothing.
    for (const id of ["agents-create", "agents-import", "agents-migrate"]) {
      expect((screen.getByTestId(id) as HTMLButtonElement).disabled).toBe(true);
    }
    fireEvent.click(screen.getByTestId("agents-create"));
    expect(screen.queryByTestId("agent-panel-create")).toBeNull();

    // The second is the panel's own: reached by any road (a stale window, a
    // bookmark), it says whose refusal the words are and offers nothing to press.
    render(
      <I18nProvider locales={APP_LOCALES}>
        <ToastProvider><ToastViewport />
        <AgentPanel
          mode="create"
          slug=""
          controller={new LiveController()}
          live={liveCurrent}
          canWrite={false}
          onClose={() => undefined}
          onChanged={() => undefined}
        />
        </ToastProvider>
      </I18nProvider>,
    );
    await screen.findByTestId("agent-panel-create");
    expect(screen.getByTestId("agent-panel-readonly").textContent).toContain("Not connected");
    expect((screen.getByTestId("agent-panel-create-run") as HTMLButtonElement).disabled).toBe(true);
    expect(daemon.methods()).not.toContain("agent.create");
  });
});
