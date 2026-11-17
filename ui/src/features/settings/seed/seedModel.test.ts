import { describe, expect, it } from "vitest";
import {
  badgeFor,
  canApplyWithoutConfirm,
  describePlan,
  isRestorable,
  summarize,
  type SeedSetStatus,
} from "./seedModel";

/** A seed set shaped like `seed.status`'s element — no id or key is hardcoded. */
function set(overrides: Partial<SeedSetStatus> = {}): SeedSetStatus {
  return {
    appliedVersion: 0,
    availableVersion: 0,
    class: "inert",
    id: "set.a",
    records: [],
    ...overrides,
  };
}

function record(overrides: { state: string; key?: string; reason?: string } = { state: "applied" }) {
  return {
    appliedVersion: 0,
    availableVersion: 0,
    key: overrides.key ?? "record.a",
    reason: overrides.reason ?? "",
    state: overrides.state,
  };
}

describe("badgeFor", () => {
  it("maps the five canonical states to five distinct labels and tones", () => {
    const states = ["applied", "removed", "diverged", "retired", "failed"];
    const badges = states.map(badgeFor);
    const labels = badges.map((b) => b.labelToken);
    const tones = badges.map((b) => b.tone);
    // 5 states ⇒ 5 labels (the states are not conflated).
    expect(new Set(labels).size).toBe(5);
    // …and five distinct colours, so the colour is not conflated either.
    expect(new Set(tones).size).toBe(5);
  });

  it("maps every known state to a distinct, non-null label token", () => {
    const states = ["applied", "behind", "absent", "removed", "diverged", "retired", "failed"];
    const badges = states.map(badgeFor);
    expect(badges.every((b) => b.labelToken !== null)).toBe(true);
    expect(new Set(badges.map((b) => b.labelToken)).size).toBe(states.length);
    expect(new Set(badges.map((b) => b.tone)).size).toBe(states.length);
  });

  it("gives an unknown state a neutral badge that still reads, never blank", () => {
    const badge = badgeFor("totally-unknown");
    expect(badge.tone).toBe("neutral");
    expect(badge.labelToken).toBeNull();
  });
});

describe("canApplyWithoutConfirm", () => {
  it("is true only for an inert set", () => {
    expect(canApplyWithoutConfirm(set({ class: "inert" }))).toBe(true);
  });
  it("is false for a guard set — a guard set is never one-clicked", () => {
    expect(canApplyWithoutConfirm(set({ class: "guard" }))).toBe(false);
  });
  it("is false for any class the daemon sends beyond inert/guard", () => {
    expect(canApplyWithoutConfirm(set({ class: "maybe" }))).toBe(false);
  });
});

describe("summarize", () => {
  it("reports 'up to date' only when every record is applied", () => {
    const summary = summarize(set({ records: [record({ state: "applied" }), record({ state: "applied" })] }));
    expect(summary.headline).toBe("upToDate");
    expect(summary.total).toBe(2);
    expect(summary.counts).toEqual({ applied: 2 });
  });

  it("names the most severe non-applied state as the headline", () => {
    const summary = summarize(
      set({
        records: [record({ state: "applied" }), record({ state: "removed" }), record({ state: "diverged" })],
      }),
    );
    // diverged outranks removed in precedence, so it is the headline.
    expect(summary.headline).toBe("diverged");
    expect(summary.headlineCount).toBe(1);
    expect(summary.counts).toEqual({ applied: 1, removed: 1, diverged: 1 });
  });

  it("counts failed above removed above diverged in the headline", () => {
    const summary = summarize(
      set({ records: [record({ state: "diverged" }), record({ state: "removed" }), record({ state: "failed" })] }),
    );
    expect(summary.headline).toBe("failed");
    expect(summary.headlineCount).toBe(1);
  });

  it("tallies counts by state without comparing versions", () => {
    const summary = summarize(
      set({
        appliedVersion: 5,
        availableVersion: 7,
        records: [
          record({ state: "applied" }),
          record({ state: "removed" }),
          record({ state: "removed" }),
          record({ state: "diverged" }),
        ],
      }),
    );
    expect(summary.counts).toEqual({ applied: 1, removed: 2, diverged: 1 });
  });

  it("reads an empty set honestly as up-to-date with a zero count, not as a confident fine", () => {
    const summary = summarize(set({ records: [] }));
    expect(summary.headline).toBe("upToDate");
    expect(summary.headlineCount).toBe(0);
    expect(summary.counts).toEqual({});
  });
});

describe("isRestorable", () => {
  it("restores only the removable states", () => {
    expect(isRestorable("removed")).toBe(true);
    expect(isRestorable("diverged")).toBe(true);
    expect(isRestorable("retired")).toBe(true);
  });
  it("never restores an up-to-date, absent or failed record", () => {
    expect(isRestorable("applied")).toBe(false);
    expect(isRestorable("absent")).toBe(false);
    expect(isRestorable("failed")).toBe(false);
  });
});

describe("describePlan", () => {
  it("names the count and carries the decisions the daemon returned", () => {
    const summary = describePlan({
      setId: "set.a",
      decisions: [
        { key: "record.a", op: "update", fromVersion: 1, toVersion: 2, reason: "update", state: "applied" },
        { key: "record.b", op: "restore", fromVersion: 0, toVersion: 2, reason: "restore", state: "removed" },
      ],
      refusal: { key: "", reason: "" },
    });
    expect(summary.count).toBe(2);
    expect(summary.decisions).toHaveLength(2);
    expect(summary.hasRefusal).toBe(false);
  });

  it("reports a refusal only when the daemon refused a record", () => {
    const refused = describePlan({
      setId: "set.a",
      decisions: [],
      refusal: { key: "record.c", reason: "a guard set cannot be applied at startup" },
    });
    expect(refused.hasRefusal).toBe(true);
    expect(refused.refusal).toEqual({ key: "record.c", reason: "a guard set cannot be applied at startup" });

    const clean = describePlan({ setId: "set.a", decisions: [], refusal: { key: "", reason: "" } });
    expect(clean.hasRefusal).toBe(false);
    expect(clean.refusal).toBeUndefined();
  });
});
