import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LadderEditor } from "./FlowBuilder";
import type { FlowModel } from "./flowModel";
import type { InspectorProps } from "./FlowInspector";

const model = (): FlowModel => ({
  id: "d", schemaVersion: 1, version: 1, questions: {},
  lanes: [{ id: "build", iterate: "tasks", rework: "implement", ladder: [
    { id: "implement", kind: "model-step", agent: "dev" },
    { id: "check", kind: "model-step", agent: "qa", onReject: { goto: "fix", then: "block" } },
    { id: "fix", kind: "model-step", agent: "dev" },
  ] }],
});

function setup() {
  const m = model();
  const edit = vi.fn();
  const select = vi.fn();
  const props = { model: m, edit, select, vocabulary: { fields: [] }, agents: [], stepCtx: {}, condCtx: {} } as unknown as InspectorProps;
  render(<LadderEditor lane={m.lanes[0]!} {...props} />);
  return { m, edit, select };
}
const dt = () => ({ setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: "", dropEffect: "" });

afterEach(cleanup);

describe("the ladder's steps", () => {
  it("shows each rung with its kind, its rework mark and where it rejects to", () => {
    setup();
    const rungs = within(screen.getByTestId("rung-list")).getAllByTestId("rung");
    expect(rungs).toHaveLength(3);
    expect(rungs[0]!.textContent).toContain("rework");
    expect(rungs[1]!.textContent).toContain("↪ on to fix");
  });

  it("moves a rung by dragging it to a place that keeps every rejection bounded", () => {
    const { m, edit } = setup();
    const [first, second] = within(screen.getByTestId("rung-list")).getAllByTestId("rung");
    const data = dt();
    fireEvent.dragStart(first!, { dataTransfer: data });
    fireEvent.dragOver(second!, { dataTransfer: data });
    fireEvent.drop(screen.getByTestId("rung-ghost"), { dataTransfer: data });
    const next = (edit.mock.calls[0]![0] as (x: FlowModel) => FlowModel)(m);
    expect(next.lanes[0]!.ladder!.map((s) => s.id)).toEqual(["check", "implement", "fix"]);
  });

  it("refuses a drop that would make a rejection loop back unbounded", () => {
    const { edit } = setup();
    const rungs = within(screen.getByTestId("rung-list")).getAllByTestId("rung");
    const data = dt();
    fireEvent.dragStart(rungs[2]!, { dataTransfer: data }); // fix goes up before check
    fireEvent.dragOver(rungs[0]!, { dataTransfer: data });  // lands after implement: check -> fix now points back
    const ghost = screen.getByTestId("rung-ghost");
    expect(ghost.getAttribute("data-refused")).toBe("true");
    fireEvent.drop(ghost, { dataTransfer: data });
    expect(edit).not.toHaveBeenCalled();
  });
});
