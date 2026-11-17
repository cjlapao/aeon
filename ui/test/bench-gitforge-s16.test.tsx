import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DemosPage } from "../src/features/test-bench/pages/DemosPage";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// S16 T13 — the "Git · forge (S16)" panel contract (the QAC added after
// T12's QAC-70–78 in docs/test_suites/aeon-loop.md).
//
// Three ways this panel can appear to pass while lying, each pre-empted:
//
//  1. AN APPROVE BUTTON IN DISGUISE. The ask is §3's whole point: a human
//     must decide the irreversible thing. This panel READS asks and never
//     answers them — answering is S9's answer path, from someone who means
//     it. Asserting no button (and no method) that answers is the test.
//  2. A FABRICATED PR. No fake daemon opened a pull request, and this is
//     the first slice where a fabricated row fabricates OTHER PEOPLE SEEING
//     SOMETHING. Every success-shaped row is labeled REPLAY citing the Go
//     test; the owed GitHub rows say `owed`, never `met`.
//  3. A GOVERNANCE GAP AT THE SURFACE. §3's two verdicts render side by
//     side VERBATIM — re-cased or paraphrased refusals are the second truth,
//     and `error (` is how a refusal gets dressed as a fault.
//
// The sentences below are COPIED FROM THE 2026-10-26 WALK
// (docs/aeon/S16-git-forge/DEMO.md's walk record — every one printed by a
// real daemon, CLI, git, or the store itself) or quoted from the Go tests
// named in the citations. The panel quotes them; this test quotes the quote.

// The ask as the STORE held it during the walk (question row 1, verbatim,
// sqlite3 output in the walk record).
const ASK_BODY = [
  "Open a pull request?",
  "",
  "forge.openPR",
  "  head: task/walk-ask2/4a17",
  "  base: feature/walk-ask2",
  "  title: Add version endpoint",
  "repository:  /tmp/opencode/s16-walk/repo/work2",
  "",
  "effect: outward — other people will see this",
].join("\n");

// Verbatim from the walk (DEMO §3): both unattended deny sentences.
const DENY_OUTWARD = "forge.openPR denied — effect class 'outward' resolves to deny (loop is unattended)";
const DENY_IRREVERSIBLE = "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)";
// Verbatim from the walk: the policy route's answer, PL4 fold and all.
const POLICY_ASKED = 'asked: agent:github-steward · command_access "corpus.github-steward.ec3539e98dc4" · target agent:github-steward · source operator · no expiry (an allow exists: "corpus.github-steward.684888c0ebb1" · agent:github-steward — asked anyway, PL4)';
// Verbatim from `aeon action check forge.merge`.
const ACTION_CHECK_MERGE = "forge.merge · irreversible · ask — cannot be undone by another action — a human decides";
// The §4 refusal and ancestor pair, from the walk and its Go pins.
const MIS_BASED = "forge.merge refused: PR #16 has base.ref 'main', expected 'feature/4a17-version'";

const VALIDATE_REPORT = {
  blueprint: "dev-loop", schemaVersion: 1, version: 1, validCount: 98, diagnostics: [],
};

const WAITING_VIEW = {
  id: "04ed504c1159414994c6dde8ec73680c", slug: "walk-ask2", status: "waiting",
  questionId: 1,
};

