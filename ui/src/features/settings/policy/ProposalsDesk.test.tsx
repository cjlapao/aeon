import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ProposalsDesk } from "./ProposalsDesk";
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

function proposalRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 7,
    kind: "policy_add",
    status: "proposed",
    createdAt: "2026-02-02T10:00:00Z",
    createdByKind: "agent",
    createdById: "golang-engineer",
    command: "aeon proposal commit 7",
    payload: { reason: "the build lane needs read access to go.mod" },
    ...overrides,
  };
}

const previewFor = (row: Record<string, unknown>) => ({
  proposal: row,
  digest: "aabbccdd11223344",
  preview: {
    kind: String(row["kind"]),
    target: "agent:golang-engineer",
    describe: "allow tool read for agent golang-engineer",
    inForce: [{ policyID: "cmd.1", state: "deny", render: "deny command git push — rule cmd.1" }],
  },
});

function renderDesk(responses: Record<string, unknown> = {}, canWrite = true): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <ProposalsDesk controller={new LiveController()} live={currentLive} canWrite={canWrite} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("ProposalsDesk", () => {
  it("the status pile is a daemon param, not a client-side filter", async () => {
    const daemon = renderDesk({ "proposal.list": { proposals: [] } });
    await screen.findByText(/No proposed proposals/i);
    expect(daemon.paramsOf("proposal.list")).toEqual({ status: "proposed" });
  });

  it("rows show identity, the agent-proposed badge and the row's own CLI command", async () => {
    renderDesk({
      "proposal.list": { proposals: [proposalRow()] },
      "proposal.get": () => previewFor(proposalRow()),
    });
    await screen.findByText(/golang-engineer/);
    expect(screen.getByText("proposed by an agent")).toBeTruthy();
    // The EXACT command the daemon sent on the row — rendered, not composed.
    expect(screen.getByText("aeon proposal commit 7")).toBeTruthy();
    // The live-vs-proposed preview, from proposal.get.
    const preview = await screen.findByTestId("proposal-preview-7");
    expect(preview.textContent).toContain("allow tool read for agent golang-engineer");
    expect(preview.textContent).toContain("deny command git push — rule cmd.1");
    expect(preview.textContent).toContain("aabbccdd11223344"); // the shown digest
  });

  it("reject REQUIRES a reason: empty sends nothing; a reason travels verbatim", async () => {
    const row = proposalRow();
    const daemon = renderDesk({
      "proposal.list": { proposals: [row] },
      "proposal.get": () => previewFor(row),
      "proposal.reject": () => ({ proposal: { ...row, status: "rejected", decideReason: "rejected: too broad for the lane" } }),
    });
    await screen.findByTestId("proposal-card-7");
    const rejectButton = screen.getByRole("button", { name: "Reject" }) as HTMLButtonElement;
    expect(rejectButton.disabled).toBe(true); // no reason typed
    expect(daemon.calls.filter((c) => c.method === "proposal.reject")).toHaveLength(0);
    fireEvent.change(screen.getByLabelText("Reason (required)"), { target: { value: "scope creep" } });
    expect((screen.getByRole("button", { name: "Reject" }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Reject" }));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "proposal.reject", params: { id: 7, reason: "scope creep" } }));
    // The daemon's decideReason replaces the row's decision state.
    const outcome = await screen.findByTestId("proposal-outcome-7");
    expect(outcome.textContent).toContain("rejected: too broad for the lane");
  });

  it("commit runs behind the before/after confirm and carries the SHOWN digest; the result says effectApplied honestly", async () => {
    const row = proposalRow();
    const daemon = renderDesk({
      "proposal.list": { proposals: [row] },
      "proposal.get": () => previewFor(row),
      "proposal.commit": () => ({
        proposal: { ...row, status: "committed" },
        effectApplied: false,
        before: { answers: [{ policyID: "cmd.1", state: "deny", render: "deny command git push — rule cmd.1" }], target: "agent:golang-engineer" },
        after: { answers: [{ policyID: "tool.9", state: "allow", render: "allow tool read — rule tool.9" }], target: "agent:golang-engineer" },
        warnings: ["this widens the agent's reach"],
      }),
    });
    await screen.findByTestId("proposal-digest-7");
    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    const dialog = await screen.findByRole("alertdialog");
    // The confirmation shows the preview sentence and the CURRENT in-force
    // answer the daemon attached — the person sees what changes before it does.
    expect(dialog.textContent).toContain("allow tool read for agent golang-engineer");
    expect(dialog.textContent).toContain("deny command git push — rule cmd.1");
    expect(dialog.textContent).toContain("aabbccdd11223344");
    // Decline sends nothing.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(daemon.calls.filter((c) => c.method === "proposal.commit")).toHaveLength(0);
    // Confirm: type the proposal id.
    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    const confirmInput = await screen.findByPlaceholderText("7");
    fireEvent.change(confirmInput, { target: { value: "7" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    // The digest COMMITTED is the digest SHOWN — not a re-read.
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "proposal.commit", params: { id: 7, digest: "aabbccdd11223344" } }));
    const outcome = await screen.findByTestId("proposal-outcome-7");
    expect(outcome.textContent).toContain("effect not applied"); // effectApplied:false, said honestly
    expect(outcome.textContent).toContain("allow tool read — rule tool.9"); // after, verbatim
    expect(outcome.textContent).toContain("this widens the agent's reach"); // warning, verbatim
  });

  it("agent_import commits like every kind (S45 T10): consent shown before, the import's own print after — as DATA", async () => {
    // The T9 pin — the DISABLED commit button with the "not available yet
    // (S45 T10)" hold and the CLI route — is superseded by T10 itself: the
    // socket door now commits this kind through the same desk as every
    // other (PM4). What the card must do instead: show the CONSENT bytes in
    // the confirmation (proposal.get answers the kind's preview with S37's
    // own renderer, verbatim), and render the RESULT honestly — before is
    // the consent, after is the import body's own post-import print, and
    // the same print rides back on warnings as data (AA4: the daemon
    // captured it and printed nothing itself).
    const consent = 'agent "groot" requests: tools read\n  prompts: 1\n  carries: 0 denials';
    const imported = "imported groot as DISABLED (AP2: import runs the check and stops)";
    const row = proposalRow({ id: 9, kind: "agent_import", command: "aeon proposal commit 9", payload: { pkgPath: "/tmp/pkg" } });
    const daemon = renderDesk({
      "proposal.list": { proposals: [row] },
      "proposal.get": () => ({
        proposal: row,
        digest: "ff00",
        preview: { kind: "agent_import", target: "agent:groot", describe: consent, inForce: [] },
      }),
      "proposal.commit": () => ({
        proposal: { ...row, status: "committed" },
        effectApplied: true,
        before: { target: "agent:groot", answers: [{ policyID: "groot", state: "", render: consent }] },
        after: { target: "agent:groot", answers: [{ policyID: "groot", state: "", render: imported }] },
        warnings: [imported],
      }),
    });
    // The shown digest arrives with proposal.get; the button arms with it.
    await screen.findByTestId("proposal-digest-9");
    const commitButton = screen.getByRole("button", { name: "Commit" }) as HTMLButtonElement;
    expect(commitButton.disabled).toBe(false); // no hold, no lie — it commits
    // The hold sentence is gone with the door that needed it.
    expect(screen.queryByTestId("proposal-agent-import-hold-9")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    const dialog = await screen.findByRole("alertdialog");
    // The CONSENT — read the consent above BEFORE the commit is typed.
    expect(dialog.textContent).toContain("agent \"groot\" requests: tools read");
    expect(dialog.textContent).toContain("ff00"); // the shown digest
    // Decline sends nothing — the consent was shown, the door stays shut.
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(daemon.calls.filter((c) => c.method === "proposal.commit")).toHaveLength(0);
    // Confirm: type the proposal id.
    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    const confirmInput = await screen.findByPlaceholderText("9");
    fireEvent.change(confirmInput, { target: { value: "9" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "proposal.commit", params: { id: 9, digest: "ff00" } }));
    const outcome = await screen.findByTestId("proposal-outcome-9");
    // before → after, both the daemon's words: consent, arrow, the AP2 line;
    // the warning carries the same print — every surface prints the same bytes.
    expect(outcome.textContent).toContain("agent \"groot\" requests: tools read");
    expect(outcome.textContent).toContain("→");
    expect(outcome.textContent).toContain("imported groot as DISABLED (AP2: import runs the check and stops)");
    expect(outcome.textContent).toContain("effect applied");
  });

  it("discard stays as S35 left it — the plain socket discard, no ceremony", async () => {
    const row = proposalRow();
    const daemon = renderDesk({
      "proposal.list": { proposals: [row] },
      "proposal.get": () => previewFor(row),
      "proposal.discard": () => ({ discarded: true }),
    });
    await screen.findByTestId("proposal-card-7");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(daemon.calls).toContainEqual({ method: "proposal.discard", params: { id: 7 } }));
  });

  it("an empty pile is the normal state, stated as such", async () => {
    renderDesk({ "proposal.list": { proposals: [] } });
    expect(await screen.findByText(/No proposed proposals — the normal state when nobody has staged one/i)).toBeTruthy();
  });
});
