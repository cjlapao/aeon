import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DemosPage } from "../src/features/test-bench/pages/DemosPage";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

/**
 * S18 T10 — the provider-breadth demo run, and the one rule it must never
 * break: **no row may contact a live provider.** Every call leaving the page
 * is `rpc()` over the socket (the bench-demos discipline, asserted here), and
 * the rows that need cloud credentials say `not runnable here` naming the Go
 * test that carries the claim — never a silent skip, never a fake pass.
 */

const REFUSAL_SENTENCE =
  "refused: step requires capability ‘tools’; route ‘local’ does not have it (no model call was made)";

function localDeclaredRoute() {
  return {
    id: "local",
    provider: "openai",
    model: "qwen3.5:latest",
    baseUrl: "http://127.0.0.1:11434/v1",
    hasKey: false,
    source: "stored",
    maxOutputTokens: 2048,
    priced: false,
    noPrice: true,
    wireCapabilities: ["streaming-usage", "system-role"],
    capabilityStates: [
      { capability: "streaming-usage", state: "verified", probedAt: "2026-10-26T20:31:02Z", probedModel: "qwen3.5:latest" },
      { capability: "system-role", state: "declared" },
    ],
  };
}

function s18Daemon(overrides: Record<string, unknown | ((p: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({
    responses: {
      "routes.list": { routes: [localDeclaredRoute()], providers: ["anthropic", "openai"], persisted: true },
      "routes.probe": {
        route: "local",
        saved: true,
        probes: [
          { capability: "streaming-usage", state: "verified", probedAt: "2026-10-26T20:31:02Z", probedModel: "qwen3.5:latest" },
          { capability: "system-role", state: "declared" },
        ],
      },
      // The pre-call refusal (M29): the daemon answers the step WITHOUT ever
      // reaching a wire — the Go spy-at-zero tests own that guarantee.
      "step": { stepId: 9, kind: "complete", outcome: "failed", reason: REFUSAL_SENTENCE },
      "loop.list": {
        loops: [{
          id: "42",
          slug: "demo-s18",
          costAttribution: {
            groups: [
              { routeId: "local", provider: "openai", model: "qwen3.5:latest", steps: 2, cost: { state: "unknown", reason: "no-price-for-route" } },
              { routeId: "cloud", provider: "anthropic", model: "claude-sonnet-5", steps: 1, cost: { state: "known", usd: 0.012345 } },
            ],
            total: { state: "unknown", reason: "no-price-for-route" },
          },
        }],
      },
      ...overrides,
    },
  });
}

function runPanel() {
  fireEvent.change(screen.getByTestId("demo-workspace"), { target: { value: "/home/tester/aeon-qa-workspace" } });
  fireEvent.click(screen.getByRole("button", { name: /Run S18 providers demo/ }));
}

/** The five rows, in panel order: caps, probe, refusal, cost, round trip. */
async function rows(): Promise<[HTMLElement, HTMLElement, HTMLElement, HTMLElement, HTMLElement]> {
  const found = await screen.findAllByTestId("s18-step");
  await waitFor(() => expect(found[4]?.textContent).toContain("not runnable here"));
  const [caps, probe, refusal, cost, roundTrip] = found;
  if (!caps || !probe || !refusal || !cost || !roundTrip) {
    throw new Error(`the S18 panel renders five rows; found ${found.length}`);
  }
  return [caps, probe, refusal, cost, roundTrip];
}

afterEach(() => {
  cleanup();
  resetFakeDaemon();
  vi.unstubAllGlobals();
});

describe("the S18 demo run (bench)", () => {
  it("TestS18DemoRowsVerdicts — each row reaches its allowed verdict, the round trip cites its Go test", async () => {
    const daemon = s18Daemon();
    render(<DemosPage />);
    runPanel();
    const [caps, probe, refusal, cost, roundTrip] = await rows();

    expect(caps.textContent).toContain("streaming-usage=verified");
    expect(caps.textContent).toContain("system-role=declared");

    expect(probe.textContent).toContain("probed 2026-10-26T20:31:02Z against qwen3.5:latest");
    expect(probe.textContent).toContain("saved: yes");

    // M29's refusal, verbatim — the row's whole point.
    expect(refusal.textContent).toContain("refused");
    expect(refusal.textContent).toContain(REFUSAL_SENTENCE);
    expect(refusal.textContent).toContain("no model call was made");
    expect(daemon.paramsOf("step")).toMatchObject({ route: "local", workspaceRoot: "/home/tester/aeon-qa-workspace" });

    // The unmetered line reads unknown WITH its reason — and the literal D9
    // forbids appears nowhere in the panel.
    expect(cost.textContent).toContain("not reported");
    expect(cost.textContent).toContain("the route has no prices configured");
    expect(cost.textContent).not.toContain("$0.00");

    // The round trip is honest about being unrunnable, and NAMES the test.
    expect(roundTrip.textContent).toContain("not runnable here");
    expect(roundTrip.textContent).toContain("TestSuspendOnAnthropicResumeOnOpenAIResumeOnAnthropic");
  });

  it("TestS18DemoRowsVerdicts — a refused probe renders the daemon’s sentence; an absent local route answers with its remedy", async () => {
    // (a) probe could not check: refused-as-data, verbatim, nothing contradicted.
    const sentence = "could not probe route “local” · the server did not answer the probe window — nothing was contradicted, every capability keeps its declaration and its ‘declared’ state";
    const daemon = s18Daemon({ "routes.probe": { route: "local", saved: false, failure: sentence } });
    render(<DemosPage />);
    runPanel();
    const [, probe] = await rows();
    expect(probe.textContent).toContain(sentence);
    expect(probe.textContent).toContain("refused");
    daemon.calls.length = 0;
    cleanup();
    resetFakeDaemon();

    // (b) no local route at all: the rows say unavailable WITH the remedy,
    // and the step row starts nothing — zero `step` calls leave the page.
    const noLocal = s18Daemon({
      "routes.list": {
        routes: [{ id: "anthropic", provider: "anthropic", model: "claude-sonnet-5", baseUrl: "https://api.anthropic.com", hasKey: true, source: "environment", maxOutputTokens: 4096, priced: true }],
        providers: ["anthropic"],
        persisted: true,
      },
    });
    render(<DemosPage />);
    runPanel();
    const [caps, probe2, refusal, cost] = await rows();
    expect(caps.textContent).toContain("not runnable here");
    expect(caps.textContent).toContain("add one on the Providers tab");
    expect(probe2.textContent).toContain("not runnable here");
    expect(refusal.textContent).toContain("not runnable here");
    expect(cost.textContent).toContain("not runnable here");
    expect(noLocal.methods()).not.toContain("step");
    expect(noLocal.methods()).not.toContain("routes.probe");
    // The remedy is printed where the operator will read it.
    expect(noLocal.methods()).toContain("routes.list");
  });

  it("TestS18DemoTouchesNoLiveProvider — every call leaving the page is rpc() over the socket", async () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    const daemon = s18Daemon();
    render(<DemosPage />);
    runPanel();
    await rows();

    // No fetch, no second transport: the socket bridge is the only door.
    expect(fetchSpy).not.toHaveBeenCalled();
    // Every protocol method the panel may use, and nothing outside it.
    const ALLOWED = ["routes.list", "routes.probe", "step", "loop.list", "loop.show", "handlers.list", "handlers.verify"];
    for (const method of daemon.methods()) {
      expect(ALLOWED).toContain(method);
    }
    // The MCP mirror of the absent_test.go boundary: no tools.* ever leaves.
    expect(daemon.methods().some((m) => m.startsWith("tools."))).toBe(false);
  });
});
