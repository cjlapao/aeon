import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { PolicyDetailDrawer } from "./PolicyDetailDrawer";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";

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

function policyRow(overrides: Record<string, unknown> = {}) {
  return {
    id: "cmd.7",
    type: "command_access",
    action: "deny",
    source: "operator",
    origin: "user",
    target: "agent:golang-engineer",
    describe: "deny command git push — rule cmd.7",
    notAfter: null,
    createdAt: "2026-01-01T00:00:00Z",
    authored: true,
    acceptedBy: "user:operator",
    acceptedAt: "2026-01-01T00:00:00Z",
    spec: { pattern: "git push" },
    ...overrides,
  };
}

function renderDrawer(responses: Record<string, unknown> = {}, props: Partial<Parameters<typeof PolicyDetailDrawer>[0]> = {}): FakeDaemon {
  const daemon = installFakeDaemon({
    responses: {
      "policy.get": { policy: policyRow() },
      "policy.changes": { changes: [], statement: "no authoring changes recorded." },
      ...responses,
    },
  });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <PolicyDetailDrawer
          id="cmd.7"
          controller={new LiveController()}
          live={currentLive}
          canWrite
          onClose={() => {}}
          onChanged={() => {}}
          {...props}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("PolicyDetailDrawer", () => {
  it("shows the whole row, the raw spec READ-ONLY, and the id's audit", async () => {
    const daemon = renderDrawer({
      "policy.changes": {
        changes: [
          { id: 3, op: "add", actorKind: "user", actorId: "operator", policyId: "cmd.7", occurredAt: "2026-01-01T00:00:00Z", summary: "deny command git push (rule cmd.7)" },
        ],
      },
    });
    const drawer = await screen.findByTestId("policy-drawer");
    // describe verbatim; authored/accepted from the daemon.
    expect(within(drawer).getByTestId("drawer-describe").textContent).toBe("deny command git push — rule cmd.7");
    expect(drawer.textContent).toContain("the audit carries its authoring record");
    // The raw spec, as stored, in a read-only block.
    const spec = within(drawer).getByTestId("drawer-spec");
    expect(spec.getAttribute("aria-readonly")).toBe("true");
    expect(spec.textContent).toContain('"pattern": "git push"');
    // The audit for this id — `policy.changes {policyId}` — summary verbatim.
    expect(screen.getByTestId("drawer-audit").textContent).toContain("deny command git push (rule cmd.7)");
    expect(daemon.calls).toContainEqual({ method: "policy.changes", params: { policyId: "cmd.7" } });
  });

  it("remove names the policy, carries describe + the fetched current in-force, and the result is verbatim before/after", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "policy.get": { policy: policyRow() },
        "policy.changes": { changes: [] },
        "policy.inForce": { target: "agent:golang-engineer", answers: [{ policyID: "cmd.1", state: "deny", render: "deny command rm -rf — rule cmd.1" }] },
        "policy.remove": {
          removed: true,
          policy: policyRow(),
          before: { target: "agent:golang-engineer", answers: [{ policyID: "cmd.7", state: "deny", render: "deny command git push — rule cmd.7" }] },
          after: { target: "agent:golang-engineer", answers: [] },
        },
      },
    });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <PolicyDetailDrawer id="cmd.7" controller={new LiveController()} live={currentLive} canWrite onClose={() => {}} onChanged={() => {}} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await screen.findByTestId("policy-drawer");
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    // The in-force read happens BEFORE the confirm — the evidence is fetched.
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "policy.inForce", params: { agent: "golang-engineer" } }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("Remove policy cmd.7?");
    expect(dialog.textContent).toContain("deny command git push — rule cmd.7"); // describe, verbatim
    expect(dialog.textContent).toContain("deny command rm -rf — rule cmd.1"); // current in-force, fetched
    // Decline sends nothing.
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(daemon.calls.filter((c) => c.method === "policy.remove")).toHaveLength(0);
    // Confirm: type the policy id.
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    const confirmInput = await within(await screen.findByRole("alertdialog")).findByPlaceholderText("cmd.7");
    fireEvent.change(confirmInput, { target: { value: "cmd.7" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "policy.remove", params: { id: "cmd.7" } }));
    const result = await screen.findByTestId("remove-result");
    expect(result.textContent).toContain("deny command git push — rule cmd.7"); // before, verbatim
    expect(result.textContent).toContain("Removed"); // and the change is said
  });

  it("a system row shows the PL4 hint, its confirm carries the PM16 statement, and no edit control exists anywhere", async () => {
    const daemon = renderDrawer({
      "policy.get": { policy: policyRow({ origin: "system", source: "import" }) },
      "policy.changes": { changes: [] },
    });
    const drawer = await screen.findByTestId("policy-drawer");
    // The provenance badge is visible and the shipped-rule hint is said.
    expect(within(drawer).getByTestId("drawer-pl4-hint").textContent).toContain("Shipped rules are not edited");
    // No edit affordance at all (PM16) — Remove is the ONLY write offered.
    expect(within(drawer).queryByRole("button", { name: /edit|save/i })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    // The daemon's own PM16 line rides the confirmation for the system row.
    expect(dialog.textContent).toContain("the re-seed will not put it back");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(daemon.calls.filter((c) => c.method === "policy.remove")).toHaveLength(0);
  });
});
