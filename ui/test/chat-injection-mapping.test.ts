import { describe, expect, it } from "vitest";
import { chatItemsFromEvents } from "../src/components/agent-chat/fromEvents";
import { filterForAgent } from "../src/components/agent-chat/ChatStream";
import type { AeonEvent } from "../src/lib/protocol";

// S66 T4 (lane 1, unit): the MAPPING, independent of any DOM. The row is
// derived from the marker the daemon put on step.started — live and
// `after:0` replay run this one function, so what is pinned here is
// pinned for both lanes.

const AT = "2026-10-31T09:00:00Z";

function ev(seq: number, kind: string, payload: Record<string, unknown>): AeonEvent {
  return { subscriptionId: "s", seq, kind, at: AT, payload } as AeonEvent;
}

describe("fromEvents: step.skills marker → injection row (S66)", () => {
  it("turns a step.started that carries skills into its own item", () => {
    const items = chatItemsFromEvents([
      ev(1, "step.started", { stepId: 12, agent: "surveyor", summary: "1 declared file(s), 3 tool(s) offered (read, list, write)",
        skills: { count: 3, names: ["caveman", "docx", "pdf"], marker: "skills injected: 3 — caveman, docx, pdf" } }),
    ]);
    const row = items.find((i) => i.kind === "injection");
    expect(row).toBeDefined();
    if (row?.kind !== "injection") return;
    expect(row.count).toBe(3);
    expect(row.names).toEqual(["caveman", "docx", "pdf"]);
    expect(row.marker).toBe("skills injected: 3 — caveman, docx, pdf");
    expect(row.truncated).toBeUndefined();
  });

  it("keeps the truncation flag", () => {
    const items = chatItemsFromEvents([
      ev(1, "step.started", { stepId: 1, agent: "a",
        skills: { count: 60, names: ["a"], truncated: true, marker: "skills injected: 60 — a (+more)" } }),
    ]);
    const row = items.find((i) => i.kind === "injection");
    expect(row?.kind === "injection" && row.truncated).toBe(true);
  });

  it("injects nothing on an un-offered step — not an empty row", () => {
    const items = chatItemsFromEvents([
      ev(1, "step.started", { stepId: 1, agent: "a", summary: "0 declared file(s)" }),
    ]);
    expect(items.some((i) => i.kind === "injection")).toBe(false);
  });

  it("rejects a marker with no count or non-array names", () => {
    for (const skills of [{ names: ["a"] }, { count: 2, names: "docx" }, { count: 0, names: [] }]) {
      const items = chatItemsFromEvents([ev(1, "step.started", { stepId: 1, agent: "a", skills })]);
      expect(items.some((i) => i.kind === "injection")).toBe(false);
    }
  });

  it("builds a default marker when the daemon sentence is missing", () => {
    const items = chatItemsFromEvents([
      ev(1, "step.started", { stepId: 1, agent: "a", skills: { count: 1, names: ["solo"] } }),
    ]);
    const row = items.find((i) => i.kind === "injection");
    expect(row?.kind === "injection" && row.marker).toBe("skills injected: 1 — solo");
  });

  it("survives the agent zoom — the offer belongs to the daemon, not one agent", () => {
    const items = chatItemsFromEvents([
      ev(1, "step.started", { stepId: 1, agent: "surveyor", skills: { count: 1, names: ["docx"], marker: "m" } }),
    ]);
    const zoomed = filterForAgent(items, "somebody-else");
    expect(zoomed.some((i) => i.kind === "injection")).toBe(true);
  });
});
