import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { ProposeImportForm, type ProposalCreateResult } from "../src/features/test-bench/pages/ProposeImportForm";
import { installFakeDaemon } from "./helpers";
import { resetBridge, rpc } from "../src/lib/bridge";

// S37 T6 — the "Propose import" form on the Agents page. The form is a
// proposal, never a commit: it calls proposal.create for the agent_import
// kind, shows S35's exact toast (read from the response's command field), and
// renders the returned preview labelled advisory. No commit control, no path
// read. The assertions are about HONESTY and the thin-client boundary, not
// layout.
//
// The matchers toHaveTextContent / toHaveValue are not wired into this
// project's vitest config (the page tests read .textContent / .value), so the
// assertions read them directly.

// The form's `create` is its one path to the daemon: it is the real
// `rpc("proposal.create", …)`, so the fake daemon records the exact socket
// call (and "does not call the socket" is a real count, not a prop spy).
const create = (kind: string, payload: unknown) => rpc<ProposalCreateResult>("proposal.create", { kind, payload });

// S35's exact toast pattern, read from the response's command field — the
// string the form must produce, never one it invents from the id.
const EXPECTED_TOAST = "Proposed · commit with aeon proposal commit 12";

// A happy `proposal.create` answer, with a preview carrying sentinel strings
// the trace test (below) asserts appear in the DOM.
function happyAnswer(): ProposalCreateResult {
  return {
    id: 12,
    kind: "agent_import",
    status: "proposed",
    command: "aeon proposal commit 12",
    createdAt: "2026-10-30T00:00:00Z",
    createdById: "1000",
    createdByKind: "user",
    payload: { pkgPath: "/abs/groot" },
    preview: {
      agent: "groot (sentinel-agent)",
      consent: "CONSENT_SENTINEL — the bytes the daemon rendered, verbatim",
      carriedDeny: [
        { id: "DENY_SENTINEL-1", target: { kind: "agent", id: "groot" } },
        { id: "DENY_SENTINEL-2", target: { kind: "lane-type", id: "review" } },
      ],
    },
  };
}

function submit(path: string) {
  fireEvent.change(screen.getByTestId("propose-import-path"), { target: { value: path } });
  fireEvent.click(screen.getByTestId("propose-import-submit"));
}

