import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { PolicyForm } from "./PolicyForm";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import type { Vocabulary } from "./policyModel";

// The vocabulary the FORM is built from — test fixtures may name the closed
// set (the guard's fixture exemption) precisely to prove the production form
// recalls none of it: change this fixture and the form changes with it.
const vocabulary: Vocabulary = {
  types: ["tool_access", "command_access"],
  targetKinds: ["all-agents", "agent", "loop", "lane-type", "mcp"],
  sources: ["operator", "agent", "mcp_server", "import"],
  actions: ["allow", "deny", "ask"],
  authorship: [
    { source: "operator", action: "allow", allowed: true },
    { source: "operator", action: "deny", allowed: true },
    { source: "agent", action: "allow", allowed: false, reason: 'refused: an allow may only be authored by source "operator", not "agent" (PL1)' },
    { source: "agent", action: "deny", allowed: true },
  ],
  specs: [
    {
      type: "tool_access",
      fields: [
        { name: "toolName", kind: "string", required: true, description: "the tool" },
        { name: "state", kind: "enum", required: true, enumValues: ["allow", "deny", "ask"], description: "verdict" },
      ],
    },
  ],
};

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
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

const addOk = {
  policy: { id: "tool_access.loop", describe: "deny tool bash for loop build-1", action: "deny", source: "operator", origin: "user", target: "loop", type: "tool_access", spec: { toolName: "bash" }, notAfter: null, createdAt: "2026-01-01T00:00:00Z", authored: true },
  before: { answers: [], target: "loop" },
  after: { answers: [{ policyID: "tool_access.loop", state: "deny", render: "deny tool bash — rule tool_access.loop" }], target: "loop" },
  warnings: ["broad wildcard: this allow matches a quarter of the corpus"],
};

function renderForm(responses: Record<string, unknown> = {}, props: Partial<Parameters<typeof PolicyForm>[0]> = {}): { daemon: FakeDaemon; onAdded: ReturnType<typeof vi.fn> } {
  const onAdded = vi.fn();
  const daemon = installFakeDaemon({
    responses: { "agent.list": { agents: [{ name: "golang-engineer", provenance: "first_party", state: "running" }] }, ...responses },
  });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <PolicyForm
          vocabulary={vocabulary}
          canWrite
          controller={new LiveController()}
          live={currentLive}
          busy={false}
          onCancel={() => {}}
          onAdded={onAdded}
          {...props}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { daemon, onAdded };
}

