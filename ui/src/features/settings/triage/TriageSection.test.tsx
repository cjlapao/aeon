import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { notifications } from "../../../lib/notifications";
import { TriageSection } from "./TriageSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { mountConfigPage } from "../../../../test/configPage";
import { triageBaseResponses, TRIAGE_VOCABULARY } from "../../../../test/triage-fixtures";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

/** The component the board opens on: the page offers the roots `component.list` answers. */
const LEDGER = { id: "cmp_ledger", name: "ledger", parentId: "", root: "/srv/projects/ledger", slug: "ledger", tags: [] };
const withComponents = (responses: Record<string, unknown>): Record<string, unknown> => ({ ...responses, "component.list": { components: [LEDGER] } });

const staleLive = {
  state: "stale",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  since: Date.now() - 4000,
  cause: "daemon restarted",
  retry: 1,
} as const satisfies LiveState;

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

function renderSection(
  responses: Record<string, unknown> = withComponents(triageBaseResponses()),
  live: LiveState = currentLive,
  canWrite?: boolean,
): FakeDaemon {
  const daemon = installFakeDaemon({ responses: withComponents(responses) });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <TriageSection controller={new LiveController()} live={live} {...(canWrite === undefined ? {} : { canWrite })} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

async function loadBoard(daemon: FakeDaemon): Promise<void> {
  await screen.findByTestId("triage-section");
  await screen.findByTestId("triage-row-7"); // the board opens on the first component
  expect(daemon.calls.some((c) => c.method === "triage.board")).toBe(true);
}

describe("TriageSection (S51 T7)", () => {
  it("the board opens on the first component and the daemon does the filtering", async () => {
    const daemon = renderSection();
    await screen.findByTestId("triage-row-7");
    const board = daemon.calls.filter((c) => c.method === "triage.board").pop();
    expect(board?.params).toMatchObject({ componentRoot: "/srv/projects/ledger" });
    // Untouched dials never ride the wire (omitempty, all three).
    expect(board?.params).not.toHaveProperty("validation");
    expect(board?.params).not.toHaveProperty("priority");
    expect(board?.params).not.toHaveProperty("handedOff");
  });

  it("with no component to point at, the board is not asked at all", async () => {
    const daemon = renderSection({ ...triageBaseResponses(), "component.list": { components: [] } });
    expect(await screen.findByTestId("triage-needs-root")).toBeTruthy();
    expect(daemon.methods()).not.toContain("triage.board");
  });

  it("row 7: unvalidated beside known, each its own pill, each with its daemon sentence on its own line", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    const row = screen.getByTestId("triage-row-7").closest("tr") as HTMLElement;
    expect(within(row).getByTestId("triage-validation-7").textContent).toBe("unvalidated");
    expect(within(row).getByTestId("triage-priority-7").textContent).toBe("known");
    // The two closed sets never merge into one control:
    expect(within(row).getByTestId("triage-validation-7")).not.toBe(within(row).getByTestId("triage-priority-7"));
    expect(screen.getByTestId("triage-validation-sentence-7").textContent).toContain("no attempt has run (D1)");
    expect(screen.getByTestId("triage-priority-sentence-7").textContent).toContain("\"known\"");
    // Attempts, counted by the daemon:
    expect(screen.getByTestId("triage-attempts-7").textContent).toContain("0");
    // The scored candidate rides the row, and is offered in the bug's panel:
    fireEvent.click(row);
    expect((await screen.findByTestId("triage-duplicate-candidate-9")).textContent).toContain("#9");
  });

  it("could-not-tell is visibly NOT unvalidated, and an assumed priority says assumed", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    expect(screen.getByTestId("triage-validation-10").getAttribute("class")).not.toBe(
      screen.getByTestId("triage-validation-7").getAttribute("class"),
    );
    expect(screen.getByTestId("triage-validation-10").textContent).toBe("could-not-tell");
    expect(screen.getByTestId("triage-priority-10").textContent).toBe("assumed");
    // A landed hand-off names the roadmap item and links to the roadmap screen (in the bug's panel):
    fireEvent.click(screen.getByTestId("triage-row-10"));
    expect((await screen.findByTestId("triage-drawer-handoff")).textContent).toContain("roadmap item 42");
    expect(screen.getByTestId("triage-roadmap-link").getAttribute("href")).toBe("/roadmap#item-42");
  });

  it("the vocabulary is read once for the section and its words are the filter dials", async () => {
    const daemon = renderSection();
    await screen.findByTestId("triage-section");
    expect(daemon.calls.filter((c) => c.method === "triage.vocabulary")).toHaveLength(1);
    const validation = screen.getByTestId("triage-filter-validation") as HTMLSelectElement;
    const options = [...validation.options].map((o) => o.value).filter((v) => v !== "");
    expect(options).toEqual(TRIAGE_VOCABULARY.validationStates);
    const priority = screen.getByTestId("triage-filter-priority") as HTMLSelectElement;
    expect([...priority.options].map((o) => o.value).filter((v) => v !== "")).toEqual(TRIAGE_VOCABULARY.priorityStates);
  });

  it("validate is a two-click act that quotes the daemon's OWN statement, byte for byte", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    await screen.findByTestId("triage-drawer-7");
    fireEvent.click(screen.getByTestId("triage-ask-validate"));
    // The confirm shows the statement and NOTHING has run yet:
    expect(screen.getByTestId("triage-statement-validate").textContent).toBe(TRIAGE_VOCABULARY.validateStatement);
    expect(daemon.methods()).not.toContain("triage.validate");
    fireEvent.click(screen.getByTestId("triage-go-validate"));
    await waitFor(() => expect(daemon.methods()).toContain("triage.validate"));
    expect(daemon.paramsOf("triage.validate")).toEqual({ id: 7 });
    expect((await screen.findByTestId("triage-answer")).textContent).toContain("loopId: triage-validate-7");
  });

  it("declining a confirm sends NOTHING", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.click(await screen.findByTestId("triage-ask-handoff"));
    expect(screen.getByTestId("triage-statement-handoff").textContent).toBe(TRIAGE_VOCABULARY.handoffStatement);
    fireEvent.click(screen.getByTestId("triage-confirm-cancel"));
    expect(daemon.methods()).not.toContain("triage.handoff");
  });

  it("hand-off answers with the roadmap item and links to the screen it fed", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.click(await screen.findByTestId("triage-ask-handoff"));
    fireEvent.click(screen.getByTestId("triage-go-handoff"));
    expect(await screen.findByTestId("triage-handoff-link")).toBeTruthy();
    expect(screen.getByTestId("triage-handoff-link").textContent).toContain("#42");
  });

  it("the edit form can only send title and body (TM1), and answers changed:false honestly", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.change(await screen.findByTestId("triage-edit-title"), { target: { value: "a better title" } });
    fireEvent.click(screen.getByTestId("triage-edit-save"));
    await waitFor(() => expect(daemon.methods()).toContain("triage.update"));
    const params = daemon.paramsOf("triage.update") as Record<string, unknown>;
    expect(Object.keys(params).sort()).toEqual(["body", "id", "title"]); // nothing else CAN ride
    expect(params.title).toBe("a better title");
    expect(await screen.findByTestId("triage-edit-saved")).toBeTruthy();
  });

  it("a refusal is a verbatim toast, keeps the draft, and names nothing of its own", async () => {
    const push = vi.spyOn(notifications, "push");
    const sentence = "triage refused the edit: the validation state is not editable by hand";
    const daemon = installFakeDaemon({
      responses: withComponents({ ...triageBaseResponses(), "triage.update": () => { throw JSON.stringify({ code: -31910, message: sentence }); } }),
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <TriageSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.change(await screen.findByTestId("triage-edit-title"), { target: { value: "my kept draft" } });
    fireEvent.click(screen.getByTestId("triage-edit-save"));
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain(sentence); // VERBATIM, as a toast
    expect((screen.getByTestId("triage-edit-title") as HTMLInputElement).value).toBe("my kept draft"); // draft SURVIVES
  });

  it("confirm-duplicate names the pair and demands the person's name; the daemon checks it", async () => {
    const daemon = renderSection();
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.click(await screen.findByTestId("triage-mark-duplicate-9"));
    expect(await screen.findByTestId("triage-duplicate-confirm")).toBeTruthy();
    // No name, no call:
    fireEvent.click(screen.getByTestId("triage-duplicate-go"));
    expect(daemon.methods()).not.toContain("triage.confirm-duplicate");
    fireEvent.change(screen.getByTestId("triage-confirmed-by"), { target: { value: "Carlos" } });
    fireEvent.click(screen.getByTestId("triage-duplicate-go"));
    await waitFor(() => expect(daemon.methods()).toContain("triage.confirm-duplicate"));
    expect(daemon.paramsOf("triage.confirm-duplicate")).toEqual({
      componentRoot: "/srv/projects/ledger",
      id: 7,
      of: 9,
      confirmedBy: "Carlos",
    });
  });

  it("a machine confirmer is REFUSED BY NAME and the toast shows that refusal, not a rewrite", async () => {
    const push = vi.spyOn(notifications, "push");
    const sentence = "triage refused: confirmedBy \"golang-engineer\" names an agent, not a person (D5)";
    const daemon = installFakeDaemon({
      responses: withComponents({ ...triageBaseResponses(), "triage.confirm-duplicate": () => { throw JSON.stringify({ code: -31910, message: sentence }); } }),
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <TriageSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.click(await screen.findByTestId("triage-mark-duplicate-9"));
    fireEvent.change(screen.getByTestId("triage-confirmed-by"), { target: { value: "golang-engineer" } });
    fireEvent.click(screen.getByTestId("triage-duplicate-go"));
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain(sentence);
  });

  it("the blueprint card shows the daemon's View, its health word as text, and saves through settings.set", async () => {
    const daemon = renderSection();
    await screen.findByTestId("triage-section");
    fireEvent.click(screen.getByTestId("triage-open-setting"));
    await screen.findByTestId("triage-blueprint-card");
    expect(screen.getByTestId("triage-setting-key-triage.validationBlueprint")).toBeTruthy();
    expect((await screen.findByTestId("triage-health-dot")).textContent).toBe("ok");
    expect(screen.getByTestId("triage-setting-source-triage.validationBlueprint").textContent).toBe("default");
    fireEvent.change(within(screen.getByTestId("blueprint-picker")).getByRole("combobox"), { target: { value: "brainstorm" } });
    fireEvent.click(screen.getByTestId("triage-setting-save"));
    await waitFor(() => expect(daemon.methods()).toContain("settings.set"));
    expect(daemon.paramsOf("settings.set")).toEqual({ key: "triage.validationBlueprint", value: "brainstorm" });
    expect(await screen.findByTestId("triage-setting-saved")).toBeTruthy();
  });

  it("a missing blueprint answers its named refusal verbatim — no fallback is offered", async () => {
    const push = vi.spyOn(notifications, "push");
    const sentence = "triage refused validation: the blueprint named by triage.validationBlueprint does not exist — nothing was started (TM4: refused, not defaulted)";
    const daemon = installFakeDaemon({
      responses: withComponents({
        ...triageBaseResponses(),
        "triage.health": { settings: [{ key: "triage.validationBlueprint", name: "Validation blueprint", state: "missing" }], evaluatedAt: "2026-11-04T12:00:00Z" },
        "triage.validate": () => { throw JSON.stringify({ code: -31910, message: sentence }); },
      }),
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <TriageSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await loadBoard(daemon);
    fireEvent.click(screen.getByTestId("triage-open-setting"));
    expect((await screen.findByTestId("triage-health-dot")).textContent).toBe("missing");
    fireEvent.keyDown(document.body, { key: "Escape" });
    fireEvent.click(screen.getByTestId("triage-row-7"));
    fireEvent.click(await screen.findByTestId("triage-ask-validate"));
    fireEvent.click(await screen.findByTestId("triage-go-validate"));
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(String(push.mock.calls[0]?.[0]?.body ?? "")).toContain(sentence);
  });

  it("a stale connection asks nothing at all and says so", async () => {
    const daemon = renderSection(withComponents(triageBaseResponses()), staleLive);
    expect(await screen.findByTestId("triage-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("triage.board");
    expect(daemon.methods()).not.toContain("triage.vocabulary");
  });

  it("offline disables writes without hiding the reads", async () => {
    const daemon = renderSection(withComponents(triageBaseResponses()), currentLive, false);
    await loadBoard(daemon);
    expect(screen.getByTestId("triage-row-7")).toBeTruthy();
    fireEvent.click(screen.getByTestId("triage-row-7"));
    expect(((await screen.findByTestId("triage-edit-title")) as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByTestId("triage-ask-validate") as HTMLButtonElement).disabled).toBe(true);
    expect(daemon.methods()).not.toContain("triage.update");
  });

  it("a daemon answering bare {} is survivable: an honest empty, not a crash", async () => {
    installFakeDaemon({ responses: {} });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <TriageSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByTestId("triage-section");
    // Nothing named, nothing asked: an honest "name a component", never a crash.
    expect(await screen.findByTestId("triage-needs-root")).toBeTruthy();
  });

  it("the section moved into Work: /config/triage redirects to its Work tab", async () => {
    // S72 T2: this test pinned "mounted inside SettingsScreen". The mount
    // moved to the Configuration page (CF3/CF8); the assertion follows it —
    // recorded in the slice Notes, step 4.
    installFakeDaemon({
      responses: withComponents({
        ...triageBaseResponses(),
      }),
    });
    mountConfigPage("triage", currentLive);
    expect((await screen.findByTestId("moved-to")).textContent).toBe("/work/all/guardrails");
  });
});
