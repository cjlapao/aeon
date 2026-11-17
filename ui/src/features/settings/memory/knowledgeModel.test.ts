import { describe, expect, it } from "vitest";
import type { MemoryRow } from "./memoryModel";
import { daemonMapOf, layoutOf, recordsOf, relationshipsOf, semanticGraphOf, statusOf, titleOf, topicsOf } from "./knowledgeModel";

const row = (id: number, over: Partial<MemoryRow> = {}): MemoryRow =>
  ({
    id,
    body: `Blocked loop number ${id}: fact for this component`,
    createdAt: "2026-11-01T00:00:00Z",
    describe: `told by a person (${id})`,
    observedCount: 1,
    provenance: "observed",
    scope: "project",
    scopeKey: "/srv/ledger",
    sourceLoopSlug: "",
    tags: ["retro"],
    ...over,
  }) as MemoryRow;

describe("knowledgeModel", () => {
  it("cuts a title at the first sentence break, or at a word", () => {
    expect(titleOf("Blocked loop (x): fact 1 for this component")).toBe("Blocked loop (x)");
    expect(titleOf("a".repeat(30) + " " + "b".repeat(80)).length).toBeLessThanOrEqual(72);
    expect(titleOf("   ")).toBe("—");
  });

  it("derives a status only from what the row says", () => {
    expect(statusOf(row(1))).toBe("active");
    expect(statusOf(row(2, { provenance: "assumed" }))).toBe("needs-review");
    expect(statusOf(row(3, { provenance: "assumed", retracted: true }))).toBe("archived");
  });

  it("groups by the biggest non-universal tag shared by two or more", () => {
    const rows = [
      row(1, { tags: ["retro", "bp:a"] }),
      row(2, { tags: ["retro", "bp:a", "out:x"] }),
      row(3, { tags: ["retro", "bp:a", "out:x"] }),
      row(4, { tags: ["retro", "bp:b"] }),
    ];
    const topics = topicsOf(rows);
    expect(topics.get(1)).toBe("bp:a");
    expect(topics.get(2)).toBe("bp:a");
    expect(topics.get(4)).toBe("ledger"); // nothing shared: the scope key's last segment
  });

  it("links learnings that share a tag or a loop — once per pair — never one that is on every row", () => {
    const rows = [
      row(1, { tags: ["retro", "bp:a"], sourceLoopSlug: "loop-1" }),
      row(2, { tags: ["retro", "bp:a"], sourceLoopSlug: "loop-1" }),
      row(3, { tags: ["retro", "bp:b"] }),
    ];
    const edges = relationshipsOf(rows);
    expect(edges.map((edge) => `${edge.source}-${edge.target}:${edge.label}`).sort()).toEqual(["1-2:loop-1"]); // one edge per pair: the first reason names it
    expect(edges.some((edge) => edge.label === "retro")).toBe(false);
  });

  it("hands the explorer records with the daemon's sentence as the summary and symmetric related ids", () => {
    const rows = [row(1, { tags: ["x", "y"] }), row(2, { tags: ["x"] }), row(3, { tags: ["y"] })];
    const records = recordsOf(rows, relationshipsOf(rows));
    const first = records.find((record) => record.id === "1");
    expect(first?.summary).toBe("told by a person (1)");
    expect(first?.relatedIds).toEqual(["2", "3"]);
    expect(records.find((record) => record.id === "2")?.relatedIds).toEqual(["1"]);
  });

  it("lays the map out the same way every time, one point per record", () => {
    const rows = [row(1, { tags: ["x"] }), row(2, { tags: ["x"] }), row(3, { tags: ["y"] }), row(4, { tags: ["y"] })];
    const records = recordsOf(rows, relationshipsOf(rows));
    const edges = relationshipsOf(rows);
    const a = layoutOf(records, edges);
    const b = layoutOf(records, edges);
    expect(a).toEqual(b);
    expect(a).toHaveLength(4);
    expect(a?.every((point) => Number.isFinite(point.x) && Number.isFinite(point.y))).toBe(true);
    expect(layoutOf([], [])).toBeUndefined();
  });

  it("reads the daemon's map answer defensively: a bare {} is no map", () => {
    expect(daemonMapOf({})).toBeUndefined();
    expect(daemonMapOf(undefined)).toBeUndefined();
    expect(daemonMapOf({ projected: false, total: 3, embedded: 0, canEmbed: true, basis: "no vectors" })?.points).toEqual([]);
  });

  it("uses the daemon's positions and similarity links, and gives an unplaced memory no dot", () => {
    const rows = [row(1, { tags: ["x"] }), row(2, { tags: ["x"] }), row(3, { tags: ["y"] })];
    const records = recordsOf(rows, relationshipsOf(rows));
    const map = daemonMapOf({
      projected: true, total: 3, embedded: 2, omitted: 0, canEmbed: true, basis: "projected",
      points: [{ id: 1, x: 10, y: 20 }, { id: 2, x: -10, y: 5 }],
      links: [{ a: 1, b: 2, similarity: 0.913 }, { a: 2, b: 3, similarity: 0.7 }],
    });
    expect(map).toBeDefined();
    const graph = semanticGraphOf(map!, records);
    expect(graph.points.map((point) => point.memoryId)).toEqual(["1", "2"]); // 3 was never placed
    expect(graph.points[0]).toMatchObject({ x: 10, y: 20 });
    expect(graph.relationships).toEqual([{ id: "1-2", source: "1", target: "2", label: "0.91" }]); // a link to an unplaced one is dropped
  });
});
