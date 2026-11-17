import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SkillsPage } from "../src/features/test-bench/pages/SkillsPage";
import { AgentChatPage } from "../src/features/test-bench/pages/AgentChatPage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";
import type { SkillListResult } from "../src/lib/skills";

// S66 T4: the two visualization lanes.
//
//   * the chat: a step started WITH the catalog shows its own compact
//     injection row, rendering the daemon's marker sentence verbatim (the
//     same field feeds live and `after:0` replay, so one mapping covers
//     both lanes by construction);
//   * the catalog page: the watcher's three states are rendered —
//     watching, blind-with-the-daemon's-reason, and unknown on an old
//     daemon — and a `skills.changed` event makes the page re-read, which
//     is the whole "new skills arrive without a restart" promise, seen at
//     the place it is consumed.

const AT = "2026-10-31T09:00:00Z";

function listing(overrides: Partial<SkillListResult> = {}): SkillListResult {
  return {
    skills: [],
    userRoot: "/home/dev/.agents/skills",
    managedRoot: "/home/dev/.local/share/aeon/skills",
    scannedAt: AT,
    summary: { valid: 0, warn: 0, invalid: 0, shadowed: 0 },
    ...overrides,
  };
}

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetBridge();
});

describe("SkillsPage watch state (S66)", () => {
  it("renders the watched state without hedging", async () => {
    installFakeDaemon({ responses: { "skills.list": listing({ watching: true }) } });
    render(<SkillsPage connected />);
    const line = await screen.findByTestId("skills-watch-line");
    expect(line.textContent).toMatch(/watching/);
    expect(line.textContent).toMatch(/without a restart/);
    expect(line.textContent).not.toMatch(/not watching/);
  });

  it("renders the blind state with the daemon's own reason, verbatim", async () => {
    installFakeDaemon({ responses: { "skills.list": listing({
      watching: false,
      watchError: "no root existed to watch (/home/dev/.agents/skills) — a root created later is discovered by the next scan",
    }) } });
    render(<SkillsPage connected />);
    const line = await screen.findByTestId("skills-watch-line");
    expect(line.textContent).toMatch(/not watching/);
    expect(line.textContent).toMatch(/no root existed to watch/);
  });

  it("calls the missing state unknown, not off — an old daemon predates the watcher", async () => {
    installFakeDaemon({ responses: { "skills.list": listing() } });
    render(<SkillsPage connected />);
    const line = await screen.findByTestId("skills-watch-line");
    expect(line.textContent).toMatch(/unknown/);
    expect(line.textContent).toMatch(/predates the watcher/);
  });

  it("re-reads the catalog the moment the watcher announces a change", async () => {
    const daemon: FakeDaemon = installFakeDaemon({ responses: {
      "skills.list": listing({ watching: true }),
      subscribe: { subscriptionId: "s-skills", from: 0 },
    } });
    render(<SkillsPage connected />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const before = daemon.calls.filter((c) => c.method === "skills.list").length;
    act(() => daemon.emit({ subscriptionId: "s-skills", seq: 9, kind: "skills.changed", at: AT,
      payload: { added: ["docx"], removed: [], changed: [], watching: true } }));
    await waitFor(() =>
      expect(daemon.calls.filter((c) => c.method === "skills.list").length).toBe(before + 1));
  });

  it("ignores another subscription's skills.changed — one listener, one subscription", async () => {
    const daemon: FakeDaemon = installFakeDaemon({ responses: {
      "skills.list": listing({ watching: true }),
      subscribe: { subscriptionId: "s-skills", from: 0 },
    } });
    render(<SkillsPage connected />);
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    const before = daemon.calls.filter((c) => c.method === "skills.list").length;
    act(() => daemon.emit({ subscriptionId: "somebody-elses", seq: 9, kind: "skills.changed", at: AT,
      payload: { added: ["x"], watching: true } }));
    await new Promise((r) => setTimeout(r, 50));
    expect(daemon.calls.filter((c) => c.method === "skills.list").length).toBe(before);
  });

  it("keeps the manual doors when the daemon has no subscribe", async () => {
    const daemon: FakeDaemon = installFakeDaemon({ responses: { "skills.list": listing() } });
    render(<SkillsPage connected />);
    await screen.findByTestId("skills-watch-line");
    // A failing subscribe must not surface as an error panel: absence of
    // events is absence of news, never a fault.
    fireEvent.click(screen.getByText(/validate \(re-read disk\)/));
    await waitFor(() => expect(daemon.methods()).toContain("skills.validate"));
  });
});

describe("chat injection row (S66)", () => {
  it("shows its own row for a step started with the catalog offered", async () => {
    const daemon: FakeDaemon = installFakeDaemon({ responses: {
      "loop.list": { all: true, loops: [{ id: "loop-1", title: "one-step", status: "running", componentRoot: "/repo", slug: "one-step" }] },
      "routes.list": { routes: [] },
      subscribe: { subscriptionId: "s-live", from: 0 },
    } });
    render(<AgentChatPage />);
    fireEvent.click(screen.getByText("Live (daemon)"));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 5, kind: "loop.step.dispatched", at: AT,
      payload: { loopId: "loop-1", step: "survey", stepId: 12, agent: "surveyor" } }));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 6, kind: "step.started", at: AT,
      payload: { stepId: 12, agent: "surveyor", summary: "1 declared file(s)",
        skills: { count: 2, names: ["caveman", "docx"], marker: "skills injected: 2 — caveman, docx" } } }));
    const row = await screen.findByTestId("agent-chat-injection");
    expect(row.textContent).toMatch(/skills injected/);
    expect(row.textContent).toMatch(/caveman/);
    expect(row.textContent).toMatch(/docx/);
    expect(row.getAttribute("title")).toBe("skills injected: 2 — caveman, docx");
  });

  it("shows no row at all when nothing was offered", async () => {
    const daemon: FakeDaemon = installFakeDaemon({ responses: {
      "loop.list": { all: true, loops: [{ id: "loop-1", title: "one-step", status: "running", componentRoot: "/repo", slug: "one-step" }] },
      "routes.list": { routes: [] },
      subscribe: { subscriptionId: "s-live", from: 0 },
    } });
    render(<AgentChatPage />);
    fireEvent.click(screen.getByText("Live (daemon)"));
    await waitFor(() => expect(daemon.methods()).toContain("subscribe"));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 5, kind: "loop.step.dispatched", at: AT,
      payload: { loopId: "loop-1", step: "survey", stepId: 12, agent: "surveyor" } }));
    act(() => daemon.emit({ subscriptionId: "s-live", seq: 6, kind: "step.started", at: AT,
      payload: { stepId: 12, agent: "surveyor", summary: "1 declared file(s)" } }));
    // Give the stream a beat to paint anything it was going to paint, then
    // insist: an un-offered step shows no injection row — absence is the
    // honest rendering of `skills: null`, never an empty pill.
    await new Promise((r) => setTimeout(r, 80));
    expect(screen.queryByTestId("agent-chat-injection")).toBeNull();
  });
});
