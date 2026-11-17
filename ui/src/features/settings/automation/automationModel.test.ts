import { describe, expect, it } from "vitest";
import {
  allowedValuesForKey,
  applicableKeys,
  blueprintState,
  buildMatch,
  canSubmit,
  describeHealth,
  emptyEventDraft,
  emptyFilterDraft,
  emptyScheduleDraft,
  envNameOnly,
  isAutomationProposal,
  isOutcome,
  listParams,
  needsAttention,
  needsAttentionCount,
  outcomePill,
  parseLimit,
  rowKey,
  stateWord,
  DISARM_ON_CREATE_REASON,
  type HealthView,
  type Vocabulary,
} from "./automationModel";

/**
 * The pure model (spec file `automationModel.ts`): the pieces that decide
 * WHAT to show and WHEN the form may send, with no daemon to ask. The
 * daemon's own doors, words and validators are tested in the section test
 * with a fake daemon; here nothing reaches the network — every input is a
 * literal.
 */

const VOLUME = {
  authorityStatement: "x",
  events: [
    { event: "loop.finished", meaning: "m", applicableKeys: ["task_type", "goal_type"] },
    { event: "component.review_requested", meaning: "m", applicableKeys: ["task_type"] },
  ],
  goalTypes: ["ship"],
  limits: { auditPageMax: 100, maxValueLen: 128, maxValuesPerKey: 16, previewMax: 5, triggerIdMaxLen: 64 },
  matchKeys: ["task_type", "goal_type"],
  outcomes: [
    { family: "worked", meaning: "m", word: "started" },
    { family: "declined", meaning: "m", word: "refused" },
    { family: "declined", meaning: "m", word: "limited" },
    { family: "silent", meaning: "m", word: "skipped" },
    { family: "unknown", meaning: "m", word: "could-not-say" },
  ],
  taskTypes: ["code", "docs"],
  windowGrammar: "HH:MM",
  zones: ["UTC", "Europe/Lisbon"],
} as unknown as Vocabulary;

function health(over: Partial<HealthView>): HealthView {
  return {
    source: "schedule", id: "x", canFire: true, reasons: [],
    blueprint: { state: "ok" }, scope: { exists: true }, workspace: { exists: true },
    ...over,
  } as HealthView;
}

describe("stateWord (AN6: four states, archived outranks all)", () => {
  it("reads the four states in the plane's own precedence", () => {
    expect(stateWord({ enabled: true, paused: false, archived: false })).toBe("enabled");
    expect(stateWord({ enabled: true, paused: true, archived: false })).toBe("paused");
    expect(stateWord({ enabled: false, paused: false, archived: false })).toBe("disabled");
    // archived is the outermost state: an archived row that still reads
    // enabled is archived, because the engine will not fire it either way.
    expect(stateWord({ enabled: true, paused: true, archived: true })).toBe("archived");
  });
});

describe("outcomePill (AN16: five outcomes, never merged)", () => {
  it("recognises exactly the five words", () => {
    expect(["started", "refused", "limited", "skipped", "could-not-say"].filter(isOutcome)).toHaveLength(5);
    expect(isOutcome("deferred")).toBe(false);
  });
  it("gives every outcome its own tone; could-not-say is not folded into skipped", () => {
    const tones = ["started", "refused", "limited", "skipped", "could-not-say"].map((w) => outcomePill(w).tone);
    expect(new Set(tones).size).toBeGreaterThanOrEqual(4); // red/green/amber/slate/purple family
    expect(outcomePill("could-not-say").tone).not.toBe(outcomePill("skipped").tone);
  });
  it("colors an unknown word could-not-say-adjacent while callers render the raw wire word", () => {
    const pill = outcomePill("deferred");
    expect(pill.tone).toBe("purple");
    expect(pill.key).toBe("could-not-say"); // tone family only — never a rename
  });
});

describe("describeHealth (V13: absence is not a verdict)", () => {
  it("says nothing when no answer has arrived — it does not invent 'invalid'", () => {
    expect(blueprintState(undefined)).toBe("unverifiable");
    expect(describeHealth(undefined)).toEqual([]);
  });
  it("carries the daemon's words per facet, including the webhook NAME", () => {
    const chips = describeHealth(health({
      canFire: false,
      reasons: ["blueprint missing"],
      blueprint: { state: "missing" },
      webhook: { envName: "AEON_X", set: false },
    }));
    const byKey = Object.fromEntries(chips.map((c) => [c.key, c.word]));
    expect(byKey.blueprint).toBe("missing");
    expect(byKey.webhook).toBe("AEON_X unset"); // NAME + presence, never a value (§4c)
    const fired = describeHealth(health({ webhook: { envName: "AEON_X", set: true } }));
    expect(fired.find((c) => c.key === "webhook")?.word).toBe("AEON_X set");
  });
});

