import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ProvidersPage } from "../src/features/test-bench/pages/ProvidersPage";
import { installFakeDaemon, resetFakeDaemon, routesList, routeView, type FakeDaemon } from "./helpers";

/**
 * S18 T10 — the capability states and the ONE probe button.
 *
 * The claims under test are M32 and P31 in DOM form: the probe answers only
 * to a click (mount, refresh and save fire ZERO probes); a second click while
 * one is in flight sends nothing; and the three capability states render from
 * `routes.list` ALONE — `verified` only ever beside the probe's timestamp and
 * the model it was earned against, `contradicted` carrying the daemon's
 * sentence unaltered, and the ABSENCE of a probe verdict rendering as
 * `declared` — never as a softened yes.
 */

const PROBE_FAIL =
  "could not probe route “local” · the server did not answer the probe window — nothing was contradicted, every capability keeps its declaration and its ‘declared’ state";

const CONTRADICTION =
  "Probe: capability “tools” contradicted · asked for a tool_use block and the answer came back as prose — the declaration stands, the verdict says otherwise";

function localRoute(overrides: Record<string, unknown> = {}) {
  return routeView({
    id: "local",
    provider: "openai",
    model: "qwen3.5:latest",
    baseUrl: "http://127.0.0.1:11434/v1",
    hasKey: false,
    keyHint: undefined,
    source: "stored",
    priced: false,
    noPrice: true,
    wireCapabilities: ["streaming-usage", "system-role"],
    ...overrides,
  });
}

