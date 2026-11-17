import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ConfigPageDef } from "../src/features/config/lib/configRegistry";

// The registry is module state; a fresh graph per test keeps one test's
// registrations out of another's — the same discipline as
// test/bench-registry.test.ts.
beforeEach(() => {
  vi.resetModules();
});

async function freshRegistry() {
  return import("../src/features/config/lib/configRegistry");
}

function page(overrides: Partial<ConfigPageDef> & { id: string }): ConfigPageDef {
  return { label: overrides.id, group: "general", order: 0, status: "ready", render: () => null, ...overrides };
}

describe("configRegistry (S72 CF2/CF10)", () => {
  it("throws on a duplicate id", async () => {
    const { registerConfigPage } = await freshRegistry();
    registerConfigPage(page({ id: "twice" }));
    expect(() => registerConfigPage(page({ id: "twice" }))).toThrow(/twice.*already registered/);
  });

  it("throws when a ready page registers no render", async () => {
    const { registerConfigPage } = await freshRegistry();
    expect(() => registerConfigPage(page({ id: "ready-no-render", render: undefined }))).toThrow(/ready but registers no render/);
  });

  it("throws when a planned row carries a render", async () => {
    const { registerConfigPage } = await freshRegistry();
    expect(() => registerConfigPage(page({ id: "planned-with-render", status: "planned" }))).toThrow(/planned but carries a render/);
  });

  it("orders by group (CONFIG_GROUPS order) then by order within the group", async () => {
    const { registerConfigPage, configPages } = await freshRegistry();
    registerConfigPage(page({ id: "platform-a", group: "platform", order: 1 }));
    registerConfigPage(page({ id: "work-a", group: "work", order: 1 }));
    registerConfigPage(page({ id: "access-b", group: "access", order: 20 }));
    registerConfigPage(page({ id: "general-late", order: 90 }));
    registerConfigPage(page({ id: "access-a", group: "access", order: 10 }));
    registerConfigPage(page({ id: "general-early", order: 1 }));
    expect(configPages().map((p) => p.id)).toEqual([
      "general-early", "general-late", "access-a", "access-b", "work-a", "platform-a",
    ]);
  });

  it("planned rows carry no render, and the defaults resolve at registration", async () => {
    const { registerConfigPage, getConfigPage } = await freshRegistry();
    registerConfigPage({ id: "skills", label: "Skills", group: "access", order: 60, status: "planned", summary: "someday" });
    const planned = getConfigPage("skills");
    expect(planned).toBeTruthy();
    expect(planned?.render).toBeUndefined();
    expect(planned?.needsConnection).toBe(true); // the CF2 default
    expect(planned?.frame).toBe("titled"); // the CF6 default
    registerConfigPage(page({ id: "bare-one", frame: "bare", needsConnection: false }));
    expect(getConfigPage("bare-one")).toMatchObject({ frame: "bare", needsConnection: false });
  });

  it("firstReadyConfigPage returns the first READY page in panel order", async () => {
    const { registerConfigPage, firstReadyConfigPage } = await freshRegistry();
    registerConfigPage({ id: "planned-first", label: "Planned first", group: "general", order: 1, status: "planned" });
    expect(firstReadyConfigPage()).toBeUndefined(); // nothing ready yet
    registerConfigPage({ id: "planned-later", label: "Later", group: "access", order: 1, status: "planned" });
    registerConfigPage(page({ id: "ready-in-work", group: "work", order: 5 }));
    registerConfigPage(page({ id: "ready-late-general", order: 50 }));
    expect(firstReadyConfigPage()?.id).toBe("ready-late-general"); // general group, first ready by order
  });

  it("the shipped registration files keep the panel's promised shape", async () => {
    // routes.ts imports pages/registerAll for exactly this side effect.
    await import("../src/features/config/pages/registerAll");
    const { configPages, getConfigPage } = await freshRegistry();
    // Skills landed (its row left planned.ts); nothing shipped is planned any more.
    expect(getConfigPage("skills")?.status).toBe("ready");
    expect(configPages().filter((p) => p.status === "planned")).toEqual([]);
    // T1's fixture is gone (T2's "Do not"); the thirteen sections T2 moved
    // are all ready (agents arrives with T3), and Q3 kept blueprints off the
    // planned list.
    expect(getConfigPage("config-fixture")).toBeUndefined();
    for (const id of ["daemon-settings", "backups", "system-records", "policy", "mcp", "tools",
      "gates", "environments", "automation", "triage", "memory", "continuous", "platform-status", "variables", "skills"]) {
      expect(getConfigPage(id)?.status, id).toBe("ready");
    }
    expect(configPages().find((p) => p.id === "blueprints")).toBeUndefined(); // Q3: Flows exists, no planned row
  });
});
