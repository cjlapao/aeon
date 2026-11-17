import { describe, expect, it } from "vitest";
import { finishedToday, needsYouRows, rowsFromInbox, type InboxItem } from "./HomeOverview";

const loop = (id: string, status: string, updatedAt: string) => ({ id, slug: id, title: `loop ${id}`, componentRoot: "/w/a", status, phase: "", updatedAt, startedAt: updatedAt, awaiting: false, mode: "standard" }) as never;

describe("home needs-you feed", () => {
  it("lists questions and blocked loops, oldest first, one row per loop", () => {
    const q = loop("q", "waiting", "2026-11-14T10:00:00Z");
    const rows = needsYouRows(
      [q, loop("b", "blocked", "2026-11-14T08:00:00Z"), loop("r", "running", "2026-11-14T07:00:00Z"), loop("qb", "blocked", "2026-11-14T09:00:00Z")],
      [{ loop: q, prompt: "which one?" }, { loop: loop("qb", "blocked", "2026-11-14T09:00:00Z"), prompt: "and this?" }],
    );
    expect(rows.map((r) => r.key)).toEqual(["b-b", "q-qb", "q-q"]);
  });
  it("counts what finished in the last day and how many stopped", () => {
    const now = Date.parse("2026-11-15T09:00:00Z");
    const out = finishedToday([loop("a", "done", "2026-11-15T01:00:00Z"), loop("b", "blocked", "2026-11-15T02:00:00Z"), loop("c", "done", "2026-11-10T01:00:00Z"), loop("d", "running", "2026-11-15T02:00:00Z")], now);
    expect(out).toEqual({ total: 2, blocked: 1 });
  });

  it("maps the daemon's feed: questions and blocked loops open the loop, the new kinds open their own place", () => {
    const comps = [{ id: "ui", name: "ui", root: "/w/a" }] as never;
    const loops = [loop("l1", "waiting", "2026-11-14T10:00:00Z")];
    const item = (over: Partial<InboxItem>): InboxItem => ({ key: "k", kind: "question", severity: "needs-you", componentId: "ui", loopId: "l1", loopSlug: "l1", reason: "waiting for your answer", since: "2026-11-14T10:00:00Z", ...over });
    const rows = rowsFromInbox([
      item({ key: "q", prompt: "which one?" }),
      item({ key: "p", kind: "proposal", loopId: undefined, loopSlug: undefined, reason: "a policy_add proposal waits for a decision", door: { sentence: "Read it, then decide with `aeon proposal commit 7`" } }),
      item({ key: "pu", kind: "proposal", loopId: undefined, loopSlug: undefined, reason: "a charter_save proposal waits for a decision", door: { sentence: "decide with `aeon proposal commit 8`" } }),
      item({ key: "g", kind: "gate-failed", loopSlug: "l1", reason: "the latest verification run failed" }),
      item({ key: "t", kind: "bugs-unvalidated", loopId: undefined, loopSlug: undefined, reason: "3 bugs wait for validation" }),
    ], loops, [], comps);
    expect(rows.map((r) => [r.kind, r.to])).toEqual([
      ["question", "/loops/l1"], ["proposal", "/config/policy"], ["proposal", undefined], ["gate", "/work/ui/guardrails"], ["triage", "/work/ui/guardrails"],
    ]);
    expect(rows[0]!.title).toBe("which one?");
    expect(rows[2]!.door).toContain("aeon proposal commit 8"); // no known page: the command is shown instead
  });
});

import { clip, MAX_TITLE_CHARS } from "./HomeOverview";

describe("clipping a long title", () => {
  it("leaves a short one alone and cuts a long one at the limit with an ellipsis", () => {
    expect(clip("Survey the Go packages")).toBe("Survey the Go packages");
    const out = clip("x".repeat(1000));
    expect(out.length).toBe(MAX_TITLE_CHARS);
    expect(out.endsWith("…")).toBe(true);
    expect(clip(undefined)).toBe("");
    expect(clip("a".repeat(130), 120).length).toBe(120);
  });
});
