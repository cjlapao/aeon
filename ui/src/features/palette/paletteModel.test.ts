import { describe, expect, it } from "vitest";
import { buildItems, score } from "./paletteModel";

const c = (id: string, name: string, root: string, parentId = "") => ({ id, name, slug: id, root, parentId, shortDescription: "" }) as never;
const comps = [c("a", "aeon", "/w/aeon"), c("ui", "ui", "/w/aeon/ui", "a"), c("h", "helper", "/w/helper")];
const loops = [{ id: "l1", slug: "s52-memory-map", title: "", componentRoot: "/w/aeon/ui", status: "running" }] as never;
const pages = [{ key: "agents", label: "Configuration › Agents", to: "/config/agents" }];
const tab = (t: string) => ({ overview: "Overview", loops: "Loops", plan: "Plan", knowledge: "Knowledge", automation: "Automation", guardrails: "Guardrails", files: "Files", history: "History" })[t]!;

describe("palette", () => {
  it("scores prefix above word start above inside", () => {
    expect(score("aeon › ui", "aeon")).toBe(4);
    expect(score("aeon › ui", "ui")).toBe(3);
    expect(score("helper", "el")).toBe(2);
    expect(score("helper", "zz")).toBe(0);
  });
  it("finds a component by its path and a loop by slug", () => {
    const out = buildItems("ui", comps, loops, pages, tab);
    expect(out.find((i) => i.key === "c-ui")?.label).toBe("aeon › ui");
    expect(out.find((i) => i.key === "c-ui")?.to).toBe("/work/ui");
    expect(buildItems("memory", comps, loops, pages, tab).some((i) => i.key === "l-l1")).toBe(true);
  });
  it("a trailing tab word opens that tab on the component", () => {
    const hit = buildItems("ui know", comps, loops, pages, tab).find((i) => i.key === "c-ui");
    expect(hit?.to).toBe("/work/ui/knowledge");
    expect(hit?.chip).toBe("Knowledge");
  });
  it("offers Start a loop in a component once the query names it", () => {
    const hit = buildItems("helper", comps, loops, pages, tab).find((i) => i.key === "s-h");
    expect(hit?.to).toBe("/?component=%2Fw%2Fhelper");
    expect(buildItems("", comps, loops, pages, tab).some((i) => i.key.startsWith("s-"))).toBe(false);
  });
  it("finds pages, and shows components and pages with no query but no loops", () => {
    expect(buildItems("agents", comps, loops, pages, tab).some((i) => i.key === "p-agents")).toBe(true);
    const all = buildItems("", comps, loops, pages, tab);
    expect(all.some((i) => i.group === "loops")).toBe(false);
    expect(all.length).toBeGreaterThan(0);
  });
});