describe("needsAttention", () => {
  it("counts a live automation the daemon says cannot fire — and nothing else", () => {
    const row = { source: "event", id: "e", enabled: true, paused: false, archived: false };
    expect(needsAttention(health({ canFire: false, reasons: ["r"] }), row)).toBe(true);
    expect(needsAttention(health({ canFire: true }), row)).toBe(false);
    // A disarmed row that cannot fire is not "needing attention" — it is
    // doing exactly what its owner asked (AN3).
    expect(needsAttention(health({ canFire: false, reasons: ["r"] }), { ...row, enabled: false })).toBe(false);
    expect(needsAttention(health({ canFire: false, reasons: ["r"] }), { ...row, paused: true })).toBe(false);
    expect(needsAttention(health({ canFire: false, reasons: ["r"] }), { ...row, archived: true })).toBe(false);
    expect(needsAttention(undefined, row)).toBe(false); // no answer ≠ needs attention
    expect(needsAttentionCount(
      [row, { ...row, id: "f", enabled: false }],
      new Map([["event:e", health({ canFire: false, reasons: ["r"] })], ["event:f", health({ canFire: false })]]),
    )).toBe(1);
  });
});

describe("match keys are descriptor-driven (AN9)", () => {
  it("offers only the keys the event applies to, and their vocabulary values", () => {
    expect(applicableKeys("component.review_requested", VOLUME)).toEqual(["task_type"]);
    expect(applicableKeys("nope", VOLUME)).toEqual([]);
    expect(allowedValuesForKey("task_type", VOLUME)).toEqual(["code", "docs"]);
    expect(allowedValuesForKey("goal_type", VOLUME)).toEqual(["ship"]);
    expect(allowedValuesForKey("task_type", undefined)).toEqual([]);
  });
  it("drops empty lists — an absent key is 'any', an empty one is 'nothing'", () => {
    expect(buildMatch("loop.finished", { task_type: [], goal_type: ["ship"] })).toEqual({ event: "loop.finished", goal_type: ["ship"] });
  });
});

describe("envNameOnly (§4c: the UI only ever holds a NAME)", () => {
  it("refuses anything that smells like a value and accepts only variable names", () => {
    expect(envNameOnly("AEON_WEBHOOK_SECRET").ok).toBe(true);
    expect(envNameOnly("sk=abc123").ok).toBe(false); // looks like a value assignment
    expect(envNameOnly("has space").ok).toBe(false);
    expect(envNameOnly("").ok).toBe(false);
  });
});

describe("canSubmit (the gate is descriptor + shape, never invented rules)", () => {
  const sched = { ...emptyScheduleDraft("UTC"), id: "nightly", blueprint: "digest", expr: "0 7 * * *", workspace: "~/w", goal: "g" };
  it("schedule: needs id, blueprint, cron, a zone the daemon published, workspace and goal", () => {
    expect(canSubmit("schedule", sched, VOLUME)).toBe(true);
    expect(canSubmit("schedule", { ...sched, zone: "Mars/Olympus" }, VOLUME)).toBe(false);
    expect(canSubmit("schedule", { ...sched, expr: "" }, VOLUME)).toBe(false);
    expect(canSubmit("schedule", sched, undefined)).toBe(false); // no vocabulary = no published zone set
  });
  it("webhook: has NO cron — the POST is its clock; the secret NAME is required", () => {
    const wh = { ...emptyScheduleDraft("UTC"), id: "ping", blueprint: "triage", workspace: "~/w", secretEnv: "AEON_S" };
    expect(canSubmit("webhook", wh, VOLUME)).toBe(true);
    expect(canSubmit("webhook", { ...wh, expr: "0 7 * * *" }, VOLUME)).toBe(true); // expr is ignored, not required
    expect(canSubmit("webhook", { ...wh, secretEnv: "" }, VOLUME)).toBe(false);
    expect(canSubmit("webhook", { ...wh, secretEnv: "not an identifier" }, VOLUME)).toBe(false);
  });
  it("event: name, id, blueprint and an event the vocabulary carries", () => {
    const ev = { ...emptyEventDraft(), id: "e", name: "E", blueprint: "b", event: "loop.finished" };
    expect(canSubmit("event", ev, VOLUME)).toBe(true);
    expect(canSubmit("event", { ...ev, event: "made.up" }, VOLUME)).toBe(false);
    expect(canSubmit("event", { ...ev, name: "" }, VOLUME)).toBe(false);
  });
});

describe("list params (AN10: filters travel as PARAMS)", () => {
  it("omits defaults and never sends the archived default back as a word", () => {
    expect(listParams(emptyFilterDraft())).toEqual({});
    expect(listParams({ ...emptyFilterDraft(), source: "event", enabled: "false", q: "docs" })).toEqual({ source: "event", enabled: false, q: "docs" });
    expect(listParams({ ...emptyFilterDraft(), archived: "include" }).archived).toBe("include");
    expect(listParams({ ...emptyFilterDraft(), archived: "only" }).archived).toBe("only");
  });
});

describe("proposals + limits + keys", () => {
  it("isAutomationProposal is exact on the one kind the desk narrows with", () => {
    expect(isAutomationProposal("automation_disable")).toBe(true);
    expect(isAutomationProposal("environment_restrict")).toBe(false);
    expect(isAutomationProposal("automation_disable_x")).toBe(false);
  });
  it("parseLimit: blank is absent, junk is absent, digits are digits", () => {
    expect(parseLimit("")).toBeUndefined();
    expect(parseLimit("nope")).toBeUndefined();
    expect(parseLimit("6")).toBe(6);
  });
  it("rowKey/healthKey share the join shape and the disarm reason is a constant", () => {
    expect(rowKey("schedule", "x")).toBe("schedule:x");
    expect(DISARM_ON_CREATE_REASON).toContain("disarmed");
  });
});