function gitforgeDaemon(extra: Record<string, unknown | ((p: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({ responses: {
    "routes.list": [],
    "blueprint.validate": VALIDATE_REPORT,
    "loop.show": WAITING_VIEW,
    ...extra,
  } });
}

afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("S16 git-forge panel", () => {
  it("§8 from the app: blueprint.validate for dev-loop reads ZERO unverifiable and quotes the counts", async () => {
    const daemon = gitforgeDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge demo/ }));
    const rows = await screen.findAllByTestId("gitforge-step");
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    await waitFor(() => {
      expect(joined).toContain("met");
    });
    // The walked counts, quoted — 98/0/0 is the shape §8 promised.
    expect(joined).toContain("98");
    expect(joined).toContain("0 invalid");
    expect(joined).toContain("0 unverifiable");
    expect(daemon.methods()).toContain("blueprint.validate");
  });

  it("the ask renders VERBATIM with its effect class — and nothing on the page answers it", async () => {
    const daemon = gitforgeDaemon();
    render(<DemosPage />);
    // The operator pastes the loop id the terminal printed — the panel
    // reads what exists and starts nothing.
    fireEvent.change(screen.getByTestId("gitforge-loop"), { target: { value: "04ed504c1159414994c6dde8ec73680c" } });
    // The event arrives as the shell would deliver it (S9's envelope shape).
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge demo/ }));
    await screen.findAllByTestId("gitforge-step");
    // The event listener is installed by an effect; emit only once it is, or
    // the row would be waiting for an event that was shouted before anyone
    // was listening.
    await waitFor(() => { expect(daemon.subscribes().length).toBeGreaterThanOrEqual(1); });
    daemon.emit({ subscriptionId: "s1", seq: 1, kind: "question.asked", at: new Date().toISOString(),
      payload: { id: 1, stepId: 0, kind: "permission", body: ASK_BODY, options: ["approve", "deny"] } });
    const rows = await screen.findAllByTestId("gitforge-step");
    await waitFor(() => {
      const joined = rows.map((r) => r.textContent ?? "").join("\n");
      expect(joined).toContain("Open a pull request?");
      expect(joined).toContain("head: task/walk-ask2/4a17");
      expect(joined).toContain("base: feature/walk-ask2");
      expect(joined).toContain("title: Add version endpoint");
      expect(joined).toContain("effect: outward — other people will see this");
      expect(joined).toContain("approve");
      expect(joined).toContain("deny");
    });
    // The options are TEXT the daemon wrote, never controls this page fires.
    for (const verb of [/^approve$/i, /^deny$/i, /answer/i]) {
      expect(screen.queryAllByRole("button", { name: verb })).toEqual([]);
    }
    // The operator's commands ride the row instead of a button.
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toContain("aeon answer");
  });

  it("the two §3 governance verdicts render side by side, verbatim — refusals never read `error (`", async () => {
    gitforgeDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge replay/ }));
    const rows = await screen.findAllByTestId("gitforge-replay-step");
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toContain(POLICY_ASKED);
    expect(joined).toContain(ACTION_CHECK_MERGE);
    expect(joined).toContain(DENY_IRREVERSIBLE);
    expect(joined).toContain(DENY_OUTWARD);
    // Side by side means in one row: find the row carrying BOTH verdicts.
    const paired = rows.filter((r) => (r.textContent ?? "").includes(POLICY_ASKED) && (r.textContent ?? "").includes(ACTION_CHECK_MERGE));
    expect(paired.length).toBeGreaterThanOrEqual(1);
    expect(joined).not.toContain("error (");
  });

  it("no loop started yet reads `not runnable here` naming DEMO §1 and the operator command", async () => {
    gitforgeDaemon({ "loop.show": () => { throw new Error("store: not found"); } });
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge demo/ }));
    const unavailable = await screen.findByTestId("gitforge-unavailable");
    const text = unavailable.textContent ?? "";
    expect(text).toContain("not runnable here");
    expect(text).toContain("docs/aeon/S16-git-forge/DEMO.md");
    expect(text).toContain("§1");
    expect(text).toContain("aeon loop start");
    expect(text).not.toContain("error (");
    // The same sentence must not print twice in one row (validation pass
    // 2026-10-26): the row-level pre and the unavailable block used to
    // render v.text each, so every cold row read its own sentence twice.
    const row = unavailable.closest('[data-testid="gitforge-step"]') as HTMLElement;
    expect((row.textContent ?? "").match(/not runnable here/g)).toHaveLength(1);
  });

  it("no mutation EVER leaves the page — live plus replay", async () => {
    const daemon = gitforgeDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge demo/ }));
    await screen.findAllByTestId("gitforge-step");
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge replay/ }));
    await screen.findAllByTestId("gitforge-replay-step");
    const sent = daemon.methods();
    for (const banned of ["question.answer", "loop.start", "loop.answer", "loop.stop", "step.answer", "answer", "loop.retry", "blueprint.save"]) {
      expect(sent).not.toContain(banned);
    }
    for (const m of sent) {
      expect(["blueprint.validate", "loop.show", "question.asked", "routes.list"]).toContain(m);
    }
  });

  it("every replay row carries the replay label and a grep-verified Go citation; the owed rows say `owed`", async () => {
    gitforgeDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run git-forge replay/ }));
    const rows = await screen.findAllByTestId("gitforge-replay-step");
    expect(rows.length).toBeGreaterThanOrEqual(7);
    for (const row of rows) {
      const t = row.textContent ?? "";
      expect(t).toContain("replay");
      expect(t).toMatch(/_test\.go|Test[A-Z]/);
      expect(t).not.toContain("error (");
    }
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toMatch(/phantom|base\.ref/);          // §4 — the mis-based refusal
    expect(joined).toMatch(/squash/);                     // §4 — the ancestor pair
    expect(joined).toMatch(/read.?back|closed/i);         // §5 — the close read-back
    expect(joined).toMatch(/by id|reconcil/i);            // §6 — receipt reconcile
    expect(joined).toMatch(/no-op|call log/i);            // §2 — twice-is-no-op, N4's log
    expect(joined).toMatch(/nine|9 action/i);             // §7 — the closed table
    // The GitHub-API half was never conjurable here: owed, never met.
    expect(joined).toContain("owed");
    expect(joined).toContain(MIS_BASED);
    // An owed row is labeled, and it does NOT claim its own met pill.
    const owedRows = rows.filter((r) => (r.textContent ?? "").includes("owed"));
    expect(owedRows.length).toBeGreaterThanOrEqual(1);
    for (const row of owedRows) {
      expect(row.textContent ?? "").toContain("replay");
    }
  });
});