function probeCount(fake: FakeDaemon): number {
  return fake.methods().filter((m) => m === "routes.probe").length;
}

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("the Providers page — capability states and the Probe button (S18 T10)", () => {
  it("TestProbeButtonFiresRoutesProbeOnlyOnClick", async () => {
    let resolvedProbe = false;
    const fake = installFakeDaemon({
      responses: {
        "routes.list": routesList([localRoute()], true),
        "routes.set": { route: localRoute() },
        "routes.probe": () => {
          resolvedProbe = true;
          return { route: "local", probes: [{ capability: "streaming-usage", state: "verified", probedAt: "2026-10-26T20:31:02Z", probedModel: "qwen3.5:latest" }], saved: true };
        },
      },
    });
    render(<ProvidersPage />);
    await screen.findByTestId("route-local");

    // Mount fired zero probes (M32).
    expect(probeCount(fake)).toBe(0);

    // A save fires zero probes — the editor saves a route; it does not ask a
    // server anything.
    fireEvent.click(screen.getByText("Edit"));
    fireEvent.click(screen.getByTestId("save-route"));
    await waitFor(() => expect(fake.methods()).toContain("routes.set"));
    expect(probeCount(fake)).toBe(0);

    // The click fires exactly one, on the right route.
    fireEvent.click(screen.getByTestId("probe-local"));
    await waitFor(() => expect(probeCount(fake)).toBe(1));
    expect(resolvedProbe).toBe(true);
    expect(fake.paramsOf("routes.probe")).toEqual({ route: "local" });
    // And the durable facts came back through routes.list — the refresh after
    // the probe means routes.list was called again; no private verdict state.
    expect(fake.methods().filter((m) => m === "routes.list").length).toBeGreaterThanOrEqual(2);
  });

  it("TestProbeButtonDisabledWhileInFlight", async () => {
    let release: ((v: unknown) => void) | undefined;
    const fake = installFakeDaemon({
      responses: {
        "routes.list": routesList([localRoute()], true),
        "routes.probe": () => new Promise((resolve) => { release = resolve; }),
      },
    });
    render(<ProvidersPage />);
    await screen.findByTestId("route-local");

    const button = screen.getByTestId("probe-local") as HTMLButtonElement;
    fireEvent.click(button);
    // The click is in: the request is open, the button is shut.
    await waitFor(() => expect(probeCount(fake)).toBe(1));
    expect((screen.getByTestId("probe-local") as HTMLButtonElement).disabled).toBe(true);

    // A second click during the open request sends NOTHING.
    fireEvent.click(screen.getByTestId("probe-local"));
    fireEvent.click(screen.getByTestId("probe-local"));
    expect(probeCount(fake)).toBe(1);

    release?.({ route: "local", probes: [], saved: true });
    await waitFor(() => expect((screen.getByTestId("probe-local") as HTMLButtonElement).disabled).toBe(false));
    expect(probeCount(fake)).toBe(1);
  });

  it("TestContradictionRendersVerbatim", async () => {
    installFakeDaemon({
      responses: {
        "routes.list": routesList([
          localRoute({
            wireCapabilities: ["tools"],
            capabilityStates: [{ capability: "tools", state: "contradicted", detail: CONTRADICTION }],
          }),
        ], true),
        "routes.probe": { route: "local", probes: [], saved: false, failure: PROBE_FAIL },
      },
    });
    render(<ProvidersPage />);
    await screen.findByTestId("route-local");

    // The contradicted pill is loud (red) and carries the daemon's sentence
    // UNALTERED — no re-casing, no paraphrase, no softening.
    const detail = await screen.findByTestId("contradicted-detail-local-tools");
    expect(detail.textContent).toBe(CONTRADICTION);
    expect(detail.textContent).not.toBe(CONTRADICTION.toLowerCase());
    expect(screen.getByTestId("cap-local-tools").textContent).toContain("tools: contradicted");

    // The same verbatim rule for a probe that could not check: the daemon's
    // own failure sentence, exactly as written, after the operator's click.
    fireEvent.click(screen.getByTestId("probe-local"));
    const sentence = await screen.findByTestId("probe-sentence-local");
    expect(sentence.textContent).toBe(PROBE_FAIL);
  });

  it("TestRoutesListCarriesCapabilityStates", async () => {
    installFakeDaemon({
      responses: {
        "routes.list": routesList([
          localRoute({
            id: "verified-route",
            wireCapabilities: ["streaming-usage"],
            capabilityStates: [{ capability: "streaming-usage", state: "verified", probedAt: "2026-10-26T20:31:02Z", probedModel: "qwen3.5:latest" }],
          }),
          localRoute({ id: "plain-route", wireCapabilities: ["streaming-usage"] }), // no capabilityStates AT ALL
          localRoute({ id: "none-route", wireCapabilities: [] }),
          localRoute({ id: "undeclared-route", wireCapabilities: null }),
        ], true),
      },
    });
    render(<ProvidersPage />);
    await screen.findByTestId("route-verified-route");

    // verified: the pill, the timestamp, AND the model it was earned against
    // — O2 rides the verdict or the verdict does not render as verified.
    expect(screen.getByTestId("cap-verified-route-streaming-usage").textContent).toContain("streaming-usage: verified");
    const probed = await screen.findByTestId("probed-verified-route-streaming-usage");
    expect(probed.textContent).toContain("2026-10-26T20:31:02Z");
    expect(probed.textContent).toContain("qwen3.5:latest");

    // Absence of a probe verdict renders `declared` — neutral, never a yes.
    // And exactly ONE verified pill exists in the whole document: the earned
    // one. A second one would be a state the cache never backed.
    expect(screen.getByTestId("cap-plain-route-streaming-usage").textContent).toContain("streaming-usage: declared");
    const verified = screen.getAllByTestId(/cap-.+-streaming-usage/).filter((el) => el.textContent?.includes("verified"));
    expect(verified).toHaveLength(1);

    // declared-NONE and UNDECLARED are the two honest non-states, worded apart.
    expect((await screen.findByTestId("caps-none-none-route")).textContent).toContain("declared none");
    expect((await screen.findByTestId("caps-undeclared-undeclared-route")).textContent).toContain("undeclared");
    expect(screen.queryByTestId("caps-undeclared-route")).toBeNull(); // no pills for a route that declared nothing
  });
});
