import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ParallelAgentsPage } from "../src/features/test-bench/pages/ParallelAgentsPage";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S32 bench CRUD — the cap reaches the app through the daemon's own
// component.list / component.update door (P34), never a second door. Payloads
// mirror the component view the daemon returns.

beforeEach(() => resetBridge());
afterEach(() => cleanup());

function refusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

const LIST = {
  components: [
    {
      id: "c1",
      name: "aeon",
      slug: "aeon",
      root: "/home/operator/aeon",
      maxParallelAgents: 1,
      icon: "",
      tags: [],
      createdAt: "2026-10-30T07:00:00Z",
      updatedAt: "2026-10-30T07:00:00Z",
    },
  ],
};

describe("ParallelAgentsPage", () => {
  it("renders each component with its cap, read through component.list", async () => {
    installFakeDaemon({ responses: { "component.list": LIST } });
    render(<ParallelAgentsPage />);
    const cell = await screen.findByTestId("component-c1");
    expect(cell.textContent).toContain("aeon");
    expect(screen.getByTestId("cap-c1").textContent).toContain("cap 1");
  });

  it("shows the unwired state for an old daemon's method-not-found, never a crash", async () => {
    installFakeDaemon({
      responses: { "component.list": refusal(-32601, 'method "component.list" is not registered') },
    });
    render(<ParallelAgentsPage />);
    expect(await screen.findByTestId("unwired")).toBeTruthy();
  });

  it("sets the cap through component.update, re-sending the full view", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "component.list": LIST,
        "component.update": (params: Record<string, unknown>) => ({ ...LIST.components[0], ...params }),
      },
    });
    render(<ParallelAgentsPage />);
    await screen.findByTestId("component-c1");
    fireEvent.click(screen.getByTestId("edit-c1"));
    const input = await screen.findByTestId("cap-input-c1");
    fireEvent.change(input, { target: { value: "4" } });
    fireEvent.click(screen.getByTestId("save-c1"));
    await waitFor(() => {
      expect(daemon.paramsOf("component.update")).toBeTruthy();
    });
    const params = daemon.paramsOf("component.update") as Record<string, unknown>;
    expect(params.id).toBe("c1");
    expect(params.maxParallelAgents).toBe(4);
  });
});
