import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { TelemetryPage } from "../src/features/test-bench/pages/TelemetryPage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import { getBenchPage } from "../src/features/test-bench/lib/benchRegistry";
import "../src/features/test-bench/pages/registerAll";

// S33 T9's bench page, held to what the slice is for:
//
//   * the three cost states are three renderings (TL11/P31) — known,
//     partial and unknown each get their own sentence, and a partial sum
//     is never printable as an exact figure;
//   * null tokens render as —, never 0 — the tool_result row is the
//     fixture: its absence must survive into the DOM;
//   * the workspace singleton's exists:false is a STATE ("nothing counted
//     yet"), not an error and not a zero-filled table;
//   * D2's two numbers render as two distinct labelled facts;
//   * the CRUD doors are socket calls (P34): the move rides
//     `component.attach` to a root (and `component.detach` back), the
//     archive rides `loop.archive`, and NO method starting with
//     "telemetry." ever appears as a WRITE — the page's list of methods
//     is reads plus the ordinary doors, always;
//   * refusals render as DATA, in the page's own voice line, not as
//     thrown errors.
//
// S44 dropped the project tier from the bench: a "project" is now a root
// component, and the move attaches the component to a root (parentId), not
// a group (projectId).

// A component plus two roots (parentId empty) the move control can target.
const COMPONENTS = {
  components: [
    { id: "cmp-1", name: "Shop", slug: "shop", root: "/home/dev/shop", parentId: "root-commerce" },
    { id: "root-commerce", name: "Commerce", slug: "commerce", root: "/home/dev", parentId: "" },
    { id: "root-platform", name: "Platform", slug: "platform", root: "/home/dev/platform", parentId: "" },
  ],
};

const KNOWN_TIER = {
  componentId: "cmp-1",
  loopsRun: 3, wallMsTotal: 9100,
  inputTokensTotal: 1200, outputTokensTotal: 340,
  cacheReadTokensTotal: 800, cacheWriteTokensTotal: 12, reasoningTokensTotal: 60,
  costUsdTotal: 0.034, costState: "known",
  filesTouchedTotal: 5, filesReadTotal: 4, filesWrittenTotal: 2,
  linesAddedTotal: 90, linesRemovedTotal: 11,
  commandsRunTotal: 7, commandsFailedTotal: 1, commandWallMsTotal: 4200,
  updatedAt: "2026-10-30T09:00:00Z", exists: true,
};

const PARTIAL_TIER = { ...KNOWN_TIER, costState: "partial", costUsdTotal: 0.02 };

const LOOP_DETAIL = {
  step: { id: 41, loopSlug: "walk", componentRoot: "/home/dev/shop", status: "running" },
  turns: [
    { iteration: 1, category: "assistant_output", wallMs: 2400, route: "sonnet", inputTokens: 400, outputTokens: 60, costUsd: 0.004, costState: "known" },
    { iteration: 1, category: "tool_result", wallMs: 12, route: "sonnet", costState: "unknown" },
  ],
  compactions: [
    { startIdx: 2, endIdx: 14, estimateBefore: 1800, estimateAfter: 210, inputTokens: 1500, outputTokens: 90, costUsd: 0.006, costState: "known" },
  ],
  fileActivities: [{ path: "src/checkout.ts", action: "write", linesAdded: 18, linesRemoved: 3 }],
  commandActivities: [{ command: "make test", exitCode: 1, wallMs: 4200 }],
  currentContextTokens: 240,
  cumulativeTokensTotal: 460,
  composition: { systemChars: 340, sections: [{ title: "Task", category: "system_prompt", chars: 120 }] },
};

