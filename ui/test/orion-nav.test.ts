import { describe, expect, it, vi } from "vitest";
import { NavMap } from "../src/features/orion/navmap";
import { ORION_TOOLS, type Ui } from "../src/features/orion/tools";

const components = [
  { id: "c1", name: "aeon-qa-workspace", slug: "aeon-qa-workspace", root: "/home/u/code/aeon-qa-workspace" },
  { id: "c2", name: "Aeon", slug: "aeon", root: "/home/u/code/aeon" },
  { id: "c3", name: "Billing API", slug: "billing-api", root: "/home/u/code/billing" },
];
const loops = [{ id: "L7", slug: "greeting-cli", title: "Greeting CLI", status: "running", componentRoot: "/home/u/code/aeon-qa-workspace" }];
const models = [{ id: "hexgrad/Kokoro-82M", displayName: "Kokoro 82M", model: "hexgrad/Kokoro-82M" }];

function map() { const m = new NavMap(); m.set(components, loops, models); return m; }

describe("the navigation map", () => {
  it("opens a project from how a person says it, ignoring how they ask", () => {
    const m = map();
    expect(m.resolve("go to the QA project").destination?.to).toBe("/work/c1");
    expect(m.resolve("open billing").destination?.to).toBe("/work/c3");
    expect(m.resolve("take me to settings").destination?.to).toBe("/settings");
    expect(m.resolve("show the greeting loop").destination?.to).toBe("/loops/L7");
    expect(m.resolve("kokoro").destination?.to).toBe("/models/hexgrad%2FKokoro-82M/edit");
  });
  it("opens a tab of a project when one is named", () => {
    expect(map().resolve("knowledge of the billing project").destination?.to).toBe("/work/c3/knowledge");
    expect(map().resolve("aeon qa files").destination?.to).toBe("/work/c1/files");
  });
  it("tolerates a misheard letter, and says ambiguous or none instead of guessing", () => {
    expect(map().resolve("go to biling").destination?.to).toBe("/work/c3");
    const two = map().resolve("aeon");
    expect(two.destination?.to ?? "").not.toBe("");            // the exact name Aeon wins over aeon-qa-workspace
    expect(two.destination?.to).toBe("/work/c2");
    expect(map().resolve("zebra crossing").none).toBe(true);
    const m = new NavMap();
    m.set([{ id: "a", name: "Docs site", slug: "docs-site", root: "/a" }, { id: "b", name: "Docs api", slug: "docs-api", root: "/b" }], [], []);
    expect(m.resolve("docs").ambiguous?.map((d) => d.label).sort()).toEqual(["Docs api", "Docs site"]);
  });
  it("holds its lists in memory and reads the daemon only when they are stale or forced", async () => {
    let t = 0;
    const m = new NavMap(1000, () => t);
    const rpc = vi.fn(async (method: string) => (method === "component.list" ? { components } : method === "loop.list" ? { loops } : { routes: models }));
    await m.refresh(rpc);
    await m.refresh(rpc);                       // fresh: no second read
    expect(rpc).toHaveBeenCalledTimes(3);
    t = 5000;
    await Promise.all([m.refresh(rpc), m.refresh(rpc)]);   // stale: one read, however many ask together
    expect(rpc).toHaveBeenCalledTimes(6);
    expect(m.resolve("billing").destination).toBeTruthy();
  });
});

describe("Orion's navigate tool", () => {
  const nav = ORION_TOOLS.find((t) => t.name === "navigate")!;
  const where = ORION_TOOLS.find((t) => t.name === "where_am_i")!;
  const ui = (m = map()): Ui & { went: string[] } => { const went: string[] = []; return { went, navigate: (to) => went.push(to), where: () => "/work/c3/knowledge", map: m }; };
  const rpc = async () => ({});

  it("navigates without asking for confirmation, and says where it went", async () => {
    expect(nav.kind).toBe("read");
    const u = ui();
    expect(await nav.run({ destination: "go to test qa project" }, rpc, u)).toContain("opened aeon-qa-workspace");
    expect(u.went).toEqual(["/work/c1"]);
  });
  it("does not navigate on an ambiguous or unknown name, and says what to ask", async () => {
    const m = new NavMap();
    m.set([{ id: "a", name: "Docs site", slug: "docs-site", root: "/a" }, { id: "b", name: "Docs api", slug: "docs-api", root: "/b" }], [], []);
    const u = ui(m);
    expect(await nav.run({ destination: "docs" }, rpc, u)).toContain("Ask which one");
    expect(await nav.run({ destination: "zebra" }, rpc, u)).toContain("nothing in the app");
    expect(u.went).toEqual([]);
  });
  it("says which page the person is on", async () => {
    expect(await where.run({}, rpc, ui())).toContain("Billing API");
  });
});
