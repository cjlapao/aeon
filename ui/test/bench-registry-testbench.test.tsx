import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { TestBench } from "../src/features/test-bench/TestBench";
import { registerBenchPage, __resetBenchRegistryForTests } from "../src/features/test-bench/lib/benchRegistry";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import type { BenchPage } from "../src/app/routes";

// A registry-only id is never a literal member of BenchPage (that stays
// sourced from the legacy array alone — see routes.ts's own doc comment);
// router.tsx reaches it at runtime through isBenchPage's type guard. This
// test exercises TestBench directly, under the same deliberate cast any
// caller outside that guard needs.
const asBenchPage = (id: string) => id as BenchPage;

// Proves TestBench.tsx's generic fallback branch — the thing that lets a
// NEW bench page render with zero edits to TestBench.tsx itself, once it
// has registered through pages/registerAll.ts.

beforeEach(() => {
  resetBridge();
  __resetBenchRegistryForTests();
});
afterEach(() => cleanup());

describe("TestBench's registry fallback branch", () => {
  it("renders a registered page that needs a connection, once connected", async () => {
    registerBenchPage({
      id: "fixture-tb-probe",
      label: "Fixture TB Probe",
      path: "/bench/fixture-tb-probe",
      order: 9999,
      needsConnection: true,
      render: () => <div data-testid="fixture-tb-probe-content">hello from the registry</div>,
    });
    installFakeDaemon({});
    render(<TestBench activePage={asBenchPage("fixture-tb-probe")} />);
    expect(await screen.findByTestId("fixture-tb-probe-content")).toBeTruthy();
  });

  it("shows the shared not-connected alert, not the page, when disconnected and needsConnection is true", async () => {
    registerBenchPage({
      id: "fixture-tb-probe-2",
      label: "Fixture TB Probe 2",
      path: "/bench/fixture-tb-probe-2",
      order: 9998,
      needsConnection: true,
      render: () => <div data-testid="fixture-tb-probe-2-content">should not render</div>,
    });
    render(<TestBench activePage={asBenchPage("fixture-tb-probe-2")} />);
    expect(await screen.findByTestId("tab-needs-connection")).toBeTruthy();
    expect(screen.queryByTestId("fixture-tb-probe-2-content")).toBeNull();
  });

  it("renders a registered page with needsConnection: false even while disconnected, and suppresses the shared alert", async () => {
    registerBenchPage({
      id: "fixture-tb-probe-3",
      label: "Fixture TB Probe 3",
      path: "/bench/fixture-tb-probe-3",
      order: 9997,
      needsConnection: false,
      render: () => <div data-testid="fixture-tb-probe-3-content">local only</div>,
    });
    render(<TestBench activePage={asBenchPage("fixture-tb-probe-3")} />);
    expect(await screen.findByTestId("fixture-tb-probe-3-content")).toBeTruthy();
    expect(screen.queryByTestId("tab-needs-connection")).toBeNull();
  });
});
