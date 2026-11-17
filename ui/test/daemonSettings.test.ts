import { describe, expect, it } from "vitest";
import {
  controlKindFor,
  describeLayers,
  groupByArea,
  hasEnvInvalid,
  hasStoredInvalid,
  isLocked,
  parseDraft,
  resetDisabled,
  sourceBadge,
  type SettingView,
} from "../src/features/settings/daemonSettings";

/** A declared setting, as the daemon's `settings.list` element shape. */
function makeView(overrides: Partial<SettingView> = {}): SettingView {
  return {
    area: "daemon",
    default: "false",
    description: "",
    envInvalid: "",
    key: "test.flag",
    min: undefined,
    max: undefined,
    sensitive: false,
    shadowedBy: "",
    source: "default",
    stored: null,
    storedInvalid: false,
    type: "bool",
    updatedAt: "",
    updatedBy: "",
    value: "false",
    ...overrides,
  };
}

describe("controlKindFor", () => {
  it("maps each declared type to its control", () => {
    expect(controlKindFor(makeView({ type: "bool" }))).toBe("switch");
    expect(controlKindFor(makeView({ type: "enum" }))).toBe("select");
    expect(controlKindFor(makeView({ type: "int" }))).toBe("number");
    expect(controlKindFor(makeView({ type: "string" }))).toBe("text");
  });
  it("falls back to text for any type a later slice declares", () => {
    expect(controlKindFor(makeView({ type: "path" }))).toBe("text");
  });
});

describe("parseDraft", () => {
  it("accepts only the canonical bool spellings", () => {
    expect(parseDraft(makeView({ type: "bool" }), "true").ok).toBe(true);
    expect(parseDraft(makeView({ type: "bool" }), "false").ok).toBe(true);
    expect(parseDraft(makeView({ type: "bool" }), "yes")).toMatchObject({ ok: false });
    expect(parseDraft(makeView({ type: "bool" }), "")).toMatchObject({ ok: false });
  });

  it("accepts a canonical int and returns it unchanged", () => {
    expect(parseDraft(makeView({ type: "int" }), "42")).toEqual({ ok: true, value: "42" });
    expect(parseDraft(makeView({ type: "int", min: 0, max: 100 }), "0")).toEqual({ ok: true, value: "0" });
  });

  it("rejects a non-canonical int and does not send it", () => {
    expect(parseDraft(makeView({ type: "int" }), "007")).toMatchObject({ ok: false });
    expect(parseDraft(makeView({ type: "int" }), "1e3")).toMatchObject({ ok: false });
    expect(parseDraft(makeView({ type: "int" }), "+1")).toMatchObject({ ok: false });
    expect(parseDraft(makeView({ type: "int" }), "")).toMatchObject({ ok: false });
  });

  it("honours min/max inclusive and names the range", () => {
    const view = makeView({ type: "int", min: 0, max: 10 });
    expect(parseDraft(view, "0")).toEqual({ ok: true, value: "0" });
    expect(parseDraft(view, "10")).toEqual({ ok: true, value: "10" });
    expect(parseDraft(view, "-1").reason).toContain("0");
    expect(parseDraft(view, "-1").reason).toContain("10");
    expect(parseDraft(view, "11").reason).toContain("0");
    expect(parseDraft(view, "11").reason).toContain("10");
  });

  it("requires an enum to be a declared member", () => {
    const view = makeView({ type: "enum", enumValues: ["a", "b"], value: "a" });
    expect(parseDraft(view, "a")).toEqual({ ok: true, value: "a" });
    expect(parseDraft(view, "b")).toEqual({ ok: true, value: "b" });
    expect(parseDraft(view, "c")).toMatchObject({ ok: false });
  });

  it("flags a string over its max length", () => {
    expect(parseDraft(makeView({ type: "string", max: 3 }), "abc")).toEqual({ ok: true, value: "abc" });
    expect(parseDraft(makeView({ type: "string", max: 3 }), "abcd")).toMatchObject({ ok: false });
  });
});

describe("sourceBadge", () => {
  it("maps the three layers to their three tokens", () => {
    expect(sourceBadge(makeView({ source: "default" }))).toBe("default");
    expect(sourceBadge(makeView({ source: "database" }))).toBe("saved");
    expect(sourceBadge(makeView({ source: "environment" }))).toBe("environment");
  });
});

describe("isLocked", () => {
  it("locks only an environment-controlled row", () => {
    expect(isLocked(makeView({ source: "environment" }))).toBe(true);
    expect(isLocked(makeView({ source: "default" }))).toBe(false);
    expect(isLocked(makeView({ source: "database" }))).toBe(false);
  });
});

describe("describeLayers", () => {
  it("mentions both the env layer and a shadowed saved value", () => {
    const layer = describeLayers(
      makeView({ source: "environment", shadowedBy: "AEON_X", stored: "saved-1", value: "env-1" }),
    );
    expect(layer.source).toBe("environment");
    expect(layer.shadowed).toBe("saved-1");
    expect(layer.envVar).toBe("AEON_X");
  });

  it("mentions only the default when nothing else is in force", () => {
    const layer = describeLayers(makeView({ source: "default", stored: null, value: "d" }));
    expect(layer.source).toBe("default");
    expect(layer.shadowed).toBeNull();
    expect(layer.defaultValue).toBe("false");
  });
});

describe("resetDisabled", () => {
  it("is disabled only when the default is in force and nothing is saved", () => {
    expect(resetDisabled(makeView({ source: "default", stored: null }))).toBe(true);
    expect(resetDisabled(makeView({ source: "database", stored: "x" }))).toBe(false);
    expect(resetDisabled(makeView({ source: "default", stored: "x" }))).toBe(false);
  });
});

describe("hasEnvInvalid / hasStoredInvalid", () => {
  it("report the two loud states", () => {
    expect(hasEnvInvalid(makeView({ envInvalid: "bad" }))).toBe(true);
    expect(hasEnvInvalid(makeView({ envInvalid: "" }))).toBe(false);
    expect(hasStoredInvalid(makeView({ storedInvalid: true }))).toBe(true);
    expect(hasStoredInvalid(makeView({ storedInvalid: false }))).toBe(false);
  });
});

describe("groupByArea", () => {
  it("preserves the daemon's order within and between areas", () => {
    const groups = groupByArea([
      makeView({ key: "test.b", area: "alpha" }),
      makeView({ key: "test.a", area: "alpha" }),
      makeView({ key: "test.c", area: "beta" }),
    ]);
    expect(groups.map((g) => g.area)).toEqual(["alpha", "beta"]);
    expect(groups[0]!.items.map((v) => v.key)).toEqual(["test.b", "test.a"]);
    expect(groups[1]!.items.map((v) => v.key)).toEqual(["test.c"]);
  });
});