function inputValue(): string {
  return (screen.getByTestId("propose-import-path") as HTMLInputElement).value;
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("ProposeImportForm", () => {
  it("calls proposal.create with kind agent_import and the typed path", async () => {
    const daemon = installFakeDaemon({ responses: { "proposal.create": happyAnswer() } });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    submit("/abs/groot");
    await screen.findByTestId("propose-import-preview");
    expect(daemon.calls).toContainEqual({ method: "proposal.create", params: { kind: "agent_import", payload: { pkgPath: "/abs/groot" } } });
  });

  it("shows the exact toast built from the response command field", async () => {
    installFakeDaemon({ responses: { "proposal.create": happyAnswer() } });
    const onToast = vi.fn();
    render(<ProposeImportForm create={create} onToast={onToast} />);
    submit("/abs/groot");
    await screen.findByTestId("propose-import-preview");
    expect(onToast).toHaveBeenCalledWith(EXPECTED_TOAST);
    expect(onToast).toHaveBeenCalledTimes(1);
  });

  it("renders the consent text verbatim inside a pre and labels it a preview", async () => {
    installFakeDaemon({ responses: { "proposal.create": happyAnswer() } });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    submit("/abs/groot");
    const pre = await screen.findByTestId("propose-import-preview-consent");
    // Verbatim, never summarised (S15 T4 / R2).
    expect(pre.textContent).toContain("CONSENT_SENTINEL — the bytes the daemon rendered, verbatim");
    // Labelled a preview (AA4): the advisory line, and the agent name.
    expect(screen.getByTestId("propose-import-preview-label")).toBeTruthy();
    expect(screen.getByTestId("propose-import-preview-agent").textContent).toContain("groot (sentinel-agent)");
  });

  it("renders a daemon refusal verbatim and keeps the typed path", async () => {
    // A daemon refusal: a negative code → an rpc-kind error, not transport.
    installFakeDaemon({
      responses: {
        "proposal.create": () => {
          throw JSON.stringify({ code: -32000, message: "refused: stored pkgPath is not absolute — the proposal predates the absolute-path rule" });
        },
      },
    });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    submit("/abs/groot");
    const refusal = await screen.findByTestId("propose-import-refusal");
    expect(refusal.textContent).toContain("refused: stored pkgPath is not absolute");
    // No silent reset: the typed value is kept.
    expect(inputValue()).toBe("/abs/groot");
  });

  it("does not call the socket for an empty or relative path", async () => {
    const daemon = installFakeDaemon({ responses: { "proposal.create": happyAnswer() } });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    // Relative: the cosmetic check keeps it off the wire (the daemon would refuse it anyway).
    submit("relative/pkg");
    expect(screen.getByTestId("propose-import-cosmetic").textContent).toContain("must be an absolute path");
    // Empty: same.
    submit("");
    expect(daemon.calls).toHaveLength(0);
    expect(screen.queryByTestId("propose-import-preview")).toBeNull();
  });

  it("offers no commit control: no button, link or shortcut whose text matches /commit|confirm|import now/i except the copyable command text", async () => {
    installFakeDaemon({ responses: { "proposal.create": happyAnswer() } });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    submit("/abs/groot");
    await screen.findByTestId("propose-import-preview");
    // Exactly one button, and it is the propose button — no commit/confirm button.
    const buttons = screen.getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect(buttons[0]!.textContent).toContain("Propose import");
    // No commit/confirm control anywhere in the form.
    expect(screen.queryByRole("button", { name: /commit|confirm|import now/i })).toBeNull();
    // The command text is plain text (the toast), not a control.
    expect(screen.queryByRole("link", { name: /commit/i })).toBeNull();
  });

  it("renders an unwired daemon as the unwired state, not an empty form result", async () => {
    // A transport-kind error: the daemon could not be reached.
    installFakeDaemon({
      responses: {
        "proposal.create": () => {
          throw new Error('{"code":0,"message":"not connected to the daemon","kind":"transport"}');
        },
      },
    });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    submit("/abs/groot");
    // The unwired state is shown (TD9 three states), not an empty result.
    expect(await screen.findByTestId("propose-import-unwired")).toBeTruthy();
    expect(screen.queryByTestId("propose-import-preview")).toBeNull();
    // The typed value survives.
    expect(inputValue()).toBe("/abs/groot");
  });

  it("every rendered string traces to the response or the typed input", async () => {
    installFakeDaemon({ responses: { "proposal.create": happyAnswer() } });
    render(<ProposeImportForm create={create} onToast={vi.fn()} />);
    submit("/abs/groot");
    await screen.findByTestId("propose-import-preview");
    // Each sentinel the fake returned appears in the DOM.
    expect(screen.getByTestId("propose-import-preview-agent").textContent).toContain("groot (sentinel-agent)");
    expect(screen.getByTestId("propose-import-preview-consent").textContent).toContain("CONSENT_SENTINEL");
    const carried = screen.getByTestId("propose-import-preview-carried");
    expect(within(carried).getByTestId("propose-import-carried-DENY_SENTINEL-1")).toBeTruthy();
    expect(within(carried).getByTestId("propose-import-carried-DENY_SENTINEL-2")).toBeTruthy();
    // The typed value is echoed back in the input.
    expect(inputValue()).toBe("/abs/groot");
    // Thin-client: the component source reads no filesystem — it is the daemon's job.
    const src = readFileSync(join(__dirname, "../src/features/test-bench/pages/ProposeImportForm.tsx"), "utf8");
    expect(src).not.toMatch(/node:fs|@tauri-apps\/api\/fs|\bfs\b[\s.(]|import\s+.*fs/);
  });
});
