import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { PolicySection } from "./PolicySection";
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

const vocabulary = {
  vocabulary: {
    types: ["tool_access", "command_access"],
    targetKinds: ["all-agents", "agent", "loop", "lane-type", "mcp"],
    sources: ["operator", "agent", "mcp_server", "import"],
    actions: ["allow", "deny", "ask"],
    authorship: [{ source: "operator", action: "deny", allowed: true }],
    specs: [],
  },
};

function policyRow(overrides: Record<string, unknown> = {}) {
  return {
    id: "cmd.1",
    type: "command_access",
    action: "deny",
    source: "operator",
    origin: "system",
    target: "agent:golang-engineer",
    describe: "deny command git push — rule cmd.1",
    notAfter: null,
    createdAt: "2026-01-01T00:00:00Z",
    spec: { command: "git push" },
    ...overrides,
  };
}

const baseResponses: Record<string, unknown> = {
  "policy.vocabulary": vocabulary,
  "policy.list": { policies: [policyRow(), policyRow({ id: "cmd.2", target: "agent:golang-engineer" }), policyRow({ id: "cmd.3", target: "loop:build-1" })], statement: undefined },
  "agent.list": { agents: [] },
  "seed.status": { declared: true, sets: [] },
};

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

function renderSection(responses: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { ...baseResponses, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <PolicySection controller={new LiveController()} live={currentLive} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("PolicySection", () => {
  it("the tabs are Rules · Proposals · In force · Audit · Shipped rules", async () => {
    renderSection();
    for (const label of ["Rules", "Proposals", "In force", "Audit", "Shipped rules"]) {
      expect(await screen.findByRole("tab", { name: label })).toBeTruthy();
    }
  });

  it("reads policy.vocabulary once for the section and builds the filter options from it", async () => {
    const daemon = renderSection();
    await screen.findByTestId("policy-list");
    const vocabCalls = daemon.calls.filter((c) => c.method === "policy.vocabulary");
    expect(vocabCalls).toHaveLength(1);
    expect(vocabCalls[0]).toEqual({ method: "policy.vocabulary", params: {} });
    // The type filter's options are exactly the daemon's list — none added here.
    const typeSelect = screen.getByLabelText("Type") as HTMLSelectElement;
    await waitFor(() =>
      expect(Array.from(typeSelect.options).map((o) => o.value)).toEqual(["", "tool_access", "command_access"]),
    );
  });

  it("filters travel as policy.list PARAMS — the rows are the daemon's answer verbatim", async () => {
    const daemon = renderSection();
    await screen.findByTestId("policy-list");
    expect(daemon.paramsOf("policy.list")).toEqual({ includeExpired: true });
    // The filter's options are the daemon's vocabulary, which loads async; the
    // select must be populated before it can carry a value (a value no option
    // holds is simply nothing) — wait for the option, then choose it.
    const typeSelect = screen.getByLabelText("Type") as HTMLSelectElement;
    await waitFor(() => expect(Array.from(typeSelect.options).map((o) => o.value)).toContain("command_access"));
    fireEvent.change(typeSelect, { target: { value: "command_access" } });
    await waitFor(() =>
      expect(daemon.paramsOf("policy.list")).toEqual({ type: "command_access", includeExpired: true }),
    );
    // The rows shown are what the fake (the daemon) answered — the UI adds
    // no row and removes none: three rows, describe lines word for word.
    expect(screen.getAllByTestId("policy-row")).toHaveLength(3);
    expect(screen.getAllByText("deny command git push — rule cmd.1").length).toBeGreaterThan(0);
  });

  it("the empty list shows the daemon's OWN empty sentence verbatim", async () => {
    renderSection({ "policy.list": { policies: [], statement: "no policies match this filter." } });
    expect(await screen.findByText("no policies match this filter.")).toBeTruthy();
  });

  it("an empty list with NO statement says the daemon sent no sentence — never dressed up", async () => {
    renderSection({ "policy.list": { policies: [] } });
    expect(await screen.findByText(/daemon sent no rules and no empty sentence/i)).toBeTruthy();
  });

  it("an expired row is SHOWN and marked; a row without expiry says so (D6/D8)", async () => {
    renderSection({
      "policy.list": {
        policies: [
          policyRow({ id: "cmd.old", notAfter: "2020-01-01T00:00:00Z" }),
          policyRow({ id: "cmd.free", notAfter: null }),
        ],
      },
    });
    await screen.findByTestId("policy-list");
    // The expired row is never hidden — expiry is history an operator came to read.
    expect(screen.getAllByTestId("policy-row")).toHaveLength(2);
    expect(screen.getByText("expired")).toBeTruthy();
    expect(screen.getByText("no expiry")).toBeTruthy();
  });

  it("the origin filter offers only origins the daemon has shown and travels as a param", async () => {
    const daemon = renderSection();
    await screen.findByTestId("policy-list");
    const originSelect = screen.getByLabelText("Origin") as HTMLSelectElement;
    // The rows carry origin "system" — that option appears; nothing was named here.
    await waitFor(() => expect(Array.from(originSelect.options).map((o) => o.value)).toContain("system"));
    fireEvent.change(originSelect, { target: { value: "system" } });
    await waitFor(() =>
      expect(daemon.paramsOf("policy.list")).toEqual({ origin: "system", includeExpired: true }),
    );
  });

  it("the D4 empty sentence and the filter-empty sentence are DIFFERENT daemon lines, each verbatim", async () => {
    // One render, one sentence — the section renders whichever the daemon
    // sends and composes neither.
    renderSection({ "policy.list": { policies: [], statement: "there are no policy rows" } });
    expect(await screen.findByText("there are no policy rows")).toBeTruthy();
    // The filter-shaped line is NOT present — the two empties never blend.
    expect(screen.queryByText("no policies match this filter.")).toBeNull();
  });

  it("the seed corpus runs a dry run FIRST, then the guarded apply — never one click", async () => {
    const daemon = renderSection({
      "seed.status": {
        declared: true,
        sets: [
          {
            id: "policy.corpus",
            class: "guard",
            appliedVersion: 3,
            availableVersion: 4,
            records: [
              { key: "command_access.golang-engineer.a", state: "removed", reason: "the operator removed the live row", appliedVersion: 1, availableVersion: 1 },
              { key: "command_access.golang-engineer.b", state: "applied", reason: "", appliedVersion: 3, availableVersion: 3 },
            ],
          },
        ],
      },
      "policy.seedCorpus": (params: unknown) => ({
        perAgent: [{ agent: "golang-engineer", seeded: 2, unchanged: 1 }],
        seeded: 2,
        unchanged: 1,
        dryRun: (params as { dryRun: boolean }).dryRun,
      }),
    });
    fireEvent.click(await screen.findByRole("tab", { name: "Shipped rules" }));
    await screen.findByTestId("seed-corpus-block");
    fireEvent.click(screen.getByRole("button", { name: /Seed shipped corpus/ }));
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "policy.seedCorpus", params: { agents: [], dryRun: true } }),
    );
    // The preview joins the door's per-agent counts with the ENGINE's
    // per-record states read from seed.status (removed = 1 here).
    const table = await screen.findByTestId("seed-counts-table");
    expect(table.textContent).toContain("golang-engineer");
    expect((await screen.findByTestId("seed-version-changed")).textContent).toContain(
      "the shipped corpus has changed since you applied it",
    );
    expect(daemon.calls.filter((c) => c.method === "policy.seedCorpus" && (c.params as { dryRun: boolean }).dryRun === false)).toHaveLength(0);
    // Apply is its own guarded act: type the set id.
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    const confirmInput = await screen.findByPlaceholderText("policy.corpus");
    fireEvent.change(confirmInput, { target: { value: "policy.corpus" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "policy.seedCorpus", params: { agents: [], dryRun: false } }),
    );
  });

  it("a removed corpus record offers restore, confirmed with the engine's reason", async () => {
    const daemon = renderSection({
      "seed.status": {
        declared: true,
        sets: [
          {
            id: "policy.corpus",
            class: "guard",
            appliedVersion: 3,
            availableVersion: 3,
            records: [{ key: "command_access.golang-engineer.a", state: "removed", reason: "the operator removed the live row", appliedVersion: 1, availableVersion: 1 }],
          },
        ],
      },
      "seed.restore": () => ({ applied: true }),
    });
    fireEvent.click(await screen.findByRole("tab", { name: "Shipped rules" }));
    await screen.findByTestId("seed-removed-row");
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));
    // The engine's own reason for the record rides the confirmation.
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog.textContent).toContain("the operator removed the live row");
    const confirmInput = await screen.findByPlaceholderText("command_access.golang-engineer.a");
    fireEvent.change(confirmInput, { target: { value: "command_access.golang-engineer.a" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });
    await waitFor(() =>
      expect(daemon.calls).toContainEqual({ method: "seed.restore", params: { set: "policy.corpus", key: "command_access.golang-engineer.a" } }),
    );
  });

  it("writes are refused while the live state is not current", async () => {
    const daemon = installFakeDaemon({ responses: baseResponses });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <PolicySection controller={new LiveController()} live={{ ...currentLive, state: "stale", daemonVersion: undefined, protocolVersion: 0 } as unknown as LiveState} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(await screen.findByTestId("policy-reconnecting")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Grant access" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("tab", { name: "Shipped rules" }));
    expect(((await screen.findByRole("button", { name: /Seed shipped corpus/ })) as HTMLButtonElement).disabled).toBe(true);
    // A stale connection is not re-read into a confident view — reads resume
    // when the live state returns to current (useRpcQuery gates on current).
    expect(daemon.methods()).not.toContain("policy.list");
  });
});