function mount(responses: Record<string, unknown> = {}): FakeDaemon {
  const fake = installFakeDaemon({
    responses: {
      "component.list": COMPONENTS,
      ...responses,
    },
  });
  return fake;
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("TelemetryPage", () => {
  it("is registered in the bench registry (no shared arrays touched)", () => {
    const page = getBenchPage("telemetry");
    expect(page).toBeDefined();
    expect(page?.label).toBe("Telemetry");
    expect(page?.needsConnection).toBe(true);
  });

  it("renders the cost state as a word beside every sum — three states, three renderings", async () => {
    const fake = mount({ "telemetry.component": PARTIAL_TIER });
    render(<TelemetryPage />);
    // the component options load from component.list; a controlled select rejects a value whose option has not rendered yet
    await screen.findByText("Shop · /home/dev/shop");
    fireEvent.change(screen.getByTestId("telemetry-component-pick"), { target: { value: "cmp-1" } });
    fireEvent.click(screen.getByTestId("telemetry-read-component"));
    await waitFor(() => expect(fake.methods()).toContain("telemetry.component"));
    const cost = await screen.findByTestId("telemetry-cost");
    expect(cost.textContent).toContain("partial");
    expect(cost.textContent).toContain("at least");
    expect(cost.textContent).not.toMatch(/\$0\.020000 \(known\)/);
    const pill = screen.getByTestId("telemetry-tier").querySelector("[class]") as HTMLElement;
    expect(pill?.textContent ?? "").toContain("partial");
  });

  it("renders null tokens as —, never 0, and keeps the loop tier's two D2 numbers distinct", async () => {
    const fake = mount({ "telemetry.loop": LOOP_DETAIL });
    render(<TelemetryPage />);
    fireEvent.change(screen.getByTestId("telemetry-step-id"), { target: { value: "41" } });
    fireEvent.click(screen.getByTestId("telemetry-read-loop"));
    await waitFor(() => expect(fake.methods()).toContain("telemetry.loop"));
    const rows = await screen.findAllByTestId("telemetry-turn");
    expect(rows).toHaveLength(2);
    const toolRow = rows[1]?.textContent ?? "";
    expect(toolRow).toContain("—");
    // the tool_result row must NOT claim a 0 token count anywhere
    expect(toolRow).not.toMatch(/\t0\t/);
    expect(screen.getByTestId("telemetry-current").textContent).toContain("240");
    expect(screen.getByTestId("telemetry-cumulative").textContent).toContain("460");
    expect(screen.getByTestId("telemetry-current").textContent).not.toBe(
      screen.getByTestId("telemetry-cumulative").textContent,
    );
  });

  it("shows the workspace exists:false as a state, not an error and not zeros", async () => {
    const fake = mount({
      "telemetry.workspace": { ...KNOWN_TIER, exists: false, loopsRun: 0, inputTokensTotal: 0, costState: "unknown" },
    });
    render(<TelemetryPage />);
    fireEvent.click(screen.getByTestId("telemetry-read-workspace"));
    await waitFor(() => expect(fake.methods()).toContain("telemetry.workspace"));
    const absent = await screen.findByTestId("telemetry-workspace-absent");
    expect(absent.textContent).toContain("nothing counted yet");
    expect(screen.queryByTestId("telemetry-tier")).toBeNull();
  });

  it("the move rides component.attach to a root, and the archive re-reads the tier (D8 at the bench)", async () => {
    const fake = mount({
      "component.attach": { id: "cmp-1", name: "Shop", slug: "shop", parentId: "root-platform" },
      "telemetry.component": KNOWN_TIER,
      "loop.archive": { id: "arc-1", slug: "walk", stateBytes: 404, status: "done" },
    });
    render(<TelemetryPage />);
    // the component options load from component.list; a controlled select rejects a value whose option has not rendered yet
    await screen.findByText("Shop · /home/dev/shop");
    fireEvent.change(screen.getByTestId("telemetry-component-pick"), { target: { value: "cmp-1" } });
    fireEvent.click(screen.getByTestId("telemetry-read-component"));
    await waitFor(() => expect(fake.methods()).toContain("telemetry.component"));
    fireEvent.change(screen.getByTestId("telemetry-move-target"), { target: { value: "root-platform" } });
    fireEvent.click(screen.getByTestId("telemetry-move"));
    await waitFor(() => expect(fake.methods()).toContain("component.attach"));
    expect(fake.paramsOf("component.attach")).toEqual({ id: "cmp-1", parentId: "root-platform" });

    fireEvent.change(screen.getByTestId("telemetry-archive-id"), { target: { value: "loop-9" } });
    fireEvent.click(screen.getByTestId("telemetry-archive"));
    await waitFor(() => expect(fake.methods()).toContain("loop.archive"));
    expect(fake.paramsOf("loop.archive")).toMatchObject({ id: "loop-9" });
    // the D8 demonstration: the tier was re-read AFTER the archive.
    expect(fake.methods().filter((m) => m === "telemetry.component").length).toBeGreaterThanOrEqual(2);
  });

  it("renders refusals as data and never lets a telemetry write reach the wire", async () => {
    const fake = mount({
      "loop.archive": { refused: { kind: "no-such-loop", detail: "no loop loop-9 is in this store" } },
    });
    render(<TelemetryPage />);
    fireEvent.change(screen.getByTestId("telemetry-archive-id"), { target: { value: "loop-9" } });
    fireEvent.click(screen.getByTestId("telemetry-archive"));
    await waitFor(() => expect(fake.methods()).toContain("loop.archive"));
    const note = await screen.findByTestId("telemetry-action-note");
    expect(note.textContent).toContain("refused");
    expect(note.textContent).toContain("no-such-loop");
    // The page's telemetry surface is reads only; nothing writes through it.
    const writes = fake.methods().filter((m) => m.startsWith("telemetry."));
    expect(writes.every((m) => ["telemetry.loop", "telemetry.component", "telemetry.workspace"].includes(m))).toBe(true);
  });
});