function choose(label: string, value: string): void {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

/** Choose the first agent in the tag picker: open it, click the first option. */
async function pickFirstAgent(): Promise<void> {
  const trigger = await waitFor(() => {
    const found = document.querySelector('[data-testid="agent-tag-picker"] button[aria-haspopup="listbox"]:not([disabled])');
    if (found === null) throw new Error("the agent picker is not ready");
    return found as HTMLElement;
  });
  fireEvent.click(trigger);
  // The roster loads after the trigger exists: wait for a real option, not the loading line.
  const option = await waitFor(() => {
    // The picker's rows are list items; a native <option> elsewhere on the page is not one of them.
    const found = document.querySelector('li[role="option"]');
    if (found === null) throw new Error("the roster is not in the list yet");
    return found as HTMLElement;
  });
  fireEvent.click(option);
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
}

/** Fill the form to a submittable deny of tool bash for a NAMED agent —
 *  a named target does not widen, so Submit runs the door directly. The
 *  target id arrives through the picker (only a real roster value selects). */
async function fillDeny(): Promise<void> {
  choose("Type", "tool_access");
  choose("Target kind", "agent");
  await pickFirstAgent();
  choose("Action", "deny");
  choose("toolName", "bash");
  choose("state", "deny");
}

describe("PolicyForm", () => {
  it("a refused matrix cell disables Submit and shows the daemon's reason verbatim", async () => {
    renderForm();
    choose("Type", "tool_access");
    choose("Target kind", "loop");
    choose("Action", "allow");
    choose("Source", "agent"); // the matrix cell (agent, allow) is refused
    choose("toolName", "bash");
    choose("state", "allow");
    const refusal = await screen.findByTestId("matrix-refusal");
    // Word for word from the vocabulary response — not re-authored here.
    expect(refusal.textContent).toContain('refused: an allow may only be authored by source "operator", not "agent" (PL1)');
    expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("a missing required spec field keeps Submit disabled (completeness, not guessing)", () => {
    renderForm();
    choose("Type", "tool_access");
    choose("Target kind", "loop");
    choose("Action", "deny");
    choose("toolName", "bash");
    // state (required) untouched
    expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("an empty expiry sends notAfter: null — never a far-future sentinel", async () => {
    const { daemon } = renderForm({ "policy.add": addOk });
    await fillDeny();
    await waitFor(() => expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => expect(daemon.methods()).toContain("policy.add"));
    expect(daemon.paramsOf("policy.add")).toMatchObject({
      type: "tool_access",
      action: "deny",
      target: { kind: "agent", id: "golang-engineer" },
      notAfter: null,
      spec: { toolName: "bash", state: "deny" },
    });
  });

  it("a typed expiry travels as the person typed it", async () => {
    const { daemon } = renderForm({ "policy.add": addOk });
    await fillDeny();
    fireEvent.change(screen.getByLabelText("Expiry (optional)"), { target: { value: "2030-01-01T00:00:00Z" } });
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await waitFor(() => expect(daemon.methods()).toContain("policy.add"));
    expect(daemon.paramsOf("policy.add")).toMatchObject({ notAfter: "2030-01-01T00:00:00Z" });
  });

  it("a widening submit goes through the confirm — decline sends NOTHING", async () => {
    const { daemon } = renderForm({ "policy.add": addOk });
    choose("Type", "tool_access");
    choose("Target kind", "loop");
    choose("Action", "allow");
    choose("toolName", "bash");
    choose("state", "allow");
    await waitFor(() => expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    // The confirmation carries the draft's identity — not a sentence the UI invented.
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("tool_access");
    expect(dialog.textContent).toContain("loop");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(daemon.calls.filter((c) => c.method === "policy.add")).toHaveLength(0);
  });

  it("a confirmed widening submits, and the result carries the door's before/after and warnings verbatim", async () => {
    const { daemon, onAdded } = renderForm({ "policy.add": addOk });
    choose("Type", "tool_access");
    choose("Target kind", "loop");
    choose("Action", "allow");
    choose("toolName", "bash");
    choose("state", "allow");
    await waitFor(() => expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    // The Id field's own placeholder is the suggestion too — scope the
    // confirmation input to the dialog.
    const dialog2 = await screen.findByRole("alertdialog");
    const confirmInput = await within(dialog2).findByPlaceholderText("tool_access.loop"); // the suggested id
    fireEvent.change(confirmInput, { target: { value: "tool_access.loop" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() => expect(daemon.calls).toContainEqual(
      expect.objectContaining({ method: "policy.add" }),
    ));
    // The daemon's own lines: the changed row's describe, the after answer,
    // the broad-wildcard warning — word for word.
    const result = await screen.findByTestId("add-result");
    expect(result.textContent).toContain("deny tool bash for loop build-1");
    expect(result.textContent).toContain("deny tool bash — rule tool_access.loop");
    expect(result.textContent).toContain("broad wildcard: this allow matches a quarter of the corpus");
    expect(onAdded).toHaveBeenCalledWith("tool_access.loop");
  });

  it("a refusal keeps the draft — the person corrects, never rerolls", async () => {
    const refusal = 'refused: policy id "tool_access.loop" already exists';
    const { daemon } = renderForm({
      "policy.add": () => {
        throw new Error(refusal);
      },
    });
    await fillDeny();
    await waitFor(() => expect((screen.getByRole("button", { name: "Submit" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    const alert = await screen.findByTestId("add-refusal");
    expect(alert.textContent).toContain(refusal); // the boundary's words, verbatim
    // The draft is intact for the next attempt.
    expect((screen.getByLabelText("toolName") as HTMLInputElement).value).toBe("bash");
    expect((screen.getByLabelText("Type") as HTMLSelectElement).value).toBe("tool_access");
    expect(daemon.calls.filter((c) => c.method === "policy.add")).toHaveLength(1);
  });

  it("the form is BUILT from the vocabulary — a different vocabulary, a different form, no UI edit", async () => {
    const other: Vocabulary = {
      types: ["route_access"],
      targetKinds: ["mcp"],
      sources: ["operator", "import"],
      actions: ["deny"],
      authorship: [{ source: "operator", action: "deny", allowed: true }],
      specs: [
        {
          type: "route_access",
          fields: [{ name: "route", kind: "string", required: false, description: "the route" }],
        },
      ],
    };
    renderForm({}, { vocabulary: other });
    const typeSelect = screen.getByLabelText("Type") as HTMLSelectElement;
    // Only the other vocabulary's type is offered — the first one's is gone.
    await waitFor(() => expect(Array.from(typeSelect.options).map((o) => o.value)).toEqual(["", "route_access"]));
    fireEvent.change(typeSelect, { target: { value: "route_access" } });
    // And its spec fields are the descriptor's own — `route`, not `toolName`.
    expect(await screen.findByLabelText("route")).toBeTruthy();
    expect(screen.queryByLabelText("toolName")).toBeNull();
  });

  it("the suggested id follows type+target and the person may edit it", () => {
    renderForm();
    choose("Type", "tool_access");
    choose("Target kind", "agent");
    const idInput = screen.getByLabelText("Id") as HTMLInputElement;
    // The suggestion is deterministic from the chosen values — no 8-hex noise.
    expect(idInput.value).toBe("tool_access.agent");
    choose("Target kind", "loop");
    expect(idInput.value).toBe("tool_access.loop");
    fireEvent.change(idInput, { target: { value: "my.own.rule" } });
    expect(idInput.value).toBe("my.own.rule");
  });
});
