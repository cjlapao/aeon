import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LockedFlow, type LockedInfo } from "./LockedFlow";
import type { FlowModel } from "./flowModel";
import { flowGraphNodes } from "./FlowGraph";

vi.mock("../../../lib/useRpcQuery", () => ({
  useRpcQuery: () => ({ data: { routes: [{ id: "fast", displayName: "Fast", provider: "openai", model: "mini" }, { id: "smart", provider: "anthropic", model: "big" }] }, loading: false }),
}));

const model: FlowModel = {
  id: "d", schemaVersion: 1, version: 1, questions: {},
  lanes: [
    { id: "plan", step: { kind: "model-step", agent: "planner" } },
    { id: "ship", step: { kind: "code-action", action: "git.push" } },
  ],
};
const info: LockedInfo = { overridable: { plan: ["route"] }, overrides: { plan: { route: "smart" } } };

function setup(selected: string, over = vi.fn()) {
  render(<LockedFlow model={model} info={info} selection={{ type: "lane", laneId: selected }} onSelect={() => undefined} issues={{}} problems={[]}
    onOverride={over} busy={false} controller={{} as never} live={{} as never} />);
  return over;
}

afterEach(cleanup);

describe("a shipped flow, customised", () => {
  it("offers the way to edit its steps: a copy, which is the person's own", () => {
    const duplicate = vi.fn();
    render(<LockedFlow model={model} info={info} selection={{ type: "settings" }} onSelect={() => undefined} issues={{}} problems={[]} onOverride={vi.fn()} busy={false} onDuplicate={duplicate} controller={{} as never} live={{} as never} />);
    fireEvent.click(screen.getByTestId("locked-duplicate-button"));
    expect(duplicate).toHaveBeenCalledTimes(1);
  });

  it("draws the flow as the connection graph, with nothing to drag or add, and marks the route chosen on the lane", () => {
    setup("plan");
    expect(screen.getByTestId("flow-graph")).toBeTruthy();               // the same graph the flow is read on
    expect(screen.queryAllByTestId("lane-card")).toHaveLength(0);        // not the list of cards
    expect(screen.queryByLabelText("Add a lane here")).toBeNull();
    expect(screen.queryByLabelText("Move plan down")).toBeNull();
  });

  it("puts the chosen route on the lane's card in the graph", () => {
    const nodes = flowGraphNodes(model, { type: "lane", laneId: "plan" }, {}, { plan: "smart" });
    expect(nodes.find((n) => n.id === "plan")?.items?.some((i) => i.id === "route" && i.subtitle === "smart")).toBe(true);
    expect(nodes.find((n) => n.id !== "plan")?.items?.some((i) => i.id === "route")).toBe(false);
  });

  it("offers the route on a lane that runs an agent, and sends the choice", () => {
    const over = setup("plan");
    fireEvent.change(screen.getByLabelText("Model route"), { target: { value: "fast" } });
    expect(over).toHaveBeenCalledWith("plan", "route", "fast");
    fireEvent.change(screen.getByLabelText("Model route"), { target: { value: "" } });
    expect(over).toHaveBeenLastCalledWith("plan", "route", "");
  });

  it("offers nothing on a lane with no agent to choose a model for", () => {
    setup("ship");
    expect(screen.queryByLabelText("Model route")).toBeNull();
    expect(screen.getByText(/Nothing on this lane can be changed/)).toBeTruthy();
  });
});
