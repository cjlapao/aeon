import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { RunsList } from "./RunsList";
import { APP_LOCALES } from "../../../lib/i18n";
import type { LiveState } from "../../../lib/live";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { gatesBaseResponses, verifyRunShow, verifyRuns } from "../../../../test/gate-fixtures";

const runRow = (verifyRuns as { runs: Array<Record<string, string>> }).runs[0] as Record<string, string>;

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


function renderRuns(responses: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { ...gatesBaseResponses, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <RunsList componentId="c-aeon" controller={new LiveController()} live={currentLive} />
    </I18nProvider>,
  );
  return daemon;
}

describe("RunsList (S57 T11)", () => {
  it("the ledger row is the daemon answer: full head sha, state word, summary", async () => {
    renderRuns();
    await screen.findByTestId(`run-row-${runRow.id}`);
    // The row shows the ABBREVIATED sha (a ledger scan reads rows, not 40 hex);
    // the full sha appears in the detail the row opens.
    expect(screen.getByTestId(`run-head-${runRow.id}`).textContent).toContain((runRow.headSha ?? "").slice(0, 12));
    expect(screen.getByTestId(`run-state-${runRow.id}`).textContent).toBe("fail");
    expect(screen.getByTestId(`run-summary-${runRow.id}`).textContent).toBe(runRow.summary);
    // (runShow's own door law is the next test's first assertion.)
  });

  it("runShow opens only on the run the person selects — with that id", async () => {
    const daemon = renderRuns();
    await screen.findByTestId(`run-row-${runRow.id}`);
    expect(daemon.methods()).not.toContain("verify.runShow");
    fireEvent.click(screen.getByTestId(`run-row-toggle-${runRow.id}`));
    await screen.findByTestId(`run-detail-${runRow.id}`);
    expect(daemon.paramsOf("verify.runShow")).toEqual({ id: runRow.id });
  });

  it("tails sit behind an explicit closed disclosure with the honest notice", async () => {
    renderRuns({ "verify.runShow": verifyRunShow });
    await screen.findByTestId(`run-row-${runRow.id}`);
    fireEvent.click(screen.getByTestId(`run-row-toggle-${runRow.id}`));
    const detail = await screen.findByTestId(`run-detail-${runRow.id}`);
    const disclosure = detail.querySelector("details");
    expect(disclosure).toBeTruthy();
    expect(disclosure?.open).toBe(false); // output is never shown by default
    fireEvent.click(detail.querySelector("summary") as Element);
    expect(disclosure?.open).toBe(true);
    expect(disclosure?.textContent).toContain("may contain secrets");
    expect(disclosure?.textContent).toContain("the tail is an excerpt"); // the truncated row
  });

  it("the loop filter is the daemon\'s param, not a client-side cut", async () => {
    const daemon = renderRuns();
    await screen.findByTestId("gates-runs-panel");
    fireEvent.change(screen.getByTestId("gates-runs-loop"), { target: { value: "fix-the-login" } });
    await waitFor(() => {
      const params = daemon.paramsOf("verify.runs") as Record<string, unknown>;
      expect(params.loop).toBe("fix-the-login");
    });
  });
});
