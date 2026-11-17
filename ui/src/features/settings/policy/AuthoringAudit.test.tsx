import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { AuthoringAudit } from "./AuthoringAudit";
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

function renderAudit(responses: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <AuthoringAudit controller={new LiveController()} live={currentLive} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("AuthoringAudit", () => {
  it("every row is the daemon's record — op, actor pair, summary verbatim", async () => {
    renderAudit({
      "policy.changes": {
        changes: [
          { id: 5, op: "seed", actorKind: "user", actorId: "operator", policyId: "cmd.9", occurredAt: "2026-01-01T00:00:00Z", summary: "seeded corpus rule cmd.9 (system rule — the operator's own removal; seed.apply will not resurrect it, PM16)" },
          { id: 4, op: "add", actorKind: "user", actorId: "operator", policyId: "cmd.8", occurredAt: "2026-01-01T00:00:00Z", summary: "deny command rm -rf (rule cmd.8)" },
        ],
      },
    });
    const rows = await screen.findAllByTestId("audit-row");
    expect(rows).toHaveLength(2);
    // The daemon's summary, byte for byte — including its PM16 clause.
    expect(rows[0]?.textContent).toContain("seeded corpus rule cmd.9 (system rule — the operator's own removal; seed.apply will not resurrect it, PM16)");
    expect(rows[1]?.textContent).toContain("deny command rm -rf (rule cmd.8)");
    expect(rows[0]?.textContent).toContain("user:operator");
  });

  it("the unaudited reconciliation is its OWN warning and names the ids — never hidden", async () => {
    renderAudit({
      "policy.changes": {
        changes: [{ id: 1, op: "add", actorKind: "user", actorId: "operator", policyId: "cmd.1", occurredAt: "2026-01-01T00:00:00Z", summary: "allow tool read (rule cmd.1)" }],
        unauditedPolicyIds: ["cmd.99", "cmd.98"],
      },
    });
    const warning = await screen.findByTestId("unaudited-warning");
    expect(warning.textContent).toContain("no audit row");
    expect(warning.textContent).toContain("cmd.99");
    expect(warning.textContent).toContain("cmd.98");
    // …and it does NOT swallow the rows that do exist.
    expect(screen.getAllByTestId("audit-row")).toHaveLength(1);
  });

  it("the empty table's OWN sentence is the daemon's, rendered verbatim", async () => {
    renderAudit({ "policy.changes": { changes: [], statement: "the audit holds no rows." } });
    expect(await screen.findByText("the audit holds no rows.")).toBeTruthy();
  });

  it("an empty audit with NO statement says so — never dressed as the daemon's answer", async () => {
    renderAudit({ "policy.changes": { changes: [] } });
    expect(await screen.findByText(/sent no rules and no empty sentence/i)).toBeTruthy();
  });
});
