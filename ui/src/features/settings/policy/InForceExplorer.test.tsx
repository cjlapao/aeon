import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { InForceExplorer } from "./InForceExplorer";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import type { Vocabulary } from "./policyModel";

const vocabulary: Vocabulary = {
  types: ["command_access", "cost_cap"],
  targetKinds: ["agent", "loop"],
  sources: ["operator"],
  actions: ["allow"],
  authorship: [],
  specs: [
    {
      type: "command_access",
      fields: [
        { name: "pattern", kind: "string", required: true, description: "the command pattern" },
        { name: "limit", kind: "number", required: false, description: "a bound" },
      ],
    },
    {
      type: "cost_cap",
      fields: [{ name: "cap", kind: "unknown-kind", required: false, description: "an unmapped kind" }],
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

function renderExplorer(responses: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { "agent.list": { agents: [] }, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <InForceExplorer vocabulary={vocabulary} controller={new LiveController()} live={currentLive} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

async function chooseType(value: string): Promise<void> {
  const select = screen.getByLabelText("Type") as HTMLSelectElement;
  await waitFor(() => expect(Array.from(select.options).map((o) => o.value)).toContain(value));
  fireEvent.change(select, { target: { value } });
}

describe("InForceExplorer", () => {
  it("allowed, denied and unimplemented are three VISIBLY different answers, sentences verbatim", async () => {
    // One call at a time, chosen by the test's own question — fake flips by counter.
    let whyCall = 0;
    const daemon = renderExplorer({
      "policy.why": () => {
        whyCall += 1;
        if (whyCall === 1) return { allowed: true, sentence: "allowed: rule cmd.1 admits git status for agent eng" };
        if (whyCall === 2) return { allowed: false, defaultDeny: true, sentence: "refused: no rule speaks for git push — default deny" };
        return { allowed: false, unimplemented: true, sentence: "the engine has no cost_cap evaluator yet" };
      },
    });
    await chooseType("command_access");
    fireEvent.click(screen.getByRole("button", { name: "Why?" }));
    const allowed = await screen.findByTestId("why-allowed");
    expect(allowed.textContent).toContain("allowed: rule cmd.1 admits git status for agent eng");

    fireEvent.click(screen.getByRole("button", { name: "Why?" }));
    const denied = await screen.findByTestId("why-denied");
    expect(denied.textContent).toContain("refused: no rule speaks for git push — default deny");
    expect(denied.textContent).not.toContain("no cost_cap evaluator"); // distinct from the third state
    expect(screen.getByText("default deny")).toBeTruthy();

    // The third state is not a denial: it renders in its own panel, never
    // the red one (SEC-033 — no answer yet is not a refusal).
    fireEvent.click(screen.getByRole("button", { name: "Why?" }));
    const unimpl = await screen.findByTestId("why-unimplemented");
    expect(unimpl.textContent).toContain("the engine has no cost_cap evaluator yet");
    expect(screen.queryByTestId("why-denied")).toBeNull();
    expect(whyCall).toBe(3);
    expect(daemon.methods().filter((m) => m === "policy.why")).toHaveLength(3);
  });

  it("the in-force answer renders the daemon's Render() lines verbatim", async () => {
    const daemon = renderExplorer({
      "policy.inForce": {
        target: "agent:eng",
        answers: [{ policyID: "cmd.1", state: "deny", render: "deny command git push — rule cmd.1" }],
      },
    });
    fireEvent.click(screen.getByRole("button", { name: "What is in force?" }));
    await waitFor(() => expect(daemon.calls).toContainEqual(expect.objectContaining({ method: "policy.inForce" })));
    const block = await screen.findByTestId("inforce-answer");
    expect(block.textContent).toContain("deny command git push — rule cmd.1");
    expect(block.textContent).toContain("cmd.1");
    expect(block.textContent).toContain("deny");
  });

  it("the question's fields travel under the names the door expects (pattern → command)", async () => {
    const daemon = renderExplorer({ "policy.why": { allowed: false, sentence: "refused: nothing matches" } });
    await chooseType("command_access");
    fireEvent.change(screen.getByLabelText("pattern"), { target: { value: "git push" } });
    fireEvent.click(screen.getByRole("button", { name: "Why?" }));
    await waitFor(() => expect(daemon.methods()).toContain("policy.why"));
    expect(daemon.paramsOf("policy.why")).toMatchObject({ type: "command_access", agent: "", command: "git push" });
  });

  it("an unmapped descriptor kind is stated and offered as text, never silently dropped", async () => {
    renderExplorer();
    await chooseType("cost_cap");
    expect(screen.getByText(/names kind “unknown-kind”/i)).toBeTruthy();
    const cap = screen.getByLabelText("cap") as HTMLInputElement;
    expect(cap.type).toBe("text");
  });
});
