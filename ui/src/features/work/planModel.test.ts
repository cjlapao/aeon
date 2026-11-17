import { describe, expect, it } from "vitest";
import { TASK_STATUSES } from "../goals/goalsRpc";
import { COLUMN_OF_STATUS, columnOf, ladderPosition, span, toBars, toCard, lanesOf, toLinks } from "./planModel";

const item = (o: Record<string, unknown>) => ({ id: 1, title: "t", epic: "", status: "planned", estimateMin: null, estimateMax: null, createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-03T00:00:00Z", ...o }) as never;

describe("task columns", () => {
  it("places every daemon status", () => {
    for (const s of TASK_STATUSES) expect(COLUMN_OF_STATUS[s]).toBeTruthy();
  });
  it("keeps an unknown status visible", () => expect(columnOf("brand-new")).toBe("notStarted"));
  it("cards keep the true status and cannot be dragged", () => {
    const c = toCard({ id: "9", title: "x", taskid: "T1", status: "changes-requested", manual: false, assignee: "golang-engineer" } as never);
    expect(c.column).toBe("inReview");
    expect(c.tags[0]?.label).toBe("changes-requested");
    expect(c.locked).toBe(true);
  });
  it("ladder position is 1-based", () => { expect(ladderPosition("pending")).toBe(1); expect(ladderPosition("nope")).toBe(0); });
});

describe("roadmap bars", () => {
  const now = Date.parse("2026-11-10T00:00:00Z");
  it("a running item runs from its real start to now, a finished one stops at its real finish", () => {
    const running = span(item({ status: "running", startedAt: "2026-11-02T00:00:00Z" }), now);
    expect(running).toEqual({ start: Date.parse("2026-11-02T00:00:00Z"), end: now, real: true });
    expect(span(item({ status: "done", startedAt: "2026-11-02T00:00:00Z", finishedAt: "2026-11-05T00:00:00Z" }), now).end).toBe(Date.parse("2026-11-05T00:00:00Z"));
  });
  it("an item that never ran sits on its creation day and says it is not real", () => {
    const s = span(item({}), now);
    expect(s.real).toBe(false);
    expect(s.start).toBe(Date.parse("2026-11-01T00:00:00Z"));
  });
  it("links join an upstream to the item that waits for it, only between shown items", () => {
    const links = toLinks([item({ id: 1 }), item({ id: 2, dependencies: [{ id: 1, status: "done", title: "a" }, { id: 9, status: "idea", title: "z" }] })]);
    expect(links).toEqual([{ id: "1>2", source: "1", target: "2", type: "fs" }]);
  });
  it("an item with no estimate is badged, lanes follow epics", () => {
    const bars = toBars([item({ epic: "A" }), item({ id: 2, epic: "", estimateMin: 1, estimateMax: 2 })], now, "no epic", "not estimated");
    expect(bars[0]?.badge).toBe("not estimated");
    expect(bars[1]?.badge).toBeUndefined();
    expect(lanesOf(bars).map((l) => l.id)).toEqual(["A", "no epic"]);
  });
});
