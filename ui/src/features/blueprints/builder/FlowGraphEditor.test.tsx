import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { FlowModel } from "./flowModel";

type Captured = { editMode?: boolean; onNodeCreate?: (id: string, p: { sourceId: string; side: string }) => void; onConnect?: (r: { sourceId: string; targetId: string }) => void };
const captured: Captured = {};

vi.mock("../../../components/kit", async (orig) => {
  const actual = await orig<typeof import("../../../components/kit")>();
  return { ...actual, ConnectionFlow: (props: Captured) => { Object.assign(captured, props); return <div data-testid="connection-flow" />; } };
});
vi.mock("../../../lib/useRpcQuery", () => ({ useRpcQuery: () => ({ data: { stepKinds: [], after: [], thenActions: [], operators: [], decides: [], emitTypes: [], requireTypes: [], requireStates: [], fields: [], actions: [], agents: [] }, loading: false }) }));

import { FlowGraphEditor } from "./FlowBuilder";

const model: FlowModel = {
  id: "d", schemaVersion: 1, version: 1, questions: {},
  lanes: [{ id: "plan", step: { kind: "model-step", agent: "planner" } }, { id: "build", step: { kind: "model-step", agent: "builder" } }, { id: "ship", step: { kind: "code-action", action: "git.push" } }],
};

afterEach(cleanup);

function mount() {
  const edits: ((m: FlowModel) => FlowModel)[] = [];
  const selected: unknown[] = [];
  render(<FlowGraphEditor model={model} edit={(fn) => edits.push(fn)} selection={{ type: "settings" }} onSelect={(s) => selected.push(s)} issues={{}} problems={[]} controller={{} as never} live={{} as never} loading={false} />);
  return { edits, selected };
}

describe("the connection graph, in edit mode", () => {
  it("turns the graph's own edit mode on", () => {
    mount();
    expect(captured.editMode).toBe(true);
    expect(typeof captured.onNodeCreate).toBe("function");
    expect(typeof captured.onConnect).toBe("function");
  });

  it("makes a new lane from a node's side: after it from the right, before it from the left, and selects it", () => {
    const { edits, selected } = mount();
    captured.onNodeCreate!("ghost", { sourceId: "build", side: "right" });
    expect(edits.pop()!(model).lanes.map((l) => l.id)).toEqual(["plan", "build", "lane", "ship"]);
    expect(selected.pop()).toEqual({ type: "lane", laneId: "lane" });
    captured.onNodeCreate!("ghost", { sourceId: "build", side: "left" });
    expect(edits.pop()!(model).lanes.map((l) => l.id)).toEqual(["plan", "lane", "build", "ship"]);
  });

  it("reads a link drawn between two lanes as 'when this is rejected, go there', bounding a loop back", () => {
    const { edits } = mount();
    captured.onConnect!({ sourceId: "plan", targetId: "ship" });          // skip ahead: no bound needed
    const ahead = edits.pop()!(model).lanes.find((l) => l.id === "plan")!;
    expect(ahead.onReject?.goto).toBe("ship");
    captured.onConnect!({ sourceId: "ship", targetId: "plan" });          // loop back: a counter, a bound and a place to go when it runs out
    const back = edits.pop()!(model).lanes.find((l) => l.id === "ship")!;
    expect(back.onReject).toMatchObject({ goto: "plan", counter: "ship_retries", max: 2, then: "escalate" });
  });

  it("ignores a link from a lane to itself, or to or from a rung inside a ladder", () => {
    const { edits } = mount();
    captured.onConnect!({ sourceId: "plan", targetId: "plan" });
    captured.onConnect!({ sourceId: "plan/rung", targetId: "ship" });
    captured.onConnect!({ sourceId: "plan", targetId: "ship/rung" });
    expect(edits).toHaveLength(0);
  });
});
